package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"remna-control/internal/remnawave"
)

func TestListExternalSquadsPassesPanelList(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/external-squads" {
			http.Error(w, "unexpected "+r.URL.Path, http.StatusTeapot)
			return
		}
		_, _ = w.Write([]byte(`{"response":{"total":1,"externalSquads":[{"uuid":"e1","name":"Pages","info":{"membersCount":4}}]}}`))
	}))
	t.Cleanup(panel.Close)
	h := companyHandlers(openDB(t))
	h.RW = remnawave.NewClient(panel.URL, "t", "")
	app := testApp()
	app.Get("/rw/external-squads", h.ListExternalSquads)
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/rw/external-squads", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var rows []struct {
		UUID, Name string
		Info       struct{ MembersCount int }
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || len(rows) != 1 || rows[0].UUID != "e1" || rows[0].Name != "Pages" || rows[0].Info.MembersCount != 4 {
		t.Errorf("= %d %+v", resp.StatusCode, rows)
	}

	bad := companyHandlers(openDB(t))
	bad.RW = remnawave.NewClient("http://127.0.0.1:1", "t", "")
	app2 := testApp()
	app2.Get("/rw/external-squads", bad.ListExternalSquads)
	resp2, err := app2.Test(httptest.NewRequest(http.MethodGet, "/rw/external-squads", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode < 400 {
		t.Errorf("a panel failure answered %d", resp2.StatusCode)
	}
}
