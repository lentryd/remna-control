package backup

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"remna-control/ent"
	"remna-control/ent/migrate"
	"remna-control/ent/payment"
	"remna-control/ent/tariff"
	"remna-control/internal/store"
)

func TestCategoriesCoverSchema(t *testing.T) {
	seen := map[string]string{}
	for _, c := range Categories {
		for _, tb := range c.Tables {
			if prev, dup := seen[tb]; dup {
				t.Errorf("table %s is in both %s and %s", tb, prev, c.Key)
			}
			seen[tb] = c.Key
		}
		for _, d := range c.DependsOn {
			if _, ok := categoryByKey(d); !ok {
				t.Errorf("%s depends on unknown category %s", c.Key, d)
			}
		}
	}
	excluded := map[string]bool{}
	for _, tb := range Excluded {
		excluded[tb] = true
		if _, ok := seen[tb]; ok {
			t.Errorf("excluded table %s is also in category %s", tb, seen[tb])
		}
		if !knownTables()[tb] {
			t.Errorf("excluded table %s doesn't exist in the schema", tb)
		}
	}
	for _, tb := range migrate.Tables {
		if _, ok := seen[tb.Name]; !ok && !excluded[tb.Name] {
			t.Errorf("table %s is not in any backup category", tb.Name)
		}
	}
	for tb := range seen {
		if !knownTables()[tb] {
			t.Errorf("category table %s doesn't exist in the schema", tb)
		}
	}
}

func open(t *testing.T) (*ent.Client, *sql.DB) {
	t.Helper()
	client, db, err := store.OpenDB(context.Background(), filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, db
}

func seed(t *testing.T, c *ent.Client) {
	t.Helper()
	ctx := context.Background()
	ref := c.Customer.Create().SetName("Реферер").SaveX(ctx)
	cu := c.Customer.Create().SetName("Клиент").SetReferrerID(ref.ID).SaveX(ctx)
	tr := c.Tariff.Create().SetKind(tariff.KindBase).SetName("Base").SetMonthlyPrice(15000).
		SetSquadUuids([]string{"a", "b"}).SaveX(ctx)
	c.Subscription.Create().SetCustomerID(cu.ID).SetTariffID(tr.ID).SaveX(ctx)
	c.Payment.Create().SetCustomerID(cu.ID).SetAmount(30000).
		SetDate(time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)).SaveX(ctx)
}

func all() []string {
	keys := make([]string, len(Categories))
	for i, c := range Categories {
		keys[i] = c.Key
	}
	return keys
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	src, srcDB := open(t)
	seed(t, src)

	var buf bytes.Buffer
	m, err := Export(ctx, srcDB, &buf, all(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if m.Tables["customers"] != 2 || m.Tables["payments"] != 1 {
		t.Fatalf("unexpected counts %v", m.Tables)
	}

	dst, dstDB := open(t)
	dst.Customer.Create().SetName("будет удалён").SaveX(ctx)
	a, err := Open(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, dstDB, a, a.Available()); err != nil {
		t.Fatal(err)
	}

	cs := dst.Customer.Query().WithReferrer().AllX(ctx)
	if len(cs) != 2 || cs[1].Edges.Referrer == nil || cs[1].Edges.Referrer.ID != cs[0].ID {
		t.Fatalf("customers not restored: %+v", cs)
	}
	tr := dst.Tariff.Query().OnlyX(ctx)
	if len(tr.SquadUuids) != 2 {
		t.Fatalf("json column lost: %+v", tr.SquadUuids)
	}
	p := dst.Payment.Query().OnlyX(ctx)
	if !p.Date.Equal(time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)) || p.Amount != 30000 {
		t.Fatalf("payment mismatch: %+v", p)
	}
	// Date comparisons in SQL must still work on restored text.
	if n := dst.Payment.Query().Where(payment.DateGTE(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))).CountX(ctx); n != 1 {
		t.Fatalf("date filter found %d payments", n)
	}
	// Foreign keys are back on after the import.
	if _, err := dst.Payment.Create().SetCustomerID(9999).SetAmount(1).SetDate(time.Now()).Save(ctx); err == nil {
		t.Fatal("foreign keys left disabled")
	}
}

func TestPartialImportKeepsReferences(t *testing.T) {
	ctx := context.Background()
	c, db := open(t)
	seed(t, c)

	var buf bytes.Buffer
	if _, err := Export(ctx, db, &buf, []string{"tariffs"}, "test"); err != nil {
		t.Fatal(err)
	}
	a, err := Open(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, db, a, []string{"tariffs"}); err != nil {
		t.Fatal(err)
	}
	// Replacing tariffs must not trigger ON DELETE SET NULL on subscriptions.
	if s := c.Subscription.Query().OnlyX(ctx); s.TariffID == nil {
		t.Fatal("subscription lost its tariff")
	}
}

func TestImportRejectsDanglingReferences(t *testing.T) {
	ctx := context.Background()
	src, srcDB := open(t)
	seed(t, src)
	var buf bytes.Buffer
	if _, err := Export(ctx, srcDB, &buf, []string{"payments"}, "test"); err != nil {
		t.Fatal(err)
	}

	dst, dstDB := open(t)
	a, _ := Open(buf.Bytes())
	_, err := Import(ctx, dstDB, a, []string{"payments"})
	var ie *IntegrityError
	if !errors.As(err, &ie) {
		t.Fatalf("expected integrity error, got %v", err)
	}
	if n := dst.Payment.Query().CountX(ctx); n != 0 {
		t.Fatalf("import not rolled back: %d payments", n)
	}
}
