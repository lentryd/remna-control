package handlers

import (
	"net/http"
	"testing"
)

func createToken(t *testing.T, h *Handlers, body string) (int, map[string]any) {
	t.Helper()
	app := testApp()
	app.Post("/api-tokens", h.CreateToken)
	return do(t, app, http.MethodPost, "/api-tokens", body)
}

func TestCreateTokenRequiresCompanyForCompanyScopes(t *testing.T) {
	db := openDB(t)
	_, coid, _ := companyAccount(t, db)
	h := companyHandlers(db)

	code, body := createToken(t, h, `{"name":"hr","expires_in_days":30,"scopes":["company:*"]}`)
	if code != http.StatusBadRequest || body["code"] != "token.company_required" {
		t.Errorf("without company_id = %d %v", code, body)
	}
	code, body = createToken(t, h, `{"name":"hr","expires_in_days":30,"scopes":["company:*"],"company_id":`+itoa(coid)+`}`)
	if code != http.StatusCreated || int(body["company_id"].(float64)) != coid {
		t.Fatalf("with company_id = %d %v", code, body)
	}
	tok := db.APIToken.Query().OnlyX(t.Context())
	if tok.CompanyID == nil || *tok.CompanyID != coid {
		t.Errorf("stored company_id = %v", tok.CompanyID)
	}
}

func TestCreateTokenRejectsAdminScopesForBoundToken(t *testing.T) {
	db := openDB(t)
	_, coid, _ := companyAccount(t, db)
	h := companyHandlers(db)
	code, body := createToken(t, h, `{"name":"x","expires_in_days":30,"scopes":["company:members","customers:list"],"company_id":`+itoa(coid)+`}`)
	if code != http.StatusBadRequest || body["code"] != "token.company_scopes_only" {
		t.Errorf("= %d %v", code, body)
	}
	if code, _ = createToken(t, h, `{"name":"y","expires_in_days":30,"scopes":["*"],"company_id":`+itoa(coid)+`}`); code != http.StatusCreated {
		t.Errorf("a * token bound to a company = %d, want 201 (the binding limits it)", code)
	}
}

func TestCreateTokenRejectsUnknownAndArchivedCompany(t *testing.T) {
	db := openDB(t)
	_, coid, _ := companyAccount(t, db)
	h := companyHandlers(db)
	if code, body := createToken(t, h, `{"name":"x","expires_in_days":30,"scopes":["company:*"],"company_id":999}`); code != http.StatusBadRequest || body["code"] != "token.bad_company" {
		t.Errorf("unknown company = %d %v", code, body)
	}
	db.Company.UpdateOneID(coid).SetArchived(true).ExecX(t.Context())
	if code, body := createToken(t, h, `{"name":"x","expires_in_days":30,"scopes":["company:*"],"company_id":`+itoa(coid)+`}`); code != http.StatusBadRequest || body["code"] != "token.bad_company" {
		t.Errorf("archived company = %d %v", code, body)
	}
}

func TestOrdinaryTokenStillCreatesWithoutCompany(t *testing.T) {
	db := openDB(t)
	h := companyHandlers(db)
	if code, body := createToken(t, h, `{"name":"bot","expires_in_days":30,"scopes":["customers:list"]}`); code != http.StatusCreated || body["company_id"] != nil {
		t.Errorf("= %d %v", code, body)
	}
}
