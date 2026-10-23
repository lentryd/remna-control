package billing

import (
	"testing"
	"time"

	"remna-control/ent"
	"remna-control/ent/auditlog"
)

// novFirst is a month after the fixture's start (Oct 1 12:00 UTC).
var novFirst = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// until moves a panel user's expiry, in the panel and the cache.
func (f *fixture) until(t *testing.T, rwUserID int, at time.Time) {
	t.Helper()
	f.panel.mu.Lock()
	f.panel.users[rwUserID].ExpireAt = at
	f.panel.mu.Unlock()
	f.db.RwUser.UpdateOneID(rwUserID).SetExpireAt(at).ExecX(f.ctx)
}

// limit sets a panel user's traffic limit, in the panel and the cache.
func (f *fixture) limit(t *testing.T, rwUserID int, bytes int64) {
	t.Helper()
	f.panel.mu.Lock()
	f.panel.users[rwUserID].TrafficLimitBytes = bytes
	f.panel.mu.Unlock()
	f.db.RwUser.UpdateOneID(rwUserID).SetTrafficLimitBytes(bytes).ExecX(f.ctx)
}

func (f *fixture) patches() int {
	f.panel.mu.Lock()
	defer f.panel.mu.Unlock()
	return f.panel.patches
}

func (f *fixture) provisionAs(t *testing.T, tariffID int, username string) *ent.Subscription {
	t.Helper()
	sub, err := f.svc.ProvisionSubscription(f.ctx, ProvisionInput{
		CustomerID: f.customer, TariffID: tariffID, Username: username, Months: 1, AllowDebt: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func (f *fixture) sync(t *testing.T) {
	t.Helper()
	if err := f.svc.SyncPanel(f.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPanelSync_PushesDrift(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.plain).SetManageRw(true).ExecX(f.ctx)
	sub := f.provision(t, f.plain)
	f.limit(t, *sub.RwUserID, 200<<30)
	before := f.patches()

	f.sync(t)
	if got := f.panelUser(t, "alice").TrafficLimitBytes; got != 0 {
		t.Fatalf("panel limit %d, want 0", got)
	}
	if n := f.patches() - before; n != 1 {
		t.Fatalf("%d patches, want 1", n)
	}
	f.sync(t)
	if n := f.patches() - before; n != 1 {
		t.Errorf("%d patches after the second pass, want 1", n)
	}
}

func TestPanelSync_LeavesUnsetHwid(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.plain).SetManageRw(true).ExecX(f.ctx)
	sub := f.provision(t, f.plain)
	five := 5
	f.panel.users[*sub.RwUserID].HwidDeviceLimit = &five
	f.db.RwUser.UpdateOneID(*sub.RwUserID).SetHwidDeviceLimit(5).ExecX(f.ctx)
	before := f.patches()

	f.sync(t)
	if n := f.patches() - before; n != 0 {
		t.Errorf("%d patches", n)
	}
	if h := f.panelUser(t, "alice").HwidDeviceLimit; h == nil || *h != 5 {
		t.Errorf("hwid %v, want 5", h)
	}
}

func TestPanelSync_ManageRwOff(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	f.db.Tariff.UpdateOneID(f.plain).SetManageRw(false).ExecX(f.ctx)
	f.limit(t, *sub.RwUserID, 200<<30)
	f.until(t, *sub.RwUserID, novFirst.AddDate(0, 1, 0))
	f.setNow(novFirst)
	before := f.patches()

	f.sync(t)
	if n := f.patches() - before; n != 0 {
		t.Errorf("%d patches", n)
	}
	if n := f.resets(); n != 0 {
		t.Errorf("%d resets", n)
	}
	if got := f.panelUser(t, "alice").TrafficLimitBytes; got != 200<<30 {
		t.Errorf("panel limit %d", got)
	}
}

func TestPanelSync_ResetsWhenDue(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	f.until(t, *sub.RwUserID, novFirst.AddDate(0, 1, 0))
	f.setNow(novFirst)

	f.sync(t)
	if n := f.resets(); n != 1 {
		t.Fatalf("%d resets, want 1", n)
	}
	if sub = f.db.Subscription.GetX(f.ctx, sub.ID); !sameTime(sub.TrafficResetAt, novFirst) {
		t.Errorf("reset_at %v, want %s", sub.TrafficResetAt, novFirst)
	}
	f.sync(t)
	if n := f.resets(); n != 1 {
		t.Errorf("%d resets after the second pass, want 1", n)
	}
}

func TestPanelSync_SkipsExpiredAndUnlimited(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	expired := f.provisionAs(t, f.plain, "alice")       // until Nov 1 12:00
	forever := f.provisionAs(t, f.plain, "unlimited_b") // until 2099
	f.until(t, *forever.RwUserID, time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC))
	f.setNow(novFirst.AddDate(0, 0, 3))

	f.sync(t)
	if n := f.resets(); n != 0 {
		t.Errorf("%d resets", n)
	}
	if s := f.panelUser(t, "unlimited_b").TrafficLimitStrategy; s != "MONTH_ROLLING" {
		t.Errorf("unlimited strategy %q", s)
	}
	// The expired user keeps its marker for a late renewal.
	if sub := f.db.Subscription.GetX(f.ctx, expired.ID); !sameTime(sub.TrafficResetAt, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("expired reset_at %v", sub.TrafficResetAt)
	}
}

func TestPanelSync_SkipsDisabled(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	f.until(t, *sub.RwUserID, novFirst.AddDate(0, 1, 0))
	if err := f.svc.SetEnabled(f.ctx, KindSubscription, sub.ID, false); err != nil {
		t.Fatal(err)
	}
	f.setNow(novFirst)

	f.sync(t)
	if n := f.resets(); n != 0 {
		t.Errorf("%d resets", n)
	}
}

// Review Focus 2: one user's reset fails, the others are still handled and
// the failed one is retried next pass.
func TestPanelSync_ResetFailureRetries(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	a := f.provisionAs(t, f.plain, "alice")
	b := f.provisionAs(t, f.plain, "bob")
	for _, s := range []*ent.Subscription{a, b} {
		f.until(t, *s.RwUserID, novFirst.AddDate(0, 1, 0))
	}
	f.setNow(novFirst)
	f.panel.failResetFor = *a.RwUserID

	if err := f.svc.SyncPanel(f.ctx); err == nil {
		t.Fatal("want an error for the failed reset")
	}
	if n := f.resets(); n != 1 {
		t.Fatalf("%d resets, want 1 (bob)", n)
	}
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if s := f.db.Subscription.GetX(f.ctx, a.ID); !sameTime(s.TrafficResetAt, start) {
		t.Errorf("alice reset_at %v, want the old %s", s.TrafficResetAt, start)
	}
	if s := f.db.Subscription.GetX(f.ctx, b.ID); !sameTime(s.TrafficResetAt, novFirst) {
		t.Errorf("bob reset_at %v, want %s", s.TrafficResetAt, novFirst)
	}

	f.panel.failResetFor = 0
	f.sync(t)
	f.sync(t)
	if n := f.resets(); n != 2 {
		t.Errorf("%d resets, want 2", n)
	}
	if s := f.db.Subscription.GetX(f.ctx, a.ID); !sameTime(s.TrafficResetAt, novFirst) {
		t.Errorf("alice reset_at %v, want %s", s.TrafficResetAt, novFirst)
	}
}

// Review Focus 3: back to MONTH_ROLLING the panel resets on its own, we
// don't; switching to SUBSCRIPTION again doesn't reset everyone at once.
func TestPanelSync_SwitchBack(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	f.until(t, *sub.RwUserID, time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC))

	f.db.Tariff.UpdateOneID(f.plain).SetTrafficStrategy("MONTH_ROLLING").ExecX(f.ctx)
	f.setNow(novFirst)
	f.sync(t)
	if s := f.panelUser(t, "alice").TrafficLimitStrategy; s != "MONTH_ROLLING" {
		t.Errorf("panel strategy %q", s)
	}
	sub = f.db.Subscription.GetX(f.ctx, sub.ID)
	if sub.TrafficResetAt != nil || !sameTime(sub.TrafficAnchorAt, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("anchor %v reset_at %v", sub.TrafficAnchorAt, sub.TrafficResetAt)
	}
	f.setNow(time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC))
	f.sync(t)
	if n := f.resets(); n != 0 {
		t.Fatalf("%d resets while MONTH_ROLLING", n)
	}

	// Back to SUBSCRIPTION on Dec 5: the Dec 1 point counts as handled.
	f.db.Tariff.UpdateOneID(f.plain).SetTrafficStrategy(StrategySubscription).ExecX(f.ctx)
	f.sync(t)
	if s := f.panelUser(t, "alice").TrafficLimitStrategy; s != "NO_RESET" {
		t.Errorf("panel strategy %q", s)
	}
	dec := time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	if sub = f.db.Subscription.GetX(f.ctx, sub.ID); !sameTime(sub.TrafficResetAt, dec) {
		t.Errorf("reset_at %v, want %s", sub.TrafficResetAt, dec)
	}
	if n := f.resets(); n != 0 {
		t.Fatalf("%d resets on switching back", n)
	}
	f.setNow(time.Date(2027, 1, 1, 12, 0, 0, 0, time.UTC))
	f.sync(t)
	if n := f.resets(); n != 1 {
		t.Errorf("%d resets on Jan 1, want 1", n)
	}
}

// Review Focus 5: an included add-on follows the parent's SUBSCRIPTION
// strategy and anchor, whatever its own tariff says.
func TestPanelSync_IncludedInheritsParent(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	f.db.Tariff.UpdateOneID(f.premium).SetTrafficStrategy(StrategySubscription).SetManageRw(true).ExecX(f.ctx)
	f.db.Tariff.UpdateOneID(f.white).SetTrafficStrategy("MONTH_ROLLING").SetManageRw(true).ExecX(f.ctx)
	sub := f.provision(t, f.premium)
	sa := f.addons(t, sub.ID)[0]
	// Drift on the add-on user: the panel would reset it on its own.
	f.panel.users[*sa.RwUserID].TrafficLimitStrategy = "MONTH_ROLLING"
	f.db.RwUser.UpdateOneID(*sa.RwUserID).SetTrafficLimitStrategy("MONTH_ROLLING").ExecX(f.ctx)

	f.sync(t)
	if s := f.panelUser(t, "white_alice").TrafficLimitStrategy; s != "NO_RESET" {
		t.Errorf("add-on panel strategy %q", s)
	}
	start := f.now
	if sa = f.db.SubscriptionAddon.GetX(f.ctx, sa.ID); sa.TrafficAnchorAt != nil || !sameTime(sa.TrafficResetAt, start) {
		t.Errorf("add-on anchor %v reset_at %v, want reset_at %s", sa.TrafficAnchorAt, sa.TrafficResetAt, start)
	}
	if n := f.resets(); n != 0 {
		t.Fatalf("%d resets on the first pass", n)
	}

	f.until(t, *sub.RwUserID, novFirst.AddDate(0, 1, 0))
	f.until(t, *sa.RwUserID, novFirst.AddDate(0, 1, 0))
	f.setNow(novFirst)
	f.sync(t)
	f.sync(t)
	if n := f.resets(); n != 2 {
		t.Errorf("%d resets, want 2 (subscription + add-on)", n)
	}
	if sa = f.db.SubscriptionAddon.GetX(f.ctx, sa.ID); !sameTime(sa.TrafficResetAt, novFirst) {
		t.Errorf("add-on reset_at %v, want %s", sa.TrafficResetAt, novFirst)
	}
}

// A hand-set future anchor: no reset before it, one when it arrives.
func TestPanelSync_FutureAnchorResetsOnArrival(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	f.until(t, *sub.RwUserID, novFirst.AddDate(0, 1, 0))
	anchor := f.now.AddDate(0, 0, 5)
	if err := f.svc.SetTrafficAnchor(f.ctx, KindSubscription, sub.ID, anchor); err != nil {
		t.Fatal(err)
	}

	f.sync(t)
	if n := f.resets(); n != 0 {
		t.Fatalf("%d resets before the anchor", n)
	}
	f.setNow(anchor)
	f.sync(t)
	f.sync(t)
	if n := f.resets(); n != 1 {
		t.Errorf("%d resets at the anchor, want 1", n)
	}
}

// Review Focus 4: a late renewal whose reset call fails still succeeds;
// the job resets on its next pass.
func TestExtendLate_ResetFailsJobCatchesUp(t *testing.T) {
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
	if sub = f.db.Subscription.GetX(f.ctx, sub.ID); !sameTime(sub.TrafficResetAt, start) {
		t.Fatalf("reset_at %v, want the old %s", sub.TrafficResetAt, start)
	}

	f.panel.failReset = false
	f.setNow(late.Add(5 * time.Minute))
	f.sync(t)
	f.sync(t)
	if n := f.resets(); n != 1 {
		t.Errorf("%d resets, want 1", n)
	}
	if sub = f.db.Subscription.GetX(f.ctx, sub.ID); !sameTime(sub.TrafficResetAt, late) {
		t.Errorf("reset_at %v, want %s", sub.TrafficResetAt, late)
	}
}

// A pass that loaded the subscription before a late renewal resets by the
// old anchor: the renewal's newer marker must stay.
func TestResetTraffic_MarkerNeverMovesBack(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	sub := f.provision(t, f.plain)
	f.until(t, *sub.RwUserID, late.AddDate(0, 1, 0))
	f.setNow(late)
	if err := f.svc.setTraffic(f.ctx, KindSubscription, sub.ID, &late, &late, false); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.resetTraffic(f.ctx, KindSubscription, sub.ID, *sub.RwUserID, novFirst); err != nil {
		t.Fatal(err)
	}
	if sub = f.db.Subscription.GetX(f.ctx, sub.ID); !sameTime(sub.TrafficResetAt, late) {
		t.Errorf("reset_at %v, want %s", sub.TrafficResetAt, late)
	}
	f.sync(t)
	if n := f.resets(); n != 1 {
		t.Errorf("%d resets, want 1 (the stale one)", n)
	}
}

func (f *fixture) audits(action string) int {
	return f.db.AuditLog.Query().Where(auditlog.Action(action)).CountX(f.ctx)
}

// A PATCH that keeps failing is retried every pass but audited once.
func TestPanelSync_FailingPatchAuditedOnce(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.plain).SetManageRw(true).ExecX(f.ctx)
	sub := f.provision(t, f.plain)
	f.limit(t, *sub.RwUserID, 200<<30)
	f.panel.failPatch = true
	before := f.patches()

	for range 2 {
		if err := f.svc.SyncPanel(f.ctx); err == nil {
			t.Fatal("want an error for the failed PATCH")
		}
	}
	if n := f.patches() - before; n != 2 {
		t.Errorf("%d patches, want 2", n)
	}
	if n := f.audits("rw.sync_params"); n != 1 {
		t.Errorf("%d audit rows, want 1", n)
	}

	f.panel.failPatch = false
	f.sync(t)
	if n := f.audits("rw.sync_params"); n != 2 {
		t.Errorf("%d audit rows after recovering, want 2", n)
	}
}

// Imported subscriptions without an anchor, one of them expired, switched to
// SUBSCRIPTION: nobody is reset at once, anchors come from the expiry, and
// the next anchor point resets exactly once.
func TestPanelSync_ImportedSwitchToSubscription(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	f.db.Tariff.UpdateOneID(f.plain).SetTrafficStrategy("MONTH_ROLLING").ExecX(f.ctx)
	s1 := f.provisionAs(t, f.plain, "user1")
	s2 := f.provisionAs(t, f.plain, "user2")
	s3 := f.provisionAs(t, f.plain, "user3")
	for _, s := range []*ent.Subscription{s1, s2, s3} {
		f.db.Subscription.UpdateOneID(s.ID).ClearTrafficAnchorAt().ClearTrafficResetAt().ExecX(f.ctx)
	}
	f.until(t, *s1.RwUserID, time.Date(2026, 12, 20, 9, 0, 0, 0, time.UTC))
	f.until(t, *s2.RwUserID, time.Date(2026, 10, 25, 21, 0, 0, 0, time.UTC))
	f.until(t, *s3.RwUserID, time.Date(2026, 10, 10, 21, 0, 0, 0, time.UTC)) // expired
	f.setNow(time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC))
	f.sync(t)
	f.sync(t)
	p0 := f.patches()

	f.db.Tariff.UpdateOneID(f.plain).SetTrafficStrategy(StrategySubscription).ExecX(f.ctx)
	f.sync(t)
	p1 := f.patches()
	if n := p1 - p0; n != 3 {
		t.Errorf("%d patches on the switch, want 3 (NO_RESET)", n)
	}
	f.sync(t)
	if n := f.patches() - p1; n != 0 {
		t.Errorf("%d patches on the second pass", n)
	}
	if n := f.resets(); n != 0 {
		t.Fatalf("%d resets on the switch", n)
	}
	for _, c := range []struct {
		sub    *ent.Subscription
		anchor *time.Time
	}{
		{s1, ptr(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC))},
		{s2, ptr(time.Date(2026, 9, 25, 21, 0, 0, 0, time.UTC))},
		{s3, nil},
	} {
		s := f.db.Subscription.GetX(f.ctx, c.sub.ID)
		if c.anchor == nil {
			if s.TrafficAnchorAt != nil || s.TrafficResetAt != nil {
				t.Errorf("expired sub %d anchor %v reset_at %v, want none", s.ID, s.TrafficAnchorAt, s.TrafficResetAt)
			}
			continue
		}
		if !sameTime(s.TrafficAnchorAt, *c.anchor) || !sameTime(s.TrafficResetAt, *c.anchor) {
			t.Errorf("sub %d anchor %v reset_at %v, want %s", s.ID, s.TrafficAnchorAt, s.TrafficResetAt, c.anchor)
		}
	}

	// user2 expires before its next point; user1 resets on Nov 20.
	f.setNow(time.Date(2026, 10, 25, 22, 0, 0, 0, time.UTC))
	f.sync(t)
	f.setNow(time.Date(2026, 11, 20, 9, 5, 0, 0, time.UTC))
	f.sync(t)
	f.sync(t)
	if n := f.resets(); n != 1 {
		t.Errorf("%d resets, want 1 (user1)", n)
	}
}

func TestPanelSyncJob_Counts(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.plain).SetManageRw(true).ExecX(f.ctx)
	sub := f.provision(t, f.plain)
	f.sync(t)

	res, err := f.svc.PanelSyncJob(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "" || res.Changed {
		t.Errorf("idle pass = %+v, want empty", res)
	}

	f.limit(t, *sub.RwUserID, 200<<30)
	res, err = f.svc.PanelSyncJob(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "1 params push" || !res.Changed {
		t.Errorf("drift pass = %+v, want 1 params push, changed", res)
	}
}

// A partial failure returns the error and what did get done.
func TestPanelSyncJob_PartialFailure(t *testing.T) {
	f := setup(t)
	f.subscriptionReset(t)
	a := f.provisionAs(t, f.plain, "alice")
	b := f.provisionAs(t, f.plain, "bob")
	for _, s := range []*ent.Subscription{a, b} {
		f.until(t, *s.RwUserID, novFirst.AddDate(0, 1, 0))
	}
	f.setNow(novFirst)
	f.panel.failResetFor = *a.RwUserID

	res, err := f.svc.PanelSyncJob(f.ctx)
	if err == nil {
		t.Fatal("want an error for the failed reset")
	}
	if res.Summary != "1 reset" || !res.Changed {
		t.Errorf("result = %+v, want 1 reset, changed", res)
	}
}

func TestPanelSummary(t *testing.T) {
	cases := []struct {
		n    panelCounts
		want string
	}{
		{panelCounts{}, ""},
		{panelCounts{resets: 2, pushes: 1}, "2 resets, 1 params push"},
		{panelCounts{pushes: 3, backfills: 1}, "3 params pushes, 1 anchor backfill"},
		{panelCounts{resets: 1, backfills: 2}, "1 reset, 2 anchor backfills"},
	}
	for _, c := range cases {
		if got := c.n.summary(); got != c.want {
			t.Errorf("%+v.summary() = %q, want %q", c.n, got, c.want)
		}
	}
}
