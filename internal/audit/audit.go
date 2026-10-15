// Package audit records mutating actions (who did what, with which payload,
// and whether the Remnawave call succeeded).
package audit

import (
	"context"
	"encoding/json"
	"log/slog"

	"remna-control/ent"
)

type actorKey struct{}

// WithActor stores the acting admin's name in ctx.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// Actor returns the acting admin's name, or "system".
func Actor(ctx context.Context) string {
	if a, ok := ctx.Value(actorKey{}).(string); ok && a != "" {
		return a
	}
	return "system"
}

// Log writes an audit row; failures are only logged, never returned.
func Log(ctx context.Context, db *ent.Client, action, entity string, entityID int, payload any, opErr error) {
	b, _ := json.Marshal(payload)
	q := db.AuditLog.Create().
		SetActor(Actor(ctx)).
		SetAction(action).
		SetEntity(entity).
		SetEntityID(entityID).
		SetPayload(string(b)).
		SetOk(opErr == nil)
	if opErr != nil {
		q.SetError(opErr.Error())
	}
	if err := q.Exec(context.WithoutCancel(ctx)); err != nil {
		slog.Warn("audit log write failed", "action", action, "error", err)
	}
}
