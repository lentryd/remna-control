package rwsync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"remna-control/ent/rwuser"
	"remna-control/internal/remnawave"
	"remna-control/internal/store"
)

func TestMainUsername(t *testing.T) {
	cases := []struct {
		user, prefix, suffix, want string
		ok                         bool
	}{
		{"white_vasya", "white_", "", "vasya", true},
		{"vasya_lte", "", "_lte", "vasya", true},
		{"vasya", "white_", "", "", false},
		{"white_", "white_", "", "", false},
		{"vasya", "", "", "", false},
	}
	for _, c := range cases {
		got, ok := MainUsername(c.user, c.prefix, c.suffix)
		if got != c.want || ok != c.ok {
			t.Errorf("MainUsername(%q,%q,%q) = %q,%v", c.user, c.prefix, c.suffix, got, ok)
		}
	}
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"event":"user.modified"}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	if !VerifySignature("secret", body, sig) {
		t.Error("valid signature rejected")
	}
	if VerifySignature("other", body, sig) {
		t.Error("wrong secret accepted")
	}
}

// fakeUsers serves the panel's user stream from a settable list.
type fakeUsers struct {
	mu    sync.Mutex
	users []remnawave.User
}

func (p *fakeUsers) set(users ...remnawave.User) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.users = users
}

func (p *fakeUsers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.Method != http.MethodGet || r.URL.Path != "/api/users/stream" {
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusTeapot)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"users": p.users}})
}

func TestSyncJob(t *testing.T) {
	ctx := context.Background()
	db, _, err := store.OpenDB(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	panel := &fakeUsers{}
	srv := httptest.NewServer(panel)
	t.Cleanup(srv.Close)
	s := New(db, remnawave.NewClient(srv.URL, "tok", ""), filepath.Join(t.TempDir(), "addons.yml"))

	// Cached: 1 stays, 3 is gone from the panel; 2 is new.
	for _, u := range []remnawave.User{{ID: 1, Username: "one"}, {ID: 3, Username: "three"}} {
		if err := Upsert(ctx, db, &u); err != nil {
			t.Fatal(err)
		}
	}
	panel.set(remnawave.User{ID: 1, Username: "one"}, remnawave.User{ID: 2, Username: "two"})

	res, err := s.SyncJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "synced 2 users" || !res.Changed {
		t.Errorf("first pass = %+v, want synced 2 users, changed", res)
	}
	if !db.RwUser.Query().Where(rwuser.ID(3), rwuser.Deleted(true)).ExistX(ctx) {
		t.Error("user 3 isn't marked deleted")
	}
	st, err := s.syncAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st != (stats{Users: 2}) {
		t.Errorf("second pass stats = %+v, want only Users=2", st)
	}

	// A deleted user coming back counts as added.
	panel.set(remnawave.User{ID: 1, Username: "one"}, remnawave.User{ID: 3, Username: "three"})
	if st, err = s.syncAll(ctx); err != nil {
		t.Fatal(err)
	}
	if st != (stats{Users: 2, Added: 1, Removed: 1}) {
		t.Errorf("third pass stats = %+v", st)
	}
	if res, err = s.SyncJob(ctx); err != nil || res.Changed || res.Summary != "synced 2 users" {
		t.Errorf("idle pass = %+v, %v", res, err)
	}
}
