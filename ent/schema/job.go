package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// JobStatus is the latest state of a background job (one row per job).
type JobStatus struct {
	ent.Schema
}

func (JobStatus) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "job_status"}}
}

func (JobStatus) Fields() []ent.Field {
	return []ent.Field{
		field.String("job").Unique(),
		field.Time("last_run_at").Optional().Nillable(),
		field.Time("last_success_at").Optional().Nillable(),
		field.Time("last_failure_at").Optional().Nillable(),
		field.Bool("last_ok").Default(false),
		field.Int64("last_duration_ms").Default(0),
		field.String("last_summary").Optional(),
		field.String("last_error").Optional(),
		field.Int("runs").Default(0),
		field.Int("failures").Default(0),
	}
}

// JobRun is one entry in the background jobs run history. Consecutive
// identical results are folded into one row via repeat.
type JobRun struct {
	ent.Schema
}

func (JobRun) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "job_runs"}}
}

func (JobRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("job"),
		field.Time("started_at").Default(time.Now),
		field.Time("finished_at").Default(time.Now),
		field.Int64("duration_ms").Default(0),
		field.Bool("ok"),
		field.String("text").Optional(),
		field.Int("repeat").Default(1),
	}
}

func (JobRun) Indexes() []ent.Index {
	return []ent.Index{index.Fields("job", "ok", "started_at")}
}
