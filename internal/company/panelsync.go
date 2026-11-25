package company

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"remna-control/ent"
	"remna-control/internal/jobs"
)

// SyncMembersPanel pushes the traffic limit, strategy, HWID limit and squads
// of active members to the panel when the cached panel user differs from
// what their open segment says. Expiry, status and deletion are never
// touched.
func (s *Service) SyncMembersPanel(ctx context.Context) (jobs.Result, error) {
	segs, err := s.openSegments(ctx)
	if err != nil {
		return jobs.Result{}, err
	}
	var pushed int
	var errs []error
	for _, seg := range segs {
		m := seg.Edges.Member
		if m.RwUserID == nil {
			continue
		}
		cached, err := s.db.RwUser.Get(ctx, *m.RwUserID)
		if err != nil {
			if ent.IsNotFound(err) {
				continue
			}
			return jobs.Result{}, err
		}
		v := segmentValues(seg)
		if cached.TrafficLimitBytes == v.TrafficLimitBytes &&
			cached.TrafficLimitStrategy == panelStrategy(v) &&
			sameHwid(cached.HwidDeviceLimit, v.HwidLimit) &&
			sameSet(cached.SquadUuids, v.Squads) {
			continue
		}
		if err := s.pushParams(ctx, *m.RwUserID, v); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.Username, err))
			continue
		}
		pushed++
	}
	res := jobs.Result{Changed: pushed > 0}
	if pushed > 0 {
		res.Summary = fmt.Sprintf("%d company members updated in the panel", pushed)
	}
	return res, errors.Join(errs...)
}

func sameHwid(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameSet(a, b []string) bool {
	x, y := slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b))
	return slices.Equal(x, y)
}
