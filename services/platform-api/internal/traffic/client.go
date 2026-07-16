package traffic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"website-gobased/internal/protocol"
)

const maxResponseBytes = 32 << 10

// Client submits trusted internal requests through the fixed lab gateway.
type Client struct {
	baseURL *url.URL
	client  *http.Client
}

// NewClient returns an internal traffic client with a fixed gateway and timeout.
func NewClient(baseURL string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("lab gateway address is invalid")
	}
	if timeout <= 0 {
		return nil, errors.New("traffic client timeout must be positive")
	}
	return &Client{
		baseURL: parsed,
		client:  &http.Client{Timeout: timeout},
	}, nil
}

// Submit forwards one batch while fixing the internal Host header from the lab ID.
func (c *Client) Submit(
	ctx context.Context,
	labID string,
	request protocol.TrafficBatchRequest,
) (protocol.TrafficBatchResult, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return protocol.TrafficBatchResult{}, fmt.Errorf("encode traffic batch: %w", err)
	}
	target := *c.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + "/internal/order-batch"
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		target.String(),
		bytes.NewReader(body),
	)
	if err != nil {
		return protocol.TrafficBatchResult{}, fmt.Errorf("create traffic request: %w", err)
	}
	httpRequest.Host = labID + ".lab.internal"
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(httpRequest)
	if err != nil {
		return protocol.TrafficBatchResult{}, fmt.Errorf("submit traffic batch: %w", err)
	}
	defer response.Body.Close()
	reader := io.LimitReader(response.Body, maxResponseBytes+1)
	responseBody, err := io.ReadAll(reader)
	if err != nil {
		return protocol.TrafficBatchResult{}, fmt.Errorf("read traffic response: %w", err)
	}
	if len(responseBody) > maxResponseBytes {
		return protocol.TrafficBatchResult{}, errors.New("traffic response is too large")
	}
	if response.StatusCode != http.StatusOK {
		return protocol.TrafficBatchResult{}, fmt.Errorf("traffic gateway status: %s", response.Status)
	}
	var envelope struct {
		Data struct {
			Result protocol.TrafficBatchResult `json:"result"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return protocol.TrafficBatchResult{}, fmt.Errorf("decode traffic response: %w", err)
	}
	return envelope.Data.Result, nil
}

// CloseIdleConnections closes pooled gateway connections.
func (c *Client) CloseIdleConnections() {
	c.client.CloseIdleConnections()
}
