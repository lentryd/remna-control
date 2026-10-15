package expenses

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"remna-control/ent/expenseitem"
	"remna-control/internal/fx"
	"remna-control/internal/store"
)

func TestCreateAdvancesDueDate(t *testing.T) {
	ctx := context.Background()
	db, _, err := store.OpenDB(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db, nil, fx.New(db, func(context.Context) string { return "RUB" }), time.UTC)

	due := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	monthly := db.ExpenseItem.Create().SetName("VPS").SetCurrency("RUB").SetNextDueDate(due).SaveX(ctx)
	yearly := db.ExpenseItem.Create().SetName("Domain").SetCurrency("RUB").SetPeriod(expenseitem.PeriodYear).SetNextDueDate(due).SaveX(ctx)
	undated := db.ExpenseItem.Create().SetName("Misc").SetCurrency("RUB").SaveX(ctx)

	pay := func(itemID int, kind string, advance bool) {
		t.Helper()
		_, err := svc.Create(ctx, ExpenseInput{Date: due, ItemID: &itemID, Kind: kind, OrigAmount: 10000, AdvanceDue: advance})
		if err != nil {
			t.Fatal(err)
		}
	}
	dueOf := func(id int) *time.Time { return db.ExpenseItem.GetX(ctx, id).NextDueDate }

	pay(monthly.ID, "charge", true)
	if got := dueOf(monthly.ID); !got.Equal(due.AddDate(0, 1, 0)) {
		t.Errorf("monthly due = %s", got)
	}
	pay(yearly.ID, "charge", true)
	if got := dueOf(yearly.ID); !got.Equal(due.AddDate(1, 0, 0)) {
		t.Errorf("yearly due = %s", got)
	}
	// Refunds and unasked charges keep the date; undated items stay undated.
	pay(monthly.ID, "refund", true)
	pay(monthly.ID, "charge", false)
	if got := dueOf(monthly.ID); !got.Equal(due.AddDate(0, 1, 0)) {
		t.Errorf("monthly due moved again: %s", got)
	}
	pay(undated.ID, "charge", true)
	if got := dueOf(undated.ID); got != nil {
		t.Errorf("undated item got a date: %s", got)
	}
}
