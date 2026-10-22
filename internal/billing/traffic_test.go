package billing

import (
	"testing"
	"time"

	"remna-control/ent"
)

func TestAddMonths_ClampsWithoutDrift(t *testing.T) {
	a := time.Date(2026, 1, 31, 15, 4, 0, 0, time.UTC)
	want := []string{"2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30"}
	for k, w := range want {
		got := AddMonths(a, k, time.UTC)
		if got.Format("2006-01-02") != w || got.Hour() != 15 || got.Minute() != 4 {
			t.Fatalf("k=%d: %v", k, got)
		}
	}
	if got := AddMonths(time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), -1, time.UTC); got.Format("2006-01-02") != "2026-02-28" {
		t.Fatal(got)
	}
}

func TestResetDue_MonthPlusDays(t *testing.T) {
	a := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	p, ok := ResetDue(a, now, time.UTC)
	if want := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC); !ok || !p.Equal(want) {
		t.Fatalf("ResetDue = %v, %v", p, ok)
	}
	n := NextReset(a, now, time.UTC)
	if want := time.Date(2026, 12, 17, 10, 0, 0, 0, time.UTC); !n.Equal(want) {
		t.Fatalf("NextReset = %v", n)
	}
}

func TestResetDue_FutureAnchor(t *testing.T) {
	a := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if _, ok := ResetDue(a, now, time.UTC); ok {
		t.Fatal("ok = true for future anchor")
	}
	if n := NextReset(a, now, time.UTC); !n.Equal(a) {
		t.Fatalf("NextReset = %v", n)
	}
}

func TestResetDue_ExactAndClamp(t *testing.T) {
	a := time.Date(2026, 1, 31, 8, 0, 0, 0, time.UTC)
	p, ok := ResetDue(a, a, time.UTC)
	if !ok || !p.Equal(a) {
		t.Fatalf("at anchor: %v, %v", p, ok)
	}
	p, ok = ResetDue(a, time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC), time.UTC)
	if want := time.Date(2026, 2, 28, 8, 0, 0, 0, time.UTC); !ok || !p.Equal(want) {
		t.Fatalf("clamp: %v, %v", p, ok)
	}
	n := NextReset(a, a, time.UTC)
	if want := time.Date(2026, 2, 28, 8, 0, 0, 0, time.UTC); !n.Equal(want) {
		t.Fatalf("next at anchor: %v", n)
	}
}

func TestBackfillAnchor(t *testing.T) {
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	got := BackfillAnchor(time.Date(2026, 12, 20, 9, 0, 0, 0, time.UTC), now, time.UTC)
	if want := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	got = BackfillAnchor(time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), now, time.UTC)
	if want := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBackfillAnchor_Expired(t *testing.T) {
	exp := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if got := BackfillAnchor(exp, now, time.UTC); !got.Equal(exp) {
		t.Fatalf("got %v, want %v", got, exp)
	}
}

func TestAddMonths_NegativeAcrossYear(t *testing.T) {
	got := AddMonths(time.Date(2026, 1, 31, 7, 0, 0, 0, time.UTC), -2, time.UTC)
	if want := time.Date(2025, 11, 30, 7, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAddMonths_KeepsClockAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	a := time.Date(2026, 3, 1, 10, 30, 0, 0, ny) // EST
	got := AddMonths(a, 1, ny)                   // EDT after Mar 8
	if want := time.Date(2026, 4, 1, 10, 30, 0, 0, ny); !got.Equal(want) || got.Hour() != 10 || got.Location() != ny {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPanelStrategy(t *testing.T) {
	soon := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	forever := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		in   string
		at   *time.Time
		want string
	}{
		{StrategySubscription, &soon, "NO_RESET"},
		{StrategySubscription, &forever, "MONTH_ROLLING"},
		{"MONTH", &soon, "MONTH"},
	} {
		if got := PanelStrategy(c.in, c.at); got != c.want {
			t.Errorf("PanelStrategy(%q, %v) = %q, want %q", c.in, c.at, got, c.want)
		}
	}
}

func TestEffectiveStrategy(t *testing.T) {
	parent := &ent.Tariff{TrafficStrategy: StrategySubscription}
	own := &ent.Tariff{TrafficStrategy: "MONTH_ROLLING"}
	if got := EffectiveStrategy(own, parent, true); got != StrategySubscription {
		t.Errorf("included = %q", got)
	}
	if got := EffectiveStrategy(own, parent, false); got != "MONTH_ROLLING" {
		t.Errorf("not included = %q", got)
	}
	if got := EffectiveStrategy(nil, parent, false); got != "" {
		t.Errorf("nil own = %q", got)
	}
}

func TestDiffers_UnsetFieldsIgnored(t *testing.T) {
	five := 5
	u := &ent.RwUser{TrafficLimitBytes: 200 << 30, TrafficLimitStrategy: "NO_RESET", HwidDeviceLimit: &five, SquadUuids: []string{"b", "a"}}
	p := PanelParams{TrafficLimitBytes: 200 << 30, Strategy: "NO_RESET", Squads: []string{"a", "b"}}
	if p.Differs(u) {
		t.Error("unset hwid and reordered squads must not differ")
	}
	p.TrafficLimitBytes = 0
	if !p.Differs(u) {
		t.Error("different limit must differ")
	}
}

func TestDiffers(t *testing.T) {
	five, six := 5, 6
	base := PanelParams{TrafficLimitBytes: 1 << 30, Strategy: "NO_RESET", HwidLimit: &five, Squads: []string{"a", "b"}}
	user := func() *ent.RwUser {
		return &ent.RwUser{TrafficLimitBytes: 1 << 30, TrafficLimitStrategy: "NO_RESET", HwidDeviceLimit: &five, SquadUuids: []string{"b", "a"}}
	}
	for _, c := range []struct {
		name string
		edit func(*ent.RwUser)
		want bool
	}{
		{"same", func(*ent.RwUser) {}, false},
		{"strategy mismatch", func(u *ent.RwUser) { u.TrafficLimitStrategy = "MONTH_ROLLING" }, true},
		{"set hwid vs nil", func(u *ent.RwUser) { u.HwidDeviceLimit = nil }, true},
		{"set hwid vs other", func(u *ent.RwUser) { u.HwidDeviceLimit = &six }, true},
		{"same-length squads, other members", func(u *ent.RwUser) { u.SquadUuids = []string{"a", "c"} }, true},
		{"fewer squads", func(u *ent.RwUser) { u.SquadUuids = []string{"a"} }, true},
	} {
		u := user()
		c.edit(u)
		if got := base.Differs(u); got != c.want {
			t.Errorf("%s: Differs = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestResetAnchors(t *testing.T) {
	anchor := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	own := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := &ent.Subscription{TrafficAnchorAt: &anchor, Edges: ent.SubscriptionEdges{
		Tariff: &ent.Tariff{TrafficStrategy: StrategySubscription, ManageRw: true},
		RwUser: &ent.RwUser{ExpireAt: &exp},
	}}
	if got := SubscriptionResetAnchor(sub); got == nil || !got.Equal(anchor) {
		t.Fatalf("sub anchor = %v", got)
	}
	sa := &ent.SubscriptionAddon{TrafficAnchorAt: &own, Included: true, Edges: ent.SubscriptionAddonEdges{
		Tariff: &ent.Tariff{TrafficStrategy: "NO_RESET", ManageRw: true},
		RwUser: &ent.RwUser{ExpireAt: &exp},
	}}
	if got := AddonResetAnchor(sa, sub); got == nil || !got.Equal(anchor) {
		t.Fatalf("included anchor = %v", got)
	}
	sa.Included = false // paid: own tariff's strategy (NO_RESET) -> inactive
	if got := AddonResetAnchor(sa, sub); got != nil {
		t.Fatalf("paid NO_RESET = %v", got)
	}
	sa.Edges.Tariff.TrafficStrategy = StrategySubscription
	if got := AddonResetAnchor(sa, sub); got == nil || !got.Equal(own) {
		t.Fatalf("paid anchor = %v", got)
	}
	forever := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	sub.Edges.RwUser.ExpireAt = &forever
	if SubscriptionResetAnchor(sub) != nil {
		t.Fatal("unlimited sub must be nil")
	}
}
