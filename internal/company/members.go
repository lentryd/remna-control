package company

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"remna-control/ent"
	"remna-control/ent/auditlog"
	"remna-control/ent/company"
	"remna-control/ent/companymember"
	"remna-control/ent/membersegment"
	"remna-control/ent/rwuser"
	"remna-control/ent/tariff"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/billing"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
	"remna-control/internal/store"
)

// unlimitedExpiry is the panel expiry of every member: companies pay by
// statements, not by term.
var unlimitedExpiry = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

func (s *Service) loadCompany(ctx context.Context, id int, allowArchived bool) (*ent.Company, error) {
	c, err := s.db.Company.Query().Where(company.ID(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if c.Archived && !allowArchived {
		return nil, apperr.Status(409, "company.archived", "the company is archived")
	}
	return c, nil
}

// loadMember finds a member of this company only, so another company's
// member is as good as missing.
func (s *Service) loadMember(ctx context.Context, companyID, memberID int) (*ent.CompanyMember, error) {
	m, err := s.db.CompanyMember.Query().
		Where(companymember.ID(memberID), companymember.CompanyID(companyID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Service) baseTariff(ctx context.Context, id int) (*ent.Tariff, error) {
	t, err := s.db.Tariff.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, apperr.New("company.bad_tariff", "tariff {{id}} not found", "id", id)
	}
	if err != nil {
		return nil, err
	}
	if t.Kind != tariff.KindBase {
		return nil, apperr.New("billing.base_tariff_required", "a company needs a base tariff")
	}
	return t, nil
}

func (s *Service) openSegment(ctx context.Context, memberID int) (*ent.MemberSegment, error) {
	return s.db.MemberSegment.Query().
		Where(membersegment.MemberID(memberID), membersegment.ToDateIsNil()).Only(ctx)
}

func panelStrategy(v Values) string {
	e := unlimitedExpiry
	return billing.PanelStrategy(v.TrafficStrategy, &e)
}

func descriptionOf(companyName, label string) string {
	if label != "" {
		return companyName + " · " + label
	}
	return companyName
}

var (
	// errStale: the segment or member changed while an operation was
	// preparing; nothing was written.
	errStale = errors.New("segment changed concurrently")
	// errAlreadyActive: a concurrent AddMember got there first.
	errAlreadyActive = errors.New("member is already active")
)

func (s *Service) validateOverrides(o Overrides) error {
	switch {
	case o.Price != nil && *o.Price < 0,
		o.TrafficLimitBytes != nil && *o.TrafficLimitBytes < 0,
		o.HwidLimit != nil && *o.HwidLimit < 0,
		o.Squads != nil && len(o.Squads) == 0:
		return apperr.New("company.bad_override", "price, traffic and HWID limits can't be negative and squads can't be empty")
	}
	return nil
}

// pushParams sets the traffic limit, strategy, HWID limit and squads of a
// panel user and refreshes the cache row.
func (s *Service) pushParams(ctx context.Context, rwUserID int, v Values) error {
	limit := v.TrafficLimitBytes
	u, err := s.rw.UpdateUser(ctx, remnawave.UpdateUserRequest{
		ID:                   rwUserID,
		TrafficLimitBytes:    &limit,
		TrafficLimitStrategy: panelStrategy(v),
		HwidDeviceLimit:      v.HwidLimit,
		ActiveInternalSquads: v.Squads,
	})
	if err != nil {
		return err
	}
	return rwsync.Upsert(ctx, s.db, u)
}

// panelUserFor finds or creates the panel user of a new member. A user left
// by an earlier attempt that died before saving the member is reused.
func (s *Service) panelUserFor(ctx context.Context, c *ent.Company, username, label string, v Values) (*remnawave.User, error) {
	u, err := s.rw.GetUserByUsername(ctx, username)
	switch {
	case err == nil:
		// Only a user this company's own earlier attempt created may be
		// finished; anyone else's panel user stays untouched.
		ours, err := s.db.AuditLog.Query().Where(
			auditlog.Action("rw.create_user"), auditlog.Entity("company"), auditlog.EntityID(c.ID),
			auditlog.Ok(true), auditlog.PayloadContains(`"username":"`+username+`"`),
		).Exist(ctx)
		if err != nil {
			return nil, err
		}
		if !ours {
			return nil, apperr.Status(409, "company.panel_user_exists", "panel user {{name}} already exists and wasn't created for this company", "name", username)
		}
		busy, err := s.db.RwUser.Query().Where(rwuser.ID(u.ID), rwuser.Or(rwuser.HasSubscription(), rwuser.HasSubscriptionAddon())).Exist(ctx)
		if err != nil {
			return nil, err
		}
		if busy {
			return nil, apperr.Status(409, "company.panel_user_busy", "panel user {{name}} already belongs to a subscription", "name", username)
		}
		// Make the adopted user usable: active and without a term.
		limit, expiry := v.TrafficLimitBytes, unlimitedExpiry
		adopted, err := s.rw.UpdateUser(ctx, remnawave.UpdateUserRequest{
			ID: u.ID, Status: remnawave.StatusActive, ExpireAt: &expiry,
			TrafficLimitBytes: &limit, TrafficLimitStrategy: panelStrategy(v),
			HwidDeviceLimit: v.HwidLimit, ActiveInternalSquads: v.Squads,
		})
		if err != nil {
			return nil, err
		}
		if err := rwsync.Upsert(ctx, s.db, adopted); err != nil {
			return nil, err
		}
		return adopted, nil
	case !remnawave.IsNotFound(err):
		return nil, err
	}
	if len(v.Squads) == 0 {
		return nil, apperr.New("tariff.no_squads", "the tariff has no squads; the user would get no servers")
	}
	limit := v.TrafficLimitBytes
	u, err = s.rw.CreateUser(ctx, remnawave.CreateUserRequest{
		Username:             username,
		ExpireAt:             unlimitedExpiry,
		Status:               remnawave.StatusActive,
		TrafficLimitBytes:    &limit,
		TrafficLimitStrategy: panelStrategy(v),
		HwidDeviceLimit:      v.HwidLimit,
		ActiveInternalSquads: v.Squads,
		Description:          descriptionOf(c.Name, label),
	})
	audit.Log(ctx, s.db, "rw.create_user", "company", c.ID, map[string]any{"username": username}, err)
	if err != nil {
		return nil, err
	}
	if err := rwsync.Upsert(ctx, s.db, u); err != nil {
		return nil, err
	}
	return u, nil
}

func createSegment(ctx context.Context, tx *ent.Tx, memberID int, from time.Time, tariffID int, v Values, overrides []string) error {
	return tx.MemberSegment.Create().
		SetMemberID(memberID).SetFromDate(from).SetTariffID(tariffID).
		SetPrice(v.Price).SetTrafficLimitBytes(v.TrafficLimitBytes).SetTrafficStrategy(v.TrafficStrategy).
		SetNillableHwidLimit(v.HwidLimit).SetSquadUuids(v.Squads).SetOverrides(overrides).
		Exec(ctx)
}

// AddMember adds an employee, or returns the one already there (created is
// false); a removed employee comes back: a new segment starts today and the
// panel user is enabled again.
func (s *Service) AddMember(ctx context.Context, companyID int, username, label string, tariffID *int) (m *ent.CompanyMember, created bool, err error) {
	if !billing.ValidUsername(username) {
		return nil, false, apperr.New("billing.bad_username", "username: 3–36 characters, latin letters, digits, _ and -")
	}
	c, err := s.loadCompany(ctx, companyID, false)
	if err != nil {
		return nil, false, err
	}
	m, err = s.db.CompanyMember.Query().Where(companymember.Username(username)).Only(ctx)
	switch {
	case err == nil && m.CompanyID != companyID:
		return nil, false, apperr.Status(409, "company.username_taken", "username {{name}} is taken", "name", username)
	case err == nil && m.RemovedAt == nil:
		return m, false, nil
	case err != nil && !ent.IsNotFound(err):
		return nil, false, err
	}
	reinstate := err == nil

	// Which tariff: the one asked for, else (for a pinned member coming
	// back) the one they had, else the company's.
	wanted := c.TariffID
	pinned := false
	switch {
	case tariffID != nil:
		wanted, pinned = *tariffID, *tariffID != c.TariffID
	case reinstate && m.TariffPinned:
		if last, err := s.db.MemberSegment.Query().Where(membersegment.MemberID(m.ID)).Order(ent.Desc(membersegment.FieldID)).First(ctx); err == nil {
			wanted, pinned = last.TariffID, last.TariffID != c.TariffID
		}
	}
	t, err := s.baseTariff(ctx, wanted)
	if err != nil {
		return nil, false, err
	}
	v := ValuesOf(t)

	var u *remnawave.User
	if reinstate && m.RwUserID != nil {
		if u, err = s.rw.EnableUser(ctx, *m.RwUserID); err == nil {
			err = s.pushParams(ctx, u.ID, v)
		}
	} else {
		u, err = s.panelUserFor(ctx, c, username, label, v)
	}
	if err != nil {
		return nil, false, err
	}

	today := s.today()
	err = store.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		if reinstate {
			if cur, err := tx.CompanyMember.Get(ctx, m.ID); err != nil {
				return err
			} else if cur.RemovedAt == nil {
				return errAlreadyActive
			}
			q := tx.CompanyMember.UpdateOneID(m.ID).ClearRemovedAt().SetTariffPinned(pinned).SetRwUserID(u.ID)
			if label != "" {
				q.SetLabel(label)
			}
			if m, err = q.Save(ctx); err != nil {
				return err
			}
		} else if m, err = tx.CompanyMember.Create().
			SetCompanyID(c.ID).SetUsername(username).SetLabel(label).
			SetRwUserID(u.ID).SetTariffPinned(pinned).SetAddedAt(today).Save(ctx); err != nil {
			return err
		}
		return createSegment(ctx, tx, m.ID, today, t.ID, v, nil)
	})
	if errors.Is(err, errAlreadyActive) {
		m, err = s.db.CompanyMember.Get(ctx, m.ID)
		return m, false, err
	}
	audit.Log(ctx, s.db, "company.member_add", "company", c.ID, map[string]any{"username": username, "reinstated": reinstate}, err)
	if err != nil {
		if reinstate && u != nil {
			// The panel user is on but the member isn't back: don't leave free service.
			if _, derr := s.rw.DisableUser(context.WithoutCancel(ctx), u.ID); derr != nil {
				slog.Warn("company: could not disable a panel user after a failed reinstate", "username", username, "error", derr)
			}
		}
		return nil, false, err
	}
	return m, true, nil
}

// RemoveMember ends billing today and disables the panel user (it is never
// deleted from the panel). Removing a removed member does nothing; a
// protected one can't be removed. If the panel call fails nothing is closed.
func (s *Service) RemoveMember(ctx context.Context, companyID, memberID int) error {
	m, err := s.loadMember(ctx, companyID, memberID)
	if err != nil {
		return err
	}
	if m.RemovedAt != nil {
		return nil
	}
	if m.Protected {
		return ErrProtected
	}
	if m.RwUserID != nil {
		u, err := s.rw.DisableUser(ctx, *m.RwUserID)
		if err != nil {
			audit.Log(ctx, s.db, "company.member_remove", "company", companyID, map[string]any{"username": m.Username}, err)
			return err
		}
		if err := rwsync.Upsert(ctx, s.db, u); err != nil {
			return err
		}
	}
	today := s.today()
	err = store.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		if err := tx.MemberSegment.Update().
			Where(membersegment.MemberID(m.ID), membersegment.ToDateIsNil()).SetToDate(today).Exec(ctx); err != nil {
			return err
		}
		return tx.CompanyMember.UpdateOneID(m.ID).SetRemovedAt(s.now()).Exec(ctx)
	})
	audit.Log(ctx, s.db, "company.member_remove", "company", companyID, map[string]any{"username": m.Username}, err)
	return err
}

// SetOverrides changes a member's tariff and/or fields from today: the open
// segment ends and a new one starts, so past days keep their old values.
// Nothing changes when the result equals the current state.
func (s *Service) SetOverrides(ctx context.Context, companyID, memberID int, tariffID *int, o Overrides) error {
	if err := s.validateOverrides(o); err != nil {
		return err
	}
	c, err := s.loadCompany(ctx, companyID, false)
	if err != nil {
		return err
	}
	m, err := s.loadMember(ctx, companyID, memberID)
	if err != nil {
		return err
	}
	if m.RemovedAt != nil {
		return apperr.Status(409, "company.member_removed", "the member is removed")
	}
	seg, err := s.openSegment(ctx, m.ID)
	if err != nil {
		return err
	}
	newTariff := seg.TariffID
	pinned := m.TariffPinned
	if tariffID != nil {
		newTariff, pinned = *tariffID, *tariffID != c.TariffID
	}
	t, err := s.baseTariff(ctx, newTariff)
	if err != nil {
		return err
	}
	cur := segmentValues(seg)
	v, overrides := apply(t, cur, seg.Overrides, o)

	oldOverrides := slices.Sorted(slices.Values(seg.Overrides))
	if t.ID == seg.TariffID && sameValues(v, cur) && slices.Equal(overrides, oldOverrides) {
		if pinned != m.TariffPinned {
			return s.db.CompanyMember.UpdateOneID(m.ID).SetTariffPinned(pinned).Exec(ctx)
		}
		return nil
	}

	pv, pc := v, cur
	pv.Price, pc.Price = 0, 0
	if m.RwUserID != nil && !sameValues(pv, pc) {
		if err := s.pushParams(ctx, *m.RwUserID, v); err != nil {
			audit.Log(ctx, s.db, "company.member_update", "company", companyID, map[string]any{"username": m.Username}, err)
			return err
		}
	}
	today := s.today()
	err = store.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		if err := stillOpen(ctx, tx, seg.ID); err != nil {
			return err
		}
		if err := tx.MemberSegment.UpdateOneID(seg.ID).SetToDate(today).Exec(ctx); err != nil {
			return err
		}
		if pinned != m.TariffPinned {
			if err := tx.CompanyMember.UpdateOneID(m.ID).SetTariffPinned(pinned).Exec(ctx); err != nil {
				return err
			}
		}
		return createSegment(ctx, tx, m.ID, today, t.ID, v, overrides)
	})
	audit.Log(ctx, s.db, "company.member_update", "company", companyID, map[string]any{"username": m.Username, "overrides": overrides, "tariff": t.ID}, err)
	if errors.Is(err, errStale) {
		return apperr.Status(409, "company.member_changed", "the employee was changed meanwhile; try again")
	}
	return err
}

// SetLabel changes the member's free-text label.
func (s *Service) SetLabel(ctx context.Context, companyID, memberID int, label string) error {
	m, err := s.loadMember(ctx, companyID, memberID)
	if err != nil {
		return err
	}
	err = s.db.CompanyMember.UpdateOneID(m.ID).SetLabel(label).Exec(ctx)
	audit.Log(ctx, s.db, "company.member_update", "company", companyID, map[string]any{"username": m.Username, "label": label}, err)
	return err
}

// SetProtected turns removal protection on or off.
func (s *Service) SetProtected(ctx context.Context, companyID, memberID int, v bool) error {
	m, err := s.loadMember(ctx, companyID, memberID)
	if err != nil {
		return err
	}
	err = s.db.CompanyMember.UpdateOneID(m.ID).SetProtected(v).Exec(ctx)
	audit.Log(ctx, s.db, "company.member_protect", "company", companyID, map[string]any{"username": m.Username, "protected": v}, err)
	return err
}

// stillOpen fails with errStale unless the segment is still the open one of a
// member who is still there. It runs inside the writing transaction, where
// nothing else can write.
func stillOpen(ctx context.Context, tx *ent.Tx, segmentID int) error {
	ok, err := tx.MemberSegment.Query().
		Where(membersegment.ID(segmentID), membersegment.ToDateIsNil(), membersegment.HasMemberWith(companymember.RemovedAtIsNil())).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errStale
	}
	return nil
}
