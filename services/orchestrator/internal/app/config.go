// Package app wires the orchestrator process.
package app

import sharedconfig "website-gobased/internal/config"

// Config contains orchestrator runtime settings.
type Config struct {
	SocketPath string
}

// LoadConfig loads orchestrator settings.
func LoadConfig() Config {
	return Config{
		SocketPath: sharedconfig.String(
			"ORCHESTRATOR_SOCKET_PATH",
			"/run/platform/orchestrator.sock",
		),
	}
}
