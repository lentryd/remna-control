package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/tariff"
	"remna-control/internal/apperr"
	"remna-control/internal/store"
)

func companyFixture(t *testing.T) (*ent.Client, *fiber.App, int) {
	t.Helper()
	ctx := context.Background()
	db, _, err := store.OpenDB(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tf := db.Tariff.Create().SetKind(tariff.KindBase).SetName("B").SetMonthlyPrice(1).SaveX(ctx)
	acc := db.Customer.Create().SetName("Acme").SaveX(ctx)
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	co := db.Company.Create().SetName("Acme").SetCustomerID(acc.ID).SetTariffID(tf.ID).SetReportDay(1).
		SetPeriodStart(start).SetPeriodEnd(start.AddDate(0, 1, 0)).SetPeriodDays(30).SaveX(ctx)

	app := fiber.New(fiber.Config{ErrorHandler: func(c *fiber.Ctx, err error) error {
		if ae, ok := err.(*apperr.Error); ok {
			return c.Status(ae.Status).JSON(fiber.Map{"code": ae.Code})
		}
		return c.Status(500).SendString(err.Error())
	}})
	ok := func(c *fiber.Ctx) error {
		id, _ := c.Locals(LocalsCompanyID).(int)
		return c.JSON(fiber.Map{"company_id": id})
	}
	app.Get("/admin", RequireToken(db, "customers:list"), ok)
	app.Get("/company", RequireToken(db, "company:members"), ok)
	return db, app, co.ID
}

func makeToken(t *testing.T, db *ent.Client, raw string, companyID *int, scopes ...string) {
	t.Helper()
	db.APIToken.Create().SetName(raw).SetTokenHash(HashToken(raw)).SetPrefix(raw[:4]).SetScopes(scopes).
		SetExpireAt(time.Now().Add(time.Hour)).SetNillableCompanyID(companyID).SaveX(context.Background())
}

func get(t *testing.T, app *fiber.App, path, token string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, string(buf[:n])
}

func TestBoundTokenRejectedOnAdminEndpoint(t *testing.T) {
	db, app, cid := companyFixture(t)
	makeToken(t, db, "vpc_bound1", &cid, "*")
	code, body := get(t, app, "/admin", "vpc_bound1")
	if code != http.StatusForbidden || !contains(body, "token.company_bound") {
		t.Errorf("= %d %s, want 403 token.company_bound even with the * scope", code, body)
	}
}

func TestUnboundTokenRejectedOnCompanyEndpoint(t *testing.T) {
	db, app, _ := companyFixture(t)
	makeToken(t, db, "vpc_free1", nil, "*")
	code, body := get(t, app, "/company", "vpc_free1")
	if code != http.StatusForbidden || !contains(body, "token.company_required") {
		t.Errorf("= %d %s, want 403 token.company_required", code, body)
	}
}

func TestBoundTokenPassesCompanyEndpointWithCompanyInLocals(t *testing.T) {
	db, app, cid := companyFixture(t)
	makeToken(t, db, "vpc_bound2", &cid, "company:*")
	code, body := get(t, app, "/company", "vpc_bound2")
	if code != http.StatusOK || !contains(body, `"company_id":`+itoa(cid)) {
		t.Errorf("= %d %s", code, body)
	}
}

func TestBoundTokenStillNeedsTheScope(t *testing.T) {
	db, app, cid := companyFixture(t)
	makeToken(t, db, "vpc_bound3", &cid, "company:balance")
	if code, _ := get(t, app, "/company", "vpc_bound3"); code != http.StatusForbidden {
		t.Errorf("code = %d, want 403 token.scope", code)
	}
}

func TestCompanyCatalogShape(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range ScopeCatalog {
		for _, ep := range r.Endpoints {
			if seen[ep.Key] {
				t.Errorf("duplicate key %s", ep.Key)
			}
			seen[ep.Key] = true
			if want := r.Resource == "company"; ep.Company != want {
				t.Errorf("%s: Company = %v, want %v", ep.Key, ep.Company, want)
			}
		}
	}
	for _, key := range []string{"company:members", "company:member_add", "company:member_remove", "company:member_update", "company:balance", "company:statements",
		"companies:member_override", "companies:member_protect", "companies:close_period"} {
		if !seen[key] {
			t.Errorf("missing key %s", key)
		}
	}
}

func TestGrantsCompanyAndCompanyScopesOnly(t *testing.T) {
	cases := []struct {
		scopes       []string
		grants, only bool
	}{
		{[]string{"company:*"}, true, true},
		{[]string{"company:member_add", "company:balance"}, true, true},
		{[]string{"*"}, false, true},
		{[]string{"customers:list"}, false, false},
		{[]string{"company:read", "customers:list"}, true, false},
		{[]string{"companies:list"}, false, false},
	}
	for _, c := range cases {
		if got := GrantsCompany(c.scopes); got != c.grants {
			t.Errorf("GrantsCompany(%v) = %v", c.scopes, got)
		}
		if got := CompanyScopesOnly(c.scopes); got != c.only {
			t.Errorf("CompanyScopesOnly(%v) = %v", c.scopes, got)
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func itoa(n int) string { return strconv.Itoa(n) }
