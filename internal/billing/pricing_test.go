package billing

import (
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func TestCost(t *testing.T) {
	periods := []Period{{Months: 3, Price: 25000}, {Months: 12, Price: 90000}}
	cases := []struct {
		months int
		want   int64
	}{
		{0, 0}, {1, 10000}, {2, 20000}, {3, 25000}, {4, 35000}, {6, 50000}, {12, 90000}, {13, 100000},
	}
	for _, c := range cases {
		if got := Cost(10000, periods, c.months); got != c.want {
			t.Errorf("Cost(%d) = %d, want %d", c.months, got, c.want)
		}
	}
}

func TestTermCost(t *testing.T) {
	periods := []Period{{Months: 3, Price: 25000}, {Days: 7, Price: 0}, {Months: 1, Days: 15, Price: 12000}}
	cases := []struct {
		months, days int
		want         int64
	}{
		{0, 7, 0}, {1, 15, 12000}, {0, 6, 2000}, {3, 0, 25000}, {3, 7, 25000 + 2333}, {1, 0, 10000},
	}
	for _, c := range cases {
		if got := TermCost(10000, periods, c.months, c.days); got != c.want {
			t.Errorf("TermCost(%d, %d) = %d, want %d", c.months, c.days, got, c.want)
		}
	}
}

func TestUnlimited(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	forever := time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
	soon := now.AddDate(1, 0, 0)
	if !Unlimited(&forever) || Unlimited(&soon) || Unlimited(nil) {
		t.Error("Unlimited: 2099 is forever, next year and nil are not")
	}
	if got := ProrateSurcharge(10000, 20000, now, &forever); got != 0 {
		t.Errorf("surcharge on an unlimited user = %d, want 0", got)
	}
}

func TestExtendFrom(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	future := now.AddDate(0, 0, 5)
	past := now.AddDate(0, 0, -5)
	if got := ExtendFrom(now, &future); !got.Equal(future) {
		t.Errorf("future: got %v", got)
	}
	if got := ExtendFrom(now, &past); !got.Equal(now) {
		t.Errorf("past: got %v", got)
	}
	if got := ExtendFrom(now, nil); !got.Equal(now) {
		t.Errorf("nil: got %v", got)
	}
}

func TestPlanSingle(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	exp := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	items := []PlanItem{{Kind: "subscription", ID: 1, Monthly: 20000, ExpireAt: &exp}}

	alloc, rest := Plan(60000, items, now, false)
	if len(alloc) != 1 || alloc[0].Months != 3 || alloc[0].Amount != 60000 || rest != 0 {
		t.Fatalf("got %+v rest %d", alloc, rest)
	}
	if want := time.Date(2027, 1, 10, 0, 0, 0, 0, time.UTC); !alloc[0].To.Equal(want) {
		t.Errorf("to = %v, want %v", alloc[0].To, want)
	}

	alloc, rest = Plan(25000, items, now, false)
	if alloc[0].Months != 1 || rest != 5000 {
		t.Fatalf("remainder: got %+v rest %d", alloc, rest)
	}

	alloc, rest = Plan(25000, items, now, true)
	if alloc[0].Months != 1 || alloc[0].Days != 7 || rest != 333 {
		t.Fatalf("remainder to days: got %+v rest %d", alloc, rest)
	}
}

func TestPlanRoundRobinEarliestFirst(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	late := now.AddDate(0, 0, 20)
	early := now.AddDate(0, 0, 2)
	items := []PlanItem{
		{Kind: "subscription", ID: 1, Monthly: 10000, ExpireAt: &late},
		{Kind: "addon", ID: 2, Monthly: 10000, ExpireAt: &early},
		{Kind: "subscription", ID: 3, Monthly: 0}, // free: never charged
	}
	alloc, rest := Plan(30000, items, now, false)
	if len(alloc) != 2 || rest != 0 {
		t.Fatalf("got %+v rest %d", alloc, rest)
	}
	if alloc[0].ID != 2 || alloc[0].Months != 2 || alloc[1].ID != 1 || alloc[1].Months != 1 {
		t.Errorf("unexpected order/months: %+v", alloc)
	}
}

func TestPlanUsesPeriodDiscount(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	items := []PlanItem{{Kind: "subscription", ID: 1, Monthly: 10000, Periods: []Period{{Months: 3, Price: 25000}}}}
	alloc, rest := Plan(25000, items, now, false)
	if alloc[0].Months != 3 || alloc[0].Amount != 25000 || rest != 0 {
		t.Fatalf("got %+v rest %d", alloc, rest)
	}
}

func TestPlanMonthEnd(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	exp := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	alloc, _ := Plan(10000, []PlanItem{{ID: 1, Monthly: 10000, ExpireAt: ptr(exp)}}, now, false)
	// Go normalises Feb 31 to Mar 3 — same as the panel would show.
	if want := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC); !alloc[0].To.Equal(want) {
		t.Errorf("to = %v, want %v", alloc[0].To, want)
	}
}

func TestProrateSurcharge(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	exp := now.AddDate(0, 0, 15)
	if got := ProrateSurcharge(10000, 20000, now, &exp); got != 5000 {
		t.Errorf("upgrade = %d", got)
	}
	if got := ProrateSurcharge(20000, 10000, now, &exp); got != -5000 {
		t.Errorf("downgrade = %d", got)
	}
	past := now.AddDate(0, 0, -1)
	if got := ProrateSurcharge(10000, 20000, now, &past); got != 0 {
		t.Errorf("expired = %d", got)
	}
}

func TestReferralAmount(t *testing.T) {
	if got := ReferralAmount(60000, 10); got != 6000 {
		t.Errorf("got %d", got)
	}
	if got := ReferralAmount(16172, 7.5); got != 1213 {
		t.Errorf("got %d", got)
	}
}

func TestPlanAddonFollowsSubscription(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	subExp := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	addonExp := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	items := []PlanItem{
		{Kind: "subscription", ID: 1, Monthly: 20000, ExpireAt: &subExp},
		{Kind: "addon", ID: 9, Monthly: 5000, ExpireAt: &addonExp, ParentID: 1, Periods: []Period{{Months: 3, Price: 12000}}},
	}
	alloc, rest := Plan(60000, items, now, false)
	got := map[string]int{}
	for _, a := range alloc {
		got[a.Kind] = a.Months
	}
	// 2×(200+50) = 500; the add-on can't run past the subscription, so the
	// rest stays on the balance instead of buying add-on months.
	if got["subscription"] != 2 || got["addon"] != 2 || rest != 10000 {
		t.Fatalf("got %+v rest %d", alloc, rest)
	}
}

func TestPlanAddonOfFreeSubscription(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	parentExp := now.AddDate(1, 0, 0)
	items := []PlanItem{{Kind: "addon", ID: 9, Monthly: 5000, ParentID: 1, ParentExpireAt: &parentExp}}
	alloc, _ := Plan(15000, items, now, false)
	if len(alloc) != 1 || alloc[0].Months != 3 {
		t.Fatalf("got %+v", alloc)
	}
}

func TestExchangeDays(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	in := func(days int) *time.Time { return ptr(now.AddDate(0, 0, days)) }
	forever := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		old, new int64
		exp      *time.Time
		want     int
	}{
		{"upgrade", 100, 200, in(60), 30},
		{"downgrade", 100, 50, in(60), 120},
		{"expired", 100, 200, in(-1), 0},
		{"no expiry", 100, 200, nil, 0},
		{"unlimited", 100, 200, &forever, 0},
		{"free target", 100, 0, in(60), 0},
		{"under a day", 100, 100000, in(1), 0},
		{"capped", 100, 1, in(300), 730},
	}
	for _, c := range cases {
		if got := ExchangeDays(c.old, c.new, now, c.exp); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
