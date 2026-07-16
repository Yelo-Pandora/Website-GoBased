// Package dockerapi provides a bounded Docker Engine API adapter.
package dockerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

// APIError is a failed Docker Engine API response.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("docker API status %d: %s", e.StatusCode, e.Message)
}

// IsNotFound reports whether err is a Docker 404 response.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// Healthcheck defines the fixed Docker health command and timing.
type Healthcheck struct {
	Command  []string
	Interval time.Duration
	Timeout  time.Duration
	Retries  int
}

// ContainerSpec contains only trusted container settings.
type ContainerSpec struct {
	Name           string
	Image          string
	User           string
	Command        []string
	Environment    map[string]string
	Labels         map[string]string
	NetworkName    string
	NetworkAliases []string
	ReadOnlyRootFS bool
	CapDrop        []string
	MemoryBytes    int64
	NanoCPUs       int64
	PidsLimit      int64
	Healthcheck    *Healthcheck
}

// NetworkSpec contains trusted Docker network settings.
type NetworkSpec struct {
	Name       string
	Driver     string
	Internal   bool
	Attachable bool
	Labels     map[string]string
}

// EnsureResult describes an idempotently ensured Docker resource.
type EnsureResult struct {
	ID      string
	Name    string
	Created bool
}

// Resource is a managed Docker resource discovered by labels.
type Resource struct {
	ID     string
	Name   string
	Kind   string
	State  string
	Labels map[string]string
}

// Client calls a Docker Socket Proxy through a fixed HTTP endpoint.
type Client struct {
	baseURL    *url.URL
	apiVersion string
	httpClient *http.Client
}

// NewClient returns a bounded Docker Engine API client.
func NewClient(host, apiVersion string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		return nil, errors.New("docker timeout must be positive")
	}
	baseURL, err := parseHost(host)
	if err != nil {
		return nil, err
	}
	apiVersion = strings.TrimPrefix(strings.TrimSpace(apiVersion), "v")
	if !validAPIVersion(apiVersion) {
		return nil, errors.New("docker API version is invalid")
	}
	return &Client{
		baseURL:    baseURL,
		apiVersion: "v" + apiVersion,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func newClient(rawURL, apiVersion string, client *http.Client) (*Client, error) {
	baseURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &Client{baseURL: baseURL, apiVersion: apiVersion, httpClient: client}, nil
}

// Ping verifies access to the socket proxy.
func (c *Client) Ping(ctx context.Context) error {
	var body string
	if err := c.do(ctx, http.MethodGet, "/_ping", nil, &body, false); err != nil {
		return err
	}
	if strings.TrimSpace(body) != "OK" {
		return fmt.Errorf("unexpected docker ping response %q", body)
	}
	return nil
}

// InspectImage verifies that a trusted image is already present.
func (c *Client) InspectImage(ctx context.Context, image string) error {
	if strings.TrimSpace(image) == "" {
		return errors.New("image is required")
	}
	return c.do(
		ctx,
		http.MethodGet,
		"/images/"+url.PathEscape(image)+"/json",
		nil,
		&struct{}{},
		true,
	)
}

// EnsureNetwork creates a managed network or validates an existing one.
func (c *Client) EnsureNetwork(ctx context.Context, spec NetworkSpec) (EnsureResult, error) {
	if err := validateName(spec.Name); err != nil {
		return EnsureResult{}, err
	}
	existing, err := c.inspectNetwork(ctx, spec.Name)
	if err == nil {
		if err := requireLabels(existing.Labels, spec.Labels); err != nil {
			return EnsureResult{}, fmt.Errorf("validate existing network: %w", err)
		}
		return EnsureResult{ID: existing.ID, Name: spec.Name}, nil
	}
	if !IsNotFound(err) {
		return EnsureResult{}, err
	}
	request := struct {
		Name       string            `json:"Name"`
		Driver     string            `json:"Driver"`
		Internal   bool              `json:"Internal"`
		Attachable bool              `json:"Attachable"`
		Labels     map[string]string `json:"Labels"`
	}{
		Name:       spec.Name,
		Driver:     spec.Driver,
		Internal:   spec.Internal,
		Attachable: spec.Attachable,
		Labels:     spec.Labels,
	}
	var response struct {
		ID string `json:"Id"`
	}
	if err := c.do(ctx, http.MethodPost, "/networks/create", request, &response, true); err != nil {
		return EnsureResult{}, err
	}
	return EnsureResult{ID: response.ID, Name: spec.Name, Created: true}, nil
}

// ConnectNetwork idempotently connects a container to a network.
func (c *Client) ConnectNetwork(
	ctx context.Context,
	networkID string,
	containerID string,
	aliases []string,
) error {
	network, err := c.inspectNetwork(ctx, networkID)
	if err != nil {
		return err
	}
	for id := range network.Containers {
		if strings.HasPrefix(containerID, id) || strings.HasPrefix(id, containerID) {
			return nil
		}
	}
	request := struct {
		Container      string `json:"Container"`
		EndpointConfig struct {
			Aliases []string `json:"Aliases,omitempty"`
		} `json:"EndpointConfig"`
	}{Container: containerID}
	request.EndpointConfig.Aliases = aliases
	return c.do(
		ctx,
		http.MethodPost,
		"/networks/"+url.PathEscape(networkID)+"/connect",
		request,
		nil,
		true,
	)
}

// DisconnectNetwork disconnects a container when it is attached.
func (c *Client) DisconnectNetwork(ctx context.Context, networkID, containerID string) error {
	network, err := c.inspectNetwork(ctx, networkID)
	if IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	connected := false
	for id := range network.Containers {
		if strings.HasPrefix(containerID, id) || strings.HasPrefix(id, containerID) {
			connected = true
			break
		}
	}
	if !connected {
		return nil
	}
	request := struct {
		Container string `json:"Container"`
		Force     bool   `json:"Force"`
	}{Container: containerID, Force: true}
	return c.do(
		ctx,
		http.MethodPost,
		"/networks/"+url.PathEscape(networkID)+"/disconnect",
		request,
		nil,
		true,
	)
}

// RemoveNetwork removes a managed network if it exists.
func (c *Client) RemoveNetwork(ctx context.Context, networkID string) error {
	err := c.do(
		ctx,
		http.MethodDelete,
		"/networks/"+url.PathEscape(networkID),
		nil,
		nil,
		true,
	)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// EnsureContainer creates and starts a container or validates an existing one.
func (c *Client) EnsureContainer(ctx context.Context, spec ContainerSpec) (EnsureResult, error) {
	if err := validateContainerSpec(spec); err != nil {
		return EnsureResult{}, err
	}
	existing, err := c.inspectContainer(ctx, spec.Name)
	if err == nil {
		if err := requireLabels(existing.Config.Labels, spec.Labels); err != nil {
			return EnsureResult{}, fmt.Errorf("validate existing container: %w", err)
		}
		if existing.Config.Image != spec.Image {
			return EnsureResult{}, errors.New("existing container image does not match template")
		}
		if !existing.State.Running {
			if err := c.StartContainer(ctx, existing.ID); err != nil {
				return EnsureResult{}, err
			}
		}
		return EnsureResult{ID: existing.ID, Name: spec.Name}, nil
	}
	if !IsNotFound(err) {
		return EnsureResult{}, err
	}

	request := containerCreateRequest{
		Image:  spec.Image,
		User:   spec.User,
		Cmd:    spec.Command,
		Env:    sortedEnvironment(spec.Environment),
		Labels: spec.Labels,
		HostConfig: hostConfig{
			NetworkMode:    spec.NetworkName,
			ReadonlyRootfs: spec.ReadOnlyRootFS,
			Privileged:     false,
			CapDrop:        spec.CapDrop,
			Memory:         spec.MemoryBytes,
			NanoCPUs:       spec.NanoCPUs,
			PidsLimit:      spec.PidsLimit,
			SecurityOpt:    []string{"no-new-privileges"},
			Tmpfs:          map[string]string{"/tmp": "rw,noexec,nosuid,size=16777216"},
		},
		NetworkingConfig: networkingConfig{EndpointsConfig: map[string]endpointSettings{
			spec.NetworkName: {Aliases: spec.NetworkAliases},
		}},
	}
	if spec.Healthcheck != nil {
		request.Healthcheck = &healthConfig{
			Test:     append([]string{"CMD"}, spec.Healthcheck.Command...),
			Interval: spec.Healthcheck.Interval.Nanoseconds(),
			Timeout:  spec.Healthcheck.Timeout.Nanoseconds(),
			Retries:  spec.Healthcheck.Retries,
		}
	}
	var response struct {
		ID string `json:"Id"`
	}
	createPath := "/containers/create?name=" + url.QueryEscape(spec.Name)
	if err := c.do(ctx, http.MethodPost, createPath, request, &response, true); err != nil {
		return EnsureResult{}, err
	}
	if err := c.StartContainer(ctx, response.ID); err != nil {
		_ = c.RemoveContainer(context.WithoutCancel(ctx), response.ID)
		return EnsureResult{}, err
	}
	return EnsureResult{ID: response.ID, Name: spec.Name, Created: true}, nil
}

// StartContainer starts an existing container.
func (c *Client) StartContainer(ctx context.Context, containerID string) error {
	return c.do(
		ctx,
		http.MethodPost,
		"/containers/"+url.PathEscape(containerID)+"/start",
		nil,
		nil,
		true,
	)
}

// RestartContainer restarts an existing container.
func (c *Client) RestartContainer(ctx context.Context, containerID string) error {
	return c.do(
		ctx,
		http.MethodPost,
		"/containers/"+url.PathEscape(containerID)+"/restart?t=10",
		nil,
		nil,
		true,
	)
}

// UpdateContainerCPU changes only the NanoCPUs limit.
func (c *Client) UpdateContainerCPU(ctx context.Context, containerID string, nanoCPUs int64) error {
	if nanoCPUs <= 0 {
		return errors.New("NanoCPUs must be positive")
	}
	request := struct {
		NanoCPUs int64 `json:"NanoCPUs"`
	}{NanoCPUs: nanoCPUs}
	return c.do(
		ctx,
		http.MethodPost,
		"/containers/"+url.PathEscape(containerID)+"/update",
		request,
		nil,
		true,
	)
}

// RemoveContainer force-removes a container if it exists.
func (c *Client) RemoveContainer(ctx context.Context, containerID string) error {
	err := c.do(
		ctx,
		http.MethodDelete,
		"/containers/"+url.PathEscape(containerID)+"?force=1&v=1",
		nil,
		nil,
		true,
	)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// FindComposeContainer finds one container by trusted Compose labels.
func (c *Client) FindComposeContainer(ctx context.Context, project, service string) (Resource, error) {
	resources, err := c.listContainers(ctx, map[string][]string{"label": {
		"com.docker.compose.project=" + project,
		"com.docker.compose.service=" + service,
	}})
	if err != nil {
		return Resource{}, err
	}
	if len(resources) != 1 {
		return Resource{}, fmt.Errorf("compose service %s has %d containers", service, len(resources))
	}
	return resources[0], nil
}

// ListManagedContainers lists platform-managed containers for a lab or globally.
func (c *Client) ListManagedContainers(ctx context.Context, labID string) ([]Resource, error) {
	labels := []string{"platform.managed=true"}
	if labID != "" {
		labels = append(labels, "platform.labId="+labID)
	}
	return c.listContainers(ctx, map[string][]string{"label": labels})
}

// ListManagedNetworks lists platform-managed networks for a lab or globally.
func (c *Client) ListManagedNetworks(ctx context.Context, labID string) ([]Resource, error) {
	labels := []string{"platform.managed=true"}
	if labID != "" {
		labels = append(labels, "platform.labId="+labID)
	}
	filters, err := json.Marshal(map[string][]string{"label": labels})
	if err != nil {
		return nil, err
	}
	var response []struct {
		ID     string            `json:"Id"`
		Name   string            `json:"Name"`
		Labels map[string]string `json:"Labels"`
	}
	requestPath := "/networks?filters=" + url.QueryEscape(string(filters))
	if err := c.do(ctx, http.MethodGet, requestPath, nil, &response, true); err != nil {
		return nil, err
	}
	resources := make([]Resource, 0, len(response))
	for _, item := range response {
		resources = append(resources, Resource{
			ID: item.ID, Name: item.Name, Kind: "network", Labels: item.Labels,
		})
	}
	return resources, nil
}

// Exec runs one fixed argument vector in an already selected container.
func (c *Client) Exec(ctx context.Context, containerID string, command []string) (string, error) {
	if len(command) == 0 {
		return "", errors.New("exec command is required")
	}
	createRequest := struct {
		AttachStdout bool     `json:"AttachStdout"`
		AttachStderr bool     `json:"AttachStderr"`
		Tty          bool     `json:"Tty"`
		Cmd          []string `json:"Cmd"`
	}{AttachStdout: true, AttachStderr: true, Tty: true, Cmd: command}
	var createResponse struct {
		ID string `json:"Id"`
	}
	if err := c.do(
		ctx,
		http.MethodPost,
		"/containers/"+url.PathEscape(containerID)+"/exec",
		createRequest,
		&createResponse,
		true,
	); err != nil {
		return "", err
	}
	startRequest := struct {
		Detach bool `json:"Detach"`
		Tty    bool `json:"Tty"`
	}{Tty: true}
	var output string
	if err := c.do(
		ctx,
		http.MethodPost,
		"/exec/"+url.PathEscape(createResponse.ID)+"/start",
		startRequest,
		&output,
		true,
	); err != nil {
		return "", err
	}
	var inspect struct {
		Running  bool `json:"Running"`
		ExitCode int  `json:"ExitCode"`
	}
	if err := c.do(
		ctx,
		http.MethodGet,
		"/exec/"+url.PathEscape(createResponse.ID)+"/json",
		nil,
		&inspect,
		true,
	); err != nil {
		return "", err
	}
	if inspect.Running || inspect.ExitCode != 0 {
		return output, fmt.Errorf("container exec exit code %d", inspect.ExitCode)
	}
	return output, nil
}

type networkInspect struct {
	ID         string                     `json:"Id"`
	Name       string                     `json:"Name"`
	Labels     map[string]string          `json:"Labels"`
	Containers map[string]json.RawMessage `json:"Containers"`
}

func (c *Client) inspectNetwork(ctx context.Context, identifier string) (networkInspect, error) {
	var response networkInspect
	err := c.do(
		ctx,
		http.MethodGet,
		"/networks/"+url.PathEscape(identifier),
		nil,
		&response,
		true,
	)
	return response, err
}

type containerInspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
}

func (c *Client) inspectContainer(ctx context.Context, identifier string) (containerInspect, error) {
	var response containerInspect
	err := c.do(
		ctx,
		http.MethodGet,
		"/containers/"+url.PathEscape(identifier)+"/json",
		nil,
		&response,
		true,
	)
	return response, err
}

func (c *Client) listContainers(
	ctx context.Context,
	filtersValue map[string][]string,
) ([]Resource, error) {
	filters, err := json.Marshal(filtersValue)
	if err != nil {
		return nil, err
	}
	var response []struct {
		ID     string            `json:"Id"`
		Names  []string          `json:"Names"`
		State  string            `json:"State"`
		Labels map[string]string `json:"Labels"`
	}
	requestPath := "/containers/json?all=1&filters=" + url.QueryEscape(string(filters))
	if err := c.do(ctx, http.MethodGet, requestPath, nil, &response, true); err != nil {
		return nil, err
	}
	resources := make([]Resource, 0, len(response))
	for _, item := range response {
		name := ""
		if len(item.Names) > 0 {
			name = strings.TrimPrefix(item.Names[0], "/")
		}
		resources = append(resources, Resource{
			ID: item.ID, Name: name, Kind: "container", State: item.State, Labels: item.Labels,
		})
	}
	return resources, nil
}

func (c *Client) do(
	ctx context.Context,
	method string,
	requestPath string,
	requestBody any,
	responseBody any,
	versioned bool,
) error {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode docker request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	requestURL := *c.baseURL
	prefix := ""
	if versioned {
		prefix = "/" + c.apiVersion
	}
	parsedPath, err := url.Parse(prefix + requestPath)
	if err != nil {
		return fmt.Errorf("parse docker request path: %w", err)
	}
	requestURL.Path = path.Clean(parsedPath.Path)
	requestURL.RawQuery = parsedPath.RawQuery
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return fmt.Errorf("create docker request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("execute docker request: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	responseBytes, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read docker response: %w", err)
	}
	if len(responseBytes) > maxResponseBytes {
		return errors.New("docker response is too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBytes))
		var errorBody struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(responseBytes, &errorBody) == nil && errorBody.Message != "" {
			message = errorBody.Message
		}
		return &APIError{StatusCode: response.StatusCode, Message: message}
	}
	if responseBody == nil || len(responseBytes) == 0 {
		return nil
	}
	switch destination := responseBody.(type) {
	case *string:
		*destination = string(responseBytes)
		return nil
	default:
		if err := json.Unmarshal(responseBytes, responseBody); err != nil {
			return fmt.Errorf("decode docker response: %w", err)
		}
		return nil
	}
}

type containerCreateRequest struct {
	Image            string            `json:"Image"`
	User             string            `json:"User"`
	Cmd              []string          `json:"Cmd,omitempty"`
	Env              []string          `json:"Env,omitempty"`
	Labels           map[string]string `json:"Labels"`
	Healthcheck      *healthConfig     `json:"Healthcheck,omitempty"`
	HostConfig       hostConfig        `json:"HostConfig"`
	NetworkingConfig networkingConfig  `json:"NetworkingConfig"`
}

type healthConfig struct {
	Test     []string `json:"Test"`
	Interval int64    `json:"Interval"`
	Timeout  int64    `json:"Timeout"`
	Retries  int      `json:"Retries"`
}

type hostConfig struct {
	NetworkMode    string            `json:"NetworkMode"`
	ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
	Privileged     bool              `json:"Privileged"`
	CapDrop        []string          `json:"CapDrop"`
	Memory         int64             `json:"Memory"`
	NanoCPUs       int64             `json:"NanoCpus"`
	PidsLimit      int64             `json:"PidsLimit"`
	SecurityOpt    []string          `json:"SecurityOpt"`
	Tmpfs          map[string]string `json:"Tmpfs"`
}

type networkingConfig struct {
	EndpointsConfig map[string]endpointSettings `json:"EndpointsConfig"`
}

type endpointSettings struct {
	Aliases []string `json:"Aliases,omitempty"`
}

func parseHost(host string) (*url.URL, error) {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "tcp://") {
		host = "http://" + strings.TrimPrefix(host, "tcp://")
	}
	parsed, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("parse DOCKER_HOST: %w", err)
	}
	if parsed.Scheme != "http" || parsed.Host == "" || parsed.Path != "" {
		return nil, errors.New("DOCKER_HOST must be an internal tcp or http endpoint")
	}
	return parsed, nil
}

func validAPIVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major <= 0 {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	return err == nil && minor >= 0
}

func validateName(value string) error {
	if value == "" || len(value) > 128 {
		return errors.New("docker resource name is invalid")
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && (r == '-' || r == '_' || r == '.') {
			continue
		}
		return errors.New("docker resource name is invalid")
	}
	return nil
}

func validateContainerSpec(spec ContainerSpec) error {
	if err := validateName(spec.Name); err != nil {
		return err
	}
	if spec.Image == "" || spec.User == "" || spec.NetworkName == "" ||
		!spec.ReadOnlyRootFS || spec.MemoryBytes <= 0 || spec.NanoCPUs <= 0 ||
		spec.PidsLimit <= 0 {
		return errors.New("container spec is incomplete")
	}
	if len(spec.CapDrop) != 1 || spec.CapDrop[0] != "ALL" {
		return errors.New("container must drop all capabilities")
	}
	return nil
}

func requireLabels(actual, required map[string]string) error {
	for key, value := range required {
		if actual[key] != value {
			return fmt.Errorf("label %s does not match", key)
		}
	}
	return nil
}

func sortedEnvironment(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}
