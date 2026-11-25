package company

import (
	"errors"
	"testing"
	"time"

	"remna-control/ent/rwuser"
	"remna-control/ent/tariff"
	"remna-control/internal/apperr"
	"remna-control/internal/remnawave"
)

func TestCreateCompanyMakesAccountAndPeriod(t *testing.T) {
	f := setup(t)
	f.at(2026, 11, 20)
	c, err := f.svc.CreateCompany(f.ctx, "Globex", "big", f.tariff.ID, 15)
	if err != nil {
		t.Fatal(err)
	}
	acc := f.db.Customer.GetX(f.ctx, c.CustomerID)
	if acc.Name != "Globex" {
		t.Errorf("account = %+v", acc)
	}
	if !c.PeriodStart.Equal(time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)) || !c.PeriodEnd.Equal(time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("period = [%s, %s)", c.PeriodStart, c.PeriodEnd)
	}
}

func TestCreateCompanyValidation(t *testing.T) {
	f := setup(t)
	for _, day := range []int{0, 29, 31, -1} {
		_, err := f.svc.CreateCompany(f.ctx, "X", "", f.tariff.ID, day)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != "company.bad_report_day" {
			t.Errorf("report day %d: error = %v", day, err)
		}
	}
	addon := f.db.Addon.Create().SetName("white").SetPrefix("w_").SaveX(f.ctx)
	at := f.db.Tariff.Create().SetKind(tariff.KindAddon).SetAddonID(addon.ID).SetName("A").SetMonthlyPrice(1).SaveX(f.ctx)
	if _, err := f.svc.CreateCompany(f.ctx, "X", "", at.ID, 1); err == nil {
		t.Error("an add-on tariff was accepted")
	}
	if _, err := f.svc.CreateCompany(f.ctx, " ", "", f.tariff.ID, 1); err == nil {
		t.Error("empty name was accepted")
	}
}

func TestUpdateCompanyReportDayKeepsOpenPeriod(t *testing.T) {
	f := setup(t)
	before := f.db.Company.GetX(f.ctx, f.company.ID)
	if err := f.svc.UpdateCompany(f.ctx, f.company.ID, "Acme 2", "n", 10); err != nil {
		t.Fatal(err)
	}
	got := f.db.Company.GetX(f.ctx, f.company.ID)
	if got.ReportDay != 10 || got.Name != "Acme 2" || !got.PeriodStart.Equal(before.PeriodStart) || !got.PeriodEnd.Equal(before.PeriodEnd) {
		t.Errorf("company = %+v", got)
	}
	if f.db.Customer.GetX(f.ctx, got.CustomerID).Name != "Acme 2" {
		t.Error("account name did not follow")
	}
	if err := f.svc.UpdateCompany(f.ctx, f.company.ID, "Acme 2", "", 29); err == nil {
		t.Error("report day 29 accepted")
	}
}

func TestSetTariffMovesOnlyUnpinned(t *testing.T) {
	f := setup(t)
	a, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	b, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "bbb", "", &f.tariff2.ID)
	tariff3 := f.db.Tariff.Create().SetKind(tariff.KindBase).SetName("T3").SetMonthlyPrice(10000).
		SetTrafficLimitBytes(7).SetTrafficStrategy("MONTH").SetSquadUuids([]string{"sq-3"}).SaveX(f.ctx)
	f.at(2026, 11, 15)
	if err := f.svc.SetTariff(f.ctx, f.company.ID, tariff3.ID); err != nil {
		t.Fatal(err)
	}
	as := f.segments(t, a.ID)
	if len(as) != 2 || as[1].tariff != tariff3.ID || as[1].price != 10000 || as[0].to == nil {
		t.Errorf("unpinned member segments = %+v", as)
	}
	if u := f.panel.byName("aaa"); u.TrafficLimitBytes != 7 || u.ActiveInternalSquads[0].UUID != "sq-3" {
		t.Errorf("panel user = %+v", u)
	}
	bs := f.segments(t, b.ID)
	if len(bs) != 1 || bs[0].tariff != f.tariff2.ID {
		t.Errorf("pinned member segments = %+v", bs)
	}
	if f.db.Company.GetX(f.ctx, f.company.ID).TariffID != tariff3.ID {
		t.Error("company tariff not changed")
	}
}

func TestSetTariffUnpinsMemberWhoseTariffBecomesTheCompanys(t *testing.T) {
	f := setup(t)
	b, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "bbb", "", &f.tariff2.ID)
	f.at(2026, 11, 15)
	if err := f.svc.SetTariff(f.ctx, f.company.ID, f.tariff2.ID); err != nil {
		t.Fatal(err)
	}
	if f.db.CompanyMember.GetX(f.ctx, b.ID).TariffPinned {
		t.Error("the member should follow the company tariff now")
	}
	if n := len(f.segments(t, b.ID)); n != 1 {
		t.Errorf("segments = %d, nothing changed for this member's billing", n)
	}
}

func TestTariffUpdatedSplitsSegmentWithoutRewritingPast(t *testing.T) {
	f := setup(t)
	a, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	b, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "bbb", "", nil)
	price := int64(35000)
	if err := f.svc.SetOverrides(f.ctx, f.company.ID, b.ID, nil, Overrides{Price: &price}); err != nil {
		t.Fatal(err)
	}
	bBefore := len(f.segments(t, b.ID))

	f.at(2026, 11, 20)
	f.db.Tariff.UpdateOneID(f.tariff.ID).SetMonthlyPrice(60000).SetTrafficLimitBytes(200).ExecX(f.ctx)
	if err := f.svc.TariffUpdated(f.ctx, f.tariff.ID); err != nil {
		t.Fatal(err)
	}

	as := f.segments(t, a.ID)
	day20 := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	if len(as) != 2 || as[0].price != 50000 || as[0].to == nil || !as[0].to.Equal(day20) || as[1].price != 60000 || !as[1].from.Equal(day20) {
		t.Errorf("inheriting member segments = %+v", as)
	}
	bs := f.segments(t, b.ID)
	if len(bs) != bBefore+1 || bs[len(bs)-1].price != 35000 {
		t.Errorf("member with a price override: %+v (price must stay, traffic limit follows)", bs)
	}
	if u := f.panel.byName("aaa"); u.TrafficLimitBytes != 200 {
		t.Errorf("panel traffic limit = %d, want 200", u.TrafficLimitBytes)
	}
	// Nothing to change the second time.
	if err := f.svc.TariffUpdated(f.ctx, f.tariff.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(f.segments(t, a.ID)); n != 2 {
		t.Errorf("an unchanged tariff produced a segment: %d", n)
	}
}

func TestSyncMembersPanelPushesDriftOnly(t *testing.T) {
	f := setup(t)
	a, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "aaa", "", nil)
	gone, _, _ := f.svc.AddMember(f.ctx, f.company.ID, "gone", "", nil)
	if err := f.svc.RemoveMember(f.ctx, f.company.ID, gone.ID); err != nil {
		t.Fatal(err)
	}
	// The panel drifted: someone changed the limit by hand; the cache saw it.
	ua := f.panel.byName("aaa")
	ua.TrafficLimitBytes = 1
	f.db.RwUser.Update().Where(rwuser.ID(*a.RwUserID)).SetTrafficLimitBytes(1).ExecX(f.ctx)
	// A removed member stays disabled whatever its parameters.
	ug := f.panel.byName("gone")
	ug.TrafficLimitBytes = 1
	f.db.RwUser.Update().Where(rwuser.ID(*gone.RwUserID)).SetTrafficLimitBytes(1).ExecX(f.ctx)
	expiry := ua.ExpireAt

	res, err := f.svc.SyncMembersPanel(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Errorf("result = %+v, want Changed", res)
	}
	if ua.TrafficLimitBytes != 100 || !ua.ExpireAt.Equal(expiry) || ua.Status != remnawave.StatusActive {
		t.Errorf("active member = %+v", ua)
	}
	if ug.Status != remnawave.StatusDisabled || ug.TrafficLimitBytes != 1 {
		t.Errorf("removed member was touched: %+v", ug)
	}
	res, err = f.svc.SyncMembersPanel(f.ctx)
	if err != nil || res.Changed {
		t.Errorf("second run = %+v, %v; nothing to do", res, err)
	}
}
