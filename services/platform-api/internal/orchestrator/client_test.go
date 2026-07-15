package orchestrator

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"website-gobased/internal/protocol"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestClientExecute(t *testing.T) {
	t.Parallel()

	client := newClient(&http.Client{Transport: roundTripperFunc(
		func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/v1/commands" ||
				request.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected request: %s %#v", request.URL, request.Header)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json; charset=utf-8"},
				},
				Body: io.NopCloser(strings.NewReader(`{
					"commandId":"command-1",
					"operationId":"operation-1",
					"status":"succeeded",
					"result":{"created":true}
				}`)),
			}, nil
		},
	)})
	response, err := client.Execute(context.Background(), protocol.Command{
		CommandType: "PROVISION_LAB",
		CommandID:   "command-1",
		OperationID: "operation-1",
		LabID:       "lab-test",
		RequestedBy: "7",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if response.Status != "succeeded" {
		t.Fatalf("Execute() status = %q", response.Status)
	}
}

func TestClientRejectsMismatchedResponse(t *testing.T) {
	t.Parallel()

	client := newClient(&http.Client{Transport: roundTripperFunc(
		func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"commandId":"other-command",
					"operationId":"operation-1",
					"status":"succeeded"
				}`)),
			}, nil
		},
	)})
	_, err := client.Execute(context.Background(), protocol.Command{
		CommandID:   "command-1",
		OperationID: "operation-1",
	})
	if err == nil {
		t.Fatal("Execute() error = nil")
	}
}
