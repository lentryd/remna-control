package handlers

import (
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/customer"
	"remna-control/ent/extension"
	"remna-control/ent/ledgerentry"
	"remna-control/ent/payment"
	"remna-control/ent/referralaccrual"
	"remna-control/ent/rwuser"
	"remna-control/ent/subscription"
	"remna-control/ent/subscriptionaddon"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/billing"
	"remna-control/internal/money"
	"remna-control/internal/store"
)

type CustomerView struct {
	ID                 int        `json:"id"`
	Name               string     `json:"name"`
	Contact            string     `json:"contact"`
	TelegramID         *int64     `json:"telegram_id"`
	Notes              string     `json:"notes"`
	ReferrerID         *int       `json:"referrer_id"`
	ReferrerName       string     `json:"referrer_name"`
	ReferralPercent    *float64   `json:"referral_percent"`
	Archived           bool       `json:"archived"`
	Balance            float64    `json:"balance"`          // total = balance_own + balance_referral
	BalanceOwn         float64    `json:"balance_own"`      // top-ups, charges, adjustments
	BalanceReferral    float64    `json:"balance_referral"` // referral accruals credited to the balance
	SubscriptionsCount int        `json:"subscriptions_count"`
	AddonsCount        int        `json:"addons_count"`
	Monthly            float64    `json:"monthly"`
	NearestExpireAt    *time.Time `json:"nearest_expire_at"`
	ReferralsCount     int        `json:"referrals_count"`
	TotalPaid          float64    `json:"total_paid"`
	LastPaymentAt      *time.Time `json:"last_payment_at"`
	CreatedAt          time.Time  `json:"created_at"`
}

type customerStats struct {
	totalPaid int64
	lastPay   *time.Time
}

func (h *Handlers) customerViews(c *fiber.Ctx, where ...func(*ent.CustomerQuery)) ([]CustomerView, error) {
	ctx := c.UserContext()
	q := h.DB.Customer.Query().WithReferrer().WithReferrals().Order(ent.Asc(customer.FieldName))
	for _, w := range where {
		w(q)
	}
	customers, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	balances, err := h.Billing.BalanceSplits(ctx)
	if err != nil {
		return nil, err
	}
	subs, err := h.loadSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	bySub := map[int][]SubscriptionView{}
	for _, s := range subs {
		bySub[s.CustomerID] = append(bySub[s.CustomerID], s)
	}
	payments, err := h.DB.Payment.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	stats := map[int]*customerStats{}
	for _, p := range payments {
		st := stats[p.CustomerID]
		if st == nil {
			st = &customerStats{}
			stats[p.CustomerID] = st
		}
		st.totalPaid += p.Amount
		if st.lastPay == nil || p.Date.After(*st.lastPay) {
			d := p.Date
			st.lastPay = &d
		}
	}

	out := make([]CustomerView, 0, len(customers))
	for _, cu := range customers {
		v := CustomerView{
			ID: cu.ID, Name: cu.Name, Contact: cu.Contact, TelegramID: cu.TelegramID, Notes: cu.Notes,
			ReferrerID: cu.ReferrerID, ReferralPercent: cu.ReferralPercent, Archived: cu.Archived,
			Balance:    money.ToMajor(balances[cu.ID].Total()),
			BalanceOwn: money.ToMajor(balances[cu.ID].Own), BalanceReferral: money.ToMajor(balances[cu.ID].Referral),
			ReferralsCount: len(cu.Edges.Referrals),
			CreatedAt:      cu.CreatedAt,
		}
		if r := cu.Edges.Referrer; r != nil {
			v.ReferrerName = r.Name
		}
		for _, s := range bySub[cu.ID] {
			v.SubscriptionsCount++
			v.AddonsCount += len(s.Addons)
			v.Monthly += monthlyLive(s)
			v.NearestExpireAt = earliest(v.NearestExpireAt, s.RwUser)
			for _, a := range s.Addons {
				v.NearestExpireAt = earliest(v.NearestExpireAt, a.RwUser)
			}
		}
		if st := stats[cu.ID]; st != nil {
			v.TotalPaid = money.ToMajor(st.totalPaid)
			v.LastPaymentAt = st.lastPay
		}
		out = append(out, v)
	}
	return out, nil
}

func earliest(cur *time.Time, u *RwUserView) *time.Time {
	if u == nil || u.Deleted || u.ExpireAt == nil || u.Unlimited {
		return cur
	}
	if cur == nil || u.ExpireAt.Before(*cur) {
		return u.ExpireAt
	}
	return cur
}

func (h *Handlers) ListCustomers(c *fiber.Ctx) error {
	out, err := h.customerViews(c)
	if err != nil {
		return err
	}
	return c.JSON(out)
}

type CustomerDetail struct {
	CustomerView
	Subscriptions []SubscriptionView `json:"subscriptions"`
	Payments      []PaymentView      `json:"payments"`
	Ledger        []LedgerView       `json:"ledger"`
	Referrals     []CustomerView     `json:"referrals"`
	Accruals      []AccrualView      `json:"accruals"`
	Extensions    []ExtensionView    `json:"extensions"`
}

type PaymentView struct {
	ID           int       `json:"id"`
	CustomerID   int       `json:"customer_id"`
	CustomerName string    `json:"customer_name"`
	Amount       float64   `json:"amount"`
	Date         time.Time `json:"date"`
	Method       string    `json:"method"`
	Note         string    `json:"note"`
	Historical   bool      `json:"historical"`
}

type LedgerView struct {
	ID     int       `json:"id"`
	Type   string    `json:"type"`
	Amount float64   `json:"amount"`
	Date   time.Time `json:"date"`
	Note   string    `json:"note"`
}

type AccrualView struct {
	ID           int       `json:"id"`
	ReferrerID   int       `json:"referrer_id"`
	ReferrerName string    `json:"referrer_name"`
	RefereeID    int       `json:"referee_id"`
	RefereeName  string    `json:"referee_name"`
	PaymentID    int       `json:"payment_id"`
	Percent      float64   `json:"percent"`
	Amount       float64   `json:"amount"`
	Date         time.Time `json:"date"`
	Status       string    `json:"status"`
}

type ExtensionView struct {
	ID        int        `json:"id"`
	Kind      string     `json:"kind"`
	Months    int        `json:"months"`
	Days      int        `json:"days"`
	Amount    float64    `json:"amount"`
	FromAt    *time.Time `json:"from_at"`
	ToAt      *time.Time `json:"to_at"`
	Status    string     `json:"status"`
	Error     string     `json:"error"`
	Actor     string     `json:"actor"`
	CreatedAt time.Time  `json:"created_at"`
	SubID     *int       `json:"subscription_id"`
	AddonID   *int       `json:"subscription_addon_id"`
}

func paymentView(p *ent.Payment) PaymentView {
	v := PaymentView{ID: p.ID, CustomerID: p.CustomerID, Amount: money.ToMajor(p.Amount), Date: p.Date, Method: p.Method, Note: p.Note, Historical: p.Historical}
	if p.Edges.Customer != nil {
		v.CustomerName = p.Edges.Customer.Name
	}
	return v
}

func (h *Handlers) GetCustomer(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	return h.customerDetail(c, id)
}

// LookupCustomer finds a customer by Telegram ID or by the short id of one of
// their panel users (main subscription or add-on) and answers like GetCustomer.
func (h *Handlers) LookupCustomer(c *fiber.Ctx) error {
	ctx := c.UserContext()
	var q *ent.CustomerQuery
	switch {
	case c.Query("telegram_id") != "":
		tg, err := strconv.ParseInt(c.Query("telegram_id"), 10, 64)
		if err != nil || tg <= 0 {
			return apperr.Status(fiber.StatusBadRequest, "customer.bad_telegram_id", "telegram id must be a positive number")
		}
		q = h.DB.Customer.Query().Where(customer.TelegramID(tg))
	case c.Query("short_uuid") != "":
		short := c.Query("short_uuid")
		q = h.DB.Customer.Query().Where(customer.Or(
			customer.HasSubscriptionsWith(subscription.HasRwUserWith(rwuser.ShortUUID(short))),
			customer.HasSubscriptionsWith(subscription.HasAddonsWith(subscriptionaddon.HasRwUserWith(rwuser.ShortUUID(short)))),
		))
	default:
		return apperr.Status(fiber.StatusBadRequest, "customer.lookup_key", "pass telegram_id or short_uuid")
	}
	// Telegram IDs aren't unique; prefer an active customer, then the oldest.
	cu, err := q.Order(ent.Asc(customer.FieldArchived), ent.Asc(customer.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return apperr.Status(fiber.StatusNotFound, "customer.not_found", "customer not found")
	}
	if err != nil {
		return err
	}
	return h.customerDetail(c, cu.ID)
}

func (h *Handlers) customerDetail(c *fiber.Ctx, id int) error {
	ctx := c.UserContext()
	views, err := h.customerViews(c, func(q *ent.CustomerQuery) {
		q.Where(customer.Or(customer.ID(id), customer.ReferrerID(id)))
	})
	if err != nil {
		return err
	}
	var d CustomerDetail
	found := false
	for _, v := range views {
		if v.ID == id {
			d.CustomerView, found = v, true
		} else {
			d.Referrals = append(d.Referrals, v)
		}
	}
	if !found {
		return apperr.Status(fiber.StatusNotFound, "customer.not_found", "customer not found")
	}

	if d.Subscriptions, err = h.loadSubscriptions(ctx, func(q *ent.SubscriptionQuery) {
		q.Where(subscription.CustomerID(id))
	}); err != nil {
		return err
	}
	pays, err := h.DB.Payment.Query().Where(payment.CustomerID(id)).Order(ent.Desc(payment.FieldDate)).All(ctx)
	if err != nil {
		return err
	}
	for _, p := range pays {
		d.Payments = append(d.Payments, paymentView(p))
	}
	ledger, err := h.DB.LedgerEntry.Query().Where(ledgerentry.CustomerID(id)).Order(ent.Desc(ledgerentry.FieldDate), ent.Desc(ledgerentry.FieldID)).All(ctx)
	if err != nil {
		return err
	}
	for _, e := range ledger {
		d.Ledger = append(d.Ledger, LedgerView{ID: e.ID, Type: e.Type.String(), Amount: money.ToMajor(e.Amount), Date: e.Date, Note: e.Note})
	}
	if d.Accruals, err = h.accrualViews(c, func(q *ent.ReferralAccrualQuery) {
		q.Where(referralaccrual.Or(referralaccrual.ReferrerID(id), referralaccrual.RefereeID(id)))
	}); err != nil {
		return err
	}
	exts, err := h.DB.Extension.Query().Where(extension.CustomerID(id)).Order(ent.Desc(extension.FieldID)).Limit(100).All(ctx)
	if err != nil {
		return err
	}
	for _, e := range exts {
		d.Extensions = append(d.Extensions, ExtensionView{
			ID: e.ID, Kind: e.Kind.String(), Months: e.Months, Days: e.Days, Amount: money.ToMajor(e.Amount),
			FromAt: e.FromAt, ToAt: e.ToAt, Status: e.Status.String(), Error: e.Error, Actor: e.Actor,
			CreatedAt: e.CreatedAt, SubID: e.SubscriptionID, AddonID: e.SubscriptionAddonID,
		})
	}
	return c.JSON(d)
}

type customerInput struct {
	Name            string   `json:"name"`
	Contact         string   `json:"contact"`
	TelegramID      *int64   `json:"telegram_id"`
	Notes           string   `json:"notes"`
	ReferrerID      *int     `json:"referrer_id"`
	ReferralPercent *float64 `json:"referral_percent"`
	Archived        bool     `json:"archived"`
}

func (h *Handlers) CreateCustomer(c *fiber.Ctx) error {
	var in customerInput
	if err := bind(c, &in); err != nil {
		return err
	}
	cu, err := h.DB.Customer.Create().
		SetName(in.Name).SetContact(in.Contact).SetNotes(in.Notes).SetNillableTelegramID(in.TelegramID).
		SetNillableReferrerID(in.ReferrerID).SetNillableReferralPercent(in.ReferralPercent).
		SetArchived(in.Archived).
		Save(c.UserContext())
	audit.Log(c.UserContext(), h.DB, "customer.create", "customer", idOrZero(cu), in, err)
	if err != nil {
		return badRequest(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": cu.ID})
}

func idOrZero(cu *ent.Customer) int {
	if cu == nil {
		return 0
	}
	return cu.ID
}

func (h *Handlers) UpdateCustomer(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var in customerInput
	if err := bind(c, &in); err != nil {
		return err
	}
	if in.ReferrerID != nil && *in.ReferrerID == id {
		return apperr.New("customer.self_referral", "a customer can't refer themselves")
	}
	if in.TelegramID != nil && *in.TelegramID <= 0 {
		return apperr.New("customer.bad_telegram_id", "telegram id must be a positive number")
	}
	q := h.DB.Customer.UpdateOneID(id).
		SetName(in.Name).SetContact(in.Contact).SetNotes(in.Notes).SetArchived(in.Archived)
	if in.TelegramID != nil {
		q.SetTelegramID(*in.TelegramID)
	} else {
		q.ClearTelegramID()
	}
	if in.ReferrerID != nil {
		q.SetReferrerID(*in.ReferrerID)
	} else {
		q.ClearReferrerID()
	}
	if in.ReferralPercent != nil {
		q.SetReferralPercent(*in.ReferralPercent)
	} else {
		q.ClearReferralPercent()
	}
	err = q.Exec(c.UserContext())
	audit.Log(c.UserContext(), h.DB, "customer.update", "customer", id, in, err)
	if err != nil {
		return badRequest(err)
	}
	if h.Sync != nil {
		perr := h.Sync.PushCustomerTelegram(c.UserContext(), id, in.TelegramID)
		audit.Log(c.UserContext(), h.DB, "customer.push_telegram", "customer", id, in.TelegramID, perr)
		if perr != nil {
			slog.Warn("push customer telegram id to panel", "customer", id, "err", perr)
		}
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// DeleteCustomer only removes customers without money history; archive the
// rest instead.
func (h *Handlers) DeleteCustomer(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	if n, _ := h.DB.Payment.Query().Where(payment.CustomerID(id)).Count(ctx); n > 0 {
		return apperr.Status(fiber.StatusConflict, "customer.has_payments", "the customer has payments; archive them instead")
	}
	if n, _ := h.DB.Subscription.Query().Where(subscription.CustomerID(id)).Count(ctx); n > 0 {
		return apperr.Status(fiber.StatusConflict, "customer.has_subscriptions", "unlink the customer's subscriptions first")
	}
	if _, err := h.DB.Customer.Update().Where(customer.ReferrerID(id)).ClearReferrerID().Save(ctx); err != nil {
		return err
	}
	if _, err := h.DB.LedgerEntry.Delete().Where(ledgerentry.CustomerID(id)).Exec(ctx); err != nil {
		return err
	}
	err = h.DB.Customer.DeleteOneID(id).Exec(ctx)
	audit.Log(ctx, h.DB, "customer.delete", "customer", id, nil, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type paymentPreviewRequest struct {
	Amount          float64 `json:"amount"`
	RemainderToDays bool    `json:"remainder_to_days"`
}

type allocationView struct {
	Kind   string    `json:"kind"`
	ID     int       `json:"id"`
	Title  string    `json:"title"`
	Months int       `json:"months"`
	Days   int       `json:"days"`
	Amount float64   `json:"amount"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
}

type planItemView struct {
	Kind       string       `json:"kind"`
	ID         int          `json:"id"`
	Title      string       `json:"title"`
	Monthly    float64      `json:"monthly"`
	ExpireAt   *time.Time   `json:"expire_at"`
	Periods    []PeriodView `json:"periods"`
	AutoExtend bool         `json:"auto_extend"`
}

func (h *Handlers) PreviewPayment(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var req paymentPreviewRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	p, err := h.Billing.PreviewPayment(c.UserContext(), id, money.FromMajor(req.Amount), req.RemainderToDays)
	if err != nil {
		return badRequest(err)
	}
	allocs := make([]allocationView, 0, len(p.Allocations))
	for _, a := range p.Allocations {
		allocs = append(allocs, allocationView{Kind: a.Kind, ID: a.ID, Title: a.Title, Months: a.Months, Days: a.Days, Amount: money.ToMajor(a.Amount), From: a.From, To: a.To})
	}
	items := make([]planItemView, 0, len(p.Items))
	for _, it := range p.Items {
		v := planItemView{Kind: it.Kind, ID: it.ID, Title: it.Title, Monthly: money.ToMajor(it.Monthly), ExpireAt: it.ExpireAt, AutoExtend: it.AutoExtend}
		for _, per := range it.Periods {
			v.Periods = append(v.Periods, PeriodView{Months: per.Months, Days: per.Days, Price: money.ToMajor(per.Price)})
		}
		items = append(items, v)
	}
	return c.JSON(fiber.Map{
		"balance_before":        money.ToMajor(p.BalanceBefore),
		"balance_after_payment": money.ToMajor(p.BalanceAfter),
		"allocations":           allocs,
		"remainder":             money.ToMajor(p.Remainder),
		"items":                 items,
		"referral":              referralView(p.Referral),
	})
}

func referralView(r *billing.Referral) fiber.Map {
	if r == nil {
		return nil
	}
	return fiber.Map{"referrer_id": r.ReferrerID, "referrer_name": r.ReferrerName, "percent": r.Percent, "amount": money.ToMajor(r.Amount)}
}

type commitPaymentRequest struct {
	Amount      float64 `json:"amount"`
	Date        Date    `json:"date"`
	Method      string  `json:"method"`
	Note        string  `json:"note"`
	Allocations []struct {
		Kind   string  `json:"kind"`
		ID     int     `json:"id"`
		Months int     `json:"months"`
		Days   int     `json:"days"`
		Amount float64 `json:"amount"`
	} `json:"allocations"`
}

func (h *Handlers) CommitPayment(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var req commitPaymentRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	in := billing.PaymentInput{
		CustomerID: id, Amount: money.FromMajor(req.Amount), Date: req.Date.Time,
		Method: req.Method, Note: req.Note,
	}
	for _, a := range req.Allocations {
		in.Allocations = append(in.Allocations, billing.AllocationInput{Kind: a.Kind, ID: a.ID, Months: a.Months, Days: a.Days, Amount: money.FromMajor(a.Amount)})
	}
	res, err := h.Billing.CommitPayment(c.UserContext(), in)
	if err != nil {
		return badRequest(err)
	}
	return c.JSON(fiber.Map{
		"payment_id": res.Payment.ID,
		"extensions": extensionResults(res.Extensions),
		"balance":    money.ToMajor(res.Balance),
		"referral":   referralView(res.Referral),
	})
}

func extensionResults(rs []billing.ExtensionResult) []fiber.Map {
	out := make([]fiber.Map, 0, len(rs))
	for _, r := range rs {
		out = append(out, extensionResult(r))
	}
	return out
}

func extensionResult(r billing.ExtensionResult) fiber.Map {
	return fiber.Map{"kind": r.Kind, "id": r.ID, "title": r.Title, "months": r.Months, "days": r.Days,
		"amount": money.ToMajor(r.Amount), "from": r.From, "to": r.To, "ok": r.OK, "error": r.Error}
}

type adjustRequest struct {
	Amount float64 `json:"amount"`
	Note   string  `json:"note"`
}

func (h *Handlers) AdjustBalance(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var req adjustRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	if err := h.Billing.Adjust(c.UserContext(), id, money.FromMajor(req.Amount), req.Note); err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) ListPayments(c *fiber.Ctx) error {
	pays, err := h.DB.Payment.Query().WithCustomer().Order(ent.Desc(payment.FieldDate), ent.Desc(payment.FieldID)).All(c.UserContext())
	if err != nil {
		return err
	}
	out := make([]PaymentView, 0, len(pays))
	for _, p := range pays {
		out = append(out, paymentView(p))
	}
	return c.JSON(out)
}

type updatePaymentRequest struct {
	Amount float64 `json:"amount"`
	Date   Date    `json:"date"`
	Method string  `json:"method"`
	Note   string  `json:"note"`
}

// UpdatePayment edits a payment together with its balance credit and the
// referral accrual (recalculated while it is not paid out). Extensions
// already made from it stay as they are.
func (h *Handlers) UpdatePayment(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	var req updatePaymentRequest
	if err := bind(c, &req); err != nil {
		return err
	}
	amount := money.FromMajor(req.Amount)
	if amount <= 0 {
		return apperr.New("amount_positive", "amount must be greater than zero")
	}
	date := req.Date.Ptr()
	if date == nil {
		return apperr.New("date_required", "set a date")
	}
	ctx := c.UserContext()
	err = store.WithTx(ctx, h.DB, func(tx *ent.Tx) error {
		if err := tx.Payment.UpdateOneID(id).
			SetAmount(amount).SetDate(*date).SetMethod(req.Method).SetNote(req.Note).
			Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.LedgerEntry.Update().
			Where(ledgerentry.PaymentID(id), ledgerentry.TypeEQ(ledgerentry.TypePayment)).
			SetAmount(amount).SetDate(*date).SetNote(req.Note).
			Save(ctx); err != nil {
			return err
		}
		accs, err := tx.ReferralAccrual.Query().Where(referralaccrual.PaymentID(id)).All(ctx)
		if err != nil {
			return err
		}
		for _, a := range accs {
			share := billing.ReferralAmount(amount, a.Percent)
			if err := a.Update().SetDate(*date).SetAmount(share).Exec(ctx); err != nil {
				return err
			}
			// the referrer's balance credit follows the accrual
			if _, err := tx.LedgerEntry.Update().
				Where(ledgerentry.PaymentID(id), ledgerentry.TypeEQ(ledgerentry.TypeReferral), ledgerentry.CustomerID(a.ReferrerID)).
				SetAmount(share).SetDate(*date).
				Save(ctx); err != nil {
				return err
			}
		}
		return nil
	})
	audit.Log(ctx, h.DB, "payment.update", "payment", id, req, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// DeletePayment removes a payment, its balance credit and referral accrual
// (with the referrer's credit).
// Extensions bought with it stay (and so do their charges).
func (h *Handlers) DeletePayment(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	if _, err := h.DB.LedgerEntry.Delete().Where(ledgerentry.PaymentID(id), ledgerentry.TypeIn(ledgerentry.TypePayment, ledgerentry.TypeReferral)).Exec(ctx); err != nil {
		return err
	}
	if _, err := h.DB.LedgerEntry.Update().Where(ledgerentry.PaymentID(id)).ClearPaymentID().Save(ctx); err != nil {
		return err
	}
	if _, err := h.DB.ReferralAccrual.Delete().Where(referralaccrual.PaymentID(id)).Exec(ctx); err != nil {
		return err
	}
	if _, err := h.DB.Extension.Update().Where(extension.PaymentID(id)).ClearPaymentID().Save(ctx); err != nil {
		return err
	}
	err = h.DB.Payment.DeleteOneID(id).Exec(ctx)
	audit.Log(ctx, h.DB, "payment.delete", "payment", id, nil, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) accrualViews(c *fiber.Ctx, where ...func(*ent.ReferralAccrualQuery)) ([]AccrualView, error) {
	ctx := c.UserContext()
	q := h.DB.ReferralAccrual.Query().Order(ent.Desc(referralaccrual.FieldDate))
	for _, w := range where {
		w(q)
	}
	rows, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	names, err := h.customerNames(c)
	if err != nil {
		return nil, err
	}
	out := make([]AccrualView, 0, len(rows))
	for _, r := range rows {
		out = append(out, AccrualView{
			ID: r.ID, ReferrerID: r.ReferrerID, ReferrerName: names[r.ReferrerID],
			RefereeID: r.RefereeID, RefereeName: names[r.RefereeID], PaymentID: r.PaymentID,
			Percent: r.Percent, Amount: money.ToMajor(r.Amount), Date: r.Date, Status: r.Status.String(),
		})
	}
	return out, nil
}

func (h *Handlers) customerNames(c *fiber.Ctx) (map[int]string, error) {
	all, err := h.DB.Customer.Query().Select(customer.FieldID, customer.FieldName).All(c.UserContext())
	if err != nil {
		return nil, err
	}
	m := make(map[int]string, len(all))
	for _, cu := range all {
		m[cu.ID] = cu.Name
	}
	return m, nil
}

// ReferralNode is a node of the "who brought whom" tree.
type ReferralNode struct {
	ID            int            `json:"id"`
	Name          string         `json:"name"`
	Archived      bool           `json:"archived"`
	Monthly       float64        `json:"monthly"`
	TotalPaid     float64        `json:"total_paid"`
	DirectCount   int            `json:"direct_count"`
	DirectActive  int            `json:"direct_active"`
	BranchCount   int            `json:"branch_count"`
	BranchPaid    float64        `json:"branch_paid"`
	BranchMonthly float64        `json:"branch_monthly"`
	AccruedTotal  float64        `json:"accrued_total"`
	Children      []ReferralNode `json:"children"`
}

func (h *Handlers) ReferralTree(c *fiber.Ctx) error {
	views, err := h.customerViews(c)
	if err != nil {
		return err
	}
	accruals, err := h.DB.ReferralAccrual.Query().All(c.UserContext())
	if err != nil {
		return err
	}
	accrued := map[int]int64{}
	for _, a := range accruals {
		accrued[a.ReferrerID] += a.Amount
	}
	children := map[int][]CustomerView{}
	var roots []CustomerView
	byID := map[int]bool{}
	for _, v := range views {
		byID[v.ID] = true
	}
	for _, v := range views {
		if v.ReferrerID != nil && byID[*v.ReferrerID] {
			children[*v.ReferrerID] = append(children[*v.ReferrerID], v)
		} else {
			roots = append(roots, v)
		}
	}
	var build func(v CustomerView, depth int) ReferralNode
	build = func(v CustomerView, depth int) ReferralNode {
		n := ReferralNode{
			ID: v.ID, Name: v.Name, Archived: v.Archived, Monthly: v.Monthly, TotalPaid: v.TotalPaid,
			AccruedTotal: money.ToMajor(accrued[v.ID]), Children: []ReferralNode{},
		}
		if depth > 50 { // guard against referral cycles
			return n
		}
		for _, ch := range children[v.ID] {
			cn := build(ch, depth+1)
			n.Children = append(n.Children, cn)
			n.DirectCount++
			if !ch.Archived {
				n.DirectActive++
			}
			n.BranchCount += 1 + cn.BranchCount
			n.BranchPaid += cn.TotalPaid + cn.BranchPaid
			n.BranchMonthly += cn.Monthly + cn.BranchMonthly
		}
		sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].BranchCount > n.Children[j].BranchCount })
		return n
	}
	out := make([]ReferralNode, 0, len(roots))
	for _, r := range roots {
		out = append(out, build(r, 0))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BranchCount > out[j].BranchCount })
	return c.JSON(out)
}

func (h *Handlers) ListAccruals(c *fiber.Ctx) error {
	out, err := h.accrualViews(c)
	if err != nil {
		return err
	}
	return c.JSON(out)
}
