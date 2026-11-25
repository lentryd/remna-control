package company

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"remna-control/ent"
	"remna-control/ent/companymember"
	"remna-control/ent/membersegment"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/store"
)

func checkReportDay(day int) error {
	if day < 1 || day > 28 {
		return apperr.New("company.bad_report_day", "report day must be between 1 and 28")
	}
	return nil
}

// CreateCompany makes a company with its billing-account customer. The first
// period is the cycle containing today; members are billed only for the days
// they were on it.
func (s *Service) CreateCompany(ctx context.Context, name, notes string, tariffID, reportDay int) (*ent.Company, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apperr.New("company.name_required", "name is required")
	}
	if err := checkReportDay(reportDay); err != nil {
		return nil, err
	}
	if _, err := s.baseTariff(ctx, tariffID); err != nil {
		return nil, err
	}
	p := PeriodContaining(reportDay, s.now(), s.loc)
	var c *ent.Company
	err := store.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		acc, err := tx.Customer.Create().SetName(name).SetNotes("Company billing account").Save(ctx)
		if err != nil {
			return err
		}
		c, err = tx.Company.Create().
			SetName(name).SetNotes(notes).SetCustomerID(acc.ID).SetTariffID(tariffID).SetReportDay(reportDay).
			SetPeriodStart(p.From).SetPeriodEnd(p.To).SetPeriodDays(p.Days()).Save(ctx)
		return err
	})
	if err != nil {
		audit.Log(ctx, s.db, "company.create", "company", 0, map[string]any{"name": name}, err)
		return nil, err
	}
	audit.Log(ctx, s.db, "company.create", "company", c.ID, map[string]any{"name": name, "tariff": tariffID, "report_day": reportDay}, nil)
	return c, nil
}

// UpdateCompany renames the company and sets the report day. The open
// period keeps its end: a new report day applies from the next period.
func (s *Service) UpdateCompany(ctx context.Context, id int, name, notes string, reportDay int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return apperr.New("company.name_required", "name is required")
	}
	if err := checkReportDay(reportDay); err != nil {
		return err
	}
	c, err := s.loadCompany(ctx, id, false)
	if err != nil {
		return err
	}
	err = store.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		if err := tx.Company.UpdateOneID(c.ID).SetName(name).SetNotes(notes).SetReportDay(reportDay).Exec(ctx); err != nil {
			return err
		}
		return tx.Customer.UpdateOneID(c.CustomerID).SetName(name).Exec(ctx)
	})
	audit.Log(ctx, s.db, "company.update", "company", id, map[string]any{"name": name, "report_day": reportDay}, err)
	return err
}

// moveSegment ends the member's open segment today and opens one on tariff t
// with values v; the caller pushes the panel parameters afterwards.
func (s *Service) moveSegment(ctx context.Context, seg *ent.MemberSegment, t *ent.Tariff, v Values) error {
	today := s.today()
	return store.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		if err := stillOpen(ctx, tx, seg.ID); err != nil {
			return err
		}
		if err := tx.MemberSegment.UpdateOneID(seg.ID).SetToDate(today).Exec(ctx); err != nil {
			return err
		}
		return createSegment(ctx, tx, seg.MemberID, today, t.ID, v, seg.Overrides)
	})
}

// openSegments returns the open segments of active members matching where,
// with their members.
func (s *Service) openSegments(ctx context.Context, where ...func(*ent.MemberSegmentQuery)) ([]*ent.MemberSegment, error) {
	q := s.db.MemberSegment.Query().
		Where(membersegment.ToDateIsNil(), membersegment.HasMemberWith(companymember.RemovedAtIsNil())).
		WithMember()
	for _, w := range where {
		w(q)
	}
	return q.All(ctx)
}

// SetTariff gives the company a new tariff and moves every member whose
// tariff followed the company's. Members pinned to another tariff stay; a
// pinned member whose tariff is now the company's follows it again. The
// database changes first; a failed panel push is left to the drift job.
func (s *Service) SetTariff(ctx context.Context, id, tariffID int) error {
	c, err := s.loadCompany(ctx, id, false)
	if err != nil {
		return err
	}
	t, err := s.baseTariff(ctx, tariffID)
	if err != nil {
		return err
	}
	if err := s.db.Company.UpdateOneID(c.ID).SetTariffID(t.ID).Exec(ctx); err != nil {
		return err
	}
	segs, err := s.openSegments(ctx, func(q *ent.MemberSegmentQuery) {
		q.Where(membersegment.HasMemberWith(companymember.CompanyID(c.ID)))
	})
	if err != nil {
		return err
	}
	for _, seg := range segs {
		m := seg.Edges.Member
		if m.TariffPinned && seg.TariffID != t.ID {
			continue
		}
		if m.TariffPinned {
			if err := s.db.CompanyMember.UpdateOneID(m.ID).SetTariffPinned(false).Exec(ctx); err != nil {
				return err
			}
		}
		if seg.TariffID == t.ID {
			continue
		}
		cur := segmentValues(seg)
		v := Resolve(t, cur, seg.Overrides)
		if err := s.moveSegment(ctx, seg, t, v); errors.Is(err, errStale) {
			continue // removed meanwhile: nothing left to move
		} else if err != nil {
			return err
		}
		s.pushQuietly(ctx, m, v)
	}
	audit.Log(ctx, s.db, "company.set_tariff", "company", id, map[string]any{"tariff": t.ID}, nil)
	return nil
}

// TariffUpdated applies an edit of tariff tariffID to the members that
// inherit the changed fields. Their open segment ends today and a new one
// starts with the new values, so days already lived keep their old price.
func (s *Service) TariffUpdated(ctx context.Context, tariffID int) error {
	t, err := s.db.Tariff.Get(ctx, tariffID)
	if err != nil {
		return err
	}
	segs, err := s.openSegments(ctx, func(q *ent.MemberSegmentQuery) { q.Where(membersegment.TariffID(tariffID)) })
	if err != nil {
		return err
	}
	for _, seg := range segs {
		cur := segmentValues(seg)
		v := Resolve(t, cur, seg.Overrides)
		if sameValues(v, cur) {
			continue
		}
		if err := s.moveSegment(ctx, seg, t, v); errors.Is(err, errStale) {
			continue
		} else if err != nil {
			return err
		}
		pv, pc := v, cur
		pv.Price, pc.Price = 0, 0
		if !sameValues(pv, pc) {
			s.pushQuietly(ctx, seg.Edges.Member, v)
		}
	}
	return nil
}

// pushQuietly pushes member parameters to the panel; a failure is logged and
// left to SyncMembersPanel, because the database already says what is due.
func (s *Service) pushQuietly(ctx context.Context, m *ent.CompanyMember, v Values) {
	if m.RwUserID == nil {
		return
	}
	if err := s.pushParams(ctx, *m.RwUserID, v); err != nil {
		slog.Warn("company: panel push failed, the sync job will retry", "member", m.Username, "error", err)
	}
}
