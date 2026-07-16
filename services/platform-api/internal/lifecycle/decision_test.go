package lifecycle

import (
	"testing"
	"time"
)

func TestEvaluateLifecycleDeadlines(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	config := Config{
		IdleTimeout:  10 * time.Minute,
		MaxDuration:  30 * time.Minute,
		ExpiringLead: time.Minute,
	}
	started := now.Add(-20 * time.Minute)
	lastAction := now.Add(-9 * time.Minute)
	decision := evaluate(now, "Running", &started, &lastAction, config)
	if decision.kind != decisionExpiring || decision.reason != "idle_timeout" {
		t.Fatalf("decision = %#v; want idle expiration warning", decision)
	}

	started = now.Add(-30 * time.Minute)
	lastAction = now.Add(-2 * time.Minute)
	decision = evaluate(now, "Expiring", &started, &lastAction, config)
	if decision.kind != decisionTerminating || decision.reason != "maximum_duration" {
		t.Fatalf("decision = %#v; want maximum duration termination", decision)
	}

	started = now.Add(-10 * time.Minute)
	lastAction = now.Add(-2 * time.Minute)
	decision = evaluate(now, "Expiring", &started, &lastAction, config)
	if decision.kind != decisionRunning {
		t.Fatalf("decision = %#v; want refreshed lab to return Running", decision)
	}
}

func TestEvaluateRequiresPersistedActivityTimes(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	started := now
	decision := evaluate(now, "Running", &started, nil, Config{
		IdleTimeout:  10 * time.Minute,
		MaxDuration:  30 * time.Minute,
		ExpiringLead: time.Minute,
	})
	if decision.kind != decisionNone {
		t.Fatalf("decision = %#v; want none", decision)
	}
}
