package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/company"
	"remna-control/ent/companymember"
	"remna-control/ent/companystatement"
	"remna-control/ent/membersegment"
	"remna-control/ent/statementline"
	appmiddleware "remna-control/internal/api/middleware"
	"remna-control/internal/apperr"
	companysvc "remna-control/internal/company"
	"remna-control/internal/money"
)

// CompanyView is a company with its balance and open-period figures.
type CompanyView struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Notes      string `json:"notes"`
	CustomerID int    `json:"customer_id"` // the billing account: payments and history live there
	TariffID   int    `json:"tariff_id"`
	TariffName string `json:"tariff_name"`
	// ExternalSquadUUID overrides the tariff's external squad; null follows the tariff.
	ExternalSquadUUID *string   `json:"external_squad_uuid"`
	ReportDay         int       `json:"report_day"`
	PeriodStart       time.Time `json:"period_start"`
	PeriodEnd         time.Time `json:"period_end"` // the next report date
	Archived          bool      `json:"archived"`
	MembersCount      int       `json:"members_count"` // active employees
	ProtectedCnt      int       `json:"protected_count"`
	Balance           float64   `json:"balance"`
	Accrued           float64   `json:"accrued"`  // the open period so far
	Forecast          float64   `json:"forecast"` // the open period if nothing changes
	CreatedAt         time.Time `json:"created_at"`
}

// MemberView is an employee: who they are, what they are billed on now and
// how the panel sees them.
type MemberView struct {
	ID                int        `json:"id"`
	Username          string     `json:"username"`
	Label             string     `json:"label"`
	Active            bool       `json:"active"`
	Protected         bool       `json:"protected"`
	TariffPinned      bool       `json:"tariff_pinned"`
	AddedAt           time.Time  `json:"added_at"`
	RemovedAt         *time.Time `json:"removed_at"`
	TariffID          int        `json:"tariff_id"`
	TariffName        string     `json:"tariff_name"`
	Price             float64    `json:"price"` // per month
	TrafficLimitBytes int64      `json:"traffic_limit_bytes"`
	TrafficStrategy   string     `json:"traffic_strategy"`
	HwidLimit         *int       `json:"hwid_limit"`
	SquadUUIDs        []string   `json:"squad_uuids"`
	// ExternalSquadUUID: null = none set, "" = explicitly none for this employee.
	ExternalSquadUUID *string       `json:"external_squad_uuid"`
	Overrides         []string      `json:"overrides"` // fields set for this employee, the rest follow the tariff
	PanelStatus       string        `json:"panel_status"`
	PanelDeleted      bool          `json:"panel_deleted"` // gone from the panel behind our back
	SubscriptionURL   string        `json:"subscription_url"`
	RwUser            *RwUserView   `json:"rw_user"`  // the panel user, as on a subscription
	Segments          []SegmentView `json:"segments"` // what the employee was billed on, oldest first
}

// SegmentView is a stretch of days an employee was billed on one set of
// values. To is the exclusive end; null means it is still running.
type SegmentView struct {
	ID                int        `json:"id"`
	From              time.Time  `json:"from"`
	To                *time.Time `json:"to"`
	TariffID          int        `json:"tariff_id"`
	TariffName        string     `json:"tariff_name"`
	Price             float64    `json:"price"` // per month
	TrafficLimitBytes int64      `json:"traffic_limit_bytes"`
	TrafficStrategy   string     `json:"traffic_strategy"`
	HwidLimit         *int       `json:"hwid_limit"`
	SquadUUIDs        []string   `json:"squad_uuids"`
	ExternalSquadUUID *string    `json:"external_squad_uuid"`
	Overrides         []string   `json:"overrides"`
}

type StatementLineView struct {
	MemberID     *int      `json:"member_id"`
	Username     string    `json:"username"`
	TariffName   string    `json:"tariff_name"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"` // exclusive
	Days         int       `json:"days"`
	DaysInPeriod int       `json:"days_in_period"`
	MonthlyPrice float64   `json:"monthly_price"`
	Amount       float64   `json:"amount"`
}

type StatementView struct {
	ID            int                 `json:"id"`
	CompanyID     int                 `json:"company_id"`
	PeriodFrom    time.Time           `json:"period_from"`
	PeriodTo      time.Time           `json:"period_to"` // exclusive
	DaysInPeriod  int                 `json:"days_in_period"`
	Total         float64             `json:"total"`
	LedgerEntryID *int                `json:"ledger_entry_id"`
	GeneratedBy   string              `json:"generated_by"`
	CreatedAt     time.Time           `json:"created_at"`
	Lines         []StatementLineView `json:"lines,omitempty"`
}

// companyID is the company a request acts on: the one a token is bound to,
// else :id.
func (h *Handlers) companyID(c *fiber.Ctx) (int, error) {
	if id, ok := c.Locals(appmiddleware.LocalsCompanyID).(int); ok {
		return id, nil
	}
	return paramID(c, "id")
}

func isBound(c *fiber.Ctx) bool {
	_, ok := c.Locals(appmiddleware.LocalsCompanyID).(int)
	return ok
}

// bindStrict parses a JSON body and rejects fields the endpoint doesn't take.
func bindStrict(c *fiber.Ctx, v any) error {
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return apperr.Wrap(err, "bad_request", "invalid request: {{error}}")
	}
	return nil
}

func (h *Handlers) loadCompanyRow(ctx context.Context, id int) (*ent.Company, error) {
	co, err := h.DB.Company.Query().Where(company.ID(id)).WithTariff().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, companysvc.ErrNotFound
	}
	return co, err
}

func (h *Handlers) companyView(ctx context.Context, co *ent.Company, balances map[int]int64) (CompanyView, error) {
	v := CompanyView{
		ID: co.ID, Name: co.Name, Notes: co.Notes, CustomerID: co.CustomerID, TariffID: co.TariffID,
		ExternalSquadUUID: co.ExternalSquadUUID, ReportDay: co.ReportDay, PeriodStart: co.PeriodStart, PeriodEnd: co.PeriodEnd, Archived: co.Archived,
		Balance: money.ToMajor(balances[co.CustomerID]), CreatedAt: co.CreatedAt,
	}
	if t := co.Edges.Tariff; t != nil {
		v.TariffName = t.Name
	}
	var err error
	if v.MembersCount, err = h.DB.CompanyMember.Query().Where(companymember.CompanyID(co.ID), companymember.RemovedAtIsNil()).Count(ctx); err != nil {
		return v, err
	}
	if v.ProtectedCnt, err = h.DB.CompanyMember.Query().Where(companymember.CompanyID(co.ID), companymember.RemovedAtIsNil(), companymember.Protected(true)).Count(ctx); err != nil {
		return v, err
	}
	if !co.Archived {
		acc, err := h.Company.Accrued(ctx, co.ID)
		if err != nil {
			return v, err
		}
		fc, err := h.Company.Forecast(ctx, co.ID)
		if err != nil {
			return v, err
		}
		v.Accrued, v.Forecast = money.ToMajor(acc.Total), money.ToMajor(fc.Total)
	}
	return v, nil
}

func (h *Handlers) ListCompanies(c *fiber.Ctx) error {
	ctx := c.UserContext()
	cos, err := h.DB.Company.Query().WithTariff().Order(ent.Asc(company.FieldName)).All(ctx)
	if err != nil {
		return err
	}
	balances, err := h.Billing.Balances(ctx)
	if err != nil {
		return err
	}
	out := make([]CompanyView, 0, len(cos))
	for _, co := range cos {
		v, err := h.companyView(ctx, co, balances)
		if err != nil {
			return err
		}
		out = append(out, v)
	}
	return c.JSON(fiber.Map{"companies": out})
}

func (h *Handlers) GetCompany(c *fiber.Ctx) error {
	id, err := h.companyID(c)
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	co, err := h.loadCompanyRow(ctx, id)
	if err != nil {
		return err
	}
	balances, err := h.Billing.Balances(ctx)
	if err != nil {
		return err
	}
	v, err := h.companyView(ctx, co, balances)
	if err != nil {
		return err
	}
	return c.JSON(v)
}

type companyInput struct {
	Name      string `json:"name"`
	Notes     string `json:"notes"`
	TariffID  int    `json:"tariff_id"`
	ReportDay int    `json:"report_day"`
	// ExternalSquadUUID: null or blank follows the tariff.
	ExternalSquadUUID *string `json:"external_squad_uuid"`
}

func (h *Handlers) CreateCompany(c *fiber.Ctx) error {
	var in companyInput
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := h.checkExternalSquad(c.UserContext(), in.ExternalSquadUUID); err != nil {
		return err
	}
	co, err := h.Company.CreateCompany(c.UserContext(), in.Name, in.Notes, in.TariffID, in.ReportDay)
	if err != nil {
		return err
	}
	if in.ExternalSquadUUID != nil {
		if err := h.Company.SetExternalSquad(c.UserContext(), co.ID, in.ExternalSquadUUID); err != nil {
			return err
		}
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": co.ID, "customer_id": co.CustomerID})
}

func (h *Handlers) UpdateCompany(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var in companyInput
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := h.checkExternalSquad(c.UserContext(), in.ExternalSquadUUID); err != nil {
		return err
	}
	if err := h.Company.UpdateSettings(c.UserContext(), id, in.Name, in.Notes, in.ReportDay, in.ExternalSquadUUID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) ArchiveCompany(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	kept, err := h.Company.ArchiveCompany(c.UserContext(), id)
	if err != nil {
		return err
	}
	if kept == nil {
		kept = []string{}
	}
	return c.JSON(fiber.Map{"protected_left_enabled": kept})
}

func (h *Handlers) SetCompanyTariff(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var in struct {
		TariffID int `json:"tariff_id"`
	}
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := h.Company.SetTariff(c.UserContext(), id, in.TariffID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// memberViews loads employees of a company (all, or one) with the values of
// their open segment, or the last one for a removed member.
func (h *Handlers) memberViews(ctx context.Context, companyID int, memberID *int, activeOnly bool) ([]MemberView, error) {
	q := h.DB.CompanyMember.Query().Where(companymember.CompanyID(companyID)).WithRwUser().Order(ent.Asc(companymember.FieldUsername))
	if memberID != nil {
		q.Where(companymember.ID(*memberID))
	}
	if activeOnly {
		q.Where(companymember.RemovedAtIsNil())
	}
	members, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int, len(members))
	for i, m := range members {
		ids[i] = m.ID
	}
	segs, err := h.DB.MemberSegment.Query().Where(membersegment.MemberIDIn(ids...)).WithTariff().Order(ent.Asc(membersegment.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	last := map[int]*ent.MemberSegment{}
	history := map[int][]SegmentView{}
	for _, s := range segs {
		last[s.MemberID] = s
		sv := SegmentView{
			ID: s.ID, From: s.FromDate, To: s.ToDate, TariffID: s.TariffID, Price: money.ToMajor(s.Price),
			TrafficLimitBytes: s.TrafficLimitBytes, TrafficStrategy: s.TrafficStrategy, HwidLimit: s.HwidLimit,
			SquadUUIDs: nonNil(s.SquadUuids), ExternalSquadUUID: s.ExternalSquadUUID, Overrides: nonNil(s.Overrides),
		}
		if t := s.Edges.Tariff; t != nil {
			sv.TariffName = t.Name
		}
		history[s.MemberID] = append(history[s.MemberID], sv)
	}
	out := make([]MemberView, 0, len(members))
	for _, m := range members {
		v := MemberView{
			ID: m.ID, Username: m.Username, Label: m.Label, Active: m.RemovedAt == nil, Protected: m.Protected,
			TariffPinned: m.TariffPinned, AddedAt: m.AddedAt, RemovedAt: m.RemovedAt,
			SquadUUIDs: []string{}, Overrides: []string{}, Segments: history[m.ID],
		}
		if v.Segments == nil {
			v.Segments = []SegmentView{}
		}
		if s := last[m.ID]; s != nil {
			v.TariffID, v.Price = s.TariffID, money.ToMajor(s.Price)
			v.TrafficLimitBytes, v.TrafficStrategy, v.HwidLimit = s.TrafficLimitBytes, s.TrafficStrategy, s.HwidLimit
			v.SquadUUIDs, v.Overrides = nonNil(s.SquadUuids), nonNil(s.Overrides)
			v.ExternalSquadUUID = s.ExternalSquadUUID
			if t := s.Edges.Tariff; t != nil {
				v.TariffName = t.Name
			}
		}
		if u := m.Edges.RwUser; u != nil {
			v.PanelStatus, v.PanelDeleted, v.SubscriptionURL = u.Status, u.Deleted, u.SubscriptionURL
			v.RwUser = rwUserView(u)
		}
		out = append(out, v)
	}
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return slices.Clone(s)
}

func (h *Handlers) ListCompanyMembers(c *fiber.Ctx) error {
	id, err := h.companyID(c)
	if err != nil {
		return err
	}
	if _, err := h.loadCompanyRow(c.UserContext(), id); err != nil {
		return err
	}
	views, err := h.memberViews(c.UserContext(), id, nil, c.QueryBool("active"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"members": views})
}

func (h *Handlers) GetCompanyMember(c *fiber.Ctx) error {
	id, err := h.companyID(c)
	if err != nil {
		return err
	}
	mid, err := paramID(c, "mid")
	if err != nil {
		return err
	}
	views, err := h.memberViews(c.UserContext(), id, &mid, false)
	if err != nil {
		return err
	}
	if len(views) == 0 {
		return companysvc.ErrNotFound
	}
	return c.JSON(views[0])
}

// AddCompanyMember: 201 for a new or returning employee, 200 when they are
// already there. Only the admin API may pick a tariff.
func (h *Handlers) AddCompanyMember(c *fiber.Ctx) error {
	id, err := h.companyID(c)
	if err != nil {
		return err
	}
	var in struct {
		Username string `json:"username"`
		Label    string `json:"label"`
		TariffID *int   `json:"tariff_id"`
	}
	if isBound(c) {
		err = bindStrict(c, &struct {
			Username string `json:"username"`
			Label    string `json:"label"`
		}{})
		if err != nil {
			return apperr.Status(fiber.StatusForbidden, "company.admin_only", "only username and label can be set with a company token")
		}
	}
	if err := bind(c, &in); err != nil {
		return err
	}
	m, created, err := h.Company.AddMember(c.UserContext(), id, in.Username, in.Label, in.TariffID)
	if err != nil {
		return err
	}
	views, err := h.memberViews(c.UserContext(), id, &m.ID, false)
	if err != nil || len(views) == 0 {
		return err
	}
	status := fiber.StatusOK
	if created {
		status = fiber.StatusCreated
	}
	return c.Status(status).JSON(views[0])
}

// UpdateCompanyMember changes the label and nothing else.
func (h *Handlers) UpdateCompanyMember(c *fiber.Ctx) error {
	id, err := h.companyID(c)
	if err != nil {
		return err
	}
	mid, err := paramID(c, "mid")
	if err != nil {
		return err
	}
	var in struct {
		Label string `json:"label"`
	}
	if err := bindStrict(c, &in); err != nil {
		return err
	}
	if err := h.Company.SetLabel(c.UserContext(), id, mid, in.Label); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) RemoveCompanyMember(c *fiber.Ctx) error {
	id, err := h.companyID(c)
	if err != nil {
		return err
	}
	mid, err := paramID(c, "mid")
	if err != nil {
		return err
	}
	if err := h.Company.RemoveMember(c.UserContext(), id, mid); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SetMemberOverrides: PUT .../members/:mid/override. Prices are in rubles;
// "reset" names fields that go back to the tariff.
func (h *Handlers) SetMemberOverrides(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	mid, err := paramID(c, "mid")
	if err != nil {
		return err
	}
	var in struct {
		TariffID          *int     `json:"tariff_id"`
		Price             *float64 `json:"price"`
		TrafficLimitBytes *int64   `json:"traffic_limit_bytes"`
		TrafficStrategy   *string  `json:"traffic_strategy"`
		HwidLimit         *int     `json:"hwid_limit"`
		SquadUUIDs        []string `json:"squad_uuids"`
		ExternalSquadUUID *string  `json:"external_squad_uuid"`
		Reset             []string `json:"reset"`
	}
	if err := bindStrict(c, &in); err != nil {
		return err
	}
	if err := h.checkExternalSquad(c.UserContext(), in.ExternalSquadUUID); err != nil {
		return err
	}
	o := companysvc.Overrides{
		TrafficLimitBytes: in.TrafficLimitBytes, TrafficStrategy: in.TrafficStrategy,
		HwidLimit: in.HwidLimit, Squads: in.SquadUUIDs, ExternalSquad: in.ExternalSquadUUID, Reset: in.Reset,
	}
	if in.Price != nil {
		p := money.FromMajor(*in.Price)
		o.Price = &p
	}
	if err := h.Company.SetOverrides(c.UserContext(), id, mid, in.TariffID, o); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) SetMemberProtected(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	mid, err := paramID(c, "mid")
	if err != nil {
		return err
	}
	var in struct {
		Protected bool `json:"protected"`
	}
	if err := bindStrict(c, &in); err != nil {
		return err
	}
	if err := h.Company.SetProtected(c.UserContext(), id, mid, in.Protected); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) CompanyBalance(c *fiber.Ctx) error {
	id, err := h.companyID(c)
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	co, err := h.loadCompanyRow(ctx, id)
	if err != nil {
		return err
	}
	bal, err := h.Billing.Balance(ctx, co.CustomerID)
	if err != nil {
		return err
	}
	acc, err := h.Company.Accrued(ctx, id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"balance": money.ToMajor(bal), "accrued": money.ToMajor(acc.Total),
		"period_start": co.PeriodStart, "next_report_date": co.PeriodEnd,
	})
}

func calcView(cl *companysvc.Calc) fiber.Map {
	lines := make([]StatementLineView, 0, len(cl.Lines))
	for _, l := range cl.Lines {
		lines = append(lines, StatementLineView{
			MemberID: l.MemberID, Username: l.Username, TariffName: l.TariffName, From: l.From, To: l.To,
			Days: l.Days, DaysInPeriod: l.DaysInPeriod, MonthlyPrice: money.ToMajor(l.MonthlyPrice), Amount: money.ToMajor(l.Amount),
		})
	}
	return fiber.Map{"period_from": cl.Period.From, "period_to": cl.Period.To, "days_in_period": cl.DaysInPeriod, "total": money.ToMajor(cl.Total), "lines": lines}
}

// CompanyAccrued: the open period by employee, so far and as forecast.
func (h *Handlers) CompanyAccrued(c *fiber.Ctx) error {
	id, err := h.companyID(c)
	if err != nil {
		return err
	}
	acc, err := h.Company.Accrued(c.UserContext(), id)
	if err != nil {
		return err
	}
	fc, err := h.Company.Forecast(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"accrued": calcView(acc), "forecast": calcView(fc)})
}

func statementView(st *ent.CompanyStatement) StatementView {
	return StatementView{
		ID: st.ID, CompanyID: st.CompanyID, PeriodFrom: st.PeriodFrom, PeriodTo: st.PeriodTo,
		DaysInPeriod: st.DaysInPeriod, Total: money.ToMajor(st.Total), LedgerEntryID: st.LedgerEntryID,
		GeneratedBy: st.GeneratedBy, CreatedAt: st.CreatedAt,
	}
}

func (h *Handlers) ListCompanyStatements(c *fiber.Ctx) error {
	id, err := h.companyID(c)
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	if _, err := h.loadCompanyRow(ctx, id); err != nil {
		return err
	}
	sts, err := h.DB.CompanyStatement.Query().Where(companystatement.CompanyID(id)).Order(ent.Desc(companystatement.FieldPeriodFrom)).All(ctx)
	if err != nil {
		return err
	}
	out := make([]StatementView, 0, len(sts))
	for _, st := range sts {
		out = append(out, statementView(st))
	}
	return c.JSON(fiber.Map{"statements": out})
}

// GetCompanyStatement is looked up inside the company, so another company's
// statement is 404.
func (h *Handlers) GetCompanyStatement(c *fiber.Ctx) error {
	id, err := h.companyID(c)
	if err != nil {
		return err
	}
	sid, err := paramID(c, "sid")
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	st, err := h.DB.CompanyStatement.Query().Where(companystatement.ID(sid), companystatement.CompanyID(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return companysvc.ErrNotFound
	}
	if err != nil {
		return err
	}
	v := statementView(st)
	lines, err := h.DB.StatementLine.Query().Where(statementline.StatementID(st.ID)).Order(ent.Asc(statementline.FieldUsername), ent.Asc(statementline.FieldFromDate)).All(ctx)
	if err != nil {
		return err
	}
	v.Lines = make([]StatementLineView, 0, len(lines))
	for _, l := range lines {
		v.Lines = append(v.Lines, StatementLineView{
			MemberID: l.MemberID, Username: l.Username, TariffName: l.TariffName, From: l.FromDate, To: l.ToDate,
			Days: l.Days, DaysInPeriod: l.DaysInPeriod, MonthlyPrice: money.ToMajor(l.MonthlyPrice), Amount: money.ToMajor(l.Amount),
		})
	}
	return c.JSON(v)
}

// CloseCompanyPeriod posts every period that has ended, like the job does.
func (h *Handlers) CloseCompanyPeriod(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	n, err := h.Company.CloseEnded(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"posted": n})
}

// activeMember is a current employee of an active company, for the figures
// that count what is billed now.
type activeMember struct {
	CompanyID       int
	TariffID        int
	Price           float64 // per month
	PriceOverridden bool
	Live            bool // the panel user exists and isn't disabled: it counts towards MRR
}

// activeMembers lists the employees on an open segment in companies that
// aren't archived.
func (h *Handlers) activeMembers(ctx context.Context) ([]activeMember, error) {
	segs, err := h.DB.MemberSegment.Query().
		Where(membersegment.ToDateIsNil(), membersegment.HasMemberWith(
			companymember.RemovedAtIsNil(), companymember.HasCompanyWith(company.Archived(false)))).
		WithMember(func(q *ent.CompanyMemberQuery) { q.WithRwUser() }).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]activeMember, 0, len(segs))
	for _, s := range segs {
		m := s.Edges.Member
		u := m.Edges.RwUser
		out = append(out, activeMember{
			CompanyID: m.CompanyID, TariffID: s.TariffID, Price: money.ToMajor(s.Price),
			PriceOverridden: slices.Contains(s.Overrides, "price"),
			Live:            u != nil && !u.Deleted && u.Status != "DISABLED",
		})
	}
	return out, nil
}
