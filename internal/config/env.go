// Package config provides environment-backed configuration helpers.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// String returns an environment value or fallback when the value is empty.
func String(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// Required returns a non-empty environment value.
func Required(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("environment variable %s is required", key)
	}
	return value, nil
}

// Int returns an integer environment value or fallback when the value is empty.
func Int(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse environment variable %s: %w", key, err)
	}
	return parsed, nil
}
