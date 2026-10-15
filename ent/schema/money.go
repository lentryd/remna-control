package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"remna-control/internal/metered"
)

// Payment is money received from a customer.
type Payment struct {
	ent.Schema
}

func (Payment) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (Payment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("customer_id"),
		// Amount is in kopecks.
		field.Int64("amount").Positive(),
		field.Time("date"),
		field.String("method").Optional(),
		field.String("note").Optional(),
		// Historical payments come from the spreadsheet import: they count
		// as income but don't touch the customer's balance.
		field.Bool("historical").Default(false),
	}
}

func (Payment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("customer", Customer.Type).Ref("payments").Field("customer_id").Unique().Required(),
		edge.To("referral_accruals", ReferralAccrual.Type),
	}
}

// LedgerEntry is one movement of a customer's balance; the balance is the sum.
type LedgerEntry struct {
	ent.Schema
}

func (LedgerEntry) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (LedgerEntry) Fields() []ent.Field {
	return []ent.Field{
		field.Int("customer_id"),
		field.Enum("type").Values("payment", "charge", "adjustment", "refund", "referral"),
		// Amount is signed, in kopecks: payments are positive, charges negative.
		field.Int64("amount"),
		field.Time("date").Default(time.Now),
		field.Int("payment_id").Optional().Nillable(),
		field.String("note").Optional(),
	}
}

func (LedgerEntry) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("customer", Customer.Type).Ref("ledger_entries").Field("customer_id").Unique().Required(),
	}
}

// ReferralAccrual records what a referrer earned from a referee's payment.
// The amount is credited to the referrer's balance via a "referral" ledger
// entry linked to the same payment.
type ReferralAccrual struct {
	ent.Schema
}

func (ReferralAccrual) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (ReferralAccrual) Fields() []ent.Field {
	return []ent.Field{
		field.Int("referrer_id"),
		field.Int("referee_id"),
		field.Int("payment_id"),
		field.Float("percent"),
		field.Int64("amount"),
		field.Time("date"),
		field.Enum("status").Values("accrued", "paid", "credited").Default("accrued"),
	}
}

func (ReferralAccrual) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("payment", Payment.Type).Ref("referral_accruals").Field("payment_id").Unique().Required(),
	}
}

// Extension is an audit record of every expiry change we pushed to
// Remnawave (paid extension, add-on connection, tariff change surcharge).
type Extension struct {
	ent.Schema
}

func (Extension) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (Extension) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("kind").Values("extend", "connect", "tariff_change"),
		field.Int("customer_id"),
		field.Int("subscription_id").Optional().Nillable(),
		field.Int("subscription_addon_id").Optional().Nillable(),
		field.Int("payment_id").Optional().Nillable(),
		field.Int("months").Default(0),
		field.Int("days").Default(0),
		field.Int64("amount").Default(0),
		field.Time("from_at").Optional().Nillable(),
		field.Time("to_at").Optional().Nillable(),
		field.Int("ledger_entry_id").Optional().Nillable(),
		field.Enum("status").Values("ok", "failed"),
		field.String("error").Optional(),
		field.String("actor").Optional(),
	}
}

// ExpenseItem is a recurring infrastructure cost: either a fixed amount per
// period, or metered by the traffic of a Remnawave node (see package
// metered for the pricing models).
type ExpenseItem struct {
	ent.Schema
}

func (ExpenseItem) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (ExpenseItem) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("provider").Optional(),
		// RwProviderUUID links the item to an infra provider of the panel.
		field.String("rw_provider_uuid").Optional(),
		field.String("currency").Default("RUB"),
		field.Enum("pricing").Values("fixed", "metered").Default("fixed"),
		// Amount is the fixed price per period in minor units of Currency.
		field.Int64("amount").Default(0),
		field.Enum("period").Values("month", "year").Default("month"),
		field.Float("fee_percent").Default(0),
		field.Float("share_percent").Default(100),
		// Metered pricing: PricePerGB and MinCharge in minor units of Currency.
		field.Int64("price_per_gb").Default(0),
		field.Int64("min_charge").Default(0),
		// GBUnit is how the provider counts a GB: binary (1024³) or decimal (10⁹).
		field.Enum("gb_unit").Values("binary", "decimal").Default("binary"),
		// MinMode: floor = max(min_charge, usage); free = min_charge plus
		// usage beyond free_gb.
		field.Enum("min_mode").Values("floor", "free").Default("floor"),
		field.Float("free_gb").Default(0),
		// Tiers, when set, replace price_per_gb with graduated prices.
		field.JSON("tiers", []metered.Tier{}).Optional(),
		// BillingDay is the day of month a metered period starts on (1–28).
		field.Int("billing_day").Range(1, 28).Default(1),
		field.String("rw_node_uuid").Optional(),
		// RwSquadUUID narrows metered traffic to the users of one internal
		// squad: the node's traffic is split by their share of it.
		field.String("rw_squad_uuid").Optional(),
		field.Time("next_due_date").Optional().Nillable(),
		field.Bool("active").Default(true),
		field.String("notes").Optional(),
	}
}

// Expense is an actual charge or refund. Its RUB amount is frozen at
// creation, so later rate changes never rewrite history.
type Expense struct {
	ent.Schema
}

func (Expense) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (Expense) Fields() []ent.Field {
	return []ent.Field{
		field.Time("date"),
		field.String("provider").Optional(),
		field.String("rw_provider_uuid").Optional(),
		field.Int("expense_item_id").Optional().Nillable(),
		field.Enum("kind").Values("charge", "refund").Default("charge"),
		// OrigAmount is positive, in minor units of OrigCurrency.
		field.Int64("orig_amount").Positive(),
		field.String("orig_currency").Default("RUB"),
		field.Float("fx_rate").Default(1),
		field.Float("fee_percent").Default(0),
		field.Float("share_percent").Default(100),
		// RubAmount is signed kopecks: refunds are negative.
		field.Int64("rub_amount"),
		field.Int("refund_of_id").Optional().Nillable(),
		// Metered close-out: the computed cost and period, kept next to the
		// actual billed amount for "calculated vs. billed" history.
		field.Int64("calc_rub_amount").Optional().Nillable(),
		field.Float("metered_gb").Optional().Nillable(),
		field.String("period").Optional(),
		field.String("note").Optional(),
	}
}

// FxRate caches a rate (units of Base per one unit of Currency) for a date.
type FxRate struct {
	ent.Schema
}

func (FxRate) Fields() []ent.Field {
	return []ent.Field{
		field.String("date"),
		field.String("currency"),
		field.String("base").Default("RUB"),
		field.Float("rate"),
	}
}

func (FxRate) Indexes() []ent.Index {
	return []ent.Index{index.Fields("date", "currency", "base").Unique()}
}

// TrafficSnapshot is a node's traffic for one day.
type TrafficSnapshot struct {
	ent.Schema
}

func (TrafficSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.String("date"),
		field.String("node_uuid"),
		field.String("node_name").Optional(),
		field.Int64("bytes"),
		field.Time("synced_at").Default(time.Now),
	}
}

func (TrafficSnapshot) Indexes() []ent.Index {
	return []ent.Index{index.Fields("date", "node_uuid").Unique()}
}

// Setting is a key/value app setting editable from the UI.
type Setting struct {
	ent.Schema
}

func (Setting) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").Unique(),
		field.String("value"),
	}
}

// AuditLog records every mutating action, especially Remnawave calls.
type AuditLog struct {
	ent.Schema
}

func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.Time("at").Default(time.Now),
		field.String("actor").Optional(),
		field.String("action"),
		field.String("entity").Optional(),
		field.Int("entity_id").Optional(),
		field.String("payload").Optional(),
		field.Bool("ok").Default(true),
		field.String("error").Optional(),
	}
}
