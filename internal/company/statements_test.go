package company

import (
	"testing"
	"time"

	"remna-control/ent"
	"remna-control/ent/companystatement"
	"remna-control/ent/ledgerentry"
	"remna-control/ent/statementline"
	"remna-control/internal/remnawave"
)

func (f *fixture) lines(t *testing.T, statementID int) map[string][]int64 {
	t.Helper()
	out := map[string][]int64{}
	for _, l := range f.db.StatementLine.Query().Where(statementline.StatementID(statementID)).Order(statementline.ByID()).AllX(f.ctx) {
		out[l.Username] = append(out[l.Username], l.Amount)
	}
	return out
}

func (f *fixture) statements(t *testing.T) []*ent.CompanyStatement {
	t.Helper()
	return f.db.CompanyStatement.Query().Where(companystatement.CompanyID(f.company.ID)).Order(companystatement.ByPeriodFrom()).AllX(f.ctx)
}

func TestStatementProratesJoinerAndLeaver(t *testing.T) {
	f := setup(t) // report day 1, November: 30 days, 500 ₽ per month
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	c, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "ccc", "", nil)
	f.at(2026, 11, 19)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "bbb", "", nil)
	f.at(2026, 11, 25)
	if err := f.svc.RemoveMember(f.ctx, f.company.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	f.at(2026, 12, 1)

	st, err := f.svc.ClosePeriod(f.ctx, f.company.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := f.lines(t, st.ID)
	if len(got) != 3 || got["aaa"][0] != 50000 || got["bbb"][0] != 20000 || got["ccc"][0] != 40000 {
		t.Errorf("lines = %v, want aaa 50000, bbb 20000 (12 days), ccc 40000 (24 days)", got)
	}
	if st.Total != 110000 || st.DaysInPeriod != 30 {
		t.Errorf("statement = %+v", st)
	}
	co := f.db.Company.GetX(f.ctx, f.company.ID)
	if !co.PeriodStart.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) || !co.PeriodEnd.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) || co.PeriodDays != 31 {
		t.Errorf("next period = [%s, %s) %d days", co.PeriodStart, co.PeriodEnd, co.PeriodDays)
	}
}

func TestStatementPostsLedgerCharge(t *testing.T) {
	f := setup(t)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.at(2026, 12, 1)
	st, err := f.svc.ClosePeriod(f.ctx, f.company.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries := f.db.LedgerEntry.Query().Where(ledgerentry.CustomerID(f.company.CustomerID)).AllX(f.ctx)
	if len(entries) != 1 || entries[0].Type != ledgerentry.TypeCompanyCharge || entries[0].Amount != -50000 {
		t.Fatalf("ledger = %+v", entries)
	}
	if st.LedgerEntryID == nil || *st.LedgerEntryID != entries[0].ID {
		t.Errorf("statement ledger entry = %v, want %d", st.LedgerEntryID, entries[0].ID)
	}
	if !entries[0].Date.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ledger date = %s, want the report date", entries[0].Date)
	}
}

func TestTariffChangeMidPeriodTwoLines(t *testing.T) {
	f := setup(t)
	a, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.at(2026, 11, 16)
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, a.ID, &f.tariff2.ID, Overrides{}); err != nil {
		t.Fatal(err)
	}
	f.at(2026, 12, 1)
	st, err := f.svc.ClosePeriod(f.ctx, f.company.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := f.lines(t, st.ID)["aaa"]
	if len(got) != 2 || got[0] != 25000 || got[1] != 40000 || st.Total != 65000 {
		t.Errorf("lines = %v total %d, want 15 days at 500 ₽ then 15 days at 800 ₽", got, st.Total)
	}
}

func TestCloseDueTwiceCreatesOnce(t *testing.T) {
	f := setup(t)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.at(2026, 11, 30)
	if res, err := f.svc.CloseDue(f.ctx); err != nil || res.Changed {
		t.Fatalf("before the report date: %+v %v", res, err)
	}
	f.at(2026, 12, 1)
	res, err := f.svc.CloseDue(f.ctx)
	if err != nil || !res.Changed {
		t.Fatalf("on the report date: %+v %v", res, err)
	}
	if res, err = f.svc.CloseDue(f.ctx); err != nil || res.Changed {
		t.Errorf("second run: %+v %v", res, err)
	}
	if n := len(f.statements(t)); n != 1 {
		t.Errorf("statements = %d", n)
	}
	if n := f.db.LedgerEntry.Query().Where(ledgerentry.CustomerID(f.company.CustomerID)).CountX(f.ctx); n != 1 {
		t.Errorf("ledger entries = %d", n)
	}
}

func TestCloseDueCatchesUpSeveralPeriodsInOrder(t *testing.T) {
	f := setup(t)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.at(2027, 1, 15) // the service was down over two report dates
	if _, err := f.svc.CloseDue(f.ctx); err != nil {
		t.Fatal(err)
	}
	sts := f.statements(t)
	if len(sts) != 2 || !sts[0].PeriodFrom.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || !sts[1].PeriodFrom.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("statements = %+v", sts)
	}
	if sts[0].Total != 50000 || sts[1].Total != 50000 {
		t.Errorf("totals = %d, %d; a whole month each", sts[0].Total, sts[1].Total)
	}
}

func TestZeroTotalCreatesStatementWithoutLedgerEntry(t *testing.T) {
	f := setup(t)
	f.at(2026, 12, 1)
	st, err := f.svc.ClosePeriod(f.ctx, f.company.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 0 || st.LedgerEntryID != nil {
		t.Errorf("statement = %+v", st)
	}
	if n := f.db.LedgerEntry.Query().CountX(f.ctx); n != 0 {
		t.Errorf("ledger entries = %d", n)
	}
}

func TestAddedAfterPeriodEndIsNotBilledInIt(t *testing.T) {
	f := setup(t)
	f.at(2026, 12, 5) // the period closes late, the member joined after its end
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "late", "", nil)
	st, err := f.svc.ClosePeriod(f.ctx, f.company.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 0 || len(f.lines(t, st.ID)) != 0 {
		t.Errorf("statement = %+v", st)
	}
}

func TestAccruedAndForecastWriteNothing(t *testing.T) {
	f := setup(t)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.at(2026, 11, 10)
	acc, err := f.svc.Accrued(f.ctx, f.company.ID)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Total != 16667 || len(acc.Lines) != 1 || acc.Lines[0].Days != 10 {
		t.Errorf("accrued = %+v, want 10 days through today", acc)
	}
	fc, err := f.svc.Forecast(f.ctx, f.company.ID)
	if err != nil || fc.Total != 50000 {
		t.Errorf("forecast = %+v %v", fc, err)
	}
	if len(f.statements(t)) != 0 || f.db.LedgerEntry.Query().CountX(f.ctx) != 0 {
		t.Error("a preview wrote rows")
	}
}

func TestReportDayChangeAppliesToNextPeriod(t *testing.T) {
	f := setup(t)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.at(2026, 11, 10)
	if err := f.svc.UpdateCompany(f.ctx, f.company.ID, "Acme", "", 15); err != nil {
		t.Fatal(err)
	}
	f.at(2026, 12, 1)
	if _, err := f.svc.CloseDue(f.ctx); err != nil {
		t.Fatal(err)
	}
	co := f.db.Company.GetX(f.ctx, f.company.ID)
	if !co.PeriodEnd.Equal(time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)) || co.PeriodDays != 30 {
		t.Fatalf("period after the change = [%s, %s) priced on %d days", co.PeriodStart, co.PeriodEnd, co.PeriodDays)
	}
	if sts := f.statements(t); len(sts) != 1 || sts[0].Total != 50000 || sts[0].DaysInPeriod != 30 {
		t.Fatalf("november statement = %+v: the old report day closes the open period", sts)
	}
	f.at(2026, 12, 15)
	if _, err := f.svc.CloseDue(f.ctx); err != nil {
		t.Fatal(err)
	}
	sts := f.statements(t)
	if len(sts) != 2 || sts[1].Total != 23333 {
		t.Errorf("short period = %+v, want 14 of 30 days: 23333", sts)
	}
}

func TestArchiveCreatesFinalStatement(t *testing.T) {
	f := setup(t)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	boss, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "boss", "", nil)
	if err := f.svc.SetProtected(f.ctx, f.company.ID, boss.ID, true); err != nil {
		t.Fatal(err)
	}
	f.at(2026, 11, 10)
	kept, err := f.svc.ArchiveCompany(f.ctx, f.company.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0] != "boss" {
		t.Errorf("protected members left enabled = %v", kept)
	}
	sts := f.statements(t)
	if len(sts) != 1 || sts[0].Total != 30000 { // 9 days each, two members
		t.Fatalf("final statement = %+v", sts)
	}
	co := f.db.Company.GetX(f.ctx, f.company.ID)
	if !co.Archived || co.ArchivedAt == nil {
		t.Errorf("company = %+v", co)
	}
	if u := f.panel.byName("aaa"); u.Status != remnawave.StatusDisabled {
		t.Errorf("aaa = %s, want DISABLED", u.Status)
	}
	if u := f.panel.byName("boss"); u.Status != remnawave.StatusActive {
		t.Errorf("a protected member must stay as it is, got %s", u.Status)
	}
	f.at(2027, 3, 1)
	if res, err := f.svc.CloseDue(f.ctx); err != nil || res.Changed {
		t.Errorf("archived company still billed: %+v %v", res, err)
	}
	if _, _, err := f.svc.AddMember(f.ctx, f.company.ID, "new", "", nil); err == nil {
		t.Error("added a member to an archived company")
	}
}

func TestArchiveFailsWithoutChangesWhenPanelFails(t *testing.T) {
	f := setup(t)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.panel.failDisable = true
	f.at(2026, 11, 10)
	if _, err := f.svc.ArchiveCompany(f.ctx, f.company.ID); err == nil {
		t.Fatal("expected the panel error")
	}
	if len(f.statements(t)) != 0 || f.db.Company.GetX(f.ctx, f.company.ID).Archived {
		t.Error("a failed archive left changes behind")
	}
}

func TestPostedStatementIsNotRewrittenByTariffEdits(t *testing.T) {
	f := setup(t)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.at(2026, 12, 1)
	st, _ := f.svc.ClosePeriod(f.ctx, f.company.ID, nil)
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetMonthlyPrice(99900).ExecX(f.ctx)
	if err := f.svc.TariffUpdated(f.ctx, f.tariff.ID); err != nil {
		t.Fatal(err)
	}
	got := f.db.CompanyStatement.GetX(f.ctx, st.ID)
	if got.Total != 50000 || f.lines(t, st.ID)["aaa"][0] != 50000 {
		t.Errorf("posted statement changed: %+v", got)
	}
}
