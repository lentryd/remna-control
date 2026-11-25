package company

import (
	"errors"
	"testing"
	"time"

	"remna-control/ent/companymember"
	"remna-control/ent/membersegment"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/remnawave"
)

func (f *fixture) segments(t *testing.T, memberID int) []segmentRow {
	t.Helper()
	var out []segmentRow
	for _, s := range f.db.MemberSegment.Query().Where(membersegment.MemberID(memberID)).Order(membersegment.ByID()).AllX(f.ctx) {
		out = append(out, segmentRow{from: s.FromDate, to: s.ToDate, price: s.Price, tariff: s.TariffID, overrides: s.Overrides})
	}
	return out
}

type segmentRow struct {
	from      time.Time
	to        *time.Time
	price     int64
	tariff    int
	overrides []string
}

func TestAddMemberCreatesPanelUserWithUnlimitedExpiry(t *testing.T) {
	f := setup(t)
	m, created, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "Ivan", nil)
	if err != nil || !created {
		t.Fatalf("AddMember = %v, created=%v", err, created)
	}
	u := f.panel.byName("ivanov")
	if u == nil || u.ExpireAt.Year() != 2099 || u.Status != remnawave.StatusActive {
		t.Fatalf("panel user = %+v", u)
	}
	if u.TrafficLimitBytes != 100 || len(u.ActiveInternalSquads) != 1 || u.ActiveInternalSquads[0].UUID != "sq-main" {
		t.Errorf("panel params = %+v", u)
	}
	if m.RwUserID == nil || *m.RwUserID != u.ID {
		t.Errorf("member rw_user_id = %v, want %d", m.RwUserID, u.ID)
	}
	segs := f.segments(t, m.ID)
	if len(segs) != 1 || segs[0].to != nil || segs[0].price != 50000 || len(segs[0].overrides) != 0 {
		t.Errorf("segments = %+v", segs)
	}
	if !segs[0].from.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("segment starts %s, want the clock's calendar day", segs[0].from)
	}
}

func TestAddMemberRejectsBadUsernameAndForeignUsername(t *testing.T) {
	f := setup(t)
	if _, _, err := f.svc.AddMember(f.ctx, f.company.ID, "a b", "", nil); err == nil {
		t.Error("username with a space accepted")
	}
	other := f.newCompany("Other", f.tariff.ID, 1)
	if _, _, err := f.svc.AddMember(f.ctx, other.ID, "ivanov", "", nil); err != nil {
		t.Fatal(err)
	}
	_, _, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "company.username_taken" {
		t.Errorf("foreign username error = %v", err)
	}
}

func TestAddMemberIsIdempotent(t *testing.T) {
	f := setup(t)
	a, _, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, created, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "other label", nil)
	if err != nil || created || b.ID != a.ID {
		t.Fatalf("second add = %v created=%v id=%d want %d", err, created, b.ID, a.ID)
	}
	if f.panel.creates != 1 || f.db.MemberSegment.Query().CountX(f.ctx) != 1 {
		t.Errorf("panel creates = %d, segments = %d", f.panel.creates, f.db.MemberSegment.Query().CountX(f.ctx))
	}
}

func TestAddRetryAfterPanelCreateSucceededButDBFailed(t *testing.T) {
	f := setup(t)
	f.panel.users[1] = &remnawave.User{ID: 1, Username: "ivanov", Status: remnawave.StatusActive, ExpireAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}
	f.panel.next = 1
	audit.Log(f.ctx, f.db, "rw.create_user", "company", f.company.ID, map[string]any{"username": "ivanov"}, nil) // the first attempt's footprint
	m, created, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	if err != nil || !created {
		t.Fatalf("AddMember = %v created=%v", err, created)
	}
	if f.panel.creates != 0 || m.RwUserID == nil || *m.RwUserID != 1 {
		t.Errorf("creates = %d, rw_user_id = %v: the existing panel user must be linked", f.panel.creates, m.RwUserID)
	}
}

func TestRemoveDisablesPanelUserAndClosesSegment(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	f.at(2026, 11, 10)
	if err := f.svc.RemoveMember(f.ctx, f.company.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	if u := f.panel.byName("ivanov"); u == nil || u.Status != remnawave.StatusDisabled {
		t.Errorf("panel user = %+v, want DISABLED and still present", u)
	}
	segs := f.segments(t, m.ID)
	if segs[0].to == nil || !segs[0].to.Equal(time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("segment closed at %v", segs[0].to)
	}
	got := f.db.CompanyMember.GetX(f.ctx, m.ID)
	if got.RemovedAt == nil {
		t.Error("removed_at not set")
	}
	if err := f.svc.RemoveMember(f.ctx, f.company.ID, m.ID); err != nil {
		t.Errorf("removing twice must be a no-op, got %v", err)
	}
}

func TestRemovePanelFailureClosesNothing(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	f.panel.failDisable = true
	f.at(2026, 11, 10)
	if err := f.svc.RemoveMember(f.ctx, f.company.ID, m.ID); err == nil {
		t.Fatal("expected the panel error")
	}
	if segs := f.segments(t, m.ID); segs[0].to != nil {
		t.Errorf("segment closed although the panel call failed: %+v", segs[0])
	}
	if f.db.CompanyMember.GetX(f.ctx, m.ID).RemovedAt != nil {
		t.Error("removed_at set although the panel call failed")
	}
}

func TestRemoveProtectedIs409(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "boss", "", nil)
	if err := f.svc.SetProtected(f.ctx, f.company.ID, m.ID, true); err != nil {
		t.Fatal(err)
	}
	err := f.svc.RemoveMember(f.ctx, f.company.ID, m.ID)
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "company.member_protected" || ae.Status != 409 {
		t.Fatalf("error = %v", err)
	}
	if u := f.panel.byName("boss"); u.Status != remnawave.StatusActive {
		t.Error("protected member was disabled in the panel")
	}
}

func TestForeignMemberIsNotFound(t *testing.T) {
	f := setup(t)
	other := f.newCompany("Other", f.tariff.ID, 1)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	err := f.svc.RemoveMember(f.ctx, other.ID, m.ID)
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Errorf("error = %v, want 404", err)
	}
}

func TestSameDayAddRemoveCountsZero(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	if err := f.svc.RemoveMember(f.ctx, f.company.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	s := f.segments(t, m.ID)[0]
	p := Period{From: f.company.PeriodStart, To: f.company.PeriodEnd}
	if got := DaysIn(p, s.from, s.to, f.loc); got != 0 {
		t.Errorf("same-day add and remove = %d days, want 0", got)
	}
}

func TestReAddOnRemovalDayOpensNewSegment(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	f.at(2026, 11, 10)
	_ = f.svc.RemoveMember(f.ctx, f.company.ID, m.ID)
	again, created, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	if err != nil || !created || again.ID != m.ID {
		t.Fatalf("re-add = %v created=%v id=%d want %d", err, created, again.ID, m.ID)
	}
	segs := f.segments(t, m.ID)
	if len(segs) != 2 || segs[1].to != nil || !segs[1].from.Equal(segs[0].to.UTC()) {
		t.Errorf("segments = %+v", segs)
	}
	if u := f.panel.byName("ivanov"); u.Status != remnawave.StatusActive {
		t.Errorf("panel status = %s, want ACTIVE", u.Status)
	}
	if f.db.CompanyMember.Query().Where(companymember.Username("ivanov")).CountX(f.ctx) != 1 {
		t.Error("re-adding must reuse the member row")
	}
	if f.db.CompanyMember.GetX(f.ctx, m.ID).RemovedAt != nil {
		t.Error("removed_at must be cleared")
	}
}

func TestSetOverridesClosesAndOpensSegment(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	f.at(2026, 11, 15)
	price := int64(35000)
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, nil, Overrides{Price: &price}); err != nil {
		t.Fatal(err)
	}
	segs := f.segments(t, m.ID)
	if len(segs) != 2 {
		t.Fatalf("segments = %+v", segs)
	}
	day15 := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	if segs[0].to == nil || !segs[0].to.Equal(day15) || segs[0].price != 50000 {
		t.Errorf("old segment = %+v", segs[0])
	}
	if segs[1].to != nil || !segs[1].from.Equal(day15) || segs[1].price != 35000 || len(segs[1].overrides) != 1 || segs[1].overrides[0] != "price" {
		t.Errorf("new segment = %+v", segs[1])
	}
	// No change: no new segment.
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, nil, Overrides{Price: &price}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.segments(t, m.ID)); n != 2 {
		t.Errorf("a no-op override made a segment: %d", n)
	}
	// Reset returns the field to the tariff.
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, nil, Overrides{Reset: []string{"price"}}); err != nil {
		t.Fatal(err)
	}
	last := f.segments(t, m.ID)[2]
	if last.price != 50000 || len(last.overrides) != 0 {
		t.Errorf("after reset = %+v", last)
	}
}

func TestSetOverridesTariffAndPanelParams(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	f.at(2026, 11, 15)
	limit := int64(777)
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, &f.tariff2.ID, Overrides{TrafficLimitBytes: &limit}); err != nil {
		t.Fatal(err)
	}
	last := f.segments(t, m.ID)[1]
	if last.tariff != f.tariff2.ID || last.price != 80000 {
		t.Errorf("segment = %+v", last)
	}
	if !f.db.CompanyMember.GetX(f.ctx, m.ID).TariffPinned {
		t.Error("a tariff different from the company's must pin the member")
	}
	u := f.panel.byName("ivanov")
	if u.TrafficLimitBytes != 777 || u.ActiveInternalSquads[0].UUID != "sq-pro" {
		t.Errorf("panel user = %+v", u)
	}
}

func TestSetOverridesPanelFailureChangesNothing(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	f.panel.failPatch = true
	f.at(2026, 11, 15)
	price := int64(1)
	limit := int64(5)
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, nil, Overrides{Price: &price, TrafficLimitBytes: &limit}); err == nil {
		t.Fatal("expected the panel error")
	}
	if n := len(f.segments(t, m.ID)); n != 1 {
		t.Errorf("segments = %d, want the original only", n)
	}
}

func TestResolveInheritsUnlisted(t *testing.T) {
	f := setup(t)
	cur := Values{Price: 35000, TrafficLimitBytes: 999, TrafficStrategy: "DAY", Squads: []string{"x"}}
	got := Resolve(f.tariff, nil, cur, []string{"price"})
	if got.Price != 35000 {
		t.Errorf("price = %d, the override must be kept", got.Price)
	}
	if got.TrafficLimitBytes != 100 || got.TrafficStrategy != "MONTH" || got.Squads[0] != "sq-main" || got.HwidLimit == nil || *got.HwidLimit != 3 {
		t.Errorf("inherited fields = %+v", got)
	}
}

func TestSetLabelAndProtected(t *testing.T) {
	f := setup(t)
	m, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	if err := f.svc.SetLabel(f.ctx, f.company.ID, m.ID, "Ivan I."); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetProtected(f.ctx, f.company.ID, m.ID, true); err != nil {
		t.Fatal(err)
	}
	got := f.db.CompanyMember.GetX(f.ctx, m.ID)
	if got.Label != "Ivan I." || !got.Protected {
		t.Errorf("member = %+v", got)
	}
}
