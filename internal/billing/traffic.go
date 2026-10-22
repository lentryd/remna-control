package billing

import (
	"context"
	"fmt"
	"time"

	"remna-control/ent"
	"remna-control/ent/subscription"
	"remna-control/ent/subscriptionaddon"
	"remna-control/internal/audit"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
)

// StrategySubscription resets traffic monthly from the start of paid coverage.
const StrategySubscription = "SUBSCRIPTION"

// AddMonths moves t (in loc) by k months, clamping the day to the target
// month's last day and keeping the clock time.
func AddMonths(t time.Time, k int, loc *time.Location) time.Time {
	t = t.In(loc)
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(k), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
	if last := first.AddDate(0, 1, -1).Day(); d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
}

// monthsBetween is the calendar month difference b - a.
func monthsBetween(a, b time.Time) int {
	return (b.Year()-a.Year())*12 + int(b.Month()-a.Month())
}

// latestAtOrBefore returns the largest k with AddMonths(anchor, k) <= now.
func latestAtOrBefore(anchor, now time.Time, loc *time.Location) int {
	anchor, now = anchor.In(loc), now.In(loc)
	k := monthsBetween(anchor, now)
	for AddMonths(anchor, k, loc).After(now) {
		k--
	}
	for !AddMonths(anchor, k+1, loc).After(now) {
		k++
	}
	return k
}

// ResetDue returns the latest scheduled reset <= now; false if anchor is in the future.
func ResetDue(anchor, now time.Time, loc *time.Location) (time.Time, bool) {
	if anchor.After(now) {
		return time.Time{}, false
	}
	return AddMonths(anchor, latestAtOrBefore(anchor, now, loc), loc), true
}

// NextReset returns the first scheduled reset strictly after now.
func NextReset(anchor, now time.Time, loc *time.Location) time.Time {
	if anchor.After(now) {
		return anchor.In(loc)
	}
	return AddMonths(anchor, latestAtOrBefore(anchor, now, loc)+1, loc)
}

// BackfillAnchor returns the latest expireAt minus k months that is <= now.
func BackfillAnchor(expireAt, now time.Time, loc *time.Location) time.Time {
	expireAt, now = expireAt.In(loc), now.In(loc)
	k := max(monthsBetween(now, expireAt), 0) // expireAt - k months lands near now
	for AddMonths(expireAt, -k, loc).After(now) {
		k++
	}
	for k > 0 && !AddMonths(expireAt, -(k-1), loc).After(now) {
		k--
	}
	return AddMonths(expireAt, -k, loc)
}

// EffectiveStrategy is the reset strategy a user follows: an included add-on
// takes its parent subscription's tariff strategy, anything else its own.
// It is "" when the chosen tariff is nil.
func EffectiveStrategy(own, parent *ent.Tariff, included bool) string {
	t := own
	if included && parent != nil {
		t = parent
	}
	if t == nil {
		return ""
	}
	return t.TrafficStrategy
}

// PanelStrategy maps a tariff strategy to what the panel is told: SUBSCRIPTION
// is reset by us, so the panel gets NO_RESET (MONTH_ROLLING for unlimited
// users, who have no paid coverage to anchor on).
func PanelStrategy(strategy string, expireAt *time.Time) string {
	if strategy != StrategySubscription {
		return strategy
	}
	if Unlimited(expireAt) {
		return "MONTH_ROLLING"
	}
	return "NO_RESET"
}

// PanelParams are the tariff-driven parameters of a panel user.
type PanelParams struct {
	TrafficLimitBytes int64
	Strategy          string
	HwidLimit         *int
	Squads            []string
}

// DesiredParams is what the panel user should have under tariff t.
func DesiredParams(t *ent.Tariff, strategy string, expireAt *time.Time) PanelParams {
	return PanelParams{
		TrafficLimitBytes: t.TrafficLimitBytes,
		Strategy:          PanelStrategy(strategy, expireAt),
		HwidLimit:         t.HwidLimit,
		Squads:            t.SquadUuids,
	}
}

// Apply copies the parameters into a panel update; unset HWID and squads
// are left alone.
func (p PanelParams) Apply(upd *remnawave.UpdateUserRequest) {
	limit := p.TrafficLimitBytes
	upd.TrafficLimitBytes = &limit
	upd.TrafficLimitStrategy = p.Strategy
	if p.HwidLimit != nil {
		upd.HwidDeviceLimit = p.HwidLimit
	}
	if len(p.Squads) > 0 {
		upd.ActiveInternalSquads = p.Squads
	}
}

// Differs reports whether the cached panel user deviates from the params.
// Unset HWID and squads are not compared.
func (p PanelParams) Differs(u *ent.RwUser) bool {
	if u.TrafficLimitBytes != p.TrafficLimitBytes || u.TrafficLimitStrategy != p.Strategy {
		return true
	}
	if p.HwidLimit != nil && (u.HwidDeviceLimit == nil || *u.HwidDeviceLimit != *p.HwidLimit) {
		return true
	}
	return len(p.Squads) > 0 && squadsDiffer(u.SquadUuids, p.Squads)
}

// squadsDiffer compares two squad lists as sets.
func squadsDiffer(have, want []string) bool {
	h, w := map[string]bool{}, map[string]bool{}
	for _, s := range have {
		h[s] = true
	}
	for _, s := range want {
		w[s] = true
	}
	if len(h) != len(w) {
		return true
	}
	for s := range w {
		if !h[s] {
			return true
		}
	}
	return false
}

// resetsActive reports whether we reset a user's traffic: its effective
// strategy is SUBSCRIPTION, its own tariff manages the panel user and its
// expiry isn't unlimited. For an included add-on the strategy is the
// parent's (EffectiveStrategy), manageRw its own tariff's.
func resetsActive(strategy string, manageRw bool, expireAt *time.Time) bool {
	return strategy == StrategySubscription && manageRw && !Unlimited(expireAt)
}

// trafficMutation is what Subscription and SubscriptionAddon mutations share.
type trafficMutation interface {
	SetTrafficAnchorAt(time.Time)
	SetTrafficResetAt(time.Time)
	ClearTrafficResetAt()
}

// setTraffic stores an entity's anchor and handled reset point; a nil
// value is left alone, unless clearReset clears the reset point.
func (s *Service) setTraffic(ctx context.Context, kind string, id int, anchor, resetAt *time.Time, clearReset bool) error {
	apply := func(m trafficMutation) {
		if anchor != nil {
			m.SetTrafficAnchorAt(*anchor)
		}
		if resetAt != nil {
			m.SetTrafficResetAt(*resetAt)
		} else if clearReset {
			m.ClearTrafficResetAt()
		}
	}
	switch kind {
	case KindSubscription:
		q := s.db.Subscription.UpdateOneID(id)
		apply(q.Mutation())
		return q.Exec(ctx)
	case KindAddon:
		q := s.db.SubscriptionAddon.UpdateOneID(id)
		apply(q.Mutation())
		return q.Exec(ctx)
	}
	return fmt.Errorf("unknown kind %q", kind)
}

// markHandled moves the handled reset point forward to point; a later
// marker is kept, so acting on a stale anchor can't move it back.
func (s *Service) markHandled(ctx context.Context, kind string, id int, point time.Time) error {
	switch kind {
	case KindSubscription:
		return s.db.Subscription.Update().
			Where(subscription.ID(id), subscription.Or(subscription.TrafficResetAtIsNil(), subscription.TrafficResetAtLT(point))).
			SetTrafficResetAt(point).Exec(ctx)
	case KindAddon:
		return s.db.SubscriptionAddon.Update().
			Where(subscriptionaddon.ID(id), subscriptionaddon.Or(subscriptionaddon.TrafficResetAtIsNil(), subscriptionaddon.TrafficResetAtLT(point))).
			SetTrafficResetAt(point).Exec(ctx)
	}
	return fmt.Errorf("unknown kind %q", kind)
}

// resetTraffic resets the panel user's traffic and marks point as handled;
// on a panel error the marker stays, so the job retries.
func (s *Service) resetTraffic(ctx context.Context, kind string, id, rwUserID int, point time.Time) error {
	u, err := s.rw.ResetTraffic(ctx, rwUserID)
	if err == nil {
		// Mark first: a failed cache write must not cause a second reset.
		err = s.markHandled(ctx, kind, id, point)
		if uerr := rwsync.Upsert(ctx, s.db, u); err == nil {
			err = uerr
		}
	}
	audit.Log(ctx, s.db, "traffic.reset", kind, id, map[string]any{"point": point}, err)
	return err
}

// backfillAnchor keeps the anchor, or derives a missing one from the
// expiry; the due point already passed counts as handled.
func (s *Service) backfillAnchor(ctx context.Context, kind string, id int, anchor *time.Time, expireAt, now time.Time) error {
	if anchor == nil {
		a := BackfillAnchor(expireAt, now, time.Local)
		anchor = &a
	}
	var resetAt *time.Time
	if p, ok := ResetDue(*anchor, now, time.Local); ok {
		resetAt = &p
	}
	return s.setTraffic(ctx, kind, id, anchor, resetAt, true)
}

// SetTrafficAnchor sets the anchor by hand; reset points before now count
// as handled when resets are active. Included add-ons follow their
// subscription's anchor.
func (s *Service) SetTrafficAnchor(ctx context.Context, kind string, id int, anchor time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.loadTarget(ctx, kind, id)
	if err != nil {
		return err
	}
	if t.included {
		return ErrIncluded
	}
	var resetAt *time.Time
	if p, ok := ResetDue(anchor, s.now(), time.Local); ok && resetsActive(t.strategy, t.manageRw, t.expireAt) {
		resetAt = &p
	}
	err = s.setTraffic(ctx, kind, id, &anchor, resetAt, true)
	audit.Log(ctx, s.db, "traffic.anchor", kind, id, map[string]any{"anchor": anchor}, err)
	return err
}

// SubscriptionResetAnchor is the anchor driving a subscription's resets (the
// Tariff and RwUser edges must be loaded); nil when resets aren't active.
func SubscriptionResetAnchor(sub *ent.Subscription) *time.Time {
	t, u := sub.Edges.Tariff, sub.Edges.RwUser
	if t == nil || u == nil || !resetsActive(EffectiveStrategy(t, nil, false), t.ManageRw, u.ExpireAt) {
		return nil
	}
	return sub.TrafficAnchorAt
}

// AddonResetAnchor is the anchor driving an add-on's resets: the parent
// subscription's for an included one, its own otherwise; nil when inactive.
func AddonResetAnchor(sa *ent.SubscriptionAddon, sub *ent.Subscription) *time.Time {
	t, u := sa.Edges.Tariff, sa.Edges.RwUser
	var parent *ent.Tariff
	if sub != nil {
		parent = sub.Edges.Tariff
	}
	if t == nil || u == nil || !resetsActive(EffectiveStrategy(t, parent, sa.Included), t.ManageRw, u.ExpireAt) {
		return nil
	}
	if sa.Included {
		if sub == nil {
			return nil
		}
		return sub.TrafficAnchorAt
	}
	return sa.TrafficAnchorAt
}
