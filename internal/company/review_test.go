package company

import (
	"errors"
	"sync"
	"testing"
	"time"

	"remna-control/ent"
	"remna-control/ent/companystatement"
	"remna-control/ent/membersegment"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/remnawave"
)

func codeOf(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func openSegments(f *fixture, memberID int) int {
	return f.db.MemberSegment.Query().Where(membersegment.MemberID(memberID), membersegment.ToDateIsNil()).CountX(f.ctx)
}

func TestConcurrentReAddKeepsOneOpenSegment(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "bob", "", nil)
	f.at(2026, 11, 10)
	if err := f.svc.RemoveMember(f.ctx, f.company.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "bob", "", nil)
		}()
	}
	wg.Wait()
	if n := openSegments(f, m.ID); n != 1 {
		t.Errorf("open segments = %d, want exactly 1 (double billing)", n)
	}
}

func TestStaleSegmentCannotBeMovedAfterRemoval(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "bob", "", nil)
	stale := f.svc.openSegmentOf(t, f, m.ID)
	f.at(2026, 11, 10)
	if err := f.svc.RemoveMember(f.ctx, f.company.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	// SetOverrides read the segment before the removal landed.
	err := f.svc.moveSegment(f.ctx, stale, f.tariff, ValuesOf(f.tariff2, nil))
	if !errors.Is(err, errStale) {
		t.Fatalf("moveSegment on a closed member = %v, want errStale", err)
	}
	if n := openSegments(f, m.ID); n != 0 {
		t.Errorf("a removed member got an open segment: %d", n)
	}
}

func (s *Service) openSegmentOf(t *testing.T, f *fixture, memberID int) *ent.MemberSegment {
	t.Helper()
	seg, err := s.openSegment(f.ctx, memberID)
	if err != nil {
		t.Fatal(err)
	}
	return seg
}

func TestClosePeriodBeforeItEndsIsRefused(t *testing.T) {
	f := setup(t)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.at(2026, 11, 10)
	_, err := f.svc.ClosePeriod(f.ctx, f.company.ID, nil)
	if codeOf(err) != "company.period_not_ended" {
		t.Fatalf("error = %v", err)
	}
	if len(f.statements(t)) != 0 || f.db.LedgerEntry.Query().CountX(f.ctx) != 0 {
		t.Error("an early close wrote rows")
	}
}

func TestAddFinishesItsOwnHalfDoneCreateAndMakesItUsable(t *testing.T) {
	f := setup(t)
	f.panel.users[1] = &remnawave.User{ID: 1, Username: "hand", Status: remnawave.StatusDisabled, ExpireAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	f.panel.next = 1
	audit.Log(f.ctx, f.db, "rw.create_user", "company", f.company.ID, map[string]any{"username": "hand"}, nil)
	if _, _, err := f.svc.AddMember(f.ctx, f.company.ID, "hand", "", nil); err != nil {
		t.Fatal(err)
	}
	u := f.panel.byName("hand")
	if u.Status != remnawave.StatusActive || u.ExpireAt.Year() != 2099 {
		t.Errorf("finished user = %s until %s, want ACTIVE and unlimited", u.Status, u.ExpireAt)
	}
}

func TestAddRefusesAPanelUserThisCompanyDidNotCreate(t *testing.T) {
	f := setup(t)
	expiry := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	f.panel.users[1] = &remnawave.User{ID: 1, Username: "alice", Status: remnawave.StatusDisabled, ExpireAt: expiry}
	f.panel.next = 1
	_, _, err := f.svc.AddMember(f.ctx, f.company.ID, "alice", "", nil)
	if codeOf(err) != "company.panel_user_exists" {
		t.Fatalf("error = %v", err)
	}
	if u := f.panel.byName("alice"); u.Status != remnawave.StatusDisabled || !u.ExpireAt.Equal(expiry) {
		t.Errorf("someone else's panel user was touched: %+v", u)
	}
	if f.db.CompanyMember.Query().CountX(f.ctx) != 0 {
		t.Error("a member was created")
	}
}

func TestReinstateRollsBackThePanelWhenTheDatabaseFails(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "bob", "", nil)
	f.at(2026, 11, 10)
	_ = f.svc.RemoveMember(f.ctx, f.company.ID, m.ID)
	// Break the write: a second open segment is impossible, so make the
	// reinstating transaction fail by leaving an open one behind.
	f.db.MemberSegment.Create().SetMemberID(m.ID).SetFromDate(f.clock).SetTariffID(f.tariff.ID).SetPrice(1).
		SetTrafficLimitBytes(0).SetTrafficStrategy("MONTH").SaveX(f.ctx)
	if _, _, err := f.svc.AddMember(f.ctx, f.company.ID, "bob", "", nil); err == nil {
		t.Fatal("expected the transaction to fail")
	}
	if u := f.panel.byName("bob"); u.Status != remnawave.StatusDisabled {
		t.Errorf("panel user = %s after a failed reinstate, want DISABLED again (no free service)", u.Status)
	}
}

func TestSetOverridesValidates(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "bob", "", nil)
	neg, negHwid, zero := int64(-1), -1, int64(0)
	for name, o := range map[string]Overrides{
		"negative price":   {Price: &neg},
		"negative traffic": {TrafficLimitBytes: &neg},
		"negative hwid":    {HwidLimit: &negHwid},
		"empty squads":     {Squads: []string{}},
	} {
		if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, nil, o); codeOf(err) != "company.bad_override" {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, nil, Overrides{Price: &zero}); err != nil {
		t.Errorf("a free employee (price 0) must be allowed: %v", err)
	}
}

func TestReorderedSquadsAreNotAChange(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetSquadUuids([]string{"a", "b"}).ExecX(f.ctx)
	a, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.at(2026, 11, 20)
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetSquadUuids([]string{"b", "a"}).ExecX(f.ctx)
	if err := f.svc.TariffUpdated(f.ctx, f.tariff.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(f.segments(t, a.ID)); n != 1 {
		t.Errorf("segments = %d, a reorder of squads must not split billing", n)
	}
}

func TestArchiveStatementEndsAtTheArchiveDay(t *testing.T) {
	f := setup(t)
	_, _, _ = f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	f.at(2026, 11, 10)
	if _, err := f.svc.ArchiveCompany(f.ctx, f.company.ID); err != nil {
		t.Fatal(err)
	}
	st := f.statements(t)[0]
	day10 := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	if !st.PeriodTo.Equal(day10) {
		t.Errorf("final statement ends %s, want the archive day", st.PeriodTo)
	}
	if e := f.db.LedgerEntry.GetX(f.ctx, *st.LedgerEntryID); !e.Date.Equal(day10) {
		t.Errorf("ledger date = %s, want the archive day", e.Date)
	}
	if co := f.db.Company.GetX(f.ctx, f.company.ID); !co.PeriodStart.Equal(f.company.PeriodStart) {
		t.Errorf("an archived company rolled its period to %s", co.PeriodStart)
	}
}

func TestCloseDueSkipsAnArchivedCompanyWithoutError(t *testing.T) {
	f := setup(t)
	f.db.Company.UpdateOneID(f.company.ID).SetArchived(true).ExecX(f.ctx)
	f.at(2027, 3, 1)
	if _, err := f.svc.CloseEnded(f.ctx, f.company.ID); codeOf(err) != "company.archived" {
		t.Fatalf("closing an archived company = %v", err)
	}
	if res, err := f.svc.CloseDue(f.ctx); err != nil || res.Changed {
		t.Errorf("CloseDue = %+v, %v", res, err)
	}
}

func TestTransitionalPeriodWhenTheNewReportDayIsEarlier(t *testing.T) {
	f := setup(t)
	f.at(2027, 1, 25)
	co := f.newCompany("Early", f.tariff.ID, 20)
	m, _, _ := f.svc.AddMember(f.ctx, co.ID, "aaa", "", nil)
	if err := f.svc.UpdateCompany(f.ctx, co.ID, "Early", "", 10); err != nil {
		t.Fatal(err)
	}
	f.at(2027, 2, 20)
	if _, err := f.svc.CloseEnded(f.ctx, co.ID); err != nil {
		t.Fatal(err)
	}
	got := f.db.Company.GetX(f.ctx, co.ID)
	if !got.PeriodEnd.Equal(time.Date(2027, 3, 10, 0, 0, 0, 0, time.UTC)) || got.PeriodDays != 28 {
		t.Fatalf("period = [%s, %s) priced on %d days, want [Feb 20, Mar 10) on 28", got.PeriodStart, got.PeriodEnd, got.PeriodDays)
	}
	f.at(2027, 3, 10)
	if _, err := f.svc.CloseEnded(f.ctx, co.ID); err != nil {
		t.Fatal(err)
	}
	sts := f.db.CompanyStatement.Query().Where(companystatement.CompanyID(co.ID)).AllX(f.ctx)
	var last int64
	for _, s := range sts {
		if s.PeriodFrom.Equal(time.Date(2027, 2, 20, 0, 0, 0, 0, time.UTC)) {
			last = s.Total
		}
	}
	if last != 32143 { // 18 of 28 days of 500 ₽, from Feb 20 through Mar 9
		t.Errorf("short period total = %d, want 32143 (member %d)", last, m.ID)
	}
}

func TestStatementInBerlinAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tzdata")
	}
	f := setupIn(t, loc)
	f.at(2026, 10, 20)
	co := f.newCompany("Berlin", f.tariff.ID, 20) // [Oct 20, Nov 20), DST ends on Oct 25
	_, _, _ = f.svc.AddMember(f.ctx, co.ID, "aaa", "", nil)
	f.at(2026, 11, 20)
	if _, err := f.svc.CloseEnded(f.ctx, co.ID); err != nil {
		t.Fatal(err)
	}
	st := f.db.CompanyStatement.Query().Where(companystatement.CompanyID(co.ID)).OnlyX(f.ctx)
	if st.DaysInPeriod != 31 || st.Total != 50000 {
		t.Errorf("statement = %d days, total %d; a member on the whole period pays the full price", st.DaysInPeriod, st.Total)
	}
}
