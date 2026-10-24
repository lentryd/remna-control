package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"remna-control/ent"
	"remna-control/ent/customer"
	"remna-control/ent/ledgerentry"
	"remna-control/ent/referralaccrual"
	"remna-control/ent/subscription"
	"remna-control/ent/subscriptionaddon"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
	"remna-control/internal/settings"
	"remna-control/internal/store"
)

const (
	KindSubscription = "subscription"
	KindAddon        = "addon"
)

var (
	ErrInsufficientBalance = apperr.New("billing.insufficient_balance", "insufficient balance")
	ErrNotLinked           = apperr.New("billing.not_linked", "the subscription isn't linked to a Remnawave user")
	ErrUnlimited           = apperr.New("billing.unlimited", "unlimited subscription (expires in 2099); nothing to extend")
	ErrIncluded            = apperr.New("billing.included", "the add-on comes with the subscription's tariff and is extended with it")
	usernameRe             = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,36}$`)
)

type Service struct {
	db       *ent.Client
	rw       *remnawave.Client
	settings *settings.Store
	now      func() time.Time

	// mu serializes the panel sync job's per-entity work with admin actions
	// that move a traffic anchor or marker, so neither acts on stale ones.
	mu sync.Mutex
	// panelOutcome is the last audited PATCH outcome per panel user (guarded
	// by mu): a PATCH failing the same way each pass is audited once.
	panelOutcome map[int]string
}

func New(db *ent.Client, rw *remnawave.Client, st *settings.Store) *Service {
	return &Service{db: db, rw: rw, settings: st, now: time.Now, panelOutcome: map[int]string{}}
}

// Balance is the sum of a customer's ledger entries (kopecks).
func (s *Service) Balance(ctx context.Context, customerID int) (int64, error) {
	var res []struct {
		Sum int64 `json:"sum"`
	}
	err := s.db.LedgerEntry.Query().
		Where(ledgerentry.CustomerID(customerID)).
		Aggregate(ent.Sum(ledgerentry.FieldAmount)).
		Scan(ctx, &res)
	if err != nil || len(res) == 0 {
		return 0, err
	}
	return res[0].Sum, nil
}

// Balances returns every customer's balance.
func (s *Service) Balances(ctx context.Context) (map[int]int64, error) {
	var rows []struct {
		CustomerID int   `json:"customer_id"`
		Sum        int64 `json:"sum"`
	}
	err := s.db.LedgerEntry.Query().
		GroupBy(ledgerentry.FieldCustomerID).
		Aggregate(ent.Sum(ledgerentry.FieldAmount)).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make(map[int]int64, len(rows))
	for _, r := range rows {
		out[r.CustomerID] = r.Sum
	}
	return out, nil
}

// BalanceSplit breaks a balance into the money that came from the
// customer's own payments/adjustments and the part earned as referral
// accruals (kopecks). Total = Own + Referral.
type BalanceSplit struct {
	Own      int64
	Referral int64
}

func (b BalanceSplit) Total() int64 { return b.Own + b.Referral }

// BalanceSplits returns every customer's balance split into own and referral money.
func (s *Service) BalanceSplits(ctx context.Context) (map[int]BalanceSplit, error) {
	var rows []struct {
		CustomerID int    `json:"customer_id"`
		Type       string `json:"type"`
		Sum        int64  `json:"sum"`
	}
	err := s.db.LedgerEntry.Query().
		GroupBy(ledgerentry.FieldCustomerID, ledgerentry.FieldType).
		Aggregate(ent.Sum(ledgerentry.FieldAmount)).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := map[int]BalanceSplit{}
	for _, r := range rows {
		b := out[r.CustomerID]
		if r.Type == string(ledgerentry.TypeReferral) {
			b.Referral += r.Sum
		} else {
			b.Own += r.Sum
		}
		out[r.CustomerID] = b
	}
	return out, nil
}

// EffectivePrice is the override if set, else the tariff's monthly price.
func EffectivePrice(override *int64, t *ent.Tariff) int64 {
	if override != nil {
		return *override
	}
	if t != nil {
		return t.MonthlyPrice
	}
	return 0
}

// AddonPrice is an add-on's monthly price: nothing when it's included in
// the subscription's tariff.
func AddonPrice(sa *ent.SubscriptionAddon) int64 {
	if sa.Included {
		return 0
	}
	return EffectivePrice(sa.PriceOverride, sa.Edges.Tariff)
}

func periodsOf(t *ent.Tariff) []Period {
	if t == nil {
		return nil
	}
	out := make([]Period, 0, len(t.Edges.Periods))
	for _, p := range t.Edges.Periods {
		out = append(out, Period{Months: p.Months, Days: p.Days, Price: p.Price})
	}
	return out
}

// target is a resolved subscription or add-on we can extend.
type target struct {
	kind       string
	id         int
	customerID int
	title      string
	rwUserID   *int
	monthly    int64
	periods    []Period
	expireAt   *time.Time
	// tariffID is the current tariff of a subscription (0 if none)
	tariffID int
	// included add-ons come with the subscription's tariff
	included bool
	// strategy is the effective traffic strategy, manageRw the own tariff's
	// flag; anchor and resetAt the traffic reset anchor and handled point
	strategy string
	manageRw bool
	anchor   *time.Time
	resetAt  *time.Time
}

func (s *Service) loadTarget(ctx context.Context, kind string, id int) (*target, error) {
	switch kind {
	case KindSubscription:
		sub, err := s.db.Subscription.Query().Where(subscription.ID(id)).
			WithTariff(func(q *ent.TariffQuery) { q.WithPeriods() }).
			WithRwUser().WithCustomer().
			Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("subscription %d: %w", id, err)
		}
		return subTarget(sub), nil
	case KindAddon:
		sa, err := s.db.SubscriptionAddon.Query().Where(subscriptionaddon.ID(id)).
			WithTariff(func(q *ent.TariffQuery) { q.WithPeriods() }).
			WithRwUser().WithAddon().
			WithSubscription(func(q *ent.SubscriptionQuery) { q.WithCustomer().WithRwUser().WithTariff() }).
			Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("add-on %d: %w", id, err)
		}
		return addonTarget(sa), nil
	}
	return nil, fmt.Errorf("unknown kind %q", kind)
}

func subTarget(sub *ent.Subscription) *target {
	t := &target{
		kind:       KindSubscription,
		id:         sub.ID,
		customerID: sub.CustomerID,
		rwUserID:   sub.RwUserID,
		monthly:    EffectivePrice(sub.PriceOverride, sub.Edges.Tariff),
		periods:    periodsOf(sub.Edges.Tariff),
		title:      SubscriptionTitle(sub),
		strategy:   EffectiveStrategy(sub.Edges.Tariff, nil, false),
		manageRw:   sub.Edges.Tariff != nil && sub.Edges.Tariff.ManageRw,
		anchor:     sub.TrafficAnchorAt,
		resetAt:    sub.TrafficResetAt,
	}
	if sub.Edges.Tariff != nil {
		t.tariffID = sub.Edges.Tariff.ID
	}
	if sub.PriceOverride != nil {
		t.periods = nil // a custom price has no period discounts
	}
	if u := sub.Edges.RwUser; u != nil {
		t.expireAt = u.ExpireAt
	}
	return t
}

func addonTarget(sa *ent.SubscriptionAddon) *target {
	sub := sa.Edges.Subscription
	t := &target{
		kind:       KindAddon,
		id:         sa.ID,
		customerID: sub.CustomerID,
		rwUserID:   sa.RwUserID,
		monthly:    AddonPrice(sa),
		periods:    periodsOf(sa.Edges.Tariff),
		title:      sa.Edges.Addon.Name + " · " + SubscriptionTitle(sub),
		included:   sa.Included,
		strategy:   EffectiveStrategy(sa.Edges.Tariff, sub.Edges.Tariff, sa.Included),
		manageRw:   sa.Edges.Tariff != nil && sa.Edges.Tariff.ManageRw,
		anchor:     sa.TrafficAnchorAt,
		resetAt:    sa.TrafficResetAt,
	}
	if sa.PriceOverride != nil || sa.Included {
		t.periods = nil
	}
	if u := sa.Edges.RwUser; u != nil {
		t.expireAt = u.ExpireAt
	}
	return t
}

// SubscriptionTitle is a human label: label, RW username, or "#id".
func SubscriptionTitle(sub *ent.Subscription) string {
	switch {
	case sub.Label != "" && sub.Edges.RwUser != nil:
		return sub.Label + " (" + sub.Edges.RwUser.Username + ")"
	case sub.Label != "":
		return sub.Label
	case sub.Edges.RwUser != nil:
		return sub.Edges.RwUser.Username
	}
	return fmt.Sprintf("#%d", sub.ID)
}

// planItems lists a customer's linked, paid items; AutoExtend marks the
// ones a payment is spread over by default.
func (s *Service) planItems(ctx context.Context, customerID int) ([]PlanItem, error) {
	subs, err := s.db.Subscription.Query().
		Where(subscription.CustomerID(customerID), subscription.RwUserIDNotNil()).
		WithTariff(func(q *ent.TariffQuery) { q.WithPeriods() }).
		WithRwUser().WithCustomer().
		WithAddons(func(q *ent.SubscriptionAddonQuery) {
			q.Where(subscriptionaddon.RwUserIDNotNil()).
				WithTariff(func(q *ent.TariffQuery) { q.WithPeriods() }).
				WithRwUser().WithAddon()
		}).
		All(ctx)
	if err != nil {
		return nil, err
	}
	// Unlimited items never need paying for.
	var items []PlanItem
	for _, sub := range subs {
		if it := toPlanItem(subTarget(sub)); !Unlimited(it.ExpireAt) {
			it.AutoExtend = sub.AutoExtend
			items = append(items, it)
		}
		var subExpire *time.Time
		if sub.Edges.RwUser != nil {
			subExpire = sub.Edges.RwUser.ExpireAt
		}
		for _, sa := range sub.Edges.Addons {
			if sa.Included {
				continue // extended with the subscription
			}
			sa.Edges.Subscription = sub
			it := toPlanItem(addonTarget(sa))
			if Unlimited(it.ExpireAt) {
				continue
			}
			it.ParentID, it.ParentExpireAt = sub.ID, subExpire
			it.AutoExtend = sa.AutoExtend
			items = append(items, it)
		}
	}
	return items, nil
}

func toPlanItem(t *target) PlanItem {
	return PlanItem{Kind: t.kind, ID: t.id, Title: t.title, Monthly: t.monthly, Periods: t.periods, ExpireAt: t.expireAt}
}

// Referral is what a payment accrues to the payer's referrer.
type Referral struct {
	ReferrerID   int     `json:"referrer_id"`
	ReferrerName string  `json:"referrer_name"`
	Percent      float64 `json:"percent"`
	Amount       int64   `json:"amount"`
}

func (s *Service) referralFor(ctx context.Context, payer *ent.Customer, amount int64) (*Referral, error) {
	if payer.ReferrerID == nil {
		return nil, nil
	}
	ref, err := s.db.Customer.Get(ctx, *payer.ReferrerID)
	if err != nil {
		return nil, err
	}
	pct := s.settings.Float(ctx, settings.ReferralPercent)
	if ref.ReferralPercent != nil {
		pct = *ref.ReferralPercent
	}
	if pct <= 0 {
		return nil, nil
	}
	return &Referral{ReferrerID: ref.ID, ReferrerName: ref.Name, Percent: pct, Amount: ReferralAmount(amount, pct)}, nil
}

// PaymentPreview is what recording a payment would do.
type PaymentPreview struct {
	BalanceBefore int64        `json:"balance_before"`
	BalanceAfter  int64        `json:"balance_after_payment"`
	Allocations   []Allocation `json:"allocations"`
	Remainder     int64        `json:"remainder"`
	Items         []PlanItem   `json:"items"`
	Referral      *Referral    `json:"referral"`
}

func (s *Service) PreviewPayment(ctx context.Context, customerID int, amount int64, remainderToDays bool) (*PaymentPreview, error) {
	c, err := s.db.Customer.Get(ctx, customerID)
	if err != nil {
		return nil, err
	}
	bal, err := s.Balance(ctx, customerID)
	if err != nil {
		return nil, err
	}
	items, err := s.planItems(ctx, customerID)
	if err != nil {
		return nil, err
	}
	// The money is spread over auto-extended items only; the rest are listed
	// for a manual pick.
	var auto []PlanItem
	for _, it := range items {
		if it.AutoExtend {
			auto = append(auto, it)
		}
	}
	alloc, rest := Plan(bal+amount, auto, s.now(), remainderToDays)
	ref, err := s.referralFor(ctx, c, amount)
	if err != nil {
		return nil, err
	}
	return &PaymentPreview{
		BalanceBefore: bal, BalanceAfter: bal + amount,
		Allocations: alloc, Remainder: rest, Items: items, Referral: ref,
	}, nil
}

// PaymentInput records a payment and the extensions to buy with it.
type PaymentInput struct {
	CustomerID  int
	Amount      int64
	Date        time.Time
	Method      string
	Note        string
	Allocations []AllocationInput
}

// AllocationInput is one (possibly admin-edited) allocation to apply.
type AllocationInput struct {
	Kind   string `json:"kind"`
	ID     int    `json:"id"`
	Months int    `json:"months"`
	Days   int    `json:"days"`
	Amount int64  `json:"amount"`
}

// ExtensionResult is the outcome of one extension.
type ExtensionResult struct {
	Kind   string     `json:"kind"`
	ID     int        `json:"id"`
	Title  string     `json:"title"`
	Months int        `json:"months"`
	Days   int        `json:"days"`
	Amount int64      `json:"amount"`
	From   *time.Time `json:"from,omitempty"`
	To     *time.Time `json:"to,omitempty"`
	OK     bool       `json:"ok"`
	Error  string     `json:"error,omitempty"`
}

type PaymentResult struct {
	Payment    *ent.Payment      `json:"payment"`
	Extensions []ExtensionResult `json:"extensions"`
	Balance    int64             `json:"balance"`
	Referral   *Referral         `json:"referral"`
}

// CommitPayment stores the payment (+ledger, +referral accrual) and then
// applies each allocation; a failed Remnawave call refunds that allocation's
// charge and is reported, not fatal.
func (s *Service) CommitPayment(ctx context.Context, in PaymentInput) (*PaymentResult, error) {
	if in.Amount <= 0 {
		return nil, apperr.New("amount_positive", "amount must be greater than zero")
	}
	c, err := s.db.Customer.Get(ctx, in.CustomerID)
	if err != nil {
		return nil, err
	}
	if in.Date.IsZero() {
		in.Date = s.now()
	}
	ref, err := s.referralFor(ctx, c, in.Amount)
	if err != nil {
		return nil, err
	}

	var p *ent.Payment
	err = store.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		var err error
		p, err = tx.Payment.Create().
			SetCustomerID(c.ID).SetAmount(in.Amount).SetDate(in.Date).
			SetMethod(in.Method).SetNote(in.Note).
			Save(ctx)
		if err != nil {
			return err
		}
		if err := tx.LedgerEntry.Create().
			SetCustomerID(c.ID).SetType(ledgerentry.TypePayment).SetAmount(in.Amount).
			SetDate(in.Date).SetPaymentID(p.ID).SetNote(in.Note).
			Exec(ctx); err != nil {
			return err
		}
		if ref != nil && ref.Amount > 0 {
			if err := tx.LedgerEntry.Create().
				SetCustomerID(ref.ReferrerID).SetType(ledgerentry.TypeReferral).SetAmount(ref.Amount).
				SetDate(in.Date).SetPaymentID(p.ID).SetNote(fmt.Sprintf("%s (%g%%)", c.Name, ref.Percent)).
				Exec(ctx); err != nil {
				return err
			}
			return tx.ReferralAccrual.Create().
				SetReferrerID(ref.ReferrerID).SetRefereeID(c.ID).SetPaymentID(p.ID).
				SetPercent(ref.Percent).SetAmount(ref.Amount).SetDate(in.Date).
				SetStatus(referralaccrual.StatusCredited).
				Exec(ctx)
		}
		return nil
	})
	audit.Log(ctx, s.db, "payment.create", "customer", c.ID, map[string]any{"amount": in.Amount, "note": in.Note}, err)
	if err != nil {
		return nil, fmt.Errorf("save payment: %w", err)
	}

	res := &PaymentResult{Payment: p, Referral: ref}
	for _, a := range in.Allocations {
		if a.Months <= 0 && a.Days <= 0 {
			continue
		}
		r := s.extend(ctx, ExtendInput{
			Kind: a.Kind, ID: a.ID, Months: a.Months, Days: a.Days, Amount: a.Amount,
			AllowDebt: true, PaymentID: &p.ID, CustomerID: c.ID,
		})
		res.Extensions = append(res.Extensions, r)
	}
	res.Balance, err = s.Balance(ctx, c.ID)
	return res, err
}

// ExtendInput is a manual or payment-driven extension.
type ExtendInput struct {
	Kind      string
	ID        int
	Months    int
	Days      int
	Amount    int64
	AllowDebt bool
	PaymentID *int
	// CustomerID, when set, must own the target (guards payment allocations).
	CustomerID int
}

// Extend charges the balance and moves expireAt in Remnawave.
func (s *Service) Extend(ctx context.Context, in ExtendInput) (*ExtensionResult, error) {
	r := s.extend(ctx, in)
	if !r.OK {
		return &r, errors.New(r.Error)
	}
	return &r, nil
}

// ExtendQuote is the default price of an extension plus what the form
// needs around it: the term's dates, the tariff's prices and the balance.
type ExtendQuote struct {
	CustomerID int
	Amount     int64
	From       time.Time
	To         time.Time
	Monthly    int64
	Periods    []Period
	Balance    int64
}

// QuoteExtend is the default price of extending a target by months+days.
func (s *Service) QuoteExtend(ctx context.Context, kind string, id, months, days int) (*ExtendQuote, error) {
	t, err := s.loadTarget(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	bal, err := s.Balance(ctx, t.customerID)
	if err != nil {
		return nil, err
	}
	if Unlimited(t.expireAt) {
		return nil, ErrUnlimited
	}
	from := ExtendFrom(s.now(), t.expireAt)
	return &ExtendQuote{
		CustomerID: t.customerID,
		Amount:     TermCost(t.monthly, t.periods, months, days),
		From:       from, To: from.AddDate(0, months, days),
		Monthly: t.monthly, Periods: t.periods, Balance: bal,
	}, nil
}

func (s *Service) extend(ctx context.Context, in ExtendInput) ExtensionResult {
	res := ExtensionResult{Kind: in.Kind, ID: in.ID, Months: in.Months, Days: in.Days, Amount: in.Amount}
	fail := func(err error) ExtensionResult {
		res.Error = err.Error()
		return res
	}
	if in.Months < 0 || in.Days < 0 || in.Months+in.Days == 0 {
		return fail(apperr.New("billing.term_required", "set the term"))
	}
	if in.Amount < 0 {
		return fail(apperr.New("billing.negative_amount", "amount can't be negative"))
	}
	t, err := s.loadTarget(ctx, in.Kind, in.ID)
	if err != nil {
		return fail(err)
	}
	res.Title = t.title
	if in.CustomerID != 0 && t.customerID != in.CustomerID {
		return fail(apperr.New("billing.other_customer", "the item belongs to another customer"))
	}
	if t.rwUserID == nil {
		return fail(ErrNotLinked)
	}
	if Unlimited(t.expireAt) {
		return fail(ErrUnlimited)
	}
	if t.included {
		return fail(ErrIncluded)
	}
	if !in.AllowDebt {
		bal, err := s.Balance(ctx, t.customerID)
		if err != nil {
			return fail(err)
		}
		if bal < in.Amount {
			return fail(ErrInsufficientBalance)
		}
	}

	// Fresh expiry from the panel: the cache may be minutes old.
	user, err := s.rw.GetUser(ctx, *t.rwUserID)
	if err != nil {
		return fail(err)
	}
	now := s.now()
	from := ExtendFrom(now, &user.ExpireAt)
	to := from.AddDate(0, in.Months, in.Days)
	res.From, res.To = &from, &to

	ledgerID, err := s.charge(ctx, t.customerID, in.Amount, in.PaymentID,
		fmt.Sprintf("Extension: %s, %s", t.title, durationLabel(in.Months, in.Days)))
	if err != nil {
		return fail(err)
	}

	upd := remnawave.UpdateUserRequest{ID: user.ID, ExpireAt: &to}
	if user.Status == remnawave.StatusExpired || user.Status == remnawave.StatusDisabled {
		upd.Status = remnawave.StatusActive
	}
	updated, rwErr := s.rw.UpdateUser(ctx, upd)
	if rwErr == nil {
		_ = rwsync.Upsert(ctx, s.db, updated)
	} else if ledgerID != nil {
		// Undo the charge: nothing was extended.
		_ = s.db.LedgerEntry.DeleteOneID(*ledgerID).Exec(context.WithoutCancel(ctx))
		ledgerID = nil
	}
	s.recordExtension(ctx, "extend", t, in.PaymentID, in.Months, in.Days, in.Amount, &from, &to, ledgerID, rwErr)
	if rwErr == nil && t.kind == KindSubscription {
		s.followSubscription(ctx, t.id, to)
	}
	if rwErr == nil && from.Equal(now) {
		s.restartTrafficMonth(ctx, t, now)
	}
	audit.Log(ctx, s.db, "extend", t.kind, t.id, map[string]any{"months": in.Months, "days": in.Days, "amount": in.Amount, "to": to}, rwErr)
	if rwErr != nil {
		return fail(rwErr)
	}
	res.OK = true
	return res
}

// restartTrafficMonth starts a new traffic month after a late renewal:
// anchor = now, and a reset of the user (and a subscription's included
// add-ons) where resets are active. Best-effort: the job catches up.
func (s *Service) restartTrafficMonth(ctx context.Context, t *target, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.setTraffic(ctx, t.kind, t.id, &now, nil, false); err != nil {
		slog.Error("traffic anchor", "kind", t.kind, "id", t.id, "error", err)
		return
	}
	reset := func(kind string, id, rwUserID int) {
		if err := s.resetTraffic(ctx, kind, id, rwUserID, now); err != nil {
			slog.Error("traffic reset", "kind", kind, "id", id, "error", err)
		}
	}
	if resetsActive(t.strategy, t.manageRw, t.expireAt) {
		reset(t.kind, t.id, *t.rwUserID)
	}
	if t.kind != KindSubscription || t.strategy != StrategySubscription {
		return
	}
	sas, err := s.db.SubscriptionAddon.Query().
		Where(subscriptionaddon.SubscriptionID(t.id), subscriptionaddon.Included(true), subscriptionaddon.RwUserIDNotNil()).
		WithTariff().WithRwUser().
		All(ctx)
	if err != nil {
		slog.Error("included add-ons", "subscription", t.id, "error", err)
		return
	}
	for _, sa := range sas {
		var expireAt *time.Time
		if sa.Edges.RwUser != nil {
			expireAt = sa.Edges.RwUser.ExpireAt
		}
		// Included add-ons take the parent's strategy (t.strategy).
		if sa.Edges.Tariff != nil && resetsActive(t.strategy, sa.Edges.Tariff.ManageRw, expireAt) {
			reset(KindAddon, sa.ID, *sa.RwUserID)
		}
	}
}

func (s *Service) charge(ctx context.Context, customerID int, amount int64, paymentID *int, note string) (*int, error) {
	if amount == 0 {
		return nil, nil
	}
	e, err := s.db.LedgerEntry.Create().
		SetCustomerID(customerID).SetType(ledgerentry.TypeCharge).SetAmount(-amount).
		SetDate(s.now()).SetNillablePaymentID(paymentID).SetNote(note).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return &e.ID, nil
}

func (s *Service) recordExtension(ctx context.Context, kind string, t *target, paymentID *int, months, days int, amount int64, from, to *time.Time, ledgerID *int, opErr error) {
	q := s.db.Extension.Create().
		SetKind(extensionKind(kind)).
		SetCustomerID(t.customerID).
		SetNillablePaymentID(paymentID).
		SetMonths(months).SetDays(days).SetAmount(amount).
		SetNillableFromAt(from).SetNillableToAt(to).
		SetNillableLedgerEntryID(ledgerID).
		SetActor(audit.Actor(ctx)).
		SetStatus("ok")
	if t.kind == KindSubscription {
		q.SetSubscriptionID(t.id)
	} else {
		q.SetSubscriptionAddonID(t.id)
	}
	if opErr != nil {
		q.SetStatus("failed").SetError(opErr.Error())
	}
	_ = q.Exec(context.WithoutCancel(ctx))
}

func durationLabel(months, days int) string {
	switch {
	case months > 0 && days > 0:
		return fmt.Sprintf("%d mo %d d", months, days)
	case months > 0:
		return fmt.Sprintf("%d mo", months)
	}
	return fmt.Sprintf("%d d", days)
}

// Adjust adds a manual balance correction (positive or negative).
func (s *Service) Adjust(ctx context.Context, customerID int, amount int64, note string) error {
	if amount == 0 {
		return apperr.New("amount_nonzero", "amount can't be zero")
	}
	if _, err := s.db.Customer.Query().Where(customer.ID(customerID)).Only(ctx); err != nil {
		return err
	}
	err := s.db.LedgerEntry.Create().
		SetCustomerID(customerID).SetType(ledgerentry.TypeAdjustment).SetAmount(amount).
		SetDate(s.now()).SetNote(note).Exec(ctx)
	audit.Log(ctx, s.db, "balance.adjust", "customer", customerID, map[string]any{"amount": amount, "note": note}, err)
	return err
}
