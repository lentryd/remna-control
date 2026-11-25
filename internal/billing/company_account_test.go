package billing

import (
	"errors"
	"testing"
	"time"

	"remna-control/ent/ledgerentry"
	"remna-control/internal/apperr"
	"remna-control/internal/remnawave"
	"remna-control/internal/rwsync"
)

func TestPaymentOnCompanyAccountAccruesNoReferral(t *testing.T) {
	f := setup(t)
	referrer := f.db.Customer.Create().SetName("Ref").SetReferralPercent(10).SaveX(f.ctx)
	f.db.Customer.UpdateOneID(f.customer).SetReferrerID(referrer.ID).ExecX(f.ctx)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	f.db.Company.Create().SetName("Acme").SetCustomerID(f.customer).SetTariffID(f.plain).SetReportDay(1).
		SetPeriodStart(start).SetPeriodEnd(start.AddDate(0, 1, 0)).SetPeriodDays(31).SaveX(f.ctx)

	if _, err := f.svc.CommitPayment(f.ctx, PaymentInput{CustomerID: f.customer, Amount: 100000}); err != nil {
		t.Fatal(err)
	}
	if n := f.db.LedgerEntry.Query().Where(ledgerentry.TypeEQ(ledgerentry.TypeReferral)).CountX(f.ctx); n != 0 {
		t.Errorf("referral ledger entries = %d, want 0 for a company account", n)
	}
	if n := f.db.ReferralAccrual.Query().CountX(f.ctx); n != 0 {
		t.Errorf("referral accruals = %d, want 0", n)
	}
}

func TestPaymentOnOrdinaryCustomerStillAccruesReferral(t *testing.T) {
	f := setup(t)
	referrer := f.db.Customer.Create().SetName("Ref").SetReferralPercent(10).SaveX(f.ctx)
	f.db.Customer.UpdateOneID(f.customer).SetReferrerID(referrer.ID).ExecX(f.ctx)
	if _, err := f.svc.CommitPayment(f.ctx, PaymentInput{CustomerID: f.customer, Amount: 100000}); err != nil {
		t.Fatal(err)
	}
	if n := f.db.ReferralAccrual.Query().CountX(f.ctx); n != 1 {
		t.Errorf("referral accruals = %d, want 1: the guard must not touch ordinary customers", n)
	}
}

func TestProvisionRefusesAPanelUserOfACompanyMember(t *testing.T) {
	f := setup(t)
	u := &remnawave.User{ID: 77, Username: "emp", Status: remnawave.StatusActive, ExpireAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}
	f.panel.users[77] = u
	if err := rwsync.Upsert(f.ctx, f.db, u); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	acc := f.db.Customer.Create().SetName("Acme").SaveX(f.ctx)
	co := f.db.Company.Create().SetName("Acme").SetCustomerID(acc.ID).SetTariffID(f.plain).SetReportDay(1).
		SetPeriodStart(start).SetPeriodEnd(start.AddDate(0, 1, 0)).SetPeriodDays(31).SaveX(f.ctx)
	f.db.CompanyMember.Create().SetCompanyID(co.ID).SetUsername("emp").SetRwUserID(77).SetAddedAt(start).SaveX(f.ctx)

	_, err := f.svc.ProvisionSubscription(f.ctx, ProvisionInput{CustomerID: f.customer, TariffID: f.plain, Username: "emp", Months: 1, AllowDebt: true})
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "billing.username_taken" {
		t.Fatalf("error = %v, want billing.username_taken", err)
	}
	if f.db.Subscription.Query().CountX(f.ctx) != 0 {
		t.Error("a subscription adopted the employee's panel user")
	}
}

func TestAddonAdoptionRefusesACompanyMembersPanelUser(t *testing.T) {
	f := setup(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	u := &remnawave.User{ID: 88, Username: "white_alice", Status: remnawave.StatusActive, ExpireAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}
	f.panel.users[88] = u
	if err := rwsync.Upsert(f.ctx, f.db, u); err != nil {
		t.Fatal(err)
	}
	acc := f.db.Customer.Create().SetName("Acme").SaveX(f.ctx)
	co := f.db.Company.Create().SetName("Acme").SetCustomerID(acc.ID).SetTariffID(f.plain).SetReportDay(1).
		SetPeriodStart(start).SetPeriodEnd(start.AddDate(0, 1, 0)).SetPeriodDays(31).SaveX(f.ctx)
	f.db.CompanyMember.Create().SetCompanyID(co.ID).SetUsername("white_alice").SetRwUserID(88).SetAddedAt(start).SaveX(f.ctx)

	// Premium includes the "white" add-on, whose user would be white_alice.
	_, err := f.svc.ProvisionSubscription(f.ctx, ProvisionInput{CustomerID: f.customer, TariffID: f.premium, Username: "alice", Months: 1, AllowDebt: true})
	if err == nil {
		t.Fatal("the add-on adopted an employee's panel user")
	}
	if got := f.panel.users[88]; got.Status != remnawave.StatusActive || got.ExpireAt.Year() != 2099 {
		t.Errorf("the employee's panel user was changed: %+v", got)
	}
}
