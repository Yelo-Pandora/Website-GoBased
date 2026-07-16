package lifecycle

import "time"

type decisionKind string

const (
	decisionNone        decisionKind = "none"
	decisionExpiring    decisionKind = "expiring"
	decisionRunning     decisionKind = "running"
	decisionTerminating decisionKind = "terminating"
)

type decision struct {
	kind     decisionKind
	reason   string
	deadline time.Time
}

func evaluate(
	now time.Time,
	status string,
	startedAt *time.Time,
	lastEffectiveActionAt *time.Time,
	config Config,
) decision {
	if startedAt == nil || lastEffectiveActionAt == nil {
		return decision{kind: decisionNone}
	}
	idleDeadline := lastEffectiveActionAt.UTC().Add(config.IdleTimeout)
	maximumDeadline := startedAt.UTC().Add(config.MaxDuration)
	deadline := idleDeadline
	reason := "idle_timeout"
	if maximumDeadline.Before(deadline) {
		deadline = maximumDeadline
		reason = "maximum_duration"
	}
	if !now.Before(deadline) {
		return decision{kind: decisionTerminating, reason: reason, deadline: deadline}
	}
	if !now.Before(deadline.Add(-config.ExpiringLead)) {
		if status == "Running" {
			return decision{kind: decisionExpiring, reason: reason, deadline: deadline}
		}
		return decision{kind: decisionNone, reason: reason, deadline: deadline}
	}
	if status == "Expiring" {
		return decision{kind: decisionRunning, reason: reason, deadline: deadline}
	}
	return decision{kind: decisionNone, reason: reason, deadline: deadline}
}
