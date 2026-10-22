package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Subscription is a customer's VPN access, backed by one Remnawave user. The
// RW link may be empty right after an import, until it's linked in the UI.
type Subscription struct {
	ent.Schema
}

func (Subscription) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (Subscription) Fields() []ent.Field {
	return []ent.Field{
		field.Int("customer_id"),
		field.Int("tariff_id").Optional().Nillable(),
		field.Int("rw_user_id").Optional().Nillable().Unique(),
		field.String("label").Optional(),
		// PriceOverride (kopecks) wins over the tariff's monthly price.
		field.Int64("price_override").Optional().Nillable(),
		field.Bool("auto_extend").Default(true),
		// TrafficAnchorAt is when the continuous paid coverage started;
		// TrafficResetAt is the latest reset point already handled.
		field.Time("traffic_anchor_at").Optional().Nillable(),
		field.Time("traffic_reset_at").Optional().Nillable(),
	}
}

func (Subscription) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("customer", Customer.Type).Ref("subscriptions").Field("customer_id").Unique().Required(),
		edge.From("tariff", Tariff.Type).Ref("subscriptions").Field("tariff_id").Unique(),
		edge.From("rw_user", RwUser.Type).Ref("subscription").Field("rw_user_id").Unique(),
		edge.To("addons", SubscriptionAddon.Type),
	}
}

// SubscriptionAddon is an add-on attached to a subscription, backed by its
// own Remnawave user.
type SubscriptionAddon struct {
	ent.Schema
}

func (SubscriptionAddon) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (SubscriptionAddon) Fields() []ent.Field {
	return []ent.Field{
		field.Int("subscription_id"),
		field.Int("addon_id"),
		field.Int("tariff_id").Optional().Nillable(),
		field.Int("rw_user_id").Optional().Nillable().Unique(),
		field.Int64("price_override").Optional().Nillable(),
		field.Bool("auto_extend").Default(true),
		// Included add-ons come with the subscription's tariff: free, and
		// extended, enabled and disabled together with the subscription.
		field.Bool("included").Default(false),
		// TrafficAnchorAt is when the continuous paid coverage started;
		// TrafficResetAt is the latest reset point already handled.
		field.Time("traffic_anchor_at").Optional().Nillable(),
		field.Time("traffic_reset_at").Optional().Nillable(),
	}
}

func (SubscriptionAddon) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("subscription", Subscription.Type).Ref("addons").Field("subscription_id").Unique().Required(),
		edge.From("addon", Addon.Type).Ref("subscription_addons").Field("addon_id").Unique().Required(),
		edge.From("tariff", Tariff.Type).Ref("subscription_addons").Field("tariff_id").Unique(),
		edge.From("rw_user", RwUser.Type).Ref("subscription_addon").Field("rw_user_id").Unique(),
	}
}
