package company

import (
	"time"

	"remna-control/ent"
	"remna-control/internal/apperr"
	"remna-control/internal/remnawave"
)

// Service manages companies, their employees and statements.
type Service struct {
	db  *ent.Client
	rw  *remnawave.Client
	loc *time.Location
	now func() time.Time
}

func New(db *ent.Client, rw *remnawave.Client, loc *time.Location) *Service {
	if loc == nil {
		loc = time.UTC
	}
	return &Service{db: db, rw: rw, loc: loc, now: time.Now}
}

// today is the midnight of the current calendar day.
func (s *Service) today() time.Time { return calendarDay(s.now(), s.loc) }

var (
	// ErrProtected answers an attempt to remove a protected member.
	ErrProtected = apperr.Status(409, "company.member_protected", "the member is protected")
	// ErrNotFound answers a member or statement of another company, too.
	ErrNotFound = apperr.Status(404, "company.not_found", "not found")
)
