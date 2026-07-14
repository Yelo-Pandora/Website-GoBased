package app

import (
	"strings"
	"testing"
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
