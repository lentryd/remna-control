package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"remna-control/internal/jobs"
)

// Snapshot kinds: made on a schedule, by hand (UI or API), or right before
// an import replaces data.
const (
	KindAuto      = "auto"
	KindManual    = "manual"
	KindPreImport = "pre-import"
)

// snapshotName guards file access: only names Snapshots itself produces.
var snapshotName = regexp.MustCompile(`^(auto|manual|pre-import)-\d{8}-\d{6}(-\d+)?\.zip$`)

// ErrBadName is returned for names that aren't snapshot files.
var ErrBadName = errors.New("invalid snapshot name")

// Snapshot is a backup archive kept on the server.
type Snapshot struct {
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Size       int64     `json:"size"`
	CreatedAt  time.Time `json:"created_at"`
	AppVersion string    `json:"app_version"`
	Categories []string  `json:"categories"`
}

// Snapshots keeps full backups in a directory next to the database.
type Snapshots struct {
	dir     string
	db      *sql.DB
	version string
	mu      sync.Mutex
}

// NewSnapshots stores snapshots in <db dir>/backups.
func NewSnapshots(dbPath string, db *sql.DB, appVersion string) *Snapshots {
	return &Snapshots{dir: filepath.Join(filepath.Dir(dbPath), "backups"), db: db, version: appVersion}
}

// AllCategories lists every category key.
func AllCategories() []string {
	out := make([]string, 0, len(Categories))
	for _, c := range Categories {
		out = append(out, c.Key)
	}
	return out
}

// Create writes a snapshot of categories (all when empty).
func (s *Snapshots) Create(ctx context.Context, kind string, categories []string) (*Snapshot, error) {
	if len(categories) == 0 {
		categories = AllCategories()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	base := kind + "-" + time.Now().Format("20060102-150405")
	name := base + ".zip"
	for i := 2; fileExists(filepath.Join(s.dir, name)); i++ {
		name = fmt.Sprintf("%s-%d.zip", base, i)
	}
	tmp, err := os.CreateTemp(s.dir, ".snapshot-*")
	if err != nil {
		return nil, err
	}
	m, err := Export(ctx, s.db, tmp, categories, s.version)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(s.dir, name))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	fi, err := os.Stat(filepath.Join(s.dir, name))
	if err != nil {
		return nil, err
	}
	slog.Info("snapshot saved", "name", name)
	return &Snapshot{Name: name, Kind: kind, Size: fi.Size(), CreatedAt: m.CreatedAt, AppVersion: m.AppVersion, Categories: m.Categories}, nil
}

// List returns the snapshots, newest first.
func (s *Snapshots) List() ([]Snapshot, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Snapshot{}
	for _, e := range entries {
		if e.IsDir() || !snapshotName.MatchString(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		sn := Snapshot{Name: e.Name(), Kind: kindOf(e.Name()), Size: fi.Size(), CreatedAt: fi.ModTime(), Categories: []string{}}
		if m, err := readManifest(filepath.Join(s.dir, e.Name())); err == nil {
			sn.CreatedAt, sn.AppVersion = m.CreatedAt, m.AppVersion
			if m.Categories != nil {
				sn.Categories = m.Categories
			}
		}
		out = append(out, sn)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Path is the file of a snapshot; the name must be one List returns.
func (s *Snapshots) Path(name string) (string, error) {
	if !snapshotName.MatchString(name) {
		return "", ErrBadName
	}
	p := filepath.Join(s.dir, name)
	if !fileExists(p) {
		return "", os.ErrNotExist
	}
	return p, nil
}

// Delete removes a snapshot.
func (s *Snapshots) Delete(name string) error {
	p, err := s.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// Prune keeps the newest keep automatic snapshots (manual and pre-import
// ones are only removed by hand).
func (s *Snapshots) Prune(keep int) error {
	list, err := s.List()
	if err != nil {
		return err
	}
	n := 0
	for _, sn := range list {
		if sn.Kind != KindAuto {
			continue
		}
		if n++; n > keep {
			if err := s.Delete(sn.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// TickJob takes an automatic snapshot when the newest one is older than
// interval() (0 disables), keeping keep() of them; a tick that isn't due is
// skipped. Settings are re-read on every check, so changes apply without a
// restart.
func (s *Snapshots) TickJob(interval func() time.Duration, keep func() int) jobs.Func {
	return func(ctx context.Context) (jobs.Result, error) {
		iv := interval()
		if iv <= 0 {
			return jobs.Result{Skipped: true}, nil
		}
		if last := s.lastAuto(); !last.IsZero() && time.Since(last) < iv {
			return jobs.Result{Skipped: true}, nil
		}
		return s.auto(ctx, keep())
	}
}

// ManualJob takes an automatic snapshot now, whatever the schedule.
func (s *Snapshots) ManualJob(keep func() int) jobs.Func {
	return func(ctx context.Context) (jobs.Result, error) {
		return s.auto(ctx, keep())
	}
}

func (s *Snapshots) auto(ctx context.Context, keep int) (jobs.Result, error) {
	sn, err := s.Create(ctx, KindAuto, nil)
	if err != nil {
		return jobs.Result{}, err
	}
	res := jobs.Result{Summary: "snapshot " + sn.Name, Changed: true}
	if err := s.Prune(max(1, keep)); err != nil {
		return res, fmt.Errorf("prune snapshots: %w", err)
	}
	return res, nil
}

func (s *Snapshots) lastAuto() time.Time {
	list, _ := s.List()
	for _, sn := range list {
		if sn.Kind == KindAuto {
			return sn.CreatedAt
		}
	}
	return time.Time{}
}

func kindOf(name string) string {
	for _, k := range []string{KindPreImport, KindManual, KindAuto} {
		if strings.HasPrefix(name, k+"-") {
			return k
		}
	}
	return ""
}

func readManifest(path string) (*Manifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name != manifestName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		var m Manifest
		return &m, json.NewDecoder(rc).Decode(&m)
	}
	return nil, errors.New("no manifest")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
