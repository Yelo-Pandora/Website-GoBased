// Package orchestrator provides the platform API client for the internal orchestrator.
package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"website-gobased/internal/protocol"
)

const maxResponseBodyBytes = 1 << 20

// Client executes structured commands over the orchestrator Unix Domain Socket.
type Client struct {
	httpClient *http.Client
	transport  *http.Transport
}

// NewClient returns a bounded HTTP/JSON over UDS client.
func NewClient(socketPath string, timeout time.Duration) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: timeout}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableKeepAlives:   false,
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
	}
	return &Client{
		httpClient: &http.Client{Transport: transport, Timeout: timeout},
		transport:  transport,
	}
}

func newClient(httpClient *http.Client) *Client {
	return &Client{httpClient: httpClient}
}

// CloseIdleConnections releases idle UDS connections held by the client.
func (c *Client) CloseIdleConnections() {
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
}

// Execute sends one command and validates the stable response envelope.
func (c *Client) Execute(
	ctx context.Context,
	command protocol.Command,
) (protocol.CommandResponse, error) {
	body, err := json.Marshal(command)
	if err != nil {
		return protocol.CommandResponse{}, fmt.Errorf("encode orchestrator command: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"http://orchestrator/v1/commands",
		bytes.NewReader(body),
	)
	if err != nil {
		return protocol.CommandResponse{}, fmt.Errorf("create orchestrator request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return protocol.CommandResponse{}, fmt.Errorf("execute orchestrator command: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxResponseBodyBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return protocol.CommandResponse{}, fmt.Errorf("read orchestrator response: %w", err)
	}
	if len(responseBody) > maxResponseBodyBytes {
		return protocol.CommandResponse{}, errors.New("orchestrator response is too large")
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		return protocol.CommandResponse{}, fmt.Errorf(
			"orchestrator returned content type %q",
			response.Header.Get("Content-Type"),
		)
	}
	var result protocol.CommandResponse
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return protocol.CommandResponse{}, fmt.Errorf("decode orchestrator response: %w", err)
	}
	if err := ensureResponseConsumed(decoder); err != nil {
		return protocol.CommandResponse{}, err
	}
	if result.CommandID != command.CommandID ||
		result.OperationID != command.OperationID {
		return protocol.CommandResponse{}, errors.New("orchestrator response identity mismatch")
	}
	if !validResponseStatus(result.Status) {
		return protocol.CommandResponse{}, fmt.Errorf(
			"orchestrator returned invalid status %q",
			result.Status,
		)
	}
	return result, nil
}

func ensureResponseConsumed(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode extra orchestrator response data: %w", err)
	}
	return errors.New("orchestrator returned multiple JSON values")
}

func validResponseStatus(status string) bool {
	switch status {
	case "accepted", "succeeded", "failed", "rejected":
		return true
	default:
		return false
	}
}
