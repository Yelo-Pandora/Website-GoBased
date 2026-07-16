package app

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigBuildsRuntimeSettings(t *testing.T) {
	t.Setenv("MYSQL_USER", "lab_user")
	t.Setenv("MYSQL_PASSWORD", "test-password")
	t.Setenv("MYSQL_DATABASE", "lab_test")
	t.Setenv("EFFECTIVE_CAPACITY", "30")
	t.Setenv("CAPACITY_WINDOW_MS", "1500")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.EffectiveCapacity != 30 || cfg.CapacityWindow != 1500*time.Millisecond {
		t.Fatalf("capacity config = %#v", cfg)
	}
	if !strings.Contains(cfg.DatabaseDSN, "lab_user:test-password@") ||
		!strings.Contains(cfg.DatabaseDSN, "/lab_test?") {
		t.Fatalf("DatabaseDSN = %q", cfg.DatabaseDSN)
	}
}

func TestLoadConfigRejectsInvalidCapacity(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "capacity", key: "EFFECTIVE_CAPACITY", value: "0"},
		{name: "window", key: "CAPACITY_WINDOW_MS", value: "invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("MYSQL_USER", "lab_user")
			t.Setenv("MYSQL_PASSWORD", "test-password")
			t.Setenv("MYSQL_DATABASE", "lab_test")
			t.Setenv(test.key, test.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() error = nil for %s", test.key)
			}
		})
	}
}
