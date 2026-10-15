// Package backup exports and imports the database as a zip archive: a
// manifest plus one JSON file per table. It works on raw SQLite tables so
// IDs, timestamps and columns Ent doesn't know about survive the round
// trip, and archives from older or newer builds can still be loaded.
package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"remna-control/ent/migrate"
	"remna-control/internal/apperr"
)

const (
	Format  = "vpn-control-backup"
	Version = 1

	manifestName = "manifest.json"
	tablesDir    = "tables/"
)

// Category is a user-selectable group of tables that only make sense
// together (e.g. subscriptions and their add-ons).
type Category struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tables      []string `json:"tables"`
	// DependsOn lists categories whose rows this one references; importing
	// it without them only works if the target already has matching IDs.
	DependsOn []string `json:"depends_on,omitempty"`
}

// Categories covers every table of the schema exactly once (see the test).
var Categories = []Category{
	{Key: "customers", Title: "Customers", Description: "Customers and referrals", Tables: []string{"customers"}},
	{Key: "tariffs", Title: "Tariffs and add-ons", Description: "Tariffs, term prices, add-ons, add-ons included in tariffs", Tables: []string{"addons", "tariffs", "tariff_periods", "tariff_included_addons"}},
	{Key: "subscriptions", Title: "Subscriptions", Description: "Subscriptions, connected add-ons and the Remnawave users cache", Tables: []string{"rw_users", "subscriptions", "subscription_addons"}, DependsOn: []string{"customers", "tariffs"}},
	{Key: "payments", Title: "Payments and balances", Description: "Payments, balance movements, referral accruals, extensions", Tables: []string{"payments", "ledger_entries", "referral_accruals", "extensions"}, DependsOn: []string{"customers"}},
	{Key: "expenses", Title: "Expenses", Description: "Expense items and the expense log", Tables: []string{"expense_items", "expenses"}},
	{Key: "settings", Title: "Settings", Description: "App settings and API tokens", Tables: []string{"settings", "api_tokens"}},
	{Key: "stats", Title: "Rates and traffic", Description: "Exchange rate cache and traffic statistics", Tables: []string{"fx_rates", "traffic_snapshots"}},
	{Key: "audit", Title: "Audit log", Description: "History of admin actions", Tables: []string{"audit_logs"}},
}

func categoryByKey(key string) (Category, bool) {
	for _, c := range Categories {
		if c.Key == key {
			return c, true
		}
	}
	return Category{}, false
}

// tablesOf resolves category keys to their tables, rejecting unknown keys.
func tablesOf(keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, apperr.New("backup.no_categories", "no categories selected")
	}
	var out []string
	for _, k := range keys {
		c, ok := categoryByKey(k)
		if !ok {
			return nil, apperr.New("backup.unknown_category", "unknown category {{key}}", "key", k)
		}
		out = append(out, c.Tables...)
	}
	return out, nil
}

// Validate checks a category selection.
func Validate(keys []string) error {
	_, err := tablesOf(keys)
	return err
}

// knownTables is the set of tables of the current schema.
func knownTables() map[string]bool {
	m := make(map[string]bool, len(migrate.Tables))
	for _, t := range migrate.Tables {
		m[t.Name] = true
	}
	return m
}

type Manifest struct {
	Format     string         `json:"format"`
	Version    int            `json:"version"`
	AppVersion string         `json:"app_version"`
	CreatedAt  time.Time      `json:"created_at"`
	Categories []string       `json:"categories"`
	Tables     map[string]int `json:"tables"` // table → row count
}

// Counts returns the row count of every category, for the export dialog.
func Counts(ctx context.Context, db *sql.DB) (map[string]int, error) {
	out := make(map[string]int, len(Categories))
	for _, c := range Categories {
		for _, t := range c.Tables {
			var n int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdent(t)).Scan(&n); err != nil {
				return nil, fmt.Errorf("count %s: %w", t, err)
			}
			out[c.Key] += n
		}
	}
	return out, nil
}

// Export writes the selected categories as a zip archive to w. It reads in
// one transaction so the snapshot is consistent.
func Export(ctx context.Context, db *sql.DB, w io.Writer, categories []string, appVersion string) (*Manifest, error) {
	tables, err := tablesOf(categories)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	m := &Manifest{
		Format: Format, Version: Version, AppVersion: appVersion,
		CreatedAt: time.Now(), Categories: categories, Tables: map[string]int{},
	}
	zw := zip.NewWriter(w)
	for _, t := range tables {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: tablesDir + t + ".json", Method: zip.Deflate, Modified: m.CreatedAt})
		if err != nil {
			return nil, err
		}
		n, err := dumpTable(ctx, tx, t, f)
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", t, err)
		}
		m.Tables[t] = n
	}
	f, err := zw.CreateHeader(&zip.FileHeader{Name: manifestName, Method: zip.Deflate, Modified: m.CreatedAt})
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return m, zw.Close()
}

type column struct {
	name    string
	isTime  bool
	notNull bool
}

func tableColumns(ctx context.Context, q queryer, table string) ([]column, error) {
	rows, err := q.QueryContext(ctx, "SELECT name, type, \"notnull\" FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cols []column
	for rows.Next() {
		var c column
		var typ string
		if err := rows.Scan(&c.name, &typ, &c.notNull); err != nil {
			return nil, err
		}
		typ = strings.ToLower(typ)
		c.isTime = strings.Contains(typ, "date") || strings.Contains(typ, "time")
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// dumpTable writes the table as a JSON array, one row object per line.
// Date columns are read as their stored text so they're restored verbatim
// (the driver would otherwise reformat them and break text comparisons).
func dumpTable(ctx context.Context, q queryer, table string, w io.Writer) (int, error) {
	cols, err := tableColumns(ctx, q, table)
	if err != nil {
		return 0, err
	}
	sel := make([]string, len(cols))
	for i, c := range cols {
		sel[i] = quoteIdent(c.name)
		if c.isTime {
			sel[i] = "CAST(" + sel[i] + " AS TEXT)"
		}
	}
	rows, err := q.QueryContext(ctx, "SELECT "+strings.Join(sel, ", ")+" FROM "+quoteIdent(table)+" ORDER BY rowid")
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	if _, err := io.WriteString(w, "["); err != nil {
		return 0, err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	n := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return 0, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[c.name] = v
		}
		b, err := json.Marshal(row)
		if err != nil {
			return 0, err
		}
		sep := ",\n"
		if n == 0 {
			sep = "\n"
		}
		if _, err := io.WriteString(w, sep); err != nil {
			return 0, err
		}
		if _, err := w.Write(b); err != nil {
			return 0, err
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	_, err = io.WriteString(w, "\n]\n")
	return n, err
}

// Archive is an opened backup.
type Archive struct {
	Manifest Manifest
	files    map[string]*zip.File
}

// Available lists the categories that have at least one table in the archive.
func (a *Archive) Available() []string {
	var out []string
	for _, c := range Categories {
		for _, t := range c.Tables {
			if _, ok := a.files[t]; ok {
				out = append(out, c.Key)
				break
			}
		}
	}
	return out
}

// Open reads and validates a backup archive.
func Open(data []byte) (*Archive, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, apperr.New("backup.not_zip", "the file isn't a zip archive")
	}
	a := &Archive{files: map[string]*zip.File{}}
	var manifest *zip.File
	for _, f := range zr.File {
		switch {
		case f.Name == manifestName:
			manifest = f
		case strings.HasPrefix(f.Name, tablesDir) && strings.HasSuffix(f.Name, ".json"):
			a.files[strings.TrimSuffix(strings.TrimPrefix(f.Name, tablesDir), ".json")] = f
		}
	}
	if manifest == nil {
		return nil, apperr.New("backup.no_manifest", "the archive has no manifest.json; it isn't a Remna Control backup")
	}
	rc, err := manifest.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	if err := json.NewDecoder(rc).Decode(&a.Manifest); err != nil {
		return nil, apperr.Wrap(err, "backup.bad_manifest", "invalid manifest.json: {{error}}")
	}
	if a.Manifest.Format != Format {
		return nil, apperr.New("backup.not_backup", "this isn't a Remna Control backup")
	}
	if a.Manifest.Version > Version {
		return nil, apperr.New("backup.newer_format", "the backup uses a newer format ({{version}}); update the app", "version", a.Manifest.Version)
	}
	return a, nil
}

// Report describes what an import changed.
type Report struct {
	Tables  map[string]int `json:"tables"`  // table → rows imported
	Skipped []string       `json:"skipped"` // archive tables this build doesn't have
}

// Import replaces the selected categories with the archive's contents in a
// single transaction. Foreign keys are switched off while rows are swapped
// (so ON DELETE SET NULL doesn't wipe references from untouched tables)
// and verified before commit; any dangling reference aborts the import.
func Import(ctx context.Context, db *sql.DB, a *Archive, categories []string) (*Report, error) {
	tables, err := tablesOf(categories)
	if err != nil {
		return nil, err
	}
	known := knownTables()

	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	// PRAGMA foreign_keys is a no-op inside a transaction, so it's set on
	// the connection around it.
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return nil, err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON") }()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rep := &Report{Tables: map[string]int{}, Skipped: []string{}}
	for _, t := range tables {
		if !known[t] {
			continue
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteIdent(t)); err != nil {
			return nil, fmt.Errorf("clear %s: %w", t, err)
		}
		f, ok := a.files[t]
		if !ok {
			// Older backup without this table: the category is still
			// replaced as a whole, so it stays empty.
			rep.Tables[t] = 0
			continue
		}
		n, err := loadTable(ctx, tx, t, f)
		if err != nil {
			return nil, fmt.Errorf("import %s: %w", t, err)
		}
		rep.Tables[t] = n
	}
	for t := range a.files {
		if !known[t] {
			rep.Skipped = append(rep.Skipped, t)
		}
	}
	sort.Strings(rep.Skipped)

	if err := checkForeignKeys(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rep, nil
}

func loadTable(ctx context.Context, tx *sql.Tx, table string, f *zip.File) (int, error) {
	cols, err := tableColumns(ctx, tx, table)
	if err != nil {
		return 0, err
	}
	have := make(map[string]bool, len(cols))
	for _, c := range cols {
		have[c.name] = true
	}

	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer func() { _ = rc.Close() }()
	dec := json.NewDecoder(rc)
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return 0, errors.New("expected a JSON array")
	}

	// Rows normally share one column set; statements are cached per set
	// anyway in case they don't.
	stmts := map[string]*sql.Stmt{}
	defer func() {
		for _, s := range stmts {
			_ = s.Close()
		}
	}()
	n := 0
	for dec.More() {
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			return n, fmt.Errorf("row %d: %w", n+1, err)
		}
		names := make([]string, 0, len(row))
		for k := range row {
			if have[k] { // columns dropped since the backup are ignored
				names = append(names, k)
			}
		}
		slices.Sort(names)
		key := strings.Join(names, ",")
		stmt := stmts[key]
		if stmt == nil {
			quoted := make([]string, len(names))
			for i, c := range names {
				quoted[i] = quoteIdent(c)
			}
			q := "INSERT INTO " + quoteIdent(table) + " (" + strings.Join(quoted, ", ") +
				") VALUES (" + strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ") + ")"
			if stmt, err = tx.PrepareContext(ctx, q); err != nil {
				return n, err
			}
			stmts[key] = stmt
		}
		args := make([]any, len(names))
		for i, c := range names {
			if args[i], err = sqlValue(row[c]); err != nil {
				return n, fmt.Errorf("row %d, %s: %w", n+1, c, err)
			}
		}
		if _, err := stmt.ExecContext(ctx, args...); err != nil {
			return n, fmt.Errorf("row %d: %w", n+1, err)
		}
		n++
	}
	return n, nil
}

// sqlValue converts a decoded JSON value back to a driver value.
func sqlValue(v any) (any, error) {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, nil
		}
		return x.Float64()
	case map[string]any, []any:
		b, err := json.Marshal(x)
		return string(b), err
	default: // nil, bool, string
		return x, nil
	}
}

// checkForeignKeys fails with a readable summary if any row references a
// missing parent.
func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	counts := map[string]int{}
	for rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		counts[table+" → "+parent]++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(counts) == 0 {
		return nil
	}
	parts := make([]string, 0, len(counts))
	for k, n := range counts {
		parts = append(parts, fmt.Sprintf("%s: %d", k, n))
	}
	sort.Strings(parts)
	return &IntegrityError{Details: parts}
}

// IntegrityError means the import would leave dangling references, usually
// because a dependent category was imported without its parents.
type IntegrityError struct{ Details []string }

func (e *IntegrityError) Error() string {
	return "import cancelled: rows reference missing data (" + strings.Join(e.Details, "; ") +
		"); import them together with the categories they depend on"
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
