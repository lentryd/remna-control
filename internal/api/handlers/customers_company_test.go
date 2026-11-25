package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/tariff"
	"remna-control/internal/billing"
	"remna-control/internal/remnawave"
	"remna-control/internal/settings"
)

// companyAccount makes a company and returns its account customer's id and
// the company's.
func companyAccount(t *testing.T, db *ent.Client) (customerID, companyID, tariffID int) {
	t.Helper()
	ctx := t.Context()
	tf := db.Tariff.Create().SetKind(tariff.KindBase).SetName("Base").SetMonthlyPrice(50000).SaveX(ctx)
	acc := db.Customer.Create().SetName("Acme").SaveX(ctx)
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	co := db.Company.Create().SetName("Acme").SetCustomerID(acc.ID).SetTariffID(tf.ID).SetReportDay(1).
		SetPeriodStart(start).SetPeriodEnd(start.AddDate(0, 1, 0)).SetPeriodDays(30).SaveX(ctx)
	return acc.ID, co.ID, tf.ID
}

func companyHandlers(db *ent.Client) *Handlers {
	return &Handlers{DB: db, Billing: billing.New(db, remnawave.NewClient("http://127.0.0.1:1", "t", ""), settings.New(db))}
}

func do(t *testing.T, app *fiber.App, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestDeleteCompanyAccountCustomerIs409(t *testing.T) {
	db := openDB(t)
	cid, _, _ := companyAccount(t, db)
	h := companyHandlers(db)
	app := testApp()
	app.Delete("/customers/:id", h.DeleteCustomer)
	code, body := do(t, app, http.MethodDelete, "/customers/"+itoa(cid), "")
	if code != http.StatusConflict || body["code"] != "customer.company_account" {
		t.Errorf("delete = %d %v", code, body)
	}
	if db.Customer.Query().CountX(t.Context()) != 1 {
		t.Error("the account customer was deleted")
	}
}

func TestCannotCreateSubscriptionForCompanyAccount(t *testing.T) {
	db := openDB(t)
	cid, _, tid := companyAccount(t, db)
	h := companyHandlers(db)
	app := testApp()
	app.Post("/subscriptions", h.CreateSubscription)
	app.Post("/subscriptions/provision", h.ProvisionSubscription)
	code, body := do(t, app, http.MethodPost, "/subscriptions", `{"customer_id":`+itoa(cid)+`,"label":"x"}`)
	if code != http.StatusConflict || body["code"] != "customer.company_account" {
		t.Errorf("create = %d %v", code, body)
	}
	code, body = do(t, app, http.MethodPost, "/subscriptions/provision", `{"customer_id":`+itoa(cid)+`,"tariff_id":`+itoa(tid)+`,"username":"abcd","months":1}`)
	if code != http.StatusConflict || body["code"] != "customer.company_account" {
		t.Errorf("provision = %d %v", code, body)
	}
}

func TestCustomerDTOCarriesCompanyAndLedgerCarriesStatement(t *testing.T) {
	db := openDB(t)
	ctx := t.Context()
	cid, coid, _ := companyAccount(t, db)
	le := db.LedgerEntry.Create().SetCustomerID(cid).SetType("company_charge").SetAmount(-50000).SetDate(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)).SaveX(ctx)
	st := db.CompanyStatement.Create().SetCompanyID(coid).SetPeriodFrom(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)).
		SetPeriodTo(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)).SetDaysInPeriod(30).SetTotal(50000).SetLedgerEntryID(le.ID).SaveX(ctx)
	h := companyHandlers(db)
	app := testApp()
	app.Get("/customers/:id", h.GetCustomer)
	code, body := do(t, app, http.MethodGet, "/customers/"+itoa(cid), "")
	if code != http.StatusOK {
		t.Fatalf("get = %d %v", code, body)
	}
	if int(body["company_id"].(float64)) != coid {
		t.Errorf("company_id = %v, want %d", body["company_id"], coid)
	}
	ledger := body["ledger"].([]any)
	if len(ledger) != 1 || int(ledger[0].(map[string]any)["statement_id"].(float64)) != st.ID {
		t.Errorf("ledger = %v, want statement_id %d", ledger, st.ID)
	}
}

func memberWithPanelUser(t *testing.T, db *ent.Client, companyID, tariffID int) int {
	t.Helper()
	ctx := t.Context()
	db.RwUser.Create().SetID(77).SetUsername("emp").SetShortUUID("s").SetStatus("ACTIVE").SetExpireAt(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)).
		SetSubscriptionURL("u").SaveX(ctx)
	m := db.CompanyMember.Create().SetCompanyID(companyID).SetUsername("emp").SetRwUserID(77).
		SetAddedAt(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)).SaveX(ctx)
	_ = tariffID
	return m.ID
}

func TestSubscriptionsCannotTakeACompanyMembersPanelUser(t *testing.T) {
	db := openDB(t)
	_, coid, tid := companyAccount(t, db)
	memberWithPanelUser(t, db, coid, tid)
	cust := db.Customer.Create().SetName("Alice").SaveX(t.Context())
	h := companyHandlers(db)
	app := testApp()
	app.Post("/subscriptions", h.CreateSubscription)
	app.Put("/subscriptions/:id", h.UpdateSubscription)
	code, body := do(t, app, http.MethodPost, "/subscriptions", `{"customer_id":`+itoa(cust.ID)+`,"rw_user_id":77}`)
	if code != http.StatusConflict || body["code"] != "subscription.company_member" {
		t.Errorf("create = %d %v", code, body)
	}
	sub := db.Subscription.Create().SetCustomerID(cust.ID).SaveX(t.Context())
	code, body = do(t, app, http.MethodPut, "/subscriptions/"+itoa(sub.ID), `{"customer_id":`+itoa(cust.ID)+`,"rw_user_id":77}`)
	if code != http.StatusConflict || body["code"] != "subscription.company_member" {
		t.Errorf("update = %d %v", code, body)
	}
}

func TestUpdateSubscriptionCannotMoveItToACompanyAccount(t *testing.T) {
	db := openDB(t)
	acc, _, _ := companyAccount(t, db)
	cust := db.Customer.Create().SetName("Alice").SaveX(t.Context())
	sub := db.Subscription.Create().SetCustomerID(cust.ID).SaveX(t.Context())
	h := companyHandlers(db)
	app := testApp()
	app.Put("/subscriptions/:id", h.UpdateSubscription)
	code, body := do(t, app, http.MethodPut, "/subscriptions/"+itoa(sub.ID), `{"customer_id":`+itoa(acc)+`}`)
	if code != http.StatusConflict || body["code"] != "customer.company_account" {
		t.Errorf("= %d %v", code, body)
	}
}

func TestRwUsersListMarksCompanyMembersLinked(t *testing.T) {
	db := openDB(t)
	_, coid, tid := companyAccount(t, db)
	memberWithPanelUser(t, db, coid, tid)
	h := companyHandlers(db)
	app := testApp()
	app.Get("/rw/users", h.ListRwUsers)
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/rw/users", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&rows)
	if len(rows) != 1 || rows[0]["linked"] != true || rows[0]["customer_name"] != "Acme" {
		t.Errorf("rows = %v, want the member's user linked to the company", rows)
	}
}

func TestDeleteTariffUsedByACompanyKeepsItsPeriods(t *testing.T) {
	db := openDB(t)
	_, _, tid := companyAccount(t, db)
	ctx := t.Context()
	db.Tariff.UpdateOneID(tid).SetActive(false).ExecX(ctx)
	db.TariffPeriod.Create().SetTariffID(tid).SetMonths(3).SetPrice(120000).SaveX(ctx)
	h := companyHandlers(db)
	app := testApp()
	app.Delete("/tariffs/:id", h.DeleteTariff)
	code, body := do(t, app, http.MethodDelete, "/tariffs/"+itoa(tid), "")
	if code != http.StatusConflict || body["code"] != "tariff.in_use" {
		t.Errorf("= %d %v", code, body)
	}
	if db.TariffPeriod.Query().CountX(ctx) != 1 {
		t.Error("the term prices were deleted although the tariff stayed")
	}
}
