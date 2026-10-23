package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/tariff"
	"remna-control/internal/apperr"
	"remna-control/internal/jobs"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
	"remna-control/internal/store"
)

func TestDeleteSubscription_UnlinksAddons(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)

	c := db.Customer.Create().SetName("Alice").SaveX(ctx)
	a := db.Addon.Create().SetName("white").SetPrefix("white_").SaveX(ctx)
	tr := db.Tariff.Create().SetKind(tariff.KindAddon).SetAddonID(a.ID).SetName("White").SetMonthlyPrice(100).SaveX(ctx)
	sub := db.Subscription.Create().SetCustomerID(c.ID).SaveX(ctx)
	db.SubscriptionAddon.Create().SetSubscriptionID(sub.ID).SetAddonID(a.ID).SetTariffID(tr.ID).SaveX(ctx)

	h := &Handlers{DB: db}
	app := fiber.New()
	app.Delete("/subscriptions/:id", h.DeleteSubscription)
	resp, err := app.Test(httptest.NewRequest("DELETE", "/subscriptions/"+strconv.Itoa(sub.ID), nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if n := db.Subscription.Query().CountX(ctx); n != 0 {
		t.Fatalf("subscriptions left: %d", n)
	}
	if n := db.SubscriptionAddon.Query().CountX(ctx); n != 0 {
		t.Fatalf("add-ons left: %d", n)
	}
}

func openDB(t *testing.T) *ent.Client {
	t.Helper()
	db, _, err := store.OpenDB(context.Background(), filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// testApp answers errors with their status and code, like the API's error
// handler.
func testApp() *fiber.App {
	return fiber.New(fiber.Config{ErrorHandler: func(c *fiber.Ctx, err error) error {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return c.Status(ae.Status).JSON(fiber.Map{"message": ae.Message, "code": ae.Code})
		}
		code := fiber.StatusInternalServerError
		var fe *fiber.Error
		if errors.As(err, &fe) {
			code = fe.Code
		}
		return c.Status(code).JSON(fiber.Map{"message": err.Error()})
	}})
}

func call(t *testing.T, app *fiber.App, method, path string) (int, map[string]any) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(method, path, nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func TestSyncStatus_FromJobStatus(t *testing.T) {
	db := openDB(t)
	var fail atomic.Bool
	newApp := func() *fiber.App {
		m := jobs.New(db, true)
		m.Register(jobs.Job{Key: "panel_users", Every: time.Minute, Tick: func(context.Context) (jobs.Result, error) {
			if fail.Load() {
				return jobs.Result{}, errors.New("boom")
			}
			return jobs.Result{Summary: "synced 1 users", Changed: true}, nil
		}})
		h := &Handlers{DB: db, Jobs: m}
		app := testApp()
		app.Get("/rw/sync", h.SyncStatus)
		app.Post("/rw/sync", h.SyncNow)
		return app
	}
	app := newApp()

	if code, body := call(t, app, "GET", "/rw/sync"); code != fiber.StatusOK || body["last_sync"] != nil || body["error"] != nil {
		t.Fatalf("never run: %d %v", code, body)
	}

	fail.Store(true)
	if code, body := call(t, app, "POST", "/rw/sync"); code != fiber.StatusBadRequest || body["message"] != "boom" {
		t.Errorf("failed sync: %d %v, want 400 boom", code, body)
	}
	if _, body := call(t, app, "GET", "/rw/sync"); body["last_sync"] != nil || body["error"] != "boom" {
		t.Errorf("after a failure: %v", body)
	}

	fail.Store(false)
	if code, body := call(t, app, "POST", "/rw/sync"); code != fiber.StatusNoContent {
		t.Errorf("sync: %d %v", code, body)
	}
	// A restart keeps the last success; the recovered error is gone.
	_, body := call(t, newApp(), "GET", "/rw/sync")
	at, _ := body["last_sync"].(string)
	if _, err := time.Parse(time.RFC3339, at); err != nil || body["error"] != nil {
		t.Errorf("after recovery: %v", body)
	}
}

func TestSyncNow_Running(t *testing.T) {
	db := openDB(t)
	m := jobs.New(db, true)
	started, release := make(chan struct{}), make(chan struct{})
	m.Register(jobs.Job{Key: "panel_users", Every: time.Minute, Tick: func(context.Context) (jobs.Result, error) {
		close(started)
		<-release
		return jobs.Result{}, nil
	}})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.Run(context.Background(), "panel_users")
	}()
	<-started
	defer func() { close(release); <-done }()

	h := &Handlers{DB: db, Jobs: m}
	app := testApp()
	app.Post("/rw/sync", h.SyncNow)
	if code, body := call(t, app, "POST", "/rw/sync"); code != fiber.StatusConflict || body["code"] != "jobs.running" {
		t.Errorf("sync while running: %d %v", code, body)
	}
}

// A panel error from the job keeps its mapping: 502 panel.error.
func TestSyncNow_PanelError(t *testing.T) {
	db := openDB(t)
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"down"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(panel.Close)
	svc := rwsync.New(db, remnawave.NewClient(panel.URL, "tok", ""), filepath.Join(t.TempDir(), "addons.yml"))
	m := jobs.New(db, true)
	m.Register(jobs.Job{Key: "panel_users", Every: time.Minute, Tick: svc.SyncJob})

	h := &Handlers{DB: db, Jobs: m}
	app := testApp()
	app.Post("/rw/sync", h.SyncNow)
	if code, body := call(t, app, "POST", "/rw/sync"); code != fiber.StatusBadGateway || body["code"] != "panel.error" {
		t.Errorf("panel down: %d %v, want 502 panel.error", code, body)
	}
	if v, _ := m.Status(context.Background(), "panel_users"); v.LastOK == nil || *v.LastOK {
		t.Errorf("the failure isn't recorded: %+v", v)
	}
}
