package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Tariff is a price plan, either for base subscriptions or for one add-on.
// A base tariff may include add-ons: their add-on tariffs' Remnawave
// parameters apply, they cost nothing extra and follow the subscription's
// term.
// When ManageRw is set its Remnawave parameters are pushed to the user on
// creation and on tariff change.
type Tariff struct {
	ent.Schema
}

func (Tariff) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (Tariff) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("kind").Values("base", "addon"),
		field.Int("addon_id").Optional().Nillable(),
		field.String("name").NotEmpty(),
		field.String("description").Optional(),
		// MonthlyPrice is in kopecks.
		field.Int64("monthly_price").NonNegative(),
		field.Bool("active").Default(true),
		field.Int("sort_order").Default(0),
		field.Bool("manage_rw").Default(false),
		field.Int64("traffic_limit_bytes").Default(0),
		field.String("traffic_strategy").Default("NO_RESET"),
		field.Int("hwid_limit").Optional().Nillable(),
		field.Strings("squad_uuids").Optional(),
		// ExternalSquadUUID is the Remnawave external squad (subscription page
		// and related settings); nil leaves the panel value alone.
		field.String("external_squad_uuid").Optional().Nillable(),
	}
}

func (Tariff) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("addon", Addon.Type).Ref("tariffs").Field("addon_id").Unique(),
		edge.To("periods", TariffPeriod.Type),
		edge.To("subscriptions", Subscription.Type),
		edge.To("subscription_addons", SubscriptionAddon.Type),
		edge.To("included_addons", Tariff.Type).From("included_in"),
	}
}

// TariffPeriod is a fixed price for a term: a discount for paying several
// months at once, or a short package in days (e.g. a week-long trial).
type TariffPeriod struct {
	ent.Schema
}

func (TariffPeriod) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tariff_id"),
		field.Int("months").NonNegative(),
		field.Int("days").NonNegative().Default(0),
		// Price is the total for Months+Days, in kopecks.
		field.Int64("price").NonNegative(),
	}
}

func (TariffPeriod) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tariff", Tariff.Type).Ref("periods").Field("tariff_id").Unique().Required(),
	}
}
