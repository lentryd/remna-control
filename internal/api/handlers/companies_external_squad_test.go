package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"remna-control/ent/companymember"
	"remna-control/ent/membersegment"
	"remna-control/internal/remnawave"
)

func TestCompanySettingsAndEmployeeExternalSquad(t *testing.T) {
	e := newCompanyEnv(t)
	e.app.Put("/companies/:id", e.h.UpdateCompany)
	m := e.member(t, e.company, "ivanov", false)
	base := `"name":"Acme","report_day":1`

	if code, body := e.call(t, "", http.MethodPut, "/companies/"+itoa(e.company), `{`+base+`,"external_squad_uuid":" ext-9 "}`); code != http.StatusNoContent {
		t.Fatalf("settings = %d %v", code, body)
	}
	co := e.db.Company.GetX(t.Context(), e.company)
	if co.ExternalSquadUUID == nil || *co.ExternalSquadUUID != "ext-9" {
		t.Fatalf("company squad = %v, want trimmed ext-9", co.ExternalSquadUUID)
	}
	seg := e.db.MemberSegment.Query().Where(membersegment.HasMemberWith(companymember.ID(m.ID)), membersegment.ToDateIsNil()).OnlyX(t.Context())
	if seg.ExternalSquadUUID == nil || *seg.ExternalSquadUUID != "ext-9" {
		t.Errorf("employee inherits %v, want ext-9", seg.ExternalSquadUUID)
	}

	e.token(t, "vpc_admin2", nil, "companies:member_override")
	path := "/companies/" + itoa(e.company) + "/members/" + itoa(m.ID) + "/override"
	if code, body := e.call(t, "vpc_admin2", http.MethodPut, path, `{"external_squad_uuid":""}`); code != http.StatusNoContent {
		t.Fatalf("override none = %d %v", code, body)
	}
	seg = e.db.MemberSegment.Query().Where(membersegment.HasMemberWith(companymember.ID(m.ID)), membersegment.ToDateIsNil()).OnlyX(t.Context())
	if seg.ExternalSquadUUID == nil || *seg.ExternalSquadUUID != "" {
		t.Errorf("explicit none stored as %v", seg.ExternalSquadUUID)
	}

	if code, _ := e.call(t, "vpc_admin2", http.MethodPut, path, `{"reset":["external_squad"]}`); code != http.StatusNoContent {
		t.Fatalf("reset = %d", code)
	}
	seg = e.db.MemberSegment.Query().Where(membersegment.HasMemberWith(companymember.ID(m.ID)), membersegment.ToDateIsNil()).OnlyX(t.Context())
	if seg.ExternalSquadUUID == nil || *seg.ExternalSquadUUID != "ext-9" {
		t.Errorf("after reset %v, want the company's ext-9", seg.ExternalSquadUUID)
	}
}

func TestCreateCompanyStoresExternalSquad(t *testing.T) {
	e := newCompanyEnv(t)
	e.app.Post("/companies", e.h.CreateCompany)
	code, body := e.call(t, "", http.MethodPost, "/companies", `{"name":"Beta","tariff_id":`+itoa(e.tariff)+`,"report_day":1,"external_squad_uuid":"ext-3"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %v", code, body)
	}
	co := e.db.Company.GetX(t.Context(), int(body["id"].(float64)))
	if co.ExternalSquadUUID == nil || *co.ExternalSquadUUID != "ext-3" {
		t.Errorf("squad = %v, want ext-3", co.ExternalSquadUUID)
	}
}

func TestEmployeeOverrideRejectsUnknownExternalSquad(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":{"externalSquads":[{"uuid":"e1","name":"P","info":{"membersCount":1}}]}}`))
	}))
	t.Cleanup(panel.Close)
	e := newCompanyEnv(t)
	e.h.RW = remnawave.NewClient(panel.URL, "t", "")
	m := e.member(t, e.company, "ivanov", false)
	e.token(t, "vpc_admin3", nil, "companies:member_override")
	path := "/companies/" + itoa(e.company) + "/members/" + itoa(m.ID) + "/override"
	code, body := e.call(t, "vpc_admin3", http.MethodPut, path, `{"external_squad_uuid":"gone"}`)
	if code != http.StatusBadRequest || body["code"] != "external_squad.unknown" {
		t.Errorf("unknown squad = %d %v", code, body)
	}
	if code, body = e.call(t, "vpc_admin3", http.MethodPut, path, `{"external_squad_uuid":""}`); code != http.StatusNoContent {
		t.Errorf("explicit none must pass the check: %d %v", code, body)
	}
}
