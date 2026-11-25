package company

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"remna-control/ent"
	"remna-control/ent/company"
	"remna-control/ent/companymember"
	"remna-control/ent/companystatement"
	"remna-control/ent/ledgerentry"
	"remna-control/ent/membersegment"
	"remna-control/ent/tariff"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/jobs"
	"remna-control/internal/rwsync"
	"remna-control/internal/store"
)

// Line is one member segment inside a period: what it costs for the days it
// covers. To is the exclusive end.
type Line struct {
	MemberID     *int
	Username     string
	TariffName   string
	From, To     time.Time
	Days         int
	DaysInPeriod int
	MonthlyPrice int64
	Amount       int64
}

// Calc is a company's bill for its open period, as of some day.
type Calc struct {
	Period       Period
	DaysInPeriod int
	Total        int64
	Lines        []Line
}

// calc prices the company's open period. Days of segments still open are
// counted up to upTo (exclusive), or to the period end when upTo is nil.
func (s *Service) calc(ctx context.Context, db *ent.Client, c *ent.Company, upTo *time.Time) (*Calc, error) {
	p := Period{From: c.PeriodStart, To: c.PeriodEnd}
	segs, err := db.MemberSegment.Query().
		Where(membersegment.HasMemberWith(companymember.CompanyID(c.ID)), membersegment.FromDateLT(p.To)).
		WithMember().All(ctx)
	if err != nil {
		return nil, err
	}
	names, err := tariffNames(ctx, db, segs)
	if err != nil {
		return nil, err
	}
	out := &Calc{Period: p, DaysInPeriod: c.PeriodDays}
	for _, seg := range segs {
		to := seg.ToDate
		if upTo != nil && (to == nil || upTo.Before(*to)) {
			to = upTo
		}
		days := DaysIn(p, seg.FromDate, to, s.loc)
		if days == 0 {
			continue
		}
		end := p.To
		if to != nil && to.Before(end) {
			end = calendarDay(*to, s.loc)
		}
		memberID := seg.MemberID
		out.Lines = append(out.Lines, Line{
			MemberID: &memberID, Username: seg.Edges.Member.Username, TariffName: names[seg.TariffID],
			From: laterOf(calendarDay(seg.FromDate, s.loc), p.From), To: end,
			Days: days, DaysInPeriod: c.PeriodDays, MonthlyPrice: seg.Price,
			Amount: Prorate(seg.Price, days, c.PeriodDays),
		})
	}
	slices.SortFunc(out.Lines, func(a, b Line) int {
		if c := strings.Compare(a.Username, b.Username); c != 0 {
			return c
		}
		return a.From.Compare(b.From)
	})
	for _, l := range out.Lines {
		out.Total += l.Amount
	}
	return out, nil
}

func tariffNames(ctx context.Context, db *ent.Client, segs []*ent.MemberSegment) (map[int]string, error) {
	ids := make([]int, 0, len(segs))
	for _, s := range segs {
		ids = append(ids, s.TariffID)
	}
	ts, err := db.Tariff.Query().Where(tariff.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[int]string, len(ts))
	for _, t := range ts {
		names[t.ID] = t.Name
	}
	return names, nil
}

// Accrued is what the open period has cost through the end of today. It
// writes nothing.
func (s *Service) Accrued(ctx context.Context, companyID int) (*Calc, error) {
	c, err := s.loadCompany(ctx, companyID, true)
	if err != nil {
		return nil, err
	}
	through := s.today().AddDate(0, 0, 1)
	return s.calc(ctx, s.db, c, &through)
}

// Forecast is what the open period costs if the members stay as they are
// until its end. It writes nothing.
func (s *Service) Forecast(ctx context.Context, companyID int) (*Calc, error) {
	c, err := s.loadCompany(ctx, companyID, true)
	if err != nil {
		return nil, err
	}
	return s.calc(ctx, s.db, c, nil)
}

// post writes the statement of the company's open period through db (a
// transaction's client when called from one) and opens the next period. A
// period that already has a statement returns it unchanged.
func (s *Service) post(ctx context.Context, db *ent.Client, c *ent.Company, upTo *time.Time) (*ent.CompanyStatement, error) {
	if st, err := db.CompanyStatement.Query().
		Where(companystatement.CompanyID(c.ID), companystatement.PeriodFrom(c.PeriodStart)).Only(ctx); err == nil {
		return st, nil
	} else if !ent.IsNotFound(err) {
		return nil, err
	}
	calc, err := s.calc(ctx, db, c, upTo)
	if err != nil {
		return nil, err
	}
	// A statement cut short (archiving) ends where the billing ended.
	end, early := calc.Period.To, false
	if upTo != nil && upTo.Before(end) {
		end, early = calendarDay(*upTo, s.loc), true
	}
	var ledgerID *int
	if calc.Total > 0 {
		note := fmt.Sprintf("Statement %s – %s", calc.Period.From.Format(time.DateOnly), end.AddDate(0, 0, -1).Format(time.DateOnly))
		e, err := db.LedgerEntry.Create().
			SetCustomerID(c.CustomerID).SetType(ledgerentry.TypeCompanyCharge).SetAmount(-calc.Total).
			SetDate(end).SetNote(note).Save(ctx)
		if err != nil {
			return nil, err
		}
		ledgerID = &e.ID
	}
	st, err := db.CompanyStatement.Create().
		SetCompanyID(c.ID).SetPeriodFrom(calc.Period.From).SetPeriodTo(end).
		SetDaysInPeriod(calc.DaysInPeriod).SetTotal(calc.Total).SetNillableLedgerEntryID(ledgerID).
		SetGeneratedBy(audit.Actor(ctx)).Save(ctx)
	if err != nil {
		return nil, err
	}
	if len(calc.Lines) > 0 {
		builders := make([]*ent.StatementLineCreate, len(calc.Lines))
		for i, l := range calc.Lines {
			builders[i] = db.StatementLine.Create().
				SetStatementID(st.ID).SetNillableMemberID(l.MemberID).SetUsername(l.Username).SetTariffName(l.TariffName).
				SetFromDate(l.From).SetToDate(l.To).SetDays(l.Days).SetDaysInPeriod(l.DaysInPeriod).
				SetMonthlyPrice(l.MonthlyPrice).SetAmount(l.Amount)
		}
		if err := db.StatementLine.CreateBulk(builders...).Exec(ctx); err != nil {
			return nil, err
		}
	}
	if early {
		return st, nil // archiving: no next period
	}
	next := calc.Period.To
	if err := db.Company.UpdateOneID(c.ID).
		SetPeriodStart(next).
		SetPeriodEnd(NextReportDate(next, c.ReportDay, s.loc)).
		SetPeriodDays(PeriodContaining(c.ReportDay, next, s.loc).Days()).
		Exec(ctx); err != nil {
		return nil, err
	}
	return st, nil
}

// ClosePeriod posts the statement of the company's open period and starts the
// next one. upTo (exclusive) cuts the days counted of members still open; nil
// counts to the period end.
func (s *Service) ClosePeriod(ctx context.Context, companyID int, upTo *time.Time) (*ent.CompanyStatement, error) {
	c, err := s.loadCompany(ctx, companyID, false)
	if err != nil {
		return nil, err
	}
	if upTo == nil && c.PeriodEnd.After(s.today()) {
		return nil, apperr.Status(409, "company.period_not_ended", "the period ends on {{date}}", "date", c.PeriodEnd.Format(time.DateOnly))
	}
	var st *ent.CompanyStatement
	err = store.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		var err error
		st, err = s.post(ctx, tx.Client(), c, upTo)
		return err
	})
	audit.Log(ctx, s.db, "company.close_period", "company", companyID, map[string]any{"period_from": c.PeriodStart}, err)
	return st, err
}

// closeEnded posts every period of the company that has ended by today, in
// order, and returns how many.
func (s *Service) closeEnded(ctx context.Context, companyID int) (int, error) {
	n := 0
	for {
		c, err := s.loadCompany(ctx, companyID, false)
		if err != nil {
			return n, err
		}
		if c.PeriodEnd.After(s.today()) {
			return n, nil
		}
		if _, err := s.ClosePeriod(ctx, companyID, nil); err != nil {
			return n, err
		}
		n++
		if after, err := s.loadCompany(ctx, companyID, false); err == nil && !after.PeriodStart.After(c.PeriodStart) {
			return n, fmt.Errorf("company %d: the period did not advance past %s", companyID, c.PeriodStart.Format(time.DateOnly))
		}
	}
}

// CloseDue is the background job: it posts the statements of every period
// that ended on or before today, catching up after downtime.
func (s *Service) CloseDue(ctx context.Context) (jobs.Result, error) {
	ids, err := s.db.Company.Query().Where(company.Archived(false)).IDs(ctx)
	if err != nil {
		return jobs.Result{}, err
	}
	total := 0
	var errs []error
	for _, id := range ids {
		n, err := s.closeEnded(ctx, id)
		total += n
		if err != nil && !isArchivedErr(err) {
			errs = append(errs, fmt.Errorf("company %d: %w", id, err))
		}
	}
	res := jobs.Result{Changed: total > 0}
	if total > 0 {
		res.Summary = fmt.Sprintf("%d company statements posted", total)
	}
	return res, errors.Join(errs...)
}

// ArchiveCompany stops the company: what it owes through yesterday is posted
// as a final statement, every member is removed from billing, and their
// panel users are disabled, except protected members, whose panel users are
// left as they are (their usernames are returned). If a panel call fails
// nothing is archived.
func (s *Service) ArchiveCompany(ctx context.Context, id int) (protected []string, err error) {
	if _, err := s.loadCompany(ctx, id, false); err != nil {
		return nil, err
	}
	if _, err := s.closeEnded(ctx, id); err != nil {
		return nil, err
	}
	members, err := s.db.CompanyMember.Query().
		Where(companymember.CompanyID(id), companymember.RemovedAtIsNil()).All(ctx)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, m := range members {
		if m.Protected {
			protected = append(protected, m.Username)
			continue
		}
		if m.RwUserID == nil {
			continue
		}
		u, err := s.rw.DisableUser(ctx, *m.RwUserID)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.Username, err))
			continue
		}
		if err := rwsync.Upsert(ctx, s.db, u); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		audit.Log(ctx, s.db, "company.archive", "company", id, nil, err)
		return nil, err
	}

	today := s.today()
	err = store.WithTx(ctx, s.db, func(tx *ent.Tx) error {
		c, err := tx.Company.Get(ctx, id)
		if err != nil {
			return err
		}
		if _, err := s.post(ctx, tx.Client(), c, &today); err != nil {
			return err
		}
		if err := tx.MemberSegment.Update().
			Where(membersegment.ToDateIsNil(), membersegment.HasMemberWith(companymember.CompanyID(id))).
			SetToDate(today).Exec(ctx); err != nil {
			return err
		}
		if err := tx.CompanyMember.Update().
			Where(companymember.CompanyID(id), companymember.RemovedAtIsNil()).SetRemovedAt(s.now()).Exec(ctx); err != nil {
			return err
		}
		return tx.Company.UpdateOneID(id).SetArchived(true).SetArchivedAt(s.now()).Exec(ctx)
	})
	audit.Log(ctx, s.db, "company.archive", "company", id, map[string]any{"protected": protected}, err)
	if err != nil {
		return nil, err
	}
	return protected, nil
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// CloseEnded posts every period of the company that has ended by today and
// returns how many; it is what "close now" does.
func (s *Service) CloseEnded(ctx context.Context, companyID int) (int, error) {
	return s.closeEnded(ctx, companyID)
}

// isArchivedErr: the company was archived after the job listed it.
func isArchivedErr(err error) bool {
	var ae *apperr.Error
	return errors.As(err, &ae) && ae.Code == "company.archived"
}
