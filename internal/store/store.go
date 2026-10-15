// Package store opens the SQLite database and runs Ent's auto-migration.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"modernc.org/sqlite"

	"remna-control/ent"
	"remna-control/ent/apitoken"
	"remna-control/ent/ledgerentry"
	"remna-control/ent/migrate"
	"remna-control/ent/referralaccrual"
)

func init() {
	// Ent's SQLite dialect expects the driver registered as "sqlite3"
	// (mattn's name); modernc registers itself as "sqlite".
	sql.Register("sqlite3", &sqlite.Driver{})
}

// Open opens (creating if needed) the SQLite file at path and migrates the
// schema. Additive auto-migration on every boot is fine at this scale.
func Open(ctx context.Context, path string) (*ent.Client, error) {
	client, _, err := OpenDB(ctx, path)
	return client, err
}

// OpenDB is Open that also returns the underlying connection pool, for
// table-level work Ent doesn't cover (backups).
func OpenDB(ctx context.Context, path string) (*ent.Client, *sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	// _time_format=sqlite writes times as "2006-01-02 15:04:05.999999999-07:00".
	// The driver's default is time.String(), which it can't parse back for
	// a zone without a name (an offset that isn't the local one, e.g. a panel
	// date in +03:00). Rows written the old way are still read.
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_time_format=sqlite"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	// SQLite allows one writer; a single connection avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	if err := client.Schema.Create(ctx, migrate.WithDropIndex(true)); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("run schema migration: %w", err)
	}
	if err := dropRemoved(ctx, db); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("drop removed schema: %w", err)
	}
	if err := backfillTokenUUIDs(ctx, client); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("backfill token uuids: %w", err)
	}
	if err := creditReferralAccruals(ctx, client); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("credit referral accruals: %w", err)
	}
	return client, db, nil
}

// dropRemoved deletes what the schema no longer has: auto-migration only
// adds. Per-inbound metering was replaced by squads.
func dropRemoved(ctx context.Context, db *sql.DB) error {
	for _, t := range []string{"inbound_counters", "inbound_traffics"} {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+t); err != nil {
			return err
		}
	}
	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM pragma_table_info('expense_items') WHERE name = 'rw_inbound_tag'").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		_, err := db.ExecContext(ctx, "ALTER TABLE expense_items DROP COLUMN rw_inbound_tag")
		return err
	}
	return nil
}

// backfillTokenUUIDs gives a UUID to API tokens made before they had one.
func backfillTokenUUIDs(ctx context.Context, client *ent.Client) error {
	ts, err := client.APIToken.Query().Where(apitoken.Or(apitoken.UUIDIsNil(), apitoken.UUID(""))).All(ctx)
	if err != nil {
		return err
	}
	for _, t := range ts {
		if err := client.APIToken.UpdateOne(t).SetUUID(uuid.NewString()).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// WithTx runs fn in a transaction, rolling back on error or panic.
func WithTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()
	if err := fn(tx); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			err = fmt.Errorf("%w: rolling back: %v", err, rerr)
		}
		return err
	}
	return tx.Commit()
}

// creditReferralAccruals puts accruals recorded before referrals reached the
// balance onto their referrers' balances; credited ones are skipped.
func creditReferralAccruals(ctx context.Context, client *ent.Client) error {
	accruals, err := client.ReferralAccrual.Query().
		Where(referralaccrual.StatusEQ(referralaccrual.StatusAccrued)).
		WithPayment(func(q *ent.PaymentQuery) { q.WithCustomer() }).
		All(ctx)
	if err != nil || len(accruals) == 0 {
		return err
	}
	return WithTx(ctx, client, func(tx *ent.Tx) error {
		for _, a := range accruals {
			note := fmt.Sprintf("%g%%", a.Percent)
			if p := a.Edges.Payment; p != nil && p.Edges.Customer != nil {
				note = fmt.Sprintf("%s (%g%%)", p.Edges.Customer.Name, a.Percent)
			}
			if err := tx.LedgerEntry.Create().
				SetCustomerID(a.ReferrerID).SetType(ledgerentry.TypeReferral).SetAmount(a.Amount).
				SetDate(a.Date).SetPaymentID(a.PaymentID).SetNote(note).
				Exec(ctx); err != nil {
				return err
			}
			if err := tx.ReferralAccrual.UpdateOne(a).SetStatus(referralaccrual.StatusCredited).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
