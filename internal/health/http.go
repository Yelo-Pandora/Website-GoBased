// Package health provides consistent HTTP health responses.
package health

import (
	"encoding/json"
	"net/http"
	"time"
)

// Response is the shared health endpoint payload.
type Response struct {
	Service   string         `json:"service"`
	Status    string         `json:"status"`
	Timestamp time.Time      `json:"timestamp"`
	Details   map[string]any `json:"details,omitempty"`
}

// Write writes a health response as JSON.
func Write(w http.ResponseWriter, statusCode int, response Response) error {
	response.Timestamp = time.Now().UTC()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(response)
}
