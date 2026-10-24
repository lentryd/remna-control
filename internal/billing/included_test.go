package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"remna-control/ent"
	"remna-control/ent/subscriptionaddon"
	"remna-control/ent/tariff"
	"remna-control/internal/remnawave"
	"remna-control/internal/settings"
	"remna-control/internal/store"
)

// fakePanel is an in-memory Remnawave users API.
type fakePanel struct {
	mu    sync.Mutex
	users map[int]*remnawave.User
	next  int
	// resets counts reset-traffic calls; failReset makes them answer 500.
	resets    int
	failReset bool
	// failResetFor makes reset-traffic fail for one user id; patches
	// counts PATCH calls; failPatch makes them answer 500.
	failResetFor int
	patches      int
	failPatch    bool
}

func (p *fakePanel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reply := func(u *remnawave.User) { _ = json.NewEncoder(w).Encode(map[string]any{"response": u}) }
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && path == "/api/users":
		var req remnawave.CreateUserRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		p.next++
		u := &remnawave.User{ID: p.next, Username: req.Username, Status: req.Status, ExpireAt: req.ExpireAt,
			TrafficLimitStrategy: req.TrafficLimitStrategy, HwidDeviceLimit: req.HwidDeviceLimit}
		if req.TrafficLimitBytes != nil {
			u.TrafficLimitBytes = *req.TrafficLimitBytes
		}
		for _, sq := range req.ActiveInternalSquads {
			u.ActiveInternalSquads = append(u.ActiveInternalSquads, remnawave.Squad{UUID: sq})
		}
		p.users[u.ID] = u
		reply(u)
	case r.Method == http.MethodPatch && path == "/api/users":
		var req remnawave.UpdateUserRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		p.patches++
		if p.failPatch {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			return
		}
		u := p.users[req.ID]
		if req.ExpireAt != nil {
			u.ExpireAt = *req.ExpireAt
		}
		if req.Status != "" {
			u.Status = req.Status
		}
		if req.TrafficLimitBytes != nil {
			u.TrafficLimitBytes = *req.TrafficLimitBytes
		}
		if req.TrafficLimitStrategy != "" {
			u.TrafficLimitStrategy = req.TrafficLimitStrategy
		}
		if req.HwidDeviceLimit != nil {
			u.HwidDeviceLimit = req.HwidDeviceLimit
		}
		if len(req.ActiveInternalSquads) > 0 {
			u.ActiveInternalSquads = nil
			for _, sq := range req.ActiveInternalSquads {
				u.ActiveInternalSquads = append(u.ActiveInternalSquads, remnawave.Squad{UUID: sq})
			}
		}
		reply(u)
	case strings.HasPrefix(path, "/api/users/by-username/"):
		name := strings.TrimPrefix(path, "/api/users/by-username/")
		for _, u := range p.users {
			if u.Username == name {
				reply(u)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"User not found"}`))
	case strings.HasSuffix(path, "/actions/enable"), strings.HasSuffix(path, "/actions/disable"):
		id, _ := strconv.Atoi(strings.Split(path, "/")[3])
		u := p.users[id]
		u.Status = map[bool]string{true: remnawave.StatusActive, false: remnawave.StatusDisabled}[strings.HasSuffix(path, "enable")]
		reply(u)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/actions/reset-traffic"):
		id, _ := strconv.Atoi(strings.Split(path, "/")[3])
		if p.failReset || p.failResetFor == id {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			return
		}
		u := p.users[id]
		u.UserTraffic.UsedTrafficBytes = 0
		p.resets++
		reply(u)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/users/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(path, "/api/users/"))
		reply(p.users[id])
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusTeapot)
	}
}

type fixture struct {
	ctx            context.Context
	db             *ent.Client
	svc            *Service
	panel          *fakePanel
	now            time.Time
	customer       int
	plain, premium int // base tariffs: without and with the "white" add-on
	white          int // add-on tariff
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db, _, err := store.OpenDB(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	panel := &fakePanel{users: map[int]*remnawave.User{}}
	srv := httptest.NewServer(panel)
	t.Cleanup(srv.Close)

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	svc := New(db, remnawave.NewClient(srv.URL, "tok", ""), settings.New(db))
	svc.now = func() time.Time { return now }

	a := db.Addon.Create().SetName("white").SetPrefix("white_").SaveX(ctx)
	white := db.Tariff.Create().SetKind(tariff.KindAddon).SetAddonID(a.ID).SetName("White").
		SetMonthlyPrice(10000).SetSquadUuids([]string{"sq-white"}).SaveX(ctx)
	plain := db.Tariff.Create().SetKind(tariff.KindBase).SetName("Plain").
		SetMonthlyPrice(20000).SetSquadUuids([]string{"sq-main"}).SaveX(ctx)
	premium := db.Tariff.Create().SetKind(tariff.KindBase).SetName("Premium").
		SetMonthlyPrice(25000).SetSquadUuids([]string{"sq-main"}).AddIncludedAddons(white).SaveX(ctx)
	c := db.Customer.Create().SetName("Alice").SaveX(ctx)
	return &fixture{ctx: ctx, db: db, svc: svc, panel: panel, now: now, customer: c.ID, plain: plain.ID, premium: premium.ID, white: white.ID}
}

func (f *fixture) provision(t *testing.T, tariffID int) *ent.Subscription {
	t.Helper()
	sub, err := f.svc.ProvisionSubscription(f.ctx, ProvisionInput{
		CustomerID: f.customer, TariffID: tariffID, Username: "alice", Months: 1, AllowDebt: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func (f *fixture) addons(t *testing.T, subID int) []*ent.SubscriptionAddon {
	t.Helper()
	return f.db.SubscriptionAddon.Query().Where(subscriptionaddon.SubscriptionID(subID)).WithRwUser().AllX(f.ctx)
}

func TestProvisionConnectsIncludedAddons(t *testing.T) {
	f := setup(t)
	sub := f.provision(t, f.premium)

	sas := f.addons(t, sub.ID)
	if len(sas) != 1 || !sas[0].Included || *sas[0].TariffID != f.white {
		t.Fatalf("add-ons = %+v", sas)
	}
	main := f.panel.users[*sub.RwUserID]
	white := f.panel.users[*sas[0].RwUserID]
	if white.Username != "white_alice" || !white.ExpireAt.Equal(main.ExpireAt) {
		t.Errorf("white user = %s until %s, main until %s", white.Username, white.ExpireAt, main.ExpireAt)
	}
	// Only the base subscription is charged.
	if bal, _ := f.svc.Balance(f.ctx, f.customer); bal != 0 {
		t.Errorf("balance = %d (provision charges the given amount only)", bal)
	}
	// Included add-ons aren't items to pay for, and can't be extended alone.
	items, err := f.svc.planItems(f.ctx, f.customer)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != KindSubscription {
		t.Errorf("plan items = %+v", items)
	}
	if _, err := f.svc.Extend(f.ctx, ExtendInput{Kind: KindAddon, ID: sas[0].ID, Months: 1, AllowDebt: true}); err == nil {
		t.Error("extending an included add-on must fail")
	}
}

func TestExtendMovesIncludedAddons(t *testing.T) {
	f := setup(t)
	sub := f.provision(t, f.premium)
	res, err := f.svc.Extend(f.ctx, ExtendInput{Kind: KindSubscription, ID: sub.ID, Months: 2, Amount: 50000, AllowDebt: true})
	if err != nil {
		t.Fatal(err)
	}
	sa := f.addons(t, sub.ID)[0]
	if got := f.panel.users[*sa.RwUserID].ExpireAt; !got.Equal(*res.To) {
		t.Errorf("included add-on until %s, subscription until %s", got, res.To)
	}

	if err := f.svc.SetEnabled(f.ctx, KindSubscription, sub.ID, false); err != nil {
		t.Fatal(err)
	}
	if st := f.panel.users[*sa.RwUserID].Status; st != remnawave.StatusDisabled {
		t.Errorf("included add-on status = %s after disabling the subscription", st)
	}
}

func TestTariffChangeIncludesPaidAddon(t *testing.T) {
	f := setup(t)
	sub := f.provision(t, f.plain)
	// A paid add-on with 15 days left of a 100.00/month tariff.
	until := f.now.AddDate(0, 0, 15)
	if _, err := f.svc.ConnectAddon(f.ctx, ConnectAddonInput{SubscriptionID: sub.ID, TariffID: f.white, Until: &until, Months: 0, Days: 15, AllowDebt: true}); err != nil {
		t.Fatal(err)
	}

	q, err := f.svc.QuoteTariffChange(f.ctx, KindSubscription, sub.ID, f.premium)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Addons) != 1 || q.Addons[0].Action != "include" || q.Addons[0].Credit != 5000 {
		t.Fatalf("addons = %+v", q.Addons)
	}
	if err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{Kind: KindSubscription, ID: sub.ID, TariffID: f.premium, Surcharge: q.Surcharge}); err != nil {
		t.Fatal(err)
	}
	sa := f.addons(t, sub.ID)[0]
	main := f.panel.users[*sub.RwUserID]
	if !sa.Included || !f.panel.users[*sa.RwUserID].ExpireAt.Equal(main.ExpireAt) {
		t.Errorf("add-on included=%v until %s, subscription until %s", sa.Included, f.panel.users[*sa.RwUserID].ExpireAt, main.ExpireAt)
	}
}

func TestTariffChangeDropsIncludedAddon(t *testing.T) {
	for _, how := range []string{RemoveDisable, RemoveKeepPaid} {
		t.Run(how, func(t *testing.T) {
			f := setup(t)
			sub := f.provision(t, f.premium)
			sa := f.addons(t, sub.ID)[0]

			q, err := f.svc.QuoteTariffChange(f.ctx, KindSubscription, sub.ID, f.plain)
			if err != nil {
				t.Fatal(err)
			}
			if len(q.Addons) != 1 || q.Addons[0].Action != "remove" {
				t.Fatalf("addons = %+v", q.Addons)
			}
			if err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{
				Kind: KindSubscription, ID: sub.ID, TariffID: f.plain, RemovedAddons: map[int]string{sa.ID: how},
			}); err != nil {
				t.Fatal(err)
			}
			sa = f.addons(t, sub.ID)[0]
			status := f.panel.users[*sa.RwUserID].Status
			if sa.Included {
				t.Error("still included")
			}
			if how == RemoveDisable && (status != remnawave.StatusDisabled || sa.AutoExtend) {
				t.Errorf("disable: status %s, auto-extend %v", status, sa.AutoExtend)
			}
			if how == RemoveKeepPaid && (status != remnawave.StatusActive || !sa.AutoExtend) {
				t.Errorf("keep paid: status %s, auto-extend %v", status, sa.AutoExtend)
			}
		})
	}
}

func TestSyncIncludedToSubscribers(t *testing.T) {
	f := setup(t)
	sub := f.provision(t, f.plain)
	// Plain now includes White: existing subscribers get it on sync.
	f.db.Tariff.UpdateOneID(f.plain).AddIncludedAddonIDs(f.white).ExecX(f.ctx)
	if n, err := f.svc.SyncIncluded(f.ctx, f.plain, RemoveDisable); err != nil || n != 1 {
		t.Fatalf("sync = %d, %v", n, err)
	}
	sas := f.addons(t, sub.ID)
	if len(sas) != 1 || !sas[0].Included {
		t.Fatalf("add-ons = %+v", sas)
	}
	// ...and lose it again, kept as a paid add-on.
	f.db.Tariff.UpdateOneID(f.plain).ClearIncludedAddons().ExecX(f.ctx)
	if _, err := f.svc.SyncIncluded(f.ctx, f.plain, RemoveKeepPaid); err != nil {
		t.Fatal(err)
	}
	if sa := f.addons(t, sub.ID)[0]; sa.Included || f.panel.users[*sa.RwUserID].Status != remnawave.StatusActive {
		t.Errorf("after removal: included=%v status=%s", sa.Included, f.panel.users[*sa.RwUserID].Status)
	}
}

func TestProvisionRetryIsIdempotent(t *testing.T) {
	f := setup(t)
	first := f.provision(t, f.plain)
	// The caller timed out and asks again: same subscription, no second user.
	again := f.provision(t, f.plain)
	if again.ID != first.ID || len(f.panel.users) != 1 {
		t.Fatalf("retry made sub %d (was %d), %d panel users", again.ID, first.ID, len(f.panel.users))
	}
}

func TestProvisionFinishesHalfDone(t *testing.T) {
	f := setup(t)
	// The panel user exists but the first attempt died before linking it.
	f.panel.users[1] = &remnawave.User{ID: 1, Username: "alice", Status: remnawave.StatusActive, ExpireAt: f.now.AddDate(0, 1, 0)}
	f.panel.next = 1
	sub := f.provision(t, f.plain)
	if sub.RwUserID == nil || *sub.RwUserID != 1 || len(f.panel.users) != 1 {
		t.Fatalf("sub = %+v, %d panel users", sub, len(f.panel.users))
	}
}

func TestProvisionRefusesAnotherCustomersUser(t *testing.T) {
	f := setup(t)
	f.provision(t, f.plain)
	other := f.db.Customer.Create().SetName("Bob").SaveX(f.ctx)
	_, err := f.svc.ProvisionSubscription(f.ctx, ProvisionInput{
		CustomerID: other.ID, TariffID: f.plain, Username: "alice", Months: 1, AllowDebt: true,
	})
	if err == nil {
		t.Fatal("another customer's panel user must not be adopted")
	}
}

func (f *fixture) panelUser(t *testing.T, username string) *remnawave.User {
	t.Helper()
	f.panel.mu.Lock()
	defer f.panel.mu.Unlock()
	for _, u := range f.panel.users {
		if u.Username == username {
			return u
		}
	}
	t.Fatalf("no panel user %q", username)
	return nil
}

func TestProvision_SubscriptionStrategySendsNoReset(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.plain).SetTrafficStrategy(StrategySubscription).SetTrafficLimitBytes(200 << 30).ExecX(f.ctx)
	f.provision(t, f.plain)

	u := f.panelUser(t, "alice")
	if u.TrafficLimitStrategy != "NO_RESET" || u.TrafficLimitBytes != 200<<30 {
		t.Fatalf("strategy %q limit %d", u.TrafficLimitStrategy, u.TrafficLimitBytes)
	}
}

func TestIncludedAddon_InheritsParentStrategy(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.premium).SetTrafficStrategy(StrategySubscription).ExecX(f.ctx)
	f.db.Tariff.UpdateOneID(f.white).SetTrafficStrategy("MONTH_ROLLING").SetTrafficLimitBytes(50 << 30).ExecX(f.ctx)
	f.provision(t, f.premium)

	u := f.panelUser(t, "white_alice")
	if u.TrafficLimitStrategy != "NO_RESET" || u.TrafficLimitBytes != 50<<30 {
		t.Fatalf("strategy %q limit %d", u.TrafficLimitStrategy, u.TrafficLimitBytes)
	}
}

func TestQuoteTariffChange_Exchange(t *testing.T) {
	f := setup(t)
	sub := f.provision(t, f.plain) // 200.00/month
	double := f.db.Tariff.Create().SetKind(tariff.KindBase).SetName("Double").
		SetMonthlyPrice(40000).SetSquadUuids([]string{"sq-main"}).SaveX(f.ctx)
	expire := f.now.AddDate(0, 0, 60)
	f.panel.users[*sub.RwUserID].ExpireAt = expire
	f.db.RwUser.UpdateOneID(*sub.RwUserID).SetExpireAt(expire).ExecX(f.ctx)

	q, err := f.svc.QuoteTariffChange(f.ctx, KindSubscription, sub.ID, double.ID)
	if err != nil {
		t.Fatal(err)
	}
	if q.ExchangeDays != 30 || !sameTime(q.ExchangeExpireAt, f.now.AddDate(0, 0, 30)) {
		t.Errorf("exchange = %d until %v", q.ExchangeDays, q.ExchangeExpireAt)
	}

	// The same tariff: nothing to exchange.
	if q, err = f.svc.QuoteTariffChange(f.ctx, KindSubscription, sub.ID, f.plain); err != nil {
		t.Fatal(err)
	} else if q.ExchangeDays != 0 || q.ExchangeExpireAt != nil {
		t.Errorf("same tariff: %d until %v", q.ExchangeDays, q.ExchangeExpireAt)
	}

	// Unlimited: nothing to exchange.
	forever := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	f.db.RwUser.UpdateOneID(*sub.RwUserID).SetExpireAt(forever).ExecX(f.ctx)
	if q, err = f.svc.QuoteTariffChange(f.ctx, KindSubscription, sub.ID, double.ID); err != nil {
		t.Fatal(err)
	} else if q.ExchangeDays != 0 || q.ExchangeExpireAt != nil {
		t.Errorf("unlimited: %d until %v", q.ExchangeDays, q.ExchangeExpireAt)
	}
}
