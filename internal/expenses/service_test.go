package expenses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"remna-control/internal/fx"
	"remna-control/internal/remnawave"
	"remna-control/internal/store"
)

func TestTrafficJob(t *testing.T) {
	ctx := context.Background()
	db, _, err := store.OpenDB(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var mu sync.Mutex
	var start, end string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/bandwidth-stats/nodes" {
			http.Error(w, "unexpected "+r.URL.Path, http.StatusTeapot)
			return
		}
		mu.Lock()
		start, end = r.URL.Query().Get("start"), r.URL.Query().Get("end")
		mu.Unlock()
		_, _ = w.Write([]byte(`{"response":{"categories":[],"series":[]}}`))
	}))
	t.Cleanup(srv.Close)
	loc := time.FixedZone("UTC+14", 14*3600)
	svc := New(db, remnawave.NewClient(srv.URL, "tok", ""), fx.New(db, func(context.Context) string { return "RUB" }), loc)

	res, err := svc.TrafficJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "traffic synced" || res.Changed || res.Skipped {
		t.Errorf("result = %+v", res)
	}
	now := time.Now().In(loc)
	mu.Lock()
	defer mu.Unlock()
	if want := SyncFrom(now).Format("2006-01-02"); start != want {
		t.Errorf("start = %s, want %s", start, want)
	}
	if want := now.Format("2006-01-02"); end != want {
		t.Errorf("end = %s, want %s", end, want)
	}
}
