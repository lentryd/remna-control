package billing

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"remna-control/ent"
	"remna-control/ent/extension"
	"remna-control/ent/rwuser"
	"remna-control/ent/subscription"
	"remna-control/ent/subscriptionaddon"
	"remna-control/ent/tariff"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
)

func extensionKind(k string) extension.Kind { return extension.Kind(k) }

func (s *Service) tariff(ctx context.Context, id int) (*ent.Tariff, error) {
	t, err := s.db.Tariff.Query().Where(tariff.ID(id)).WithAddon().WithPeriods().Only(ctx)
	if err != nil {
		return nil, apperr.Wrap(err, "tariff.load_failed", "tariff {{id}}: {{error}}", "id", id)
	}
	return t, nil
}

// applyTariffToUpdate copies a tariff's Remnawave user parameters.
func applyTariffToUpdate(t *ent.Tariff, upd *remnawave.UpdateUserRequest) {
	limit := t.TrafficLimitBytes
	upd.TrafficLimitBytes = &limit
	upd.TrafficLimitStrategy = t.TrafficStrategy
	upd.HwidDeviceLimit = t.HwidLimit
	if len(t.SquadUuids) > 0 {
		upd.ActiveInternalSquads = t.SquadUuids
	}
}

func createRequest(t *ent.Tariff, username string, expireAt time.Time, description string) (remnawave.CreateUserRequest, error) {
	if len(t.SquadUuids) == 0 {
		return remnawave.CreateUserRequest{}, apperr.New("tariff.no_squads", "tariff {{name}} has no squads; the user would get no servers", "name", t.Name)
	}
	limit := t.TrafficLimitBytes
	return remnawave.CreateUserRequest{
		Username:             username,
		ExpireAt:             expireAt,
		Status:               remnawave.StatusActive,
		TrafficLimitBytes:    &limit,
		TrafficLimitStrategy: t.TrafficStrategy,
		HwidDeviceLimit:      t.HwidLimit,
		ActiveInternalSquads: t.SquadUuids,
		Description:          description,
	}, nil
}

// ProvisionInput creates a brand-new Remnawave user for a customer.
type ProvisionInput struct {
	CustomerID int
	TariffID   int
	Username   string
	Label      string
	Months     int
	Days       int
	Amount     int64
	AllowDebt  bool
}

// ProvisionSubscription creates a panel user from a base tariff and links it
// as a new subscription, charging the first period.
func (s *Service) ProvisionSubscription(ctx context.Context, in ProvisionInput) (*ent.Subscription, error) {
	if !usernameRe.MatchString(in.Username) {
		return nil, apperr.New("billing.bad_username", "username: 3–36 characters, latin letters, digits, _ and -")
	}
	if in.Months+in.Days <= 0 {
		return nil, apperr.New("billing.term_required", "set the term")
	}
	t, err := s.tariff(ctx, in.TariffID)
	if err != nil {
		return nil, err
	}
	if t.Kind != tariff.KindBase {
		return nil, apperr.New("billing.base_tariff_required", "a subscription needs a base tariff")
	}
	c, err := s.db.Customer.Get(ctx, in.CustomerID)
	if err != nil {
		return nil, err
	}

	// A retry after a timeout must not fail on the user the first attempt
	// already made: replay the finished provision, or finish a half-done one.
	existing, err := s.rw.GetUserByUsername(ctx, in.Username)
	switch {
	case err == nil:
		done, err := s.provisioned(ctx, c.ID, existing)
		if err != nil || done != nil {
			return done, err
		}
	case !remnawave.IsNotFound(err):
		return nil, err
	default:
		existing = nil
	}

	if err := s.checkBalance(ctx, c.ID, in.Amount, in.AllowDebt); err != nil {
		return nil, err
	}

	now := s.now()
	to := now.AddDate(0, in.Months, in.Days)
	u := existing
	if u == nil {
		req, err := createRequest(t, in.Username, to, descriptionFor(c.Name, in.Label))
		if err != nil {
			return nil, err
		}
		req.TelegramID = c.TelegramID
		u, err = s.rw.CreateUser(ctx, req)
		audit.Log(ctx, s.db, "rw.create_user", "customer", c.ID, req, err)
		if err != nil {
			return nil, err
		}
	} else {
		// Created by an earlier attempt, never linked: keep its own expiry.
		to = u.ExpireAt
	}
	if err := rwsync.Upsert(ctx, s.db, u); err != nil {
		return nil, err
	}
	sub, err := s.db.Subscription.Create().
		SetCustomerID(c.ID).SetTariffID(t.ID).SetRwUserID(u.ID).SetLabel(in.Label).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	ledgerID, err := s.charge(ctx, c.ID, in.Amount, nil, fmt.Sprintf("New subscription %s, %s", u.Username, durationLabel(in.Months, in.Days)))
	if err != nil {
		return nil, err
	}
	s.recordExtension(ctx, "connect", &target{kind: KindSubscription, id: sub.ID, customerID: c.ID}, nil, in.Months, in.Days, in.Amount, &now, &to, ledgerID, nil)
	if err := s.applyIncluded(ctx, sub.ID, nil); err != nil {
		return sub, apperr.Wrap(err, "billing.included_partial", "the subscription was created, but not all of the tariff's add-ons connected: {{error}}")
	}
	return sub, nil
}

// provisioned says whether the panel user u already backs a subscription. It
// returns that subscription when it is the customer's (a replayed request), an
// error when u belongs to someone else, and nil, nil when u is free to adopt.
func (s *Service) provisioned(ctx context.Context, customerID int, u *remnawave.User) (*ent.Subscription, error) {
	taken := func() error {
		return apperr.Status(http.StatusConflict, "billing.username_taken", "panel user {{name}} already belongs to another subscription", "name", u.Username)
	}
	sub, err := s.db.Subscription.Query().Where(subscription.HasRwUserWith(rwuser.ID(u.ID))).Only(ctx)
	switch {
	case err == nil:
		if sub.CustomerID != customerID {
			return nil, taken()
		}
		return sub, nil
	case !ent.IsNotFound(err):
		return nil, err
	}
	if linked, err := s.db.SubscriptionAddon.Query().Where(subscriptionaddon.RwUserID(u.ID)).Exist(ctx); err != nil {
		return nil, err
	} else if linked {
		return nil, taken()
	}
	return nil, nil
}

// termLabel is durationLabel, or "бессрочно" for an unlimited expiry.
func termLabel(months, days int, to time.Time) string {
	if Unlimited(&to) {
		return "forever"
	}
	return durationLabel(months, days)
}

func descriptionFor(customerName, label string) string {
	if label != "" {
		return customerName + " · " + label
	}
	return customerName
}

func (s *Service) checkBalance(ctx context.Context, customerID int, amount int64, allowDebt bool) error {
	if allowDebt || amount <= 0 {
		return nil
	}
	bal, err := s.Balance(ctx, customerID)
	if err != nil {
		return err
	}
	if bal < amount {
		return ErrInsufficientBalance
	}
	return nil
}

// ConnectAddonInput attaches an add-on to a subscription.
type ConnectAddonInput struct {
	SubscriptionID int
	TariffID       int
	Months         int
	Days           int
	// Until, when set, is the exact expiry (e.g. aligned with the
	// subscription); Months/Days then only describe and price the term.
	Until     *time.Time
	Amount    int64
	AllowDebt bool
}

// ConnectAddon creates (or adopts, if it already exists in the panel) the
// add-on user prefix+<username>+suffix with the tariff's parameters, links
// it and charges the first period.
func (s *Service) ConnectAddon(ctx context.Context, in ConnectAddonInput) (*ent.SubscriptionAddon, error) {
	if in.Months+in.Days <= 0 && in.Until == nil {
		return nil, apperr.New("billing.term_required", "set the term")
	}
	sub, err := s.db.Subscription.Query().Where(subscription.ID(in.SubscriptionID)).
		WithRwUser().WithCustomer().Only(ctx)
	if err != nil {
		return nil, err
	}
	if sub.Edges.RwUser == nil {
		return nil, ErrNotLinked
	}
	t, err := s.tariff(ctx, in.TariffID)
	if err != nil {
		return nil, err
	}
	if t.Kind != tariff.KindAddon || t.Edges.Addon == nil {
		return nil, apperr.New("billing.addon_tariff_required", "an add-on tariff is required")
	}
	a := t.Edges.Addon
	exists, err := s.db.SubscriptionAddon.Query().
		Where(subscriptionaddon.SubscriptionID(sub.ID), subscriptionaddon.AddonID(a.ID)).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperr.Status(http.StatusConflict, "billing.addon_connected", "add-on {{name}} is already connected; extend it or change its tariff", "name", a.Name)
	}
	if err := s.checkBalance(ctx, sub.CustomerID, in.Amount, in.AllowDebt); err != nil {
		return nil, err
	}

	now := s.now()
	if in.Until != nil && !in.Until.After(now) {
		return nil, apperr.New("billing.until_past", "the end date has passed")
	}
	to := now.AddDate(0, in.Months, in.Days)
	if in.Until != nil {
		to = *in.Until
	}
	extendBy := &term{in.Months, in.Days}
	if in.Until != nil {
		extendBy = nil
	}
	sa, to, err := s.connectAddonUser(ctx, sub, t, to, extendBy, false)
	if err != nil {
		return nil, err
	}
	ledgerID, err := s.charge(ctx, sub.CustomerID, in.Amount, nil,
		fmt.Sprintf("Add-on %s (%s) for %s, %s", a.Name, t.Name, sub.Edges.RwUser.Username, termLabel(in.Months, in.Days, to)))
	if err != nil {
		return nil, err
	}
	s.recordExtension(ctx, "connect", &target{kind: KindAddon, id: sa.ID, customerID: sub.CustomerID}, nil, in.Months, in.Days, in.Amount, &now, &to, ledgerID, nil)
	return sa, nil
}

// term is a duration in months and days.
type term struct{ months, days int }

// connectAddonUser creates the add-on user prefix+<username>+suffix with the
// add-on tariff's parameters until to, or adopts one made by hand (then
// extendBy, if set, extends it from its own expiry instead), and links it
// to the subscription. sub needs its RwUser and Customer edges.
func (s *Service) connectAddonUser(ctx context.Context, sub *ent.Subscription, t *ent.Tariff, to time.Time, extendBy *term, included bool) (*ent.SubscriptionAddon, time.Time, error) {
	a := t.Edges.Addon
	username := a.Prefix + sub.Edges.RwUser.Username + a.Suffix
	var u *remnawave.User
	existing, err := s.rw.GetUserByUsername(ctx, username)
	switch {
	case err == nil:
		// Adopt a user created by hand: apply the tariff parameters, and
		// never shorten what it already has.
		if extendBy != nil {
			to = ExtendFrom(s.now(), &existing.ExpireAt).AddDate(0, extendBy.months, extendBy.days)
		} else if existing.ExpireAt.After(to) && !included {
			to = existing.ExpireAt
		}
		upd := remnawave.UpdateUserRequest{ID: existing.ID, ExpireAt: &to, Status: remnawave.StatusActive}
		applyTariffToUpdate(t, &upd)
		u, err = s.rw.UpdateUser(ctx, upd)
		audit.Log(ctx, s.db, "rw.adopt_addon_user", "subscription", sub.ID, upd, err)
	case remnawave.IsNotFound(err):
		var req remnawave.CreateUserRequest
		req, err = createRequest(t, username, to, descriptionFor(sub.Edges.Customer.Name, a.Name))
		if err != nil {
			return nil, to, err
		}
		req.TelegramID = sub.Edges.Customer.TelegramID
		u, err = s.rw.CreateUser(ctx, req)
		audit.Log(ctx, s.db, "rw.create_addon_user", "subscription", sub.ID, req, err)
	}
	if err != nil {
		return nil, to, err
	}
	if err := rwsync.Upsert(ctx, s.db, u); err != nil {
		return nil, to, err
	}
	sa, err := s.db.SubscriptionAddon.Create().
		SetSubscriptionID(sub.ID).SetAddonID(a.ID).SetTariffID(t.ID).SetRwUserID(u.ID).SetIncluded(included).
		Save(ctx)
	return sa, to, err
}

// TariffQuote is the prorated surcharge of a tariff switch.
type TariffQuote struct {
	OldMonthly int64      `json:"old_monthly"`
	NewMonthly int64      `json:"new_monthly"`
	ExpireAt   *time.Time `json:"expire_at"`
	// Surcharge includes the credit for paid add-ons that become included.
	Surcharge int64 `json:"surcharge"`
	// Addons is what the switch does to included add-ons (subscriptions).
	Addons []AddonChange `json:"addons"`
}

// QuoteTariffChange prices switching kind/id to tariffID for the time left.
func (s *Service) QuoteTariffChange(ctx context.Context, kind string, id, tariffID int) (*TariffQuote, error) {
	cur, err := s.loadTarget(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	t, err := s.tariff(ctx, tariffID)
	if err != nil {
		return nil, err
	}
	if cur.included {
		return nil, ErrIncluded
	}
	q := &TariffQuote{
		OldMonthly: cur.monthly,
		NewMonthly: t.MonthlyPrice,
		ExpireAt:   cur.expireAt,
		Surcharge:  ProrateSurcharge(cur.monthly, t.MonthlyPrice, s.now(), cur.expireAt),
		Addons:     []AddonChange{},
	}
	if kind == KindSubscription && t.Kind == tariff.KindBase {
		sub, err := s.loadSubForIncluded(ctx, id)
		if err != nil {
			return nil, err
		}
		changes, err := s.includedChanges(ctx, sub, t.ID)
		if err != nil {
			return nil, err
		}
		for _, ch := range changes {
			q.Surcharge -= ch.Credit
		}
		q.Addons = append(q.Addons, changes...)
	}
	return q, nil
}

// ChangeTariffInput switches a subscription/add-on to another tariff.
type ChangeTariffInput struct {
	Kind     string
	ID       int
	TariffID int
	// Surcharge is charged (positive) or credited (negative) to the balance.
	Surcharge int64
	// ClearOverride drops the price override so the new tariff's price applies.
	ClearOverride bool
	// RemovedAddons says, per subscription add-on the new tariff no longer
	// includes, RemoveDisable (default) or RemoveKeepPaid.
	RemovedAddons map[int]string
}

// ChangeTariff relinks the tariff, pushes its parameters to the panel when
// the tariff manages them, and books the surcharge.
func (s *Service) ChangeTariff(ctx context.Context, in ChangeTariffInput) error {
	cur, err := s.loadTarget(ctx, in.Kind, in.ID)
	if err != nil {
		return err
	}
	t, err := s.tariff(ctx, in.TariffID)
	if err != nil {
		return err
	}
	if cur.included {
		return ErrIncluded
	}
	switch in.Kind {
	case KindSubscription:
		if t.Kind != tariff.KindBase {
			return apperr.New("billing.base_tariff_required", "a subscription needs a base tariff")
		}
		q := s.db.Subscription.UpdateOneID(in.ID).SetTariffID(t.ID)
		if in.ClearOverride {
			q.ClearPriceOverride()
		}
		err = q.Exec(ctx)
	case KindAddon:
		sa, err2 := s.db.SubscriptionAddon.Get(ctx, in.ID)
		if err2 != nil {
			return err2
		}
		if t.Kind != tariff.KindAddon || t.AddonID == nil || *t.AddonID != sa.AddonID {
			return apperr.New("billing.other_addon_tariff", "the tariff belongs to another add-on")
		}
		q := s.db.SubscriptionAddon.UpdateOneID(in.ID).SetTariffID(t.ID)
		if in.ClearOverride {
			q.ClearPriceOverride()
		}
		err = q.Exec(ctx)
	}
	if err != nil {
		return err
	}

	var rwErr error
	if t.ManageRw && cur.rwUserID != nil {
		upd := remnawave.UpdateUserRequest{ID: *cur.rwUserID}
		applyTariffToUpdate(t, &upd)
		var u *remnawave.User
		u, rwErr = s.rw.UpdateUser(ctx, upd)
		if rwErr == nil {
			_ = rwsync.Upsert(ctx, s.db, u)
		}
	}

	var ledgerID *int
	if in.Surcharge != 0 {
		ledgerID, err = s.charge(ctx, cur.customerID, in.Surcharge, nil, fmt.Sprintf("Tariff change: %s → %s", cur.title, t.Name))
		if err != nil {
			return err
		}
	}
	s.recordExtension(ctx, "tariff_change", cur, nil, 0, 0, in.Surcharge, nil, nil, ledgerID, rwErr)
	audit.Log(ctx, s.db, "tariff.change", in.Kind, in.ID, in, rwErr)
	if rwErr != nil {
		return apperr.Wrap(rwErr, "billing.tariff_rw_failed", "the tariff changed, but the panel wasn't updated: {{error}}")
	}
	if in.Kind == KindSubscription {
		if err := s.applyIncluded(ctx, in.ID, in.RemovedAddons); err != nil {
			return apperr.Wrap(err, "billing.included_partial_change", "the tariff changed, but not all of its add-ons were updated: {{error}}")
		}
	}
	return nil
}

// SetEnabled enables/disables the panel user behind a subscription/add-on.
func (s *Service) SetEnabled(ctx context.Context, kind string, id int, enabled bool) error {
	t, err := s.loadTarget(ctx, kind, id)
	if err != nil {
		return err
	}
	if t.rwUserID == nil {
		return ErrNotLinked
	}
	var u *remnawave.User
	if enabled {
		u, err = s.rw.EnableUser(ctx, *t.rwUserID)
	} else {
		u, err = s.rw.DisableUser(ctx, *t.rwUserID)
	}
	audit.Log(ctx, s.db, map[bool]string{true: "rw.enable", false: "rw.disable"}[enabled], kind, id, nil, err)
	if err != nil {
		return err
	}
	if err := rwsync.Upsert(ctx, s.db, u); err != nil {
		return err
	}
	if kind == KindSubscription {
		return s.setIncludedEnabled(ctx, id, enabled)
	}
	return nil
}
