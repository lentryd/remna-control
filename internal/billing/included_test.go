package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"remna-control/ent"
	"remna-control/ent/extension"
	"remna-control/ent/subscriptionaddon"
	"remna-control/ent/tariff"
	"remna-control/internal/apperr"
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

func subTariff(f *fixture, subID int) int {
	if id := f.db.Subscription.GetX(f.ctx, subID).TariffID; id != nil {
		return *id
	}
	return 0
}

func hasCode(err error, code string) bool {
	var ae *apperr.Error
	return errors.As(err, &ae) && ae.Code == code
}

// exchangeFixture is a Plain subscription (200.00/month) with 60 days left
// and a tariff twice as expensive, so the exchange gives 30 days.
func exchangeFixture(t *testing.T) (*fixture, *ent.Subscription, *ent.Tariff) {
	t.Helper()
	f := setup(t)
	sub := f.provision(t, f.plain)
	double := f.db.Tariff.Create().SetKind(tariff.KindBase).SetName("Double").
		SetMonthlyPrice(40000).SetSquadUuids([]string{"sq-main"}).SaveX(f.ctx)
	expire := f.now.AddDate(0, 0, 60)
	f.panel.users[*sub.RwUserID].ExpireAt = expire
	f.db.RwUser.UpdateOneID(*sub.RwUserID).SetExpireAt(expire).ExecX(f.ctx)
	return f, sub, double
}

func TestChangeTariff_Exchange(t *testing.T) {
	f, sub, double := exchangeFixture(t)
	ledger := f.db.LedgerEntry.Query().CountX(f.ctx)

	// A surcharge sent along is ignored: the exchange charges nothing.
	err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{
		Kind: KindSubscription, ID: sub.ID, TariffID: double.ID, Mode: ModeExchange, ExpectedDays: 30, Surcharge: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.panel.users[*sub.RwUserID].ExpireAt; !got.Equal(f.now.AddDate(0, 0, 30)) {
		t.Errorf("panel expire_at = %s", got)
	}
	if got := subTariff(f, sub.ID); got != double.ID {
		t.Errorf("tariff = %d", got)
	}
	if n := f.db.LedgerEntry.Query().CountX(f.ctx); n != ledger {
		t.Errorf("%d ledger entries added", n-ledger)
	}
	ext := f.db.Extension.Query().Where(extension.KindEQ("tariff_change")).OnlyX(f.ctx)
	if ext.Amount != 0 || ext.Status != "ok" {
		t.Errorf("extension = %+v", ext)
	}
}

func TestChangeTariff_ExchangeStale(t *testing.T) {
	f, sub, double := exchangeFixture(t)
	// 56 days left now gives 28 days, against 30 quoted.
	expire := f.now.AddDate(0, 0, 56)
	f.panel.users[*sub.RwUserID].ExpireAt = expire
	f.db.RwUser.UpdateOneID(*sub.RwUserID).SetExpireAt(expire).ExecX(f.ctx)

	err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{
		Kind: KindSubscription, ID: sub.ID, TariffID: double.ID, Mode: ModeExchange, ExpectedDays: 30,
	})
	if !hasCode(err, "billing.exchange_stale") {
		t.Fatalf("err = %v", err)
	}
	if got := subTariff(f, sub.ID); got != f.plain {
		t.Errorf("tariff changed to %d", got)
	}
	if got := f.panel.users[*sub.RwUserID].ExpireAt; !got.Equal(expire) {
		t.Errorf("panel expire_at = %s", got)
	}
	if n := f.db.Extension.Query().Where(extension.KindEQ("tariff_change")).CountX(f.ctx); n != 0 {
		t.Errorf("%d extensions", n)
	}

	// One day short is tolerated: 29 days against 30 quoted.
	expire = f.now.AddDate(0, 0, 58)
	f.panel.users[*sub.RwUserID].ExpireAt = expire
	f.db.RwUser.UpdateOneID(*sub.RwUserID).SetExpireAt(expire).ExecX(f.ctx)
	if err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{
		Kind: KindSubscription, ID: sub.ID, TariffID: double.ID, Mode: ModeExchange, ExpectedDays: 30,
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.panel.users[*sub.RwUserID].ExpireAt; !got.Equal(f.now.AddDate(0, 0, 29)) {
		t.Errorf("panel expire_at = %s", got)
	}
}

func TestChangeTariff_ExchangeUnavailable(t *testing.T) {
	f, sub, double := exchangeFixture(t)
	forever := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	f.db.RwUser.UpdateOneID(*sub.RwUserID).SetExpireAt(forever).ExecX(f.ctx)

	err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{
		Kind: KindSubscription, ID: sub.ID, TariffID: double.ID, Mode: ModeExchange, ExpectedDays: 30,
	})
	if !hasCode(err, "billing.exchange_unavailable") {
		t.Fatalf("err = %v", err)
	}
	if got := subTariff(f, sub.ID); got != f.plain {
		t.Errorf("tariff changed to %d", got)
	}
}

func TestChangeTariff_DefaultModeUnchanged(t *testing.T) {
	f, sub, double := exchangeFixture(t)
	expire := f.panel.users[*sub.RwUserID].ExpireAt
	if err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{Kind: KindSubscription, ID: sub.ID, TariffID: double.ID, Surcharge: 5000}); err != nil {
		t.Fatal(err)
	}
	if got := f.panel.users[*sub.RwUserID].ExpireAt; !got.Equal(expire) {
		t.Errorf("expire_at moved to %s", got)
	}
	ext := f.db.Extension.Query().Where(extension.KindEQ("tariff_change")).OnlyX(f.ctx)
	if ext.Amount != 5000 {
		t.Errorf("extension amount = %d", ext.Amount)
	}
}

func TestChangeTariff_ExchangePanelFailureChangesNothing(t *testing.T) {
	f, sub, double := exchangeFixture(t)
	expire := f.panel.users[*sub.RwUserID].ExpireAt
	ledger := f.db.LedgerEntry.Query().CountX(f.ctx)
	in := ChangeTariffInput{Kind: KindSubscription, ID: sub.ID, TariffID: double.ID, Mode: ModeExchange, ExpectedDays: 30}

	f.panel.failPatch = true
	if err := f.svc.ChangeTariff(f.ctx, in); !hasCode(err, "billing.tariff_rw_failed") {
		t.Fatalf("err = %v", err)
	}
	if got := subTariff(f, sub.ID); got != f.plain {
		t.Errorf("tariff changed to %d", got)
	}
	if got := f.db.RwUser.GetX(f.ctx, *sub.RwUserID).ExpireAt; got == nil || !got.Equal(expire) {
		t.Errorf("local expire_at = %v", got)
	}
	if n := f.db.LedgerEntry.Query().CountX(f.ctx); n != ledger {
		t.Errorf("%d ledger entries added", n-ledger)
	}

	f.panel.failPatch = false
	if err := f.svc.ChangeTariff(f.ctx, in); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := subTariff(f, sub.ID); got != double.ID {
		t.Errorf("tariff = %d after retry", got)
	}
	if got := f.panel.users[*sub.RwUserID].ExpireAt; !got.Equal(f.now.AddDate(0, 0, 30)) {
		t.Errorf("panel expire_at = %s", got)
	}
}

// Included add-ons live and end with the subscription: after a term
// exchange they follow its new expiry, whether the term grows or shrinks.
func TestChangeTariff_ExchangeMovesIncludedAddons(t *testing.T) {
	for _, c := range []struct {
		name  string
		price int64
		days  int
	}{
		{"downgrade", 12500, 120}, // 250.00 -> 125.00: 60 days become 120
		{"upgrade", 50000, 30},    // 250.00 -> 500.00: 60 days become 30
	} {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			sub := f.provision(t, f.premium)
			sa := f.addons(t, sub.ID)[0]
			expire := f.now.AddDate(0, 0, 60)
			f.until(t, *sub.RwUserID, expire)
			f.until(t, *sa.RwUserID, expire)
			next := f.db.Tariff.Create().SetKind(tariff.KindBase).SetName("Next").
				SetMonthlyPrice(c.price).SetSquadUuids([]string{"sq-main"}).AddIncludedAddonIDs(f.white).SaveX(f.ctx)

			if err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{
				Kind: KindSubscription, ID: sub.ID, TariffID: next.ID, Mode: ModeExchange, ExpectedDays: c.days,
			}); err != nil {
				t.Fatal(err)
			}
			want := f.now.AddDate(0, 0, c.days)
			if got := f.panel.users[*sub.RwUserID].ExpireAt; !got.Equal(want) {
				t.Errorf("subscription until %s, want %s", got, want)
			}
			if got := f.panel.users[*sa.RwUserID].ExpireAt; !got.Equal(want) {
				t.Errorf("included add-on until %s, want %s", got, want)
			}
			if got := f.db.RwUser.GetX(f.ctx, *sa.RwUserID).ExpireAt; got == nil || !got.Equal(want) {
				t.Errorf("cached add-on expiry = %v", got)
			}
			if sa = f.addons(t, sub.ID)[0]; !sa.Included {
				t.Error("add-on is no longer included")
			}
		})
	}
}

// A paid add-on that the new tariff includes keeps the later of its own
// expiry and the exchanged subscription expiry: the exchange credits nothing
// for its paid time.
func TestChangeTariff_ExchangeIncludesPaidAddonKeepsLonger(t *testing.T) {
	for _, c := range []struct {
		name     string
		own      int // days the paid add-on has
		wantDays int
	}{
		{"own is longer", 90, 90},
		{"own is shorter", 15, 48}, // 200.00 -> 250.00: 60 days become 48
	} {
		t.Run(c.name, func(t *testing.T) {
			f, sub, _ := exchangeFixture(t)
			until := f.now.AddDate(0, 0, 15)
			sa, err := f.svc.ConnectAddon(f.ctx, ConnectAddonInput{SubscriptionID: sub.ID, TariffID: f.white, Until: &until, Days: 15, AllowDebt: true})
			if err != nil {
				t.Fatal(err)
			}
			f.until(t, *sa.RwUserID, f.now.AddDate(0, 0, c.own))

			if err := f.svc.ChangeTariff(f.ctx, ChangeTariffInput{
				Kind: KindSubscription, ID: sub.ID, TariffID: f.premium, Mode: ModeExchange, ExpectedDays: 48,
			}); err != nil {
				t.Fatal(err)
			}
			if got := f.panel.users[*sub.RwUserID].ExpireAt; !got.Equal(f.now.AddDate(0, 0, 48)) {
				t.Errorf("subscription until %s", got)
			}
			if got := f.panel.users[*sa.RwUserID].ExpireAt; !got.Equal(f.now.AddDate(0, 0, c.wantDays)) {
				t.Errorf("add-on until %s, want %d days", got, c.wantDays)
			}
			if got := f.addons(t, sub.ID)[0]; !got.Included {
				t.Error("add-on not included")
			}
		})
	}
}
