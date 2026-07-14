// Package health defines health-check response models.
package health

import (
	"time"
)

// Response is the shared health-check response.
type Response struct {
	Service   string         `json:"service"`
	Status    string         `json:"status"`
	Timestamp time.Time      `json:"timestamp"`
	Details   map[string]any `json:"details,omitempty"`
}

// NewResponse returns a timestamped health-check response.
func NewResponse(service, status string, details map[string]any) Response {
	return Response{
		Service:   service,
		Status:    status,
		Timestamp: time.Now().UTC(),
		Details:   details,
	}
}
