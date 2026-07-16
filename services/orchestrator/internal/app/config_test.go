package app

import "testing"

func TestLoadConfig(t *testing.T) {
	t.Setenv("MYSQL_ORCHESTRATOR_USER", "orchestrator")
	t.Setenv("MYSQL_ORCHESTRATOR_PASSWORD", "0123456789abcdef0123456789abcdef")
	t.Setenv("MYSQL_PLATFORM_DATABASE", "platform")
	t.Setenv("COMPOSE_PROJECT_NAME", "platform-test")
	t.Setenv("LAB_APP_IMAGE", "lab-app:test")
	t.Setenv("REDIS_IMAGE", "redis:test")
	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config.ComposeProject != "platform-test" || config.DockerAPIVersion != "1.47" {
		t.Fatalf("LoadConfig() = %#v", config)
	}
}

func TestLoadConfigRejectsWrongDatabase(t *testing.T) {
	t.Setenv("MYSQL_ORCHESTRATOR_USER", "orchestrator")
	t.Setenv("MYSQL_ORCHESTRATOR_PASSWORD", "0123456789abcdef0123456789abcdef")
	t.Setenv("MYSQL_PLATFORM_DATABASE", "other")
	t.Setenv("COMPOSE_PROJECT_NAME", "platform-test")
	t.Setenv("LAB_APP_IMAGE", "lab-app:test")
	t.Setenv("REDIS_IMAGE", "redis:test")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil; want database validation error")
	}
}
