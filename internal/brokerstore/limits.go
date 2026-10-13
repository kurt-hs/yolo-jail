package brokerstore

import (
	"fmt"
	"strings"
	"time"
)

// PendingLimitError identifies the three persisted requests already waiting for
// this jail. Lifetime and set changes do not reset the per-jail notification cap.
type PendingLimitError struct{ Requests []string }

func (e *PendingLimitError) Error() string {
	return "pending request limit (3) reached for this jail: " + strings.Join(e.Requests, ", ")
}

// DenialCooldownError identifies the previous same-command denial. Command identity
// is canonical argv/repository/stdin digest, not the permission set or broker start.
type DenialCooldownError struct {
	Request string
	Until   time.Time
}

func (e *DenialCooldownError) Error() string {
	return fmt.Sprintf("command denied by %s; quiet until %s", e.Request, e.Until.Format(time.RFC3339))
}
func filingLimit(r *records, input Request, now time.Time) error {
	var pending []string
	for _, q := range r.requests {
		if q.Jail != input.Jail {
			continue
		}
		if q.Digest == input.Digest && (q.State == "denied" || q.State == "expired") && now.Before(q.Decided.Add(10*time.Minute)) {
			return &DenialCooldownError{Request: q.ID, Until: q.Decided.Add(10 * time.Minute)}
		}
		if q.State == "pending" {
			pending = append(pending, q.ID)
		}
	}
	if len(pending) >= 3 {
		return &PendingLimitError{Requests: pending}
	}
	return nil
}
