package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"remna-control/ent/tariff"
	"remna-control/internal/company"
	"remna-control/internal/remnawave"
)

func TestTariffExternalSquadSavedAndCleared(t *testing.T) {
	db := openDB(t)
	h := companyHandlers(db)
	h.Company = company.New(db, remnawave.NewClient("http://127.0.0.1:1", "t", ""), time.UTC)
	app := testApp()
	app.Post("/tariffs", h.CreateTariff)
	app.Put("/tariffs/:id", h.UpdateTariff)

	code, body := do(t, app, http.MethodPost, "/tariffs", `{"kind":"base","name":"T","squad_uuids":["s"],"external_squad_uuid":"ext-1"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %v", code, body)
	}
	id := int(body["id"].(float64))
	got := db.Tariff.Query().Where(tariff.ID(id)).OnlyX(t.Context())
	if got.ExternalSquadUUID == nil || *got.ExternalSquadUUID != "ext-1" {
		t.Fatalf("saved = %v, want ext-1", got.ExternalSquadUUID)
	}

	code, body = do(t, app, http.MethodPut, "/tariffs/"+itoa(id), `{"kind":"base","name":"T","squad_uuids":["s"],"external_squad_uuid":""}`)
	if code != http.StatusNoContent {
		t.Fatalf("update = %d %v", code, body)
	}
	if got = db.Tariff.GetX(t.Context(), id); got.ExternalSquadUUID != nil {
		t.Errorf("an empty string must clear the squad, got %q", *got.ExternalSquadUUID)
	}
}

func TestUnknownExternalSquadIsRejectedOnTariffAndCompany(t *testing.T) {
	up := true
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"response":{"externalSquads":[{"uuid":"e1","name":"Pages","info":{"membersCount":1}}]}}`))
	}))
	t.Cleanup(panel.Close)
	e := newCompanyEnv(t)
	e.h.RW = remnawave.NewClient(panel.URL, "t", "")
	e.app.Post("/tariffs", e.h.CreateTariff)
	e.app.Put("/companies/:id", e.h.UpdateCompany)

	code, body := e.call(t, "", http.MethodPost, "/tariffs", `{"kind":"base","name":"T","external_squad_uuid":"gone"}`)
	if code != http.StatusBadRequest || body["code"] != "external_squad.unknown" {
		t.Errorf("unknown squad on a tariff = %d %v", code, body)
	}
	if code, body = e.call(t, "", http.MethodPost, "/tariffs", `{"kind":"base","name":"T","external_squad_uuid":"e1"}`); code != http.StatusCreated {
		t.Errorf("known squad = %d %v", code, body)
	}
	path := "/companies/" + itoa(e.company)
	code, body = e.call(t, "", http.MethodPut, path, `{"name":"Acme","report_day":1,"external_squad_uuid":"gone"}`)
	if code != http.StatusBadRequest || body["code"] != "external_squad.unknown" {
		t.Errorf("unknown squad on a company = %d %v", code, body)
	}
	if e.db.Company.GetX(t.Context(), e.company).ExternalSquadUUID != nil {
		t.Error("the rejected squad was stored")
	}
	// A panel that can't be asked doesn't block the edit.
	up = false
	if code, body = e.call(t, "", http.MethodPut, path, `{"name":"Acme","report_day":1,"external_squad_uuid":"e1"}`); code != http.StatusNoContent {
		t.Errorf("panel down = %d %v", code, body)
	}
}
