package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"remna-control/ent"
	"remna-control/ent/subscription"
	"remna-control/ent/subscriptionaddon"
	"remna-control/internal/audit"
	"remna-control/internal/jobs"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
)

// panelItem is a subscription or add-on the job brings in line with its
// tariff.
type panelItem struct {
	kind string
	id   int
	// tariff is the own tariff, strategy the effective one
	tariff   *ent.Tariff
	strategy string
	user     *ent.RwUser
	// anchor is the own one, or the parent's for an included add-on;
	// resetAt is always the own handled point
	anchor   *time.Time
	resetAt  *time.Time
	included bool
}

// panelCounts is what a panel sync pass got done.
type panelCounts struct {
	pushes, resets, backfills int
}

// summary is like "2 resets, 1 params push"; "" when nothing was done.
func (n panelCounts) summary() string {
	var parts []string
	add := func(k int, one, many string) {
		switch {
		case k == 1:
			parts = append(parts, "1 "+one)
		case k > 1:
			parts = append(parts, fmt.Sprintf("%d %s", k, many))
		}
	}
	add(n.resets, "reset", "resets")
	add(n.pushes, "params push", "params pushes")
	add(n.backfills, "anchor backfill", "anchor backfills")
	return strings.Join(parts, ", ")
}

// PanelSyncJob is SyncPanel as a background job; on a partial failure it
// returns the joined error along with what did get done.
func (s *Service) PanelSyncJob(ctx context.Context) (jobs.Result, error) {
	n, err := s.syncPanel(ctx)
	return jobs.Result{Summary: n.summary(), Changed: n != panelCounts{}}, err
}

// SyncPanel pushes tariff params that drifted to the panel and resets
// traffic where a SUBSCRIPTION reset point is due. One entity's failure
// doesn't stop the pass; the errors are joined.
func (s *Service) SyncPanel(ctx context.Context) error {
	_, err := s.syncPanel(ctx)
	return err
}

func (s *Service) syncPanel(ctx context.Context) (panelCounts, error) {
	var n panelCounts
	subs, err := s.db.Subscription.Query().WithAddons().All(ctx)
	if err != nil {
		return n, err
	}
	var errs []error
	sync := func(kind string, id int) {
		if err := s.syncItem(ctx, kind, id, &n); err != nil {
			slog.Error("panel sync", "kind", kind, "id", id, "error", err)
			errs = append(errs, fmt.Errorf("%s %d: %w", kind, id, err))
		}
	}
	for _, sub := range subs {
		sync(KindSubscription, sub.ID) // may backfill the anchor included add-ons follow
		for _, sa := range sub.Edges.Addons {
			sync(KindAddon, sa.ID)
		}
	}
	return n, errors.Join(errs...)
}

// loadPanelItem loads an item fresh; nil when it's gone.
func (s *Service) loadPanelItem(ctx context.Context, kind string, id int) (*panelItem, error) {
	switch kind {
	case KindSubscription:
		sub, err := s.db.Subscription.Query().Where(subscription.ID(id)).WithTariff().WithRwUser().Only(ctx)
		if err != nil {
			return nil, err
		}
		return &panelItem{
			kind: kind, id: id, tariff: sub.Edges.Tariff,
			strategy: EffectiveStrategy(sub.Edges.Tariff, nil, false),
			user:     sub.Edges.RwUser, anchor: sub.TrafficAnchorAt, resetAt: sub.TrafficResetAt,
		}, nil
	case KindAddon:
		sa, err := s.db.SubscriptionAddon.Query().Where(subscriptionaddon.ID(id)).
			WithTariff().WithRwUser().WithSubscription(func(q *ent.SubscriptionQuery) { q.WithTariff() }).
			Only(ctx)
		if err != nil {
			return nil, err
		}
		var parent *ent.Tariff
		var parentAnchor *time.Time
		if sub := sa.Edges.Subscription; sub != nil {
			parent, parentAnchor = sub.Edges.Tariff, sub.TrafficAnchorAt
		}
		it := &panelItem{
			kind: kind, id: id, tariff: sa.Edges.Tariff,
			strategy: EffectiveStrategy(sa.Edges.Tariff, parent, sa.Included),
			user:     sa.Edges.RwUser, anchor: sa.TrafficAnchorAt, resetAt: sa.TrafficResetAt,
			included: sa.Included,
		}
		if sa.Included {
			it.anchor = parentAnchor
		}
		return it, nil
	}
	return nil, fmt.Errorf("unknown kind %q", kind)
}

// syncItem reloads the item under s.mu, so an admin action that moved its
// anchor or marker since the pass started is seen, then pushes drifted
// params and handles the item's traffic resets, counting what it did in n.
func (s *Service) syncItem(ctx context.Context, kind string, id int, n *panelCounts) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.loadPanelItem(ctx, kind, id)
	if ent.IsNotFound(err) {
		return nil // deleted since the pass started
	}
	if err != nil {
		return err
	}
	u := it.user
	if u == nil || u.Deleted {
		return nil
	}
	var errs []error
	manageRw := it.tariff != nil && it.tariff.ManageRw
	if manageRw {
		errs = append(errs, s.pushParams(ctx, it, n))
	}
	errs = append(errs, s.syncResets(ctx, it, manageRw, s.now(), n))
	return errors.Join(errs...)
}

// pushParams PATCHes the panel user when it deviates from its tariff. The
// PATCH is retried every pass, but audited only when its request or
// outcome changed for the user.
func (s *Service) pushParams(ctx context.Context, it *panelItem, n *panelCounts) error {
	want := DesiredParams(it.tariff, it.strategy, it.user.ExpireAt)
	if !want.Differs(it.user) {
		return nil
	}
	upd := remnawave.UpdateUserRequest{ID: it.user.ID}
	want.Apply(&upd)
	u, err := s.rw.UpdateUser(ctx, upd)
	if err == nil {
		err = rwsync.Upsert(ctx, s.db, u)
	}
	b, _ := json.Marshal(upd)
	outcome := string(b) + " ok"
	if err != nil {
		outcome = string(b) + " " + err.Error()
	}
	if last, ok := s.panelOutcome[it.user.ID]; !ok || last != outcome {
		audit.Log(ctx, s.db, "rw.sync_params", it.kind, it.id, upd, err)
		s.panelOutcome[it.user.ID] = outcome
	}
	if err == nil {
		n.pushes++
	}
	return err
}

// syncResets resets the user when a reset point after the handled one is
// due. Inactive resets drop the marker; an active entity without one has
// its due point marked handled, so (re-)enabling resets nobody at once.
func (s *Service) syncResets(ctx context.Context, it *panelItem, manageRw bool, now time.Time, n *panelCounts) error {
	exp := it.user.ExpireAt
	if !resetsActive(it.strategy, manageRw, exp) {
		if it.resetAt != nil {
			return s.setTraffic(ctx, it.kind, it.id, nil, nil, true)
		}
		return nil
	}
	// Expired users wait for a late renewal, which resets them; DISABLED
	// ones get one catch-up reset once re-enabled.
	if exp == nil || !exp.After(now) || it.user.Status == remnawave.StatusDisabled {
		return nil
	}
	if it.anchor == nil {
		if it.included {
			return nil // the parent has no anchor to follow
		}
		a := BackfillAnchor(*exp, now, time.Local)
		err := s.backfillAnchor(ctx, it.kind, it.id, &a, *exp, now)
		if err == nil {
			n.backfills++
		}
		return err
	}
	p, due := ResetDue(*it.anchor, now, time.Local)
	if it.resetAt == nil {
		if !due {
			// A future anchor: the point before it counts as handled, so
			// the reset at the anchor itself still happens.
			p = AddMonths(*it.anchor, -1, time.Local)
		}
		return s.markHandled(ctx, it.kind, it.id, p)
	}
	if due && p.After(*it.resetAt) {
		err := s.resetTraffic(ctx, it.kind, it.id, it.user.ID, p)
		if err == nil {
			n.resets++
		}
		return err
	}
	return nil
}
