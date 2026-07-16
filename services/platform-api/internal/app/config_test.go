package app

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigRejectsUnexpectedDatabase(t *testing.T) {
	t.Setenv("MYSQL_PLATFORM_USER", "platform_api")
	t.Setenv("MYSQL_PLATFORM_PASSWORD", "test-password")
	t.Setenv("MYSQL_PLATFORM_DATABASE", "other")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig() error = nil; want database validation error")
	}
	if !strings.Contains(err.Error(), "must be \"platform\"") {
		t.Fatalf("LoadConfig() error = %q; want platform database error", err)
	}
}

func TestLoadConfigAuthDefaults(t *testing.T) {
	setRequiredConfig(t)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.AuthSessionTTL != 8*time.Hour {
		t.Fatalf("AuthSessionTTL = %v; want %v", cfg.AuthSessionTTL, 8*time.Hour)
	}
	if cfg.AuthCookieName != "session" || cfg.AuthCookieSecure {
		t.Fatalf(
			"Auth Cookie config = %q, %t",
			cfg.AuthCookieName,
			cfg.AuthCookieSecure,
		)
	}
}

func TestLoadConfigLabControlDefaults(t *testing.T) {
	setRequiredConfig(t)
	t.Setenv("HOSTNAME", "platform-api-test")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.LabMaxActive != 10 || cfg.LabMaxTempContainers != 40 ||
		cfg.LabMaxInstances != 4 {
		t.Fatalf("lab quota config = %#v", cfg)
	}
	if cfg.LabOperationPoll != 500*time.Millisecond ||
		cfg.LabOperationLease != 30*time.Second ||
		cfg.LabOrchestratorTimeout != 20*time.Second {
		t.Fatalf("lab worker duration config = %#v", cfg)
	}
	if cfg.LabOperationWorkerID != "platform-api-test" {
		t.Fatalf("LabOperationWorkerID = %q", cfg.LabOperationWorkerID)
	}
	if cfg.LabIdleTimeout != 10*time.Minute || cfg.LabMaxDuration != 30*time.Minute ||
		cfg.LabExpiringLead != time.Minute || cfg.LabLifecyclePoll != 5*time.Second ||
		cfg.LabReconcileInterval != time.Minute || !cfg.LabReconcileCleanup {
		t.Fatalf("lifecycle config = %#v", cfg)
	}
	if cfg.TrafficRequestTimeout != 3*time.Second || cfg.TrafficRatePerSecond != 10 ||
		cfg.TrafficBurst != 10 || cfg.TrafficLimiterEntries != 2048 {
		t.Fatalf("traffic config = %#v", cfg)
	}
}

func TestLoadConfigRejectsInvalidLabControlConfig(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "active limit", key: "LAB_MAX_ACTIVE", value: "0"},
		{name: "container limit", key: "LAB_MAX_TEMP_CONTAINERS", value: "0"},
		{name: "instance limit", key: "LAB_MAX_INSTANCES_PER_SESSION", value: "0"},
		{name: "worker id", key: "LAB_OPERATION_WORKER_ID", value: "bad worker"},
		{name: "poll interval", key: "LAB_OPERATION_POLL_INTERVAL", value: "0s"},
		{name: "lease duration", key: "LAB_OPERATION_LEASE_DURATION", value: "10s"},
		{name: "idle timeout", key: "LAB_IDLE_TIMEOUT", value: "0s"},
		{name: "maximum duration", key: "LAB_MAX_DURATION", value: "0s"},
		{name: "expiring lead", key: "LAB_EXPIRING_LEAD", value: "10m"},
		{name: "lifecycle poll", key: "LAB_LIFECYCLE_POLL_INTERVAL", value: "0s"},
		{name: "reconcile interval", key: "LAB_RECONCILE_INTERVAL", value: "0s"},
		{name: "reconcile cleanup", key: "LAB_RECONCILE_CLEANUP", value: "sometimes"},
		{name: "traffic timeout", key: "TRAFFIC_REQUEST_TIMEOUT", value: "0s"},
		{name: "traffic rate", key: "TRAFFIC_RATE_PER_SECOND", value: "0"},
		{name: "traffic burst", key: "TRAFFIC_BURST", value: "0"},
		{name: "traffic limiter entries", key: "TRAFFIC_LIMITER_MAX_ENTRIES", value: "0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRequiredConfig(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() error = nil for %s", test.key)
			}
		})
	}
}

func TestLoadConfigRejectsInvalidAuthConfig(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "session ttl", key: "AUTH_SESSION_TTL", value: "invalid"},
		{name: "cookie name", key: "AUTH_COOKIE_NAME", value: "bad cookie"},
		{name: "secure flag", key: "AUTH_COOKIE_SECURE", value: "sometimes"},
		{name: "max failures", key: "AUTH_LOGIN_MAX_FAILURES", value: "0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setRequiredConfig(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() error = nil for %s", test.key)
			}
		})
	}
}

func setRequiredConfig(t *testing.T) {
	t.Helper()
	t.Setenv("MYSQL_PLATFORM_USER", "platform_api")
	t.Setenv("MYSQL_PLATFORM_PASSWORD", "test-password")
	t.Setenv("MYSQL_PLATFORM_DATABASE", "platform")
}
