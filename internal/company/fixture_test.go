package company

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"remna-control/ent"
	"remna-control/ent/tariff"
	"remna-control/internal/remnawave"
	"remna-control/internal/store"
)

// fakePanel is an in-memory Remnawave users API.
type fakePanel struct {
	mu          sync.Mutex
	users       map[int]*remnawave.User
	next        int
	creates     int
	failDisable bool
	failPatch   bool
	failCreate  bool
}

func (p *fakePanel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reply := func(u *remnawave.User) { _ = json.NewEncoder(w).Encode(map[string]any{"response": u}) }
	fail := func() { http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError) }
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && path == "/api/users":
		if p.failCreate {
			fail()
			return
		}
		var req remnawave.CreateUserRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		p.next++
		p.creates++
		u := &remnawave.User{ID: p.next, Username: req.Username, Status: req.Status, ExpireAt: req.ExpireAt,
			TrafficLimitStrategy: req.TrafficLimitStrategy, HwidDeviceLimit: req.HwidDeviceLimit,
			ExternalSquadUUID: req.ExternalSquadUUID}
		if req.TrafficLimitBytes != nil {
			u.TrafficLimitBytes = *req.TrafficLimitBytes
		}
		for _, sq := range req.ActiveInternalSquads {
			u.ActiveInternalSquads = append(u.ActiveInternalSquads, remnawave.Squad{UUID: sq})
		}
		if req.Description != "" {
			d := req.Description
			u.Description = &d
		}
		p.users[u.ID] = u
		reply(u)
	case r.Method == http.MethodPatch && path == "/api/users":
		if p.failPatch {
			fail()
			return
		}
		var req remnawave.UpdateUserRequest
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		var keys map[string]json.RawMessage
		_ = json.Unmarshal(raw, &keys)
		u := p.users[req.ID]
		if v, ok := keys["externalSquadUuid"]; ok {
			if string(v) == "null" {
				u.ExternalSquadUUID = nil
			} else {
				u.ExternalSquadUUID = req.ExternalSquadUUID
			}
		}
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
		enable := strings.HasSuffix(path, "enable")
		if !enable && p.failDisable {
			fail()
			return
		}
		id, _ := strconv.Atoi(strings.Split(path, "/")[3])
		u := p.users[id]
		u.Status = map[bool]string{true: remnawave.StatusActive, false: remnawave.StatusDisabled}[enable]
		reply(u)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/users/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(path, "/api/users/"))
		if u, ok := p.users[id]; ok {
			reply(u)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusTeapot)
	}
}

func (p *fakePanel) byName(name string) *remnawave.User {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range p.users {
		if u.Username == name {
			return u
		}
	}
	return nil
}

type fixture struct {
	ctx     context.Context
	db      *ent.Client
	svc     *Service
	panel   *fakePanel
	loc     *time.Location
	clock   time.Time // moved by tests through svc.now
	tariff  *ent.Tariff
	tariff2 *ent.Tariff
	company *ent.Company
}

func (f *fixture) at(y int, m time.Month, d int) { f.clock = time.Date(y, m, d, 12, 0, 0, 0, f.loc) }

func setup(t *testing.T) *fixture { return setupIn(t, time.UTC) }

func setupIn(t *testing.T, loc *time.Location) *fixture {
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

	f := &fixture{ctx: ctx, db: db, panel: panel, loc: loc}
	f.at(2026, 11, 1)
	f.svc = New(db, remnawave.NewClient(srv.URL, "tok", ""), f.loc)
	f.svc.now = func() time.Time { return f.clock }

	f.tariff = db.Tariff.Create().SetKind(tariff.KindBase).SetName("Base").SetMonthlyPrice(50000).
		SetTrafficLimitBytes(100).SetTrafficStrategy("MONTH").SetHwidLimit(3).SetSquadUuids([]string{"sq-main"}).SaveX(ctx)
	f.tariff2 = db.Tariff.Create().SetKind(tariff.KindBase).SetName("Pro").SetMonthlyPrice(80000).
		SetTrafficLimitBytes(500).SetTrafficStrategy("MONTH").SetSquadUuids([]string{"sq-pro"}).SaveX(ctx)
	f.company = f.newCompany("Acme", f.tariff.ID, 1)
	return f
}

// newCompany makes a company the way CreateCompany will: with an account
// customer and the cycle containing the clock.
func (f *fixture) newCompany(name string, tariffID, reportDay int) *ent.Company {
	c := f.db.Customer.Create().SetName(name).SaveX(f.ctx)
	p := PeriodContaining(reportDay, f.clock, f.loc)
	return f.db.Company.Create().SetName(name).SetCustomerID(c.ID).SetTariffID(tariffID).
		SetReportDay(reportDay).SetPeriodStart(p.From).SetPeriodEnd(p.To).SetPeriodDays(p.Days()).SaveX(f.ctx)
}
