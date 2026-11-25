package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Company is a client with employees. Money goes through its billing-account
// Customer (payments, balance, ledger); the employees are CompanyMembers, not
// Subscriptions, and are billed by period statements on the report day.
type Company struct {
	ent.Schema
}

func (Company) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (Company) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("notes").Optional(),
		field.Int("customer_id").Unique(),
		field.Int("tariff_id"),
		// ReportDay is the day of month a period ends and is billed (1–28).
		field.Int("report_day").Min(1).Max(28),
		// The open period [period_start, period_end): a whole cycle, so a
		// period a company joined in the middle of still has its full length.
		field.Time("period_start"),
		field.Time("period_end"),
		// PeriodDays is the length of the cycle the open period is priced on:
		// a period that is shorter than a cycle (after a report day change)
		// is billed as its share of it.
		field.Int("period_days").Positive(),
		field.Bool("archived").Default(false),
		field.Time("archived_at").Optional().Nillable(),
	}
}

func (Company) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("customer", Customer.Type).Field("customer_id").Unique().Required(),
		edge.To("tariff", Tariff.Type).Field("tariff_id").Unique().Required(),
		edge.To("members", CompanyMember.Type),
		edge.To("statements", CompanyStatement.Type),
	}
}

// CompanyMember is an employee: one panel user, billed per day by segments.
type CompanyMember struct {
	ent.Schema
}

func (CompanyMember) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (CompanyMember) Fields() []ent.Field {
	return []ent.Field{
		field.Int("company_id"),
		// Username is the panel username and the company's external id.
		field.String("username").NotEmpty().Unique(),
		field.String("label").Optional(),
		field.Int("rw_user_id").Optional().Nillable().Unique(),
		// Protected members can't be removed through the API.
		field.Bool("protected").Default(false),
		// TariffPinned: the tariff was set for this member and doesn't follow
		// the company tariff.
		field.Bool("tariff_pinned").Default(false),
		field.Time("added_at"),
		field.Time("removed_at").Optional().Nillable(),
	}
}

func (CompanyMember) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("company", Company.Type).Ref("members").Field("company_id").Unique().Required(),
		edge.To("rw_user", RwUser.Type).Field("rw_user_id").Unique(),
		edge.To("segments", MemberSegment.Type),
	}
}

// MemberSegment is a stretch of days a member was billed on one set of values.
// The values are a snapshot, so editing a tariff never rewrites past days.
type MemberSegment struct {
	ent.Schema
}

func (MemberSegment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("member_id"),
		field.Time("from_date"),
		// ToDate nil means the segment is open.
		field.Time("to_date").Optional().Nillable(),
		field.Int("tariff_id"),
		// Price is the monthly price in kopecks.
		field.Int64("price"),
		field.Int64("traffic_limit_bytes"),
		field.String("traffic_strategy"),
		field.Int("hwid_limit").Optional().Nillable(),
		field.Strings("squad_uuids").Optional(),
		// Overrides lists the fields set explicitly for the member: price,
		// traffic_limit, traffic_strategy, hwid_limit, squads. The rest
		// follow the tariff.
		field.Strings("overrides").Optional(),
	}
}

func (MemberSegment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("member", CompanyMember.Type).Ref("segments").Field("member_id").Unique().Required(),
		edge.To("tariff", Tariff.Type).Field("tariff_id").Unique().Required(),
	}
}

func (MemberSegment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("member_id", "from_date"),
		// At most one open segment per member: a race can't bill twice.
		index.Fields("member_id").Unique().Annotations(entsql.IndexWhere("to_date IS NULL")),
	}
}

// CompanyStatement is what a company was billed for one period.
type CompanyStatement struct {
	ent.Schema
}

func (CompanyStatement) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (CompanyStatement) Fields() []ent.Field {
	return []ent.Field{
		field.Int("company_id"),
		field.Time("period_from"),
		field.Time("period_to"),
		field.Int("days_in_period"),
		// Total is in kopecks and equals the sum of the lines.
		field.Int64("total"),
		field.Int("ledger_entry_id").Optional().Nillable(),
		field.String("generated_by").Optional(),
	}
}

func (CompanyStatement) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("company", Company.Type).Ref("statements").Field("company_id").Unique().Required(),
		edge.To("lines", StatementLine.Type),
	}
}

func (CompanyStatement) Indexes() []ent.Index {
	return []ent.Index{index.Fields("company_id", "period_from").Unique()}
}

// StatementLine is one member segment inside a statement.
type StatementLine struct {
	ent.Schema
}

func (StatementLine) Fields() []ent.Field {
	return []ent.Field{
		field.Int("statement_id"),
		field.Int("member_id").Optional().Nillable(),
		field.String("username"),
		field.String("tariff_name"),
		field.Time("from_date"),
		field.Time("to_date"),
		field.Int("days"),
		field.Int("days_in_period"),
		field.Int64("monthly_price"),
		field.Int64("amount"),
	}
}

func (StatementLine) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("statement", CompanyStatement.Type).Ref("lines").Field("statement_id").Unique().Required(),
	}
}
