// Package app wires the dynamic lab application process.
package app

import sharedconfig "website-gobased/internal/config"

// Config contains lab application runtime identity and settings.
type Config struct {
	Addr         string
	LabID        string
	InstanceID   string
	ScenarioType string
}

// LoadConfig loads lab application settings.
func LoadConfig() Config {
	return Config{
		Addr:         sharedconfig.String("LAB_APP_ADDR", ":8080"),
		LabID:        sharedconfig.String("LAB_ID", "unassigned"),
		InstanceID:   sharedconfig.String("INSTANCE_ID", "unassigned"),
		ScenarioType: sharedconfig.String("SCENARIO_TYPE", "scaffold"),
	}
}
