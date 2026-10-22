package billing

import (
	"testing"
	"time"

	"remna-control/ent"
	"remna-control/ent/tariff"
)

// subscriptionReset sets Plain to the SUBSCRIPTION strategy with ManageRw
// and pins time.Local to UTC so calendar math matches the expectations.
// It swaps a global: tests using it must not call t.Parallel.
func (f *fixture) subscriptionReset(t *testing.T) {
	t.Helper()
	loc := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = loc })
	f.db.Tariff.UpdateOneID(f.plain).SetTrafficStrategy(StrategySubscription).SetManageRw(true).ExecX(f.ctx)
}

// late is a renewal after Plain's first month (until Nov 1) ran out.
var late = time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC)

func (f *fixture) setNow(at time.Time) {
	f.now = at
	f.svc.now = func() time.Time { return at }
}

func (f *fixture) resets() int {
	f.panel.mu.Lock()
	defer f.panel.mu.Unlock()
	return f.panel.resets
}

func sameTime(a *time.Time, b time.Time) bool { return a != nil && a.Equal(b) }

func TestProvision_SetsAnchor(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)

	if !sameTime(sub.TrafficAnchorAt, f.now) || !sameTime(sub.TrafficResetAt, f.now) {
		t.Fatalf("anchor %v reset_at %v, want %s", sub.TrafficAnchorAt, sub.TrafficResetAt, f.now)
	}
	if n := f.resets(); n != 0 {
		t.Errorf("%d resets", n)
	}
}

func TestProvision_AnchorWithoutResetsForOtherStrategies(t *testing.T) {
	f := setup(t)
	sub := f.provision(t, f.plain)
	// The anchor is kept for a later switch to SUBSCRIPTION; no marker.
	if !sameTime(sub.TrafficAnchorAt, f.now) || sub.TrafficResetAt != nil {
		t.Fatalf("anchor %v reset_at %v", sub.TrafficAnchorAt, sub.TrafficResetAt)
	}
}

func TestExtendEarly_KeepsAnchor(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	start := f.now
	f.setNow(start.AddDate(0, 0, 10))

	if _, err := f.svc.Extend(f.ctx, ExtendInput{Kind: KindSubscription, ID: sub.ID, Months: 1, AllowDebt: true}); err != nil {
		t.Fatal(err)
	}
	sub = f.db.Subscription.GetX(f.ctx, sub.ID)
	if !sameTime(sub.TrafficAnchorAt, start) || !sameTime(sub.TrafficResetAt, start) {
		t.Errorf("anchor %v reset_at %v, want %s", sub.TrafficAnchorAt, sub.TrafficResetAt, start)
	}
	if n := f.resets(); n != 0 {
		t.Errorf("%d resets", n)
	}
}

func TestExtendLate_MovesAnchorAndResets(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	f.panel.users[*sub.RwUserID].UserTraffic.UsedTrafficBytes = 5 << 30
	// Expired on Nov 1; paid again on Nov 20.
	f.setNow(late)

	res, err := f.svc.Extend(f.ctx, ExtendInput{Kind: KindSubscription, ID: sub.ID, Months: 1, Days: 12, AllowDebt: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.From.Equal(late) {
		t.Fatalf("from %s, want %s", res.From, late)
	}
	sub = f.db.Subscription.GetX(f.ctx, sub.ID)
	if !sameTime(sub.TrafficAnchorAt, late) || !sameTime(sub.TrafficResetAt, late) {
		t.Errorf("anchor %v reset_at %v, want %s", sub.TrafficAnchorAt, sub.TrafficResetAt, late)
	}
	if n := f.resets(); n != 1 {
		t.Errorf("%d resets, want 1", n)
	}
	if used := f.panel.users[*sub.RwUserID].UserTraffic.UsedTrafficBytes; used != 0 {
		t.Errorf("used %d after reset", used)
	}
}

func TestExtendLate_ResetFailureKeepsMarker(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	start := f.now
	f.setNow(late)
	f.panel.failReset = true

	res, err := f.svc.Extend(f.ctx, ExtendInput{Kind: KindSubscription, ID: sub.ID, Months: 1, AllowDebt: true})
	if err != nil || !res.OK {
		t.Fatalf("extension failed: %v", err)
	}
	sub = f.db.Subscription.GetX(f.ctx, sub.ID)
	if !sameTime(sub.TrafficAnchorAt, late) {
		t.Errorf("anchor %v, want %s", sub.TrafficAnchorAt, late)
	}
	if !sameTime(sub.TrafficResetAt, start) {
		t.Errorf("reset_at %v, want the old %s", sub.TrafficResetAt, start)
	}
}

func TestConnectAddon_PaidSetsAnchor(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	f.db.Tariff.UpdateOneID(f.white).SetTrafficStrategy(StrategySubscription).SetManageRw(true).ExecX(f.ctx)
	sub := f.provision(t, f.plain)
	f.setNow(f.now.Add(time.Hour))

	sa, err := f.svc.ConnectAddon(f.ctx, ConnectAddonInput{SubscriptionID: sub.ID, TariffID: f.white, Months: 1, AllowDebt: true})
	if err != nil {
		t.Fatal(err)
	}
	if !sameTime(sa.TrafficAnchorAt, f.now) || !sameTime(sa.TrafficResetAt, f.now) {
		t.Errorf("anchor %v reset_at %v, want %s", sa.TrafficAnchorAt, sa.TrafficResetAt, f.now)
	}
}

func TestIncludedAddon_NoOwnAnchor(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	f.db.Tariff.UpdateOneID(f.premium).SetTrafficStrategy(StrategySubscription).ExecX(f.ctx)
	sub := f.provision(t, f.premium)

	sa := f.addons(t, sub.ID)[0]
	if sa.TrafficAnchorAt != nil || sa.TrafficResetAt != nil {
		t.Errorf("included add-on anchor %v reset_at %v", sa.TrafficAnchorAt, sa.TrafficResetAt)
	}
	if err := f.svc.SetTrafficAnchor(f.ctx, KindAddon, sa.ID, f.now); err != ErrIncluded {
		t.Errorf("SetTrafficAnchor on an included add-on: %v", err)
	}
}

func TestChangeTariff_BackfillsWithoutReset(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	f.db.Tariff.UpdateOneID(f.plain).SetTrafficStrategy("MONTH_ROLLING").ExecX(f.ctx)
	monthly := f.db.Tariff.Create().SetKind(tariff.KindBase).SetName("Monthly").SetManageRw(true).
		SetTrafficStrategy(StrategySubscription).SetMonthlyPrice(20000).SetSquadUuids([]string{"sq-main"}).SaveX(f.ctx)
	sub := f.provision(t, f.plain)
	// An imported subscription: no anchor.
	f.db.Subscription.UpdateOneID(sub.ID).ClearTrafficAnchorAt().ExecX(f.ctx)

	expire := time.Date(2026, 12, 20, 9, 0, 0, 0, time.UTC)
	f.panel.users[*sub.RwUserID].ExpireAt = expire
	f.db.RwUser.UpdateOneID(*sub.RwUserID).SetExpireAt(expire).ExecX(f.ctx)
	f.setNow(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))

	if err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{Kind: KindSubscription, ID: sub.ID, TariffID: monthly.ID}); err != nil {
		t.Fatal(err)
	}
	sub = f.db.Subscription.GetX(f.ctx, sub.ID)
	want := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	if !sameTime(sub.TrafficAnchorAt, want) || !sameTime(sub.TrafficResetAt, want) {
		t.Errorf("anchor %v reset_at %v, want %s", sub.TrafficAnchorAt, sub.TrafficResetAt, want)
	}
	if n := f.resets(); n != 0 {
		t.Errorf("%d resets", n)
	}
}

func TestChangeTariff_KeepsExistingAnchor(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	start := f.now
	f.setNow(start.AddDate(0, 0, 3))

	if err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{Kind: KindSubscription, ID: sub.ID, TariffID: f.plain}); err != nil {
		t.Fatal(err)
	}
	if sub = f.db.Subscription.GetX(f.ctx, sub.ID); !sameTime(sub.TrafficAnchorAt, start) {
		t.Errorf("anchor %v, want %s", sub.TrafficAnchorAt, start)
	}
}

func TestSetTrafficAnchor_Future(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	anchor := f.now.AddDate(0, 0, 5)

	if err := f.svc.SetTrafficAnchor(f.ctx, KindSubscription, sub.ID, anchor); err != nil {
		t.Fatal(err)
	}
	sub = f.db.Subscription.GetX(f.ctx, sub.ID)
	if !sameTime(sub.TrafficAnchorAt, anchor) || sub.TrafficResetAt != nil {
		t.Errorf("anchor %v reset_at %v", sub.TrafficAnchorAt, sub.TrafficResetAt)
	}
}

func TestSetTrafficAnchor_PastMarksDuePointHandled(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	anchor := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)

	if err := f.svc.SetTrafficAnchor(f.ctx, KindSubscription, sub.ID, anchor); err != nil {
		t.Fatal(err)
	}
	sub = f.db.Subscription.GetX(f.ctx, sub.ID)
	want := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	if !sameTime(sub.TrafficAnchorAt, anchor) || !sameTime(sub.TrafficResetAt, want) {
		t.Errorf("anchor %v reset_at %v, want reset_at %s", sub.TrafficAnchorAt, sub.TrafficResetAt, want)
	}
	if n := f.resets(); n != 0 {
		t.Errorf("%d resets", n)
	}
}

func TestChangeTariff_MarksDuePointOfKeptAnchor(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	f.db.Tariff.UpdateOneID(f.plain).SetTrafficStrategy("MONTH_ROLLING").ExecX(f.ctx)
	monthly := f.db.Tariff.Create().SetKind(tariff.KindBase).SetName("Monthly").SetManageRw(true).
		SetTrafficStrategy(StrategySubscription).SetMonthlyPrice(20000).SetSquadUuids([]string{"sq-main"}).SaveX(f.ctx)
	sub := f.provision(t, f.plain) // anchor Oct 1, no marker
	start := f.now
	f.setNow(start.AddDate(0, 0, 10))

	if err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{Kind: KindSubscription, ID: sub.ID, TariffID: monthly.ID}); err != nil {
		t.Fatal(err)
	}
	sub = f.db.Subscription.GetX(f.ctx, sub.ID)
	if !sameTime(sub.TrafficAnchorAt, start) || !sameTime(sub.TrafficResetAt, start) {
		t.Errorf("anchor %v reset_at %v, want %s", sub.TrafficAnchorAt, sub.TrafficResetAt, start)
	}
	if n := f.resets(); n != 0 {
		t.Errorf("%d resets", n)
	}
}

func TestExtendLate_PaidAddonMovesAnchorAndResets(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	f.db.Tariff.UpdateOneID(f.white).SetTrafficStrategy(StrategySubscription).SetManageRw(true).ExecX(f.ctx)
	sub := f.provision(t, f.plain)
	sa, err := f.svc.ConnectAddon(f.ctx, ConnectAddonInput{SubscriptionID: sub.ID, TariffID: f.white, Months: 1, AllowDebt: true})
	if err != nil {
		t.Fatal(err)
	}
	f.setNow(late)

	if _, err := f.svc.Extend(f.ctx, ExtendInput{Kind: KindAddon, ID: sa.ID, Months: 1, AllowDebt: true}); err != nil {
		t.Fatal(err)
	}
	sa = f.db.SubscriptionAddon.GetX(f.ctx, sa.ID)
	if !sameTime(sa.TrafficAnchorAt, late) || !sameTime(sa.TrafficResetAt, late) {
		t.Errorf("anchor %v reset_at %v, want %s", sa.TrafficAnchorAt, sa.TrafficResetAt, late)
	}
	if n := f.resets(); n != 1 {
		t.Errorf("%d resets, want 1", n)
	}
}

func TestExtendLate_NoResetsWhenInactive(t *testing.T) {
	for name, set := range map[string]func(*ent.TariffUpdateOne){
		"other strategy": func(q *ent.TariffUpdateOne) { q.SetTrafficStrategy("MONTH_ROLLING") },
		"no manage_rw":   func(q *ent.TariffUpdateOne) { q.SetManageRw(false) },
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			f.subscriptionReset(t)
			q := f.db.Tariff.UpdateOneID(f.plain)
			set(q)
			q.ExecX(f.ctx)
			sub := f.provision(t, f.plain)
			f.setNow(late)

			if _, err := f.svc.Extend(f.ctx, ExtendInput{Kind: KindSubscription, ID: sub.ID, Months: 1, AllowDebt: true}); err != nil {
				t.Fatal(err)
			}
			sub = f.db.Subscription.GetX(f.ctx, sub.ID)
			if !sameTime(sub.TrafficAnchorAt, late) || sub.TrafficResetAt != nil {
				t.Errorf("anchor %v reset_at %v, want anchor %s", sub.TrafficAnchorAt, sub.TrafficResetAt, late)
			}
			if n := f.resets(); n != 0 {
				t.Errorf("%d resets", n)
			}
		})
	}
}

func TestExtendLate_ResetsIncludedAddons(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	f.db.Tariff.UpdateOneID(f.premium).SetTrafficStrategy(StrategySubscription).SetManageRw(true).ExecX(f.ctx)
	f.db.Tariff.UpdateOneID(f.white).SetManageRw(true).ExecX(f.ctx)
	sub := f.provision(t, f.premium)
	sa := f.addons(t, sub.ID)[0]
	f.panel.users[*sa.RwUserID].UserTraffic.UsedTrafficBytes = 3 << 30
	f.setNow(late)

	if _, err := f.svc.Extend(f.ctx, ExtendInput{Kind: KindSubscription, ID: sub.ID, Months: 1, AllowDebt: true}); err != nil {
		t.Fatal(err)
	}
	sa = f.db.SubscriptionAddon.GetX(f.ctx, sa.ID)
	if sa.TrafficAnchorAt != nil || !sameTime(sa.TrafficResetAt, late) {
		t.Errorf("included add-on anchor %v reset_at %v, want reset_at %s", sa.TrafficAnchorAt, sa.TrafficResetAt, late)
	}
	if n := f.resets(); n != 2 {
		t.Errorf("%d resets, want 2 (subscription + add-on)", n)
	}
	if used := f.panel.users[*sa.RwUserID].UserTraffic.UsedTrafficBytes; used != 0 {
		t.Errorf("add-on used %d after reset", used)
	}
}

// A paid add-on that becomes included drops its own marker and follows the
// subscription's schedule without an immediate reset.
func TestIncludePaid_FollowsParentWithoutReset(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	f.db.Tariff.UpdateOneID(f.premium).SetTrafficStrategy(StrategySubscription).SetManageRw(true).ExecX(f.ctx)
	f.db.Tariff.UpdateOneID(f.white).SetManageRw(true).ExecX(f.ctx)
	sub := f.provision(t, f.plain)
	start := f.now
	until := start.AddDate(0, 0, 15)
	if _, err := f.svc.ConnectAddon(f.ctx, ConnectAddonInput{SubscriptionID: sub.ID, TariffID: f.white, Until: &until, Days: 15, AllowDebt: true}); err != nil {
		t.Fatal(err)
	}
	sa := f.addons(t, sub.ID)[0]
	anchor := sa.TrafficAnchorAt
	// Its own marker lags behind the subscription's latest point (Oct 1).
	f.db.SubscriptionAddon.UpdateOneID(sa.ID).SetTrafficResetAt(time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)).ExecX(f.ctx)
	f.setNow(start.AddDate(0, 0, 9))

	if err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{Kind: KindSubscription, ID: sub.ID, TariffID: f.premium}); err != nil {
		t.Fatal(err)
	}
	sa = f.db.SubscriptionAddon.GetX(f.ctx, sa.ID)
	if !sa.Included || sa.TrafficResetAt != nil {
		t.Fatalf("included %v reset_at %v, want included without a marker", sa.Included, sa.TrafficResetAt)
	}
	if (anchor == nil) != (sa.TrafficAnchorAt == nil) || (anchor != nil && !anchor.Equal(*sa.TrafficAnchorAt)) {
		t.Errorf("anchor %v, want the kept %v", sa.TrafficAnchorAt, anchor)
	}

	f.sync(t)
	if n := f.resets(); n != 0 {
		t.Errorf("%d resets", n)
	}
	if sa = f.db.SubscriptionAddon.GetX(f.ctx, sa.ID); !sameTime(sa.TrafficResetAt, start) {
		t.Errorf("reset_at %v, want the subscription's %s", sa.TrafficResetAt, start)
	}
}
