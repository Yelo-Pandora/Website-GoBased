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
