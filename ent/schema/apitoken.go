package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// APIToken lets other services call the public API (/api/v1). Only a hash
// of the token is stored; the token itself is shown once, on creation.
type APIToken struct {
	ent.Schema
}

func (APIToken) Fields() []ent.Field {
	return []ent.Field{
		// UUID identifies the token in the UI (filled for old rows on start).
		field.String("uuid").Optional().Unique(),
		field.String("name").NotEmpty(),
		field.String("token_hash").Unique().Sensitive(),
		// Prefix is the token's start, to tell tokens apart in the UI.
		field.String("prefix"),
		// Scopes as in the panel: "*", "<resource>:*|read|write" or an
		// endpoint key (see middleware.ScopeCatalog).
		field.Strings("scopes"),
		// ExpireAt nil means the token never expires (tokens made before
		// expiry existed).
		field.Time("expire_at").Optional().Nillable(),
		field.Time("created_at").Immutable().Default(time.Now),
		field.Time("last_used_at").Optional().Nillable(),
		// CompanyID binds the token to one company: it then sees only that
		// company's employees, balance and statements.
		field.Int("company_id").Optional().Nillable(),
	}
}

func (APIToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("company", Company.Type).Field("company_id").Unique(),
	}
}
