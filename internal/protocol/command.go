// Package protocol defines stable control-plane message contracts.
package protocol

import "encoding/json"

// Command is the HTTP/JSON command sent to the orchestrator over UDS.
type Command struct {
	CommandType string          `json:"commandType"`
	CommandID   string          `json:"commandId"`
	OperationID string          `json:"operationId"`
	LabID       string          `json:"labId"`
	RequestedBy string          `json:"requestedBy"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// CommandResponse is the stable orchestrator response envelope.
type CommandResponse struct {
	CommandID   string `json:"commandId"`
	OperationID string `json:"operationId"`
	Status      string `json:"status"`
	Result      any    `json:"result,omitempty"`
	Error       any    `json:"error,omitempty"`
}
