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

// CacheState reads the bounded cache inventory through the fixed lab gateway.
func (c *Client) CacheState(ctx context.Context, labID string) (protocol.CacheState, error) {
	return c.cacheStateAt(ctx, labID, "/internal/cache-state")
}

func (c *Client) InstanceCacheState(
	ctx context.Context,
	labID string,
	instanceID string,
) (protocol.CacheState, error) {
	if !validInstanceID(instanceID) {
		return protocol.CacheState{}, errors.New("cache instance is invalid")
	}
	return c.cacheStateAt(ctx, labID, "/internal/instances/"+instanceID+"/cache-state")
}

func (c *Client) cacheStateAt(ctx context.Context, labID, path string) (protocol.CacheState, error) {
	target := *c.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return protocol.CacheState{}, fmt.Errorf("create cache state request: %w", err)
	}
	request.Host = labID + ".lab.internal"
	response, err := c.client.Do(request)
	if err != nil {
		return protocol.CacheState{}, fmt.Errorf("read cache state: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return protocol.CacheState{}, fmt.Errorf("cache gateway status: %s", response.Status)
	}
	reader := io.LimitReader(response.Body, maxResponseBytes+1)
	body, err := io.ReadAll(reader)
	if err != nil || len(body) > maxResponseBytes {
		return protocol.CacheState{}, errors.New("cache state response is invalid")
	}
	var envelope struct {
		Data struct {
			Cache protocol.CacheState `json:"cache"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return protocol.CacheState{}, fmt.Errorf("decode cache state: %w", err)
	}
	return envelope.Data.Cache, nil
}

func (c *Client) SetL1(
	ctx context.Context,
	labID string,
	instanceID string,
	enabled bool,
) (protocol.CacheInstanceState, error) {
	if !validInstanceID(instanceID) {
		return protocol.CacheInstanceState{}, errors.New("cache instance is invalid")
	}
	action := "remove_l1"
	if enabled {
		action = "add_l1"
	}
	body, _ := json.Marshal(map[string]string{"action": action})
	target := *c.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + "/internal/instances/" + instanceID + "/cache-actions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return protocol.CacheInstanceState{}, fmt.Errorf("create cache action request: %w", err)
	}
	request.Host = labID + ".lab.internal"
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return protocol.CacheInstanceState{}, fmt.Errorf("execute cache action: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return protocol.CacheInstanceState{}, fmt.Errorf("cache action status: %s", response.Status)
	}
	var envelope struct {
		Data struct {
			Instance protocol.CacheInstanceState `json:"instance"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || envelope.Data.Instance.InstanceID != instanceID {
		return protocol.CacheInstanceState{}, errors.New("cache action response is invalid")
	}
	return envelope.Data.Instance, nil
}

func validInstanceID(value string) bool {
	if !strings.HasPrefix(value, "app-") || len(value) < 5 || len(value) > 16 {
		return false
	}
	for _, character := range value[4:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// CloseIdleConnections closes pooled gateway connections.
func (c *Client) CloseIdleConnections() {
	c.client.CloseIdleConnections()
}
