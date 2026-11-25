package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	appmiddleware "remna-control/internal/api/middleware"
	"remna-control/internal/company"
	"remna-control/internal/remnawave"
)

// tinyPanel answers the few panel calls adding an employee makes.
type tinyPanel struct {
	mu      sync.Mutex
	users   map[string]*remnawave.User
	creates int
}

func (p *tinyPanel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reply := func(u *remnawave.User) { _ = json.NewEncoder(w).Encode(map[string]any{"response": u}) }
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/users":
		var req remnawave.CreateUserRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		p.creates++
		u := &remnawave.User{ID: p.creates, Username: req.Username, Status: req.Status, ExpireAt: req.ExpireAt}
		p.users[req.Username] = u
		reply(u)
	case strings.HasPrefix(r.URL.Path, "/api/users/by-username/"):
		if u, ok := p.users[strings.TrimPrefix(r.URL.Path, "/api/users/by-username/")]; ok {
			reply(u)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"User not found"}`))
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusTeapot)
	}
}

type companyEnv struct {
	db      *ent.Client
	h       *Handlers
	app     *fiber.App
	panel   *tinyPanel
	company int
	account int
	tariff  int
}

// newCompanyEnv builds the company routes behind the real token middleware.
func newCompanyEnv(t *testing.T) *companyEnv {
	t.Helper()
	db := openDB(t)
	cid, coid, tid := companyAccount(t, db)
	db.Tariff.UpdateOneID(tid).SetSquadUuids([]string{"sq"}).ExecX(t.Context())
	panel := &tinyPanel{users: map[string]*remnawave.User{}}
	srv := httptest.NewServer(panel)
	t.Cleanup(srv.Close)
	rw := remnawave.NewClient(srv.URL, "t", "")
	h := companyHandlers(db)
	h.Company = company.New(db, rw, time.UTC)

	app := testApp()
	tok := func(key string) fiber.Handler { return appmiddleware.RequireToken(db, key) }
	app.Get("/company/members", tok("company:members"), h.ListCompanyMembers)
	app.Get("/company/members/:mid", tok("company:member_get"), h.GetCompanyMember)
	app.Post("/company/members", tok("company:member_add"), h.AddCompanyMember)
	app.Put("/company/members/:mid", tok("company:member_update"), h.UpdateCompanyMember)
	app.Delete("/company/members/:mid", tok("company:member_remove"), h.RemoveCompanyMember)
	app.Get("/company/balance", tok("company:balance"), h.CompanyBalance)
	app.Get("/company/statements/:sid", tok("company:statement_get"), h.GetCompanyStatement)
	app.Put("/companies/:id/members/:mid/override", tok("companies:member_override"), h.SetMemberOverrides)
	return &companyEnv{db: db, h: h, app: app, panel: panel, company: coid, account: cid, tariff: tid}
}

func (e *companyEnv) token(t *testing.T, raw string, companyID *int, scopes ...string) {
	t.Helper()
	e.db.APIToken.Create().SetName(raw).SetTokenHash(appmiddleware.HashToken(raw)).SetPrefix(raw[:4]).SetScopes(scopes).
		SetExpireAt(time.Now().Add(time.Hour)).SetNillableCompanyID(companyID).SaveX(t.Context())
}

func (e *companyEnv) call(t *testing.T, token, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// member seeds an employee straight into the database (no panel user).
func (e *companyEnv) member(t *testing.T, companyID int, username string, protected bool) *ent.CompanyMember {
	t.Helper()
	m := e.db.CompanyMember.Create().SetCompanyID(companyID).SetUsername(username).SetProtected(protected).
		SetAddedAt(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)).SaveX(t.Context())
	e.db.MemberSegment.Create().SetMemberID(m.ID).SetFromDate(m.AddedAt).SetTariffID(e.tariff).SetPrice(50000).
		SetTrafficLimitBytes(0).SetTrafficStrategy("NO_RESET").SaveX(t.Context())
	return m
}

func (e *companyEnv) otherCompany(t *testing.T) int {
	t.Helper()
	acc := e.db.Customer.Create().SetName("Other").SaveX(t.Context())
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	return e.db.Company.Create().SetName("Other").SetCustomerID(acc.ID).SetTariffID(e.tariff).SetReportDay(1).
		SetPeriodStart(start).SetPeriodEnd(start.AddDate(0, 1, 0)).SetPeriodDays(30).SaveX(t.Context()).ID
}

func TestBoundTokenCannotSeeForeignMember(t *testing.T) {
	e := newCompanyEnv(t)
	other := e.otherCompany(t)
	foreign := e.member(t, other, "foreign", false)
	e.token(t, "vpc_acme1", &e.company, "company:*")

	for _, c := range []struct{ method, path string }{
		{"GET", "/company/members/" + itoa(foreign.ID)},
		{"DELETE", "/company/members/" + itoa(foreign.ID)},
		{"PUT", "/company/members/" + itoa(foreign.ID)},
	} {
		code, body := e.call(t, "vpc_acme1", c.method, c.path, `{"label":"x"}`)
		if code != http.StatusNotFound {
			t.Errorf("%s %s = %d %v, want 404", c.method, c.path, code, body)
		}
	}
	if e.db.CompanyMember.GetX(t.Context(), foreign.ID).RemovedAt != nil {
		t.Error("foreign member was removed")
	}
}

func TestMemberUpdateAcceptsOnlyLabel(t *testing.T) {
	e := newCompanyEnv(t)
	m := e.member(t, e.company, "ivanov", false)
	e.token(t, "vpc_acme2", &e.company, "company:*")
	if code, _ := e.call(t, "vpc_acme2", "PUT", "/company/members/"+itoa(m.ID), `{"label":"Ivan","price":1}`); code != http.StatusBadRequest {
		t.Errorf("label with a price = %d, want 400", code)
	}
	if code, _ := e.call(t, "vpc_acme2", "PUT", "/company/members/"+itoa(m.ID), `{"label":"Ivan"}`); code != http.StatusNoContent {
		t.Errorf("label only = %d, want 204", code)
	}
	if got := e.db.CompanyMember.GetX(t.Context(), m.ID).Label; got != "Ivan" {
		t.Errorf("label = %q", got)
	}
}

func TestProtectedMemberDeleteIs409(t *testing.T) {
	e := newCompanyEnv(t)
	m := e.member(t, e.company, "boss", true)
	e.token(t, "vpc_acme3", &e.company, "company:*")
	code, body := e.call(t, "vpc_acme3", "DELETE", "/company/members/"+itoa(m.ID), "")
	if code != http.StatusConflict || body["code"] != "company.member_protected" {
		t.Errorf("= %d %v", code, body)
	}
}

func TestAddMemberTwiceReturns200Existing(t *testing.T) {
	e := newCompanyEnv(t)
	e.token(t, "vpc_acme4", &e.company, "company:*")
	code, first := e.call(t, "vpc_acme4", "POST", "/company/members", `{"username":"ivanov","label":"Ivan"}`)
	if code != http.StatusCreated {
		t.Fatalf("first add = %d %v", code, first)
	}
	code, second := e.call(t, "vpc_acme4", "POST", "/company/members", `{"username":"ivanov"}`)
	if code != http.StatusOK || second["id"] != first["id"] {
		t.Errorf("second add = %d %v, want 200 with the same id", code, second)
	}
	if e.panel.creates != 1 {
		t.Errorf("panel creates = %d", e.panel.creates)
	}
}

func TestBoundTokenCannotPickTariffOnAdd(t *testing.T) {
	e := newCompanyEnv(t)
	e.token(t, "vpc_acme5", &e.company, "company:*")
	code, body := e.call(t, "vpc_acme5", "POST", "/company/members", `{"username":"ivanov","tariff_id":`+itoa(e.tariff)+`}`)
	if code != http.StatusForbidden || body["code"] != "company.admin_only" {
		t.Errorf("= %d %v", code, body)
	}
	if e.panel.creates != 0 {
		t.Error("a user was created")
	}
}

func TestAdminOverrideEndpointNeedsUnboundToken(t *testing.T) {
	e := newCompanyEnv(t)
	m := e.member(t, e.company, "ivanov", false)
	e.token(t, "vpc_acme6", &e.company, "*")
	code, body := e.call(t, "vpc_acme6", "PUT", "/companies/"+itoa(e.company)+"/members/"+itoa(m.ID)+"/override", `{"price":1}`)
	if code != http.StatusForbidden || body["code"] != "token.company_bound" {
		t.Errorf("bound token on the override endpoint = %d %v", code, body)
	}
	e.token(t, "vpc_admin1", nil, "companies:member_override")
	if code, body = e.call(t, "vpc_admin1", "PUT", "/companies/"+itoa(e.company)+"/members/"+itoa(m.ID)+"/override", `{"price":300}`); code != http.StatusNoContent {
		t.Errorf("admin token = %d %v", code, body)
	}
}

func TestStatementDetailListsLinesAndStaysInsideTheCompany(t *testing.T) {
	e := newCompanyEnv(t)
	ctx := t.Context()
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	st := e.db.CompanyStatement.Create().SetCompanyID(e.company).SetPeriodFrom(day(11, 1)).SetPeriodTo(day(12, 1)).
		SetDaysInPeriod(30).SetTotal(70000).SaveX(ctx)
	e.db.StatementLine.Create().SetStatementID(st.ID).SetUsername("aaa").SetTariffName("Base").SetFromDate(day(11, 1)).SetToDate(day(12, 1)).
		SetDays(30).SetDaysInPeriod(30).SetMonthlyPrice(50000).SetAmount(50000).SaveX(ctx)
	e.db.StatementLine.Create().SetStatementID(st.ID).SetUsername("bbb").SetTariffName("Base").SetFromDate(day(11, 19)).SetToDate(day(12, 1)).
		SetDays(12).SetDaysInPeriod(30).SetMonthlyPrice(50000).SetAmount(20000).SaveX(ctx)
	e.token(t, "vpc_acme7", &e.company, "company:*")

	code, body := e.call(t, "vpc_acme7", "GET", "/company/statements/"+itoa(st.ID), "")
	lines, _ := body["lines"].([]any)
	if code != http.StatusOK || len(lines) != 2 || body["total"].(float64) != 700 || lines[1].(map[string]any)["amount"].(float64) != 200 {
		t.Errorf("= %d %v", code, body)
	}

	other := e.otherCompany(t)
	e.token(t, "vpc_other1", &other, "company:*")
	if code, _ = e.call(t, "vpc_other1", "GET", "/company/statements/"+itoa(st.ID), ""); code != http.StatusNotFound {
		t.Errorf("another company's statement = %d, want 404", code)
	}
}

func TestBalanceShowsNegative(t *testing.T) {
	e := newCompanyEnv(t)
	e.db.LedgerEntry.Create().SetCustomerID(e.account).SetType("company_charge").SetAmount(-50000).SaveX(t.Context())
	e.token(t, "vpc_acme8", &e.company, "company:balance")
	code, body := e.call(t, "vpc_acme8", "GET", "/company/balance", "")
	if code != http.StatusOK || body["balance"].(float64) != -500 {
		t.Errorf("= %d %v", code, body)
	}
}

func TestMemberDetailCarriesSegmentHistoryAndPanelUser(t *testing.T) {
	e := newCompanyEnv(t)
	ctx := t.Context()
	e.db.RwUser.Create().SetID(77).SetUsername("emp").SetShortUUID("s7").SetStatus("ACTIVE").
		SetExpireAt(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)).SetSubscriptionURL("https://panel/sub/s7").SaveX(ctx)
	m := e.db.CompanyMember.Create().SetCompanyID(e.company).SetUsername("emp").SetRwUserID(77).
		SetAddedAt(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)).SaveX(ctx)
	oct1, oct15 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	e.db.MemberSegment.Create().SetMemberID(m.ID).SetFromDate(oct1).SetToDate(oct15).SetTariffID(e.tariff).SetPrice(50000).
		SetTrafficLimitBytes(100).SetTrafficStrategy("MONTH").SaveX(ctx)
	e.db.MemberSegment.Create().SetMemberID(m.ID).SetFromDate(oct15).SetTariffID(e.tariff).SetPrice(35000).
		SetTrafficLimitBytes(100).SetTrafficStrategy("MONTH").SetOverrides([]string{"price"}).SaveX(ctx)
	e.token(t, "vpc_hist1", &e.company, "company:*")

	code, body := e.call(t, "vpc_hist1", "GET", "/company/members/"+itoa(m.ID), "")
	if code != http.StatusOK {
		t.Fatalf("= %d %v", code, body)
	}
	segs, _ := body["segments"].([]any)
	if len(segs) != 2 {
		t.Fatalf("segments = %v, want 2 in time order", body["segments"])
	}
	first, second := segs[0].(map[string]any), segs[1].(map[string]any)
	if first["to"] == nil || second["to"] != nil || first["price"].(float64) != 500 || second["price"].(float64) != 350 {
		t.Errorf("segments = %v / %v", first, second)
	}
	if ov, _ := second["overrides"].([]any); len(ov) != 1 || ov[0] != "price" || first["tariff_name"] != "Base" {
		t.Errorf("second overrides = %v, first tariff = %v", second["overrides"], first["tariff_name"])
	}
	rw, _ := body["rw_user"].(map[string]any)
	if rw == nil || rw["username"] != "emp" || rw["subscription_url"] != "https://panel/sub/s7" {
		t.Errorf("rw_user = %v", body["rw_user"])
	}
}

// seedMember adds an employee with an open segment at price kopecks and a
// cached panel user with the given status (empty status = no panel user).
func seedMember(t *testing.T, db *ent.Client, companyID, tariffID int, username, status string, rwID int, price int64, overrides []string, removed bool) {
	t.Helper()
	ctx := t.Context()
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	q := db.CompanyMember.Create().SetCompanyID(companyID).SetUsername(username).SetAddedAt(day)
	if status != "" {
		db.RwUser.Create().SetID(rwID).SetUsername(username).SetShortUUID("s").SetStatus(status).SetExpireAt(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)).SetSubscriptionURL("u").SaveX(ctx)
		q.SetRwUserID(rwID)
	}
	if removed {
		q.SetRemovedAt(day)
	}
	m := q.SaveX(ctx)
	seg := db.MemberSegment.Create().SetMemberID(m.ID).SetFromDate(day).SetTariffID(tariffID).SetPrice(price).
		SetTrafficLimitBytes(0).SetTrafficStrategy("MONTH").SetOverrides(overrides)
	if removed {
		seg.SetToDate(day)
	}
	seg.SaveX(ctx)
}

func TestTariffStatsCountCompanyEmployees(t *testing.T) {
	db := openDB(t)
	_, coid, tid := companyAccount(t, db)
	seedMember(t, db, coid, tid, "live1", "ACTIVE", 1, 50000, nil, false)
	seedMember(t, db, coid, tid, "live2", "LIMITED", 2, 35000, []string{"price"}, false)
	seedMember(t, db, coid, tid, "off", "DISABLED", 3, 50000, nil, false)
	seedMember(t, db, coid, tid, "gone", "ACTIVE", 4, 50000, nil, true)

	h := companyHandlers(db)
	app := testApp()
	app.Get("/tariffs", h.ListTariffs)
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tariffs", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&rows)
	if len(rows) != 1 {
		t.Fatalf("tariffs = %v", rows)
	}
	r := rows[0]
	if r["subscribers"].(float64) != 3 || r["overridden"].(float64) != 1 || r["mrr"].(float64) != 850 {
		t.Errorf("tariff stats = %v, want 3 employees (removed excluded), 1 with an own price, MRR 500+350 (disabled excluded)", r)
	}
}

func TestActiveMembersSkipsArchivedCompaniesAndMarksLive(t *testing.T) {
	db := openDB(t)
	_, coid, tid := companyAccount(t, db)
	seedMember(t, db, coid, tid, "live1", "ACTIVE", 1, 50000, nil, false)
	seedMember(t, db, coid, tid, "off", "DISABLED", 2, 50000, nil, false)
	other := newCompanyEnvOtherArchived(t, db, tid)
	seedMember(t, db, other, tid, "arch", "ACTIVE", 3, 50000, nil, false)

	members, err := companyHandlers(db).activeMembers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	live := 0
	for _, m := range members {
		if m.Live {
			live++
		}
	}
	if len(members) != 2 || live != 1 {
		t.Errorf("members = %+v, want 2 (archived company excluded), 1 live", members)
	}
}

func newCompanyEnvOtherArchived(t *testing.T, db *ent.Client, tariffID int) int {
	t.Helper()
	acc := db.Customer.Create().SetName("Old").SaveX(t.Context())
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	return db.Company.Create().SetName("Old").SetCustomerID(acc.ID).SetTariffID(tariffID).SetReportDay(1).SetArchived(true).
		SetPeriodStart(start).SetPeriodEnd(start.AddDate(0, 1, 0)).SetPeriodDays(30).SaveX(t.Context()).ID
}
