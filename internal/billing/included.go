package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"remna-control/ent"
	"remna-control/ent/subscription"
	"remna-control/ent/subscriptionaddon"
	"remna-control/ent/tariff"
	"remna-control/internal/audit"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
)

// Included add-ons come with a base tariff (Tariff.included_addons): each is
// its own Remnawave user, as with paid add-ons, but it costs nothing on top
// of the subscription and shares its expiry, so it's extended, enabled and
// disabled together with it and never shows up as a separate item to pay.

// includedTariffs returns the add-on tariffs a base tariff includes.
func (s *Service) includedTariffs(ctx context.Context, baseID *int) ([]*ent.Tariff, error) {
	if baseID == nil {
		return nil, nil
	}
	return s.db.Tariff.Query().
		Where(tariff.HasIncludedInWith(tariff.ID(*baseID))).
		WithAddon().WithPeriods().
		All(ctx)
}

// AddonChange is what a tariff switch does to one add-on.
type AddonChange struct {
	// SubscriptionAddonID is set when the add-on is already connected.
	SubscriptionAddonID *int   `json:"subscription_addon_id"`
	AddonID             int    `json:"addon_id"`
	AddonName           string `json:"addon_name"`
	TariffName          string `json:"tariff_name"`
	// Action: "connect" (new user), "include" (a paid add-on becomes
	// included; Credit refunds its unused paid time) or "remove" (no longer
	// included; the caller decides: RemoveDisable or RemoveKeepPaid).
	Action string `json:"action"`
	Credit int64  `json:"credit"`
}

const (
	RemoveDisable  = "disable"
	RemoveKeepPaid = "keep_paid"
)

// includedChanges compares a subscription's included add-ons with what the
// tariff newID includes.
func (s *Service) includedChanges(ctx context.Context, sub *ent.Subscription, newID int) ([]AddonChange, error) {
	next, err := s.includedTariffs(ctx, &newID)
	if err != nil {
		return nil, err
	}
	byAddon := map[int]*ent.SubscriptionAddon{}
	for _, sa := range sub.Edges.Addons {
		byAddon[sa.AddonID] = sa
	}
	wanted := map[int]bool{}
	var out []AddonChange
	now := s.now()
	for _, t := range next {
		if t.AddonID == nil || t.Edges.Addon == nil {
			continue
		}
		wanted[*t.AddonID] = true
		ch := AddonChange{AddonID: *t.AddonID, AddonName: t.Edges.Addon.Name, TariffName: t.Name, Action: "connect"}
		if sa := byAddon[*t.AddonID]; sa != nil {
			if sa.Included {
				continue // stays included
			}
			ch.SubscriptionAddonID, ch.Action = &sa.ID, "include"
			if sa.Edges.RwUser != nil {
				paid := EffectivePrice(sa.PriceOverride, sa.Edges.Tariff)
				ch.Credit = -ProrateSurcharge(paid, 0, now, sa.Edges.RwUser.ExpireAt)
			}
		}
		out = append(out, ch)
	}
	for _, sa := range sub.Edges.Addons {
		if !sa.Included || wanted[sa.AddonID] {
			continue
		}
		ch := AddonChange{SubscriptionAddonID: &sa.ID, AddonID: sa.AddonID, Action: "remove"}
		if sa.Edges.Addon != nil {
			ch.AddonName = sa.Edges.Addon.Name
		}
		if sa.Edges.Tariff != nil {
			ch.TariffName = sa.Edges.Tariff.Name
		}
		out = append(out, ch)
	}
	return out, nil
}

// loadSubForIncluded loads a subscription with what included add-on work
// needs.
func (s *Service) loadSubForIncluded(ctx context.Context, id int) (*ent.Subscription, error) {
	return s.db.Subscription.Query().Where(subscription.ID(id)).
		WithRwUser().WithCustomer().WithTariff().
		WithAddons(func(q *ent.SubscriptionAddonQuery) {
			q.WithAddon().WithRwUser().WithTariff(func(q *ent.TariffQuery) { q.WithPeriods() })
		}).
		Only(ctx)
}

// applyIncluded makes the subscription's add-ons match what its tariff
// includes: connects missing ones and turns paid ones into included, all
// until the subscription's expiry. removed says, per subscription add-on,
// what to do with those no longer included; removed[0] is the default for
// the rest (RemoveDisable when unset). exchange is set after a term
// exchange moved the subscription's expiry: add-ons that stay included follow
// it (they live and end with the subscription), and a paid add-on turning
// included keeps its own expiry when that is later, so no paid time is lost.
func (s *Service) applyIncluded(ctx context.Context, subID int, removed map[int]string, exchange bool) error {
	sub, err := s.loadSubForIncluded(ctx, subID)
	if err != nil {
		return err
	}
	if sub.TariffID == nil || sub.Edges.RwUser == nil || sub.Edges.RwUser.ExpireAt == nil {
		return nil
	}
	changes, err := s.includedChanges(ctx, sub, *sub.TariffID)
	if err != nil {
		return err
	}
	tariffs, err := s.includedTariffs(ctx, sub.TariffID)
	if err != nil {
		return err
	}
	tariffFor := map[int]*ent.Tariff{}
	for _, t := range tariffs {
		tariffFor[*t.AddonID] = t
	}
	to := *sub.Edges.RwUser.ExpireAt
	var errs []error
	// handled are the add-ons this switch already set up.
	handled := map[int]bool{}
	for _, ch := range changes {
		var err error
		if ch.SubscriptionAddonID != nil {
			handled[*ch.SubscriptionAddonID] = true
		}
		switch ch.Action {
		case "connect":
			var sa *ent.SubscriptionAddon
			now := s.now()
			sa, _, err = s.connectAddonUser(ctx, sub, tariffFor[ch.AddonID], to, nil, true)
			if err == nil {
				handled[sa.ID] = true
				s.recordExtension(ctx, "connect", &target{kind: KindAddon, id: sa.ID, customerID: sub.CustomerID}, nil, 0, 0, 0, &now, &to, nil, nil)
			}
		case "include":
			err = s.includePaid(ctx, *ch.SubscriptionAddonID, tariffFor[ch.AddonID], sub.Edges.Tariff, to, exchange)
		case "remove":
			how, ok := removed[*ch.SubscriptionAddonID]
			if !ok {
				how = removed[0]
			}
			err = s.dropIncluded(ctx, *ch.SubscriptionAddonID, how)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("add-on %s: %w", ch.AddonName, err))
		}
	}
	if exchange {
		s.followSubscriptionExcept(ctx, subID, to, handled)
	}
	return errors.Join(errs...)
}

// SyncIncluded applies a base tariff's (edited) included add-ons to its
// current subscribers; removedHow is what happens to add-ons it no longer
// includes. It returns how many subscriptions were updated.
func (s *Service) SyncIncluded(ctx context.Context, tariffID int, removedHow string) (int, error) {
	ids, err := s.db.Subscription.Query().
		Where(subscription.TariffID(tariffID), subscription.RwUserIDNotNil()).IDs(ctx)
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, id := range ids {
		if err := s.applyIncluded(ctx, id, map[int]string{0: removedHow}, false); err != nil {
			errs = append(errs, fmt.Errorf("subscription %d: %w", id, err))
		}
	}
	audit.Log(ctx, s.db, "tariff.sync_included", "tariff", tariffID, map[string]any{"subscriptions": len(ids), "removed": removedHow}, errors.Join(errs...))
	return len(ids), errors.Join(errs...)
}

// includePaid turns a connected add-on into an included one: the included
// tariff's parameters, the subscription's expiry. Its own reset marker is
// dropped, so it starts following the subscription's resets without an
// extra one at once. keepLonger (a term exchange, which credits nothing for
// the add-on's paid time) keeps the add-on's own expiry when it is later.
func (s *Service) includePaid(ctx context.Context, saID int, t, parent *ent.Tariff, to time.Time, keepLonger bool) error {
	sa, err := s.db.SubscriptionAddon.Query().Where(subscriptionaddon.ID(saID)).WithRwUser().Only(ctx)
	if err != nil {
		return err
	}
	if u := sa.Edges.RwUser; keepLonger && u != nil && u.ExpireAt != nil && u.ExpireAt.After(to) {
		to = *u.ExpireAt
	}
	if sa.RwUserID != nil {
		upd := remnawave.UpdateUserRequest{ID: *sa.RwUserID, ExpireAt: &to, Status: remnawave.StatusActive}
		DesiredParams(t, EffectiveStrategy(t, parent, true), &to).Apply(&upd)
		u, err := s.rw.UpdateUser(ctx, upd)
		audit.Log(ctx, s.db, "rw.include_addon", "subscription_addon", sa.ID, upd, err)
		if err != nil {
			return err
		}
		_ = rwsync.Upsert(ctx, s.db, u)
	}
	return s.db.SubscriptionAddon.UpdateOneID(saID).
		SetIncluded(true).SetTariffID(t.ID).ClearPriceOverride().SetAutoExtend(true).
		ClearTrafficResetAt().
		Exec(ctx)
}

// dropIncluded handles an add-on the new tariff no longer includes: it's
// disabled in the panel, or kept as a paid add-on on its add-on tariff
// with the term it has.
func (s *Service) dropIncluded(ctx context.Context, saID int, how string) error {
	q := s.db.SubscriptionAddon.UpdateOneID(saID).SetIncluded(false)
	if how == RemoveKeepPaid {
		return q.Exec(ctx)
	}
	sa, err := s.db.SubscriptionAddon.Get(ctx, saID)
	if err != nil {
		return err
	}
	if sa.RwUserID != nil {
		u, err := s.rw.DisableUser(ctx, *sa.RwUserID)
		audit.Log(ctx, s.db, "rw.disable", KindAddon, sa.ID, nil, err)
		if err != nil {
			return err
		}
		_ = rwsync.Upsert(ctx, s.db, u)
	}
	return q.SetAutoExtend(false).Exec(ctx)
}

// followSubscription moves the included add-ons of a subscription to its
// new expiry (re-activating them) after the subscription was extended.
func (s *Service) followSubscription(ctx context.Context, subID int, to time.Time) {
	s.followSubscriptionExcept(ctx, subID, to, nil)
}

// followSubscriptionExcept is followSubscription leaving out the add-ons in
// skip.
func (s *Service) followSubscriptionExcept(ctx context.Context, subID int, to time.Time, skip map[int]bool) {
	sas, err := s.db.SubscriptionAddon.Query().
		Where(subscriptionaddon.SubscriptionID(subID), subscriptionaddon.Included(true), subscriptionaddon.RwUserIDNotNil()).
		WithAddon().WithRwUser().WithSubscription().
		All(ctx)
	if err != nil {
		slog.Error("included add-ons", "subscription", subID, "error", err)
		return
	}
	for _, sa := range sas {
		if skip[sa.ID] {
			continue
		}
		upd := remnawave.UpdateUserRequest{ID: *sa.RwUserID, ExpireAt: &to}
		if u := sa.Edges.RwUser; u != nil && (u.Status == remnawave.StatusExpired || u.Status == remnawave.StatusDisabled) {
			upd.Status = remnawave.StatusActive
		}
		var from *time.Time
		if sa.Edges.RwUser != nil {
			from = sa.Edges.RwUser.ExpireAt
		}
		u, err := s.rw.UpdateUser(ctx, upd)
		if err == nil {
			_ = rwsync.Upsert(ctx, s.db, u)
		}
		t := &target{kind: KindAddon, id: sa.ID, customerID: sa.Edges.Subscription.CustomerID}
		s.recordExtension(ctx, "extend", t, nil, 0, 0, 0, from, &to, nil, err)
		audit.Log(ctx, s.db, "extend.included", KindAddon, sa.ID, map[string]any{"to": to}, err)
	}
}

// setIncludedEnabled enables/disables a subscription's included add-ons.
func (s *Service) setIncludedEnabled(ctx context.Context, subID int, enabled bool) error {
	sas, err := s.db.SubscriptionAddon.Query().
		Where(subscriptionaddon.SubscriptionID(subID), subscriptionaddon.Included(true), subscriptionaddon.RwUserIDNotNil()).
		All(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, sa := range sas {
		var u *remnawave.User
		if enabled {
			u, err = s.rw.EnableUser(ctx, *sa.RwUserID)
		} else {
			u, err = s.rw.DisableUser(ctx, *sa.RwUserID)
		}
		audit.Log(ctx, s.db, map[bool]string{true: "rw.enable", false: "rw.disable"}[enabled], KindAddon, sa.ID, nil, err)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		_ = rwsync.Upsert(ctx, s.db, u)
	}
	return errors.Join(errs...)
}
