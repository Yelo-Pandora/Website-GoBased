package app

import (
	"strings"
	"testing"
)

func TestLoadConfigBuildsRuntimeSettings(t *testing.T) {
	t.Setenv("MYSQL_USER", "lab_user")
	t.Setenv("MYSQL_PASSWORD", "test-password")
	t.Setenv("MYSQL_DATABASE", "lab_test")
	t.Setenv("PROCESSING_SPEED", "6")
	t.Setenv("MAX_LOAD", "100")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.ProcessingSpeed != 6 || cfg.MaxLoad != 100 {
		t.Fatalf("load config = %#v", cfg)
	}
	if !strings.Contains(cfg.DatabaseDSN, "lab_user:test-password@") ||
		!strings.Contains(cfg.DatabaseDSN, "/lab_test?") {
		t.Fatalf("DatabaseDSN = %q", cfg.DatabaseDSN)
	}
}

func TestLoadConfigRejectsInvalidLoadModel(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "processing speed", key: "PROCESSING_SPEED", value: "0"},
		{name: "max load", key: "MAX_LOAD", value: "invalid"},
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
