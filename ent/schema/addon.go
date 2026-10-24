package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Addon is a kind of add-on: an extra Remnawave user named
// prefix+<main username>+suffix whose configs are appended to the main
// subscription (by subpage or any service reading GET /api/v1/addons). It
// comes from the add-ons file (source "file", read-only here) or is
// managed in the UI (source "ui").
type Addon struct {
	ent.Schema
}

func (Addon) Mixin() []ent.Mixin { return []ent.Mixin{TimeMixin{}} }

func (Addon) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique(),
		field.String("prefix").Optional(),
		field.String("suffix").Optional(),
		// InConfig is false once a file add-on disappears from the file.
		field.Bool("in_config").Default(true),
		field.Enum("source").Values("file", "ui").Default("file"),
		// Archived add-ons sit in the archive group; only they can be deleted.
		field.Bool("archived").Default(false),
		// Remark/RemarkUnlimited/Stubs are subpage's display settings:
		// config names ({remark}, {remaining}, {limit}, {used}) and the
		// placeholder config names per non-active status.
		field.String("remark").Optional(),
		field.String("remark_unlimited").Optional(),
		field.JSON("stubs", map[string]string{}).Optional(),
	}
}

func (Addon) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("tariffs", Tariff.Type),
		edge.To("subscription_addons", SubscriptionAddon.Type),
	}
}
