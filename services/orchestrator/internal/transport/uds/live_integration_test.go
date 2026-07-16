package uds

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"website-gobased/internal/protocol"
)

func TestLiveProvisionIsIdempotentAndDestroyCleansResources(t *testing.T) {
	if os.Getenv("ORCHESTRATOR_LIVE_TEST") != "1" {
		t.Skip("set ORCHESTRATOR_LIVE_TEST=1 to run the live UDS test")
	}
	client := liveClient(os.Getenv("ORCHESTRATOR_SOCKET_PATH"))
	labID := "lab-phase4test"
	provision := protocol.Command{
		CommandType: "PROVISION_LAB",
		CommandID:   "cmd-phase4-provision",
		OperationID: "op-phase4-provision",
		LabID:       labID,
		RequestedBy: "phase4-test",
		Payload: json.RawMessage(`{
  "courseId":3,
  "scenarioType":"application_cluster",
  "scenarioTemplateId":"application_cluster_scenario_v1",
  "initialInstances":1,
  "redisRequired":false
}`),
	}
	destroy := protocol.Command{
		CommandType: "DESTROY_LAB",
		CommandID:   "cmd-phase4-destroy",
		OperationID: "op-phase4-destroy",
		LabID:       labID,
		RequestedBy: "phase4-test",
	}
	defer func() {
		response := executeLiveCommand(t, client, destroy)
		if response.Status != "succeeded" {
			t.Errorf("destroy response = %#v", response)
		}
	}()

	first := executeLiveCommand(t, client, provision)
	if first.Status != "succeeded" {
		t.Fatalf("first provision response = %#v", first)
	}
	second := executeLiveCommand(t, client, provision)
	if second.Status != "succeeded" {
		t.Fatalf("idempotent provision response = %#v", second)
	}
	reconcile := protocol.Command{
		CommandType: "RECONCILE_RESOURCES",
		CommandID:   "cmd-phase4-reconcile",
		OperationID: "op-phase4-reconcile",
		LabID:       labID,
		RequestedBy: "phase4-test",
		Payload:     json.RawMessage(`{"cleanup":false}`),
	}
	report := executeLiveCommand(t, client, reconcile)
	if report.Status != "succeeded" || !resultContains(report.Result, "orphanLabIds", labID) {
		t.Fatalf("reconciliation report = %#v", report)
	}
	reconcile.CommandID = "cmd-phase4-reconcile-cleanup"
	reconcile.OperationID = "op-phase4-reconcile-cleanup"
	reconcile.Payload = json.RawMessage(`{"cleanup":true}`)
	cleanup := executeLiveCommand(t, client, reconcile)
	if cleanup.Status != "succeeded" || !resultContains(cleanup.Result, "cleanedLabIds", labID) {
		t.Fatalf("reconciliation cleanup = %#v", cleanup)
	}
}

func resultContains(value any, field string, expected string) bool {
	fields, ok := value.(map[string]any)
	if !ok {
		return false
	}
	values, ok := fields[field].([]any)
	if !ok {
		return false
	}
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func liveClient(socketPath string) *http.Client {
	if socketPath == "" {
		socketPath = "/run/platform/orchestrator.sock"
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(
				ctx,
				"unix",
				socketPath,
			)
		},
	}
	return &http.Client{Transport: transport, Timeout: 60 * time.Second}
}

func executeLiveCommand(
	t *testing.T,
	client *http.Client,
	command protocol.Command,
) protocol.CommandResponse {
	t.Helper()
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request, err := http.NewRequest(
		http.MethodPost,
		"http://orchestrator/v1/commands",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("command HTTP status = %s", response.Status)
	}
	var result protocol.CommandResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if result.CommandID != command.CommandID || result.OperationID != command.OperationID {
		t.Fatalf("command identity mismatch: %s", fmt.Sprintf("%#v", result))
	}
	return result
}
