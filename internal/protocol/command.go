// command.go 定义了用于与Orchestrator通信的命令和响应结构。
package protocol

import "encoding/json"

// Command 是一个稳定的Orchestrator命令信封。
type Command struct {
	CommandType string          `json:"commandType"`
	CommandID   string          `json:"commandId"`
	OperationID string          `json:"operationId"`
	LabID       string          `json:"labId"`
	RequestedBy string          `json:"requestedBy"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// CommandResponse 是一个稳定的Orchestrator命令响应信封。
type CommandResponse struct {
	CommandID   string `json:"commandId"`
	OperationID string `json:"operationId"`
	Status      string `json:"status"`
	Result      any    `json:"result,omitempty"`
	Error       any    `json:"error,omitempty"`
}
