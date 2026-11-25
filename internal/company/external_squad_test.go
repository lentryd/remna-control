package company

import (
	"slices"
	"testing"

	"remna-control/ent"
	"remna-control/ent/companymember"
	"remna-control/ent/membersegment"
)

func sp(s string) *string { return &s }

// extOf is the external squad the fake panel holds for a username ("" = none).
func (f *fixture) extOf(t *testing.T, name string) string {
	t.Helper()
	u := f.panel.byName(name)
	if u == nil {
		t.Fatalf("no panel user %q", name)
	}
	if u.ExternalSquadUUID == nil {
		return ""
	}
	return *u.ExternalSquadUUID
}

func (f *fixture) segExt(t *testing.T, username string) *string {
	t.Helper()
	m := f.db.CompanyMember.Query().Where(companymember.Username(username)).OnlyX(f.ctx)
	return f.db.MemberSegment.Query().Where(membersegment.MemberID(m.ID), membersegment.ToDateIsNil()).OnlyX(f.ctx).ExternalSquadUUID
}

func eq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestResolveExternalSquadChain(t *testing.T) {
	tariff := func(sq *string) *ent.Tariff { return &ent.Tariff{ExternalSquadUUID: sq} }
	co := func(sq *string) *ent.Company { return &ent.Company{ExternalSquadUUID: sq} }
	cases := []struct {
		name      string
		t, c      *string
		current   *string
		overrides []string
		want      *string
	}{
		{"nothing set", nil, nil, nil, nil, nil},
		{"tariff only", sp("T"), nil, nil, nil, sp("T")},
		{"company beats tariff", sp("T"), sp("C"), nil, nil, sp("C")},
		{"company without tariff", nil, sp("C"), nil, nil, sp("C")},
		{"employee beats company", sp("T"), sp("C"), sp("E"), []string{fieldExternalSquad}, sp("E")},
		{"employee none", sp("T"), sp("C"), sp(""), []string{fieldExternalSquad}, sp("")},
		{"stale current ignored without override", sp("T"), nil, sp("old"), nil, sp("T")},
	}
	for _, c := range cases {
		got := Resolve(tariff(c.t), co(c.c), Values{ExternalSquad: c.current}, c.overrides)
		if !eq(got.ExternalSquad, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got.ExternalSquad, c.want)
		}
	}
}

func TestAddMemberUsesCompanyThenTariffSquad(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetExternalSquadUUID("t-ext").ExecX(f.ctx)
	if _, _, err := f.svc.AddMember(f.ctx, f.company.ID, "viatariff", "", nil); err != nil {
		t.Fatal(err)
	}
	if got := f.extOf(t, "viatariff"); got != "t-ext" {
		t.Errorf("tariff squad = %q", got)
	}
	f.db.Company.UpdateOneID(f.company.ID).SetExternalSquadUUID("c-ext").ExecX(f.ctx)
	if _, _, err := f.svc.AddMember(f.ctx, f.company.ID, "viacompany", "", nil); err != nil {
		t.Fatal(err)
	}
	if got := f.extOf(t, "viacompany"); got != "c-ext" {
		t.Errorf("company squad = %q", got)
	}
	if got := f.segExt(t, "viacompany"); !eq(got, sp("c-ext")) {
		t.Errorf("segment squad = %v", got)
	}
}

func TestSetExternalSquadPropagatesToInheritingMembersOnly(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetExternalSquadUUID("t-ext").ExecX(f.ctx)
	for _, n := range []string{"plain", "own"} {
		if _, _, err := f.svc.AddMember(f.ctx, f.company.ID, n, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	pinnedTariff := f.tariff2.ID
	if _, _, err := f.svc.AddMember(f.ctx, f.company.ID, "pinned", "", &pinnedTariff); err != nil {
		t.Fatal(err)
	}
	own := f.db.CompanyMember.Query().Where(companymember.Username("own")).OnlyX(f.ctx)
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, own.ID, nil, Overrides{ExternalSquad: sp("own-ext")}); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.SetExternalSquad(f.ctx, f.company.ID, sp("c-ext")); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"plain": "c-ext", "pinned": "c-ext", "own": "own-ext"} {
		if got := f.extOf(t, name); got != want {
			t.Errorf("%s: panel squad = %q, want %q", name, got, want)
		}
	}
	if got := f.segExt(t, "plain"); !eq(got, sp("c-ext")) {
		t.Errorf("plain segment squad = %v", got)
	}

	// Dropping the company squad falls back to the tariff, and to an
	// explicit clear where the tariff has none.
	if err := f.svc.SetExternalSquad(f.ctx, f.company.ID, nil); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"plain": "t-ext", "pinned": "", "own": "own-ext"} {
		if got := f.extOf(t, name); got != want {
			t.Errorf("after drop %s: panel squad = %q, want %q", name, got, want)
		}
	}
}

func TestSetOverridesExternalSquadNoneAndReset(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetExternalSquadUUID("t-ext").ExecX(f.ctx)
	m, _, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, nil, Overrides{ExternalSquad: sp("")}); err != nil {
		t.Fatal(err)
	}
	if got := f.extOf(t, "ivanov"); got != "" {
		t.Errorf("explicit none left %q in the panel", got)
	}
	if got := f.segExt(t, "ivanov"); !eq(got, sp("")) {
		t.Errorf("segment = %v, want explicit empty", got)
	}
	seg := f.db.MemberSegment.Query().Where(membersegment.MemberID(m.ID), membersegment.ToDateIsNil()).OnlyX(f.ctx)
	if !slices.Contains(seg.Overrides, fieldExternalSquad) {
		t.Errorf("overrides = %v", seg.Overrides)
	}
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, nil, Overrides{Reset: []string{fieldExternalSquad}}); err != nil {
		t.Fatal(err)
	}
	if got := f.extOf(t, "ivanov"); got != "t-ext" {
		t.Errorf("after reset panel = %q, want the tariff's", got)
	}
}

func TestSyncMembersPanelFixesExternalSquadDrift(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetExternalSquadUUID("t-ext").ExecX(f.ctx)
	m, _, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.panel.byName("ivanov").ExternalSquadUUID = sp("manual")
	f.db.RwUser.UpdateOneID(*m.RwUserID).SetExternalSquadUUID("manual").ExecX(f.ctx)
	if _, err := f.svc.SyncMembersPanel(f.ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.extOf(t, "ivanov"); got != "t-ext" {
		t.Errorf("drift not fixed: %q", got)
	}

	// A tariff with no squad leaves a hand-set panel value alone.
	f.db.Tariff.UpdateOneID(f.tariff.ID).ClearExternalSquadUUID().ExecX(f.ctx)
	f.db.MemberSegment.Update().ClearExternalSquadUUID().ExecX(f.ctx)
	f.panel.byName("ivanov").ExternalSquadUUID = sp("manual")
	f.db.RwUser.UpdateOneID(*m.RwUserID).SetExternalSquadUUID("manual").ExecX(f.ctx)
	if _, err := f.svc.SyncMembersPanel(f.ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.extOf(t, "ivanov"); got != "manual" {
		t.Errorf("an unmanaged squad was overwritten: %q", got)
	}
}

func TestTariffUpdatedSquadOnlyKeepsTheSegment(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetExternalSquadUUID("t-1").ExecX(f.ctx)
	m, _, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetExternalSquadUUID("t-2").ExecX(f.ctx)
	if err := f.svc.TariffUpdated(f.ctx, f.tariff.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(f.segments(t, m.ID)); n != 1 {
		t.Errorf("%d segments after a squad-only change, want 1", n)
	}
	if got := f.segExt(t, "ivanov"); !eq(got, sp("t-2")) {
		t.Errorf("segment squad = %v", got)
	}
	if got := f.extOf(t, "ivanov"); got != "t-2" {
		t.Errorf("panel squad = %q", got)
	}
	// A price change still splits the segment.
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetMonthlyPrice(60000).ExecX(f.ctx)
	if err := f.svc.TariffUpdated(f.ctx, f.tariff.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(f.segments(t, m.ID)); n != 2 {
		t.Errorf("%d segments after a price change, want 2", n)
	}
}

func TestSetOverridesTrimsExternalSquad(t *testing.T) {
	f := setup(t)
	m, _, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, nil, Overrides{ExternalSquad: sp("  own  ")}); err != nil {
		t.Fatal(err)
	}
	if got := f.segExt(t, "ivanov"); !eq(got, sp("own")) {
		t.Errorf("stored %v, want trimmed own", got)
	}
	if got := f.extOf(t, "ivanov"); got != "own" {
		t.Errorf("panel %q", got)
	}
	// Blank means explicitly none, not a blank uuid.
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, m.ID, nil, Overrides{ExternalSquad: sp("   ")}); err != nil {
		t.Fatal(err)
	}
	if got := f.segExt(t, "ivanov"); !eq(got, sp("")) {
		t.Errorf("blank stored as %v, want explicit empty", got)
	}
}

func TestReinstateClearsASquadThatWasDropped(t *testing.T) {
	f := setup(t)
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetExternalSquadUUID("t-ext").ExecX(f.ctx)
	m, _, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RemoveMember(f.ctx, f.company.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	f.db.Tariff.UpdateOneID(f.tariff.ID).ClearExternalSquadUUID().ExecX(f.ctx)
	f.at(2026, 11, 3)
	if _, _, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil); err != nil {
		t.Fatal(err)
	}
	if got := f.extOf(t, "ivanov"); got != "" {
		t.Errorf("a dropped squad stayed in the panel: %q", got)
	}
}

func TestUpdateSettingsSavesEverythingAndPropagates(t *testing.T) {
	f := setup(t)
	if _, _, err := f.svc.AddMember(f.ctx, f.company.ID, "ivanov", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UpdateSettings(f.ctx, f.company.ID, "New", "notes", 5, sp(" c-ext ")); err != nil {
		t.Fatal(err)
	}
	c := f.db.Company.GetX(f.ctx, f.company.ID)
	if c.Name != "New" || c.Notes != "notes" || c.ReportDay != 5 || !eq(c.ExternalSquadUUID, sp("c-ext")) {
		t.Errorf("company = %+v", c)
	}
	if got := f.extOf(t, "ivanov"); got != "c-ext" {
		t.Errorf("panel = %q", got)
	}
	// Nothing is saved when any part is invalid.
	if err := f.svc.UpdateSettings(f.ctx, f.company.ID, "Other", "", 40, sp("x")); err == nil {
		t.Fatal("report day 40 accepted")
	}
	c = f.db.Company.GetX(f.ctx, f.company.ID)
	if c.Name != "New" || !eq(c.ExternalSquadUUID, sp("c-ext")) {
		t.Errorf("a rejected update changed the company: %+v", c)
	}
}
