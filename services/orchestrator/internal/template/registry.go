// Package template loads trusted orchestrator resource and scenario templates.
package template

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Healthcheck defines a fixed container health endpoint.
type Healthcheck struct {
	Path            string `json:"path"`
	Port            int    `json:"port"`
	IntervalSeconds int    `json:"intervalSeconds"`
	TimeoutSeconds  int    `json:"timeoutSeconds"`
	Retries         int    `json:"retries"`
}

type containerFile struct {
	TemplateID             string       `json:"templateId"`
	TemplateVersion        int          `json:"templateVersion"`
	ResourceType           string       `json:"resourceType"`
	Extends                string       `json:"extends,omitempty"`
	ImageEnv               string       `json:"imageEnv,omitempty"`
	User                   string       `json:"user,omitempty"`
	Command                []string     `json:"command,omitempty"`
	ReadOnlyRootFilesystem *bool        `json:"readOnlyRootFilesystem,omitempty"`
	Privileged             *bool        `json:"privileged,omitempty"`
	PublishPorts           *bool        `json:"publishPorts,omitempty"`
	CapDrop                []string     `json:"capDrop,omitempty"`
	MemoryLimitMB          *int         `json:"memoryLimitMb,omitempty"`
	PidsLimit              *int         `json:"pidsLimit,omitempty"`
	Healthcheck            *Healthcheck `json:"healthcheck,omitempty"`
	Modules                []string     `json:"modules,omitempty"`
}

// Container is a resolved, immutable container template.
type Container struct {
	TemplateID             string
	TemplateVersion        int
	ImageEnv               string
	User                   string
	Command                []string
	ReadOnlyRootFilesystem bool
	Privileged             bool
	PublishPorts           bool
	CapDrop                []string
	MemoryLimitMB          int
	PidsLimit              int
	Healthcheck            *Healthcheck
	Modules                []string
}

// Network is a trusted Docker network template.
type Network struct {
	TemplateID      string            `json:"templateId"`
	TemplateVersion int               `json:"templateVersion"`
	ResourceType    string            `json:"resourceType"`
	Driver          string            `json:"driver"`
	Internal        bool              `json:"internal"`
	Attachable      bool              `json:"attachable"`
	NamePattern     string            `json:"namePattern"`
	RequiredLabels  map[string]string `json:"requiredLabels"`
}

// Database is a trusted lab database procedure profile.
type Database struct {
	TemplateID          string `json:"templateId"`
	TemplateVersion     int    `json:"templateVersion"`
	ResourceType        string `json:"resourceType"`
	DatabaseNamePattern string `json:"databaseNamePattern"`
	UserNamePattern     string `json:"userNamePattern"`
	ProvisionProcedure  string `json:"provisionProcedure"`
	ResetProcedure      string `json:"resetProcedure"`
	DestroyProcedure    string `json:"destroyProcedure"`
}

// Nginx is a trusted dynamic fragment template.
type Nginx struct {
	TemplateID       string `json:"templateId"`
	TemplateVersion  int    `json:"templateVersion"`
	ResourceType     string `json:"resourceType"`
	TemplatePath     string `json:"templatePath"`
	OutputDirectory  string `json:"outputDirectory"`
	TestBeforeReload bool   `json:"testBeforeReload"`
	AtomicReplace    bool   `json:"atomicReplace"`
	GracefulReload   bool   `json:"gracefulReload"`
}

// ProductSeed is immutable sample data defined by a scenario.
type ProductSeed struct {
	ID          uint64  `json:"id"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Price       float64 `json:"price"`
	Currency    string  `json:"currency"`
	StockLabel  string  `json:"stockLabel"`
	Description string  `json:"description"`
	Version     int     `json:"version"`
}

// Scenario defines one trusted lab scenario and its resource references.
type Scenario struct {
	TemplateID        string `json:"templateId"`
	TemplateVersion   int    `json:"templateVersion"`
	ScenarioType      string `json:"scenarioType"`
	ResourceTemplates struct {
		Application   string `json:"application"`
		Database      string `json:"database"`
		Network       string `json:"network"`
		NginxFragment string `json:"nginxFragment"`
		Redis         string `json:"redis,omitempty"`
	} `json:"resourceTemplates"`
	Resources struct {
		BaseCPULimitCores float64 `json:"baseCpuLimitCores"`
		MemoryLimitMB     int     `json:"memoryLimitMb"`
		PidsLimit         int     `json:"pidsLimit"`
	} `json:"resources"`
	Capacity struct {
		BaseCapacity              int `json:"baseCapacity"`
		CapacityWindowMS          int `json:"capacityWindowMs"`
		InitialPerformancePercent int `json:"initialPerformancePercent"`
		MinPerformancePercent     int `json:"minPerformancePercent"`
		MaxPerformancePercent     int `json:"maxPerformancePercent"`
	} `json:"capacity"`
	LoadBalancing struct {
		InitialWeight int    `json:"initialWeight"`
		Mode          string `json:"mode"`
	} `json:"loadBalancing"`
	OrderSimulation struct {
		DefaultBatchSize            int    `json:"defaultBatchSize"`
		DefaultGenerationIntervalMS int    `json:"defaultGenerationIntervalMs"`
		ProcessingDelayMS           int    `json:"processingDelayMs"`
		OverloadPolicy              string `json:"overloadPolicy"`
		QueueMode                   string `json:"queueMode"`
		ConcurrencyControl          string `json:"concurrencyControl"`
		PreserveArrivalOrder        bool   `json:"preserveArrivalOrder"`
	} `json:"orderSimulation"`
	ProductSeeds []ProductSeed `json:"productSeeds"`
}

// Registry stores validated templates loaded at process startup.
type Registry struct {
	containers map[string]Container
	networks   map[string]Network
	databases  map[string]Database
	nginx      map[string]Nginx
	scenarios  map[string]Scenario
}

// Load reads and validates all templates below root.
func Load(root string) (*Registry, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" {
		return nil, errors.New("template root is required")
	}
	registry := &Registry{
		containers: make(map[string]Container),
		networks:   make(map[string]Network),
		databases:  make(map[string]Database),
		nginx:      make(map[string]Nginx),
		scenarios:  make(map[string]Scenario),
	}
	containerFiles := make(map[string]containerFile)
	resourcePaths, err := filepath.Glob(filepath.Join(root, "resources", "*.json"))
	if err != nil {
		return nil, fmt.Errorf("list resource templates: %w", err)
	}
	if len(resourcePaths) == 0 {
		return nil, errors.New("no resource templates found")
	}
	for _, path := range resourcePaths {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read resource template %s: %w", path, err)
		}
		var header struct {
			TemplateID   string `json:"templateId"`
			ResourceType string `json:"resourceType"`
		}
		if err := json.Unmarshal(body, &header); err != nil {
			return nil, fmt.Errorf("decode resource template header %s: %w", path, err)
		}
		if !validID(header.TemplateID) {
			return nil, fmt.Errorf("resource template %s has invalid templateId", path)
		}
		if registry.hasTemplate(header.TemplateID) || containerFiles[header.TemplateID].TemplateID != "" {
			return nil, fmt.Errorf("duplicate templateId %q", header.TemplateID)
		}
		switch header.ResourceType {
		case "container":
			var value containerFile
			if err := decodeStrict(body, &value); err != nil {
				return nil, fmt.Errorf("decode container template %s: %w", path, err)
			}
			containerFiles[value.TemplateID] = value
		case "network":
			var value Network
			if err := decodeStrict(body, &value); err != nil {
				return nil, fmt.Errorf("decode network template %s: %w", path, err)
			}
			if err := validateNetwork(value); err != nil {
				return nil, fmt.Errorf("validate network template %s: %w", path, err)
			}
			registry.networks[value.TemplateID] = value
		case "database":
			var value Database
			if err := decodeStrict(body, &value); err != nil {
				return nil, fmt.Errorf("decode database template %s: %w", path, err)
			}
			if err := validateDatabase(value); err != nil {
				return nil, fmt.Errorf("validate database template %s: %w", path, err)
			}
			registry.databases[value.TemplateID] = value
		case "nginxFragment":
			var value Nginx
			if err := decodeStrict(body, &value); err != nil {
				return nil, fmt.Errorf("decode nginx template %s: %w", path, err)
			}
			if err := validateNginx(value); err != nil {
				return nil, fmt.Errorf("validate nginx template %s: %w", path, err)
			}
			registry.nginx[value.TemplateID] = value
		default:
			return nil, fmt.Errorf("resource template %s has unsupported resourceType %q", path, header.ResourceType)
		}
	}
	for id := range containerFiles {
		value, err := resolveContainer(id, containerFiles, nil)
		if err != nil {
			return nil, err
		}
		registry.containers[id] = value
	}

	scenarioPaths, err := filepath.Glob(filepath.Join(root, "scenarios", "*.json"))
	if err != nil {
		return nil, fmt.Errorf("list scenario templates: %w", err)
	}
	if len(scenarioPaths) == 0 {
		return nil, errors.New("no scenario templates found")
	}
	for _, path := range scenarioPaths {
		var value Scenario
		if err := decodeFile(path, &value); err != nil {
			return nil, fmt.Errorf("decode scenario template %s: %w", path, err)
		}
		if registry.hasTemplate(value.TemplateID) {
			return nil, fmt.Errorf("duplicate templateId %q", value.TemplateID)
		}
		if err := registry.validateScenario(value); err != nil {
			return nil, fmt.Errorf("validate scenario template %s: %w", path, err)
		}
		registry.scenarios[value.TemplateID] = value
	}
	return registry, nil
}

// Container returns one resolved container template.
func (r *Registry) Container(id string) (Container, bool) {
	value, ok := r.containers[id]
	return value, ok
}

// Network returns one network template.
func (r *Registry) Network(id string) (Network, bool) {
	value, ok := r.networks[id]
	return value, ok
}

// Database returns one database template.
func (r *Registry) Database(id string) (Database, bool) {
	value, ok := r.databases[id]
	return value, ok
}

// Nginx returns one Nginx fragment template.
func (r *Registry) Nginx(id string) (Nginx, bool) {
	value, ok := r.nginx[id]
	return value, ok
}

// Scenario returns one scenario template.
func (r *Registry) Scenario(id string) (Scenario, bool) {
	value, ok := r.scenarios[id]
	return value, ok
}

func (r *Registry) hasTemplate(id string) bool {
	_, containerOK := r.containers[id]
	_, networkOK := r.networks[id]
	_, databaseOK := r.databases[id]
	_, nginxOK := r.nginx[id]
	_, scenarioOK := r.scenarios[id]
	return containerOK || networkOK || databaseOK || nginxOK || scenarioOK
}

func (r *Registry) validateScenario(value Scenario) error {
	if !validID(value.TemplateID) || value.TemplateVersion <= 0 || !validID(value.ScenarioType) {
		return errors.New("scenario identity is invalid")
	}
	if _, ok := r.containers[value.ResourceTemplates.Application]; !ok {
		return fmt.Errorf("application template %q does not exist", value.ResourceTemplates.Application)
	}
	if _, ok := r.databases[value.ResourceTemplates.Database]; !ok {
		return fmt.Errorf("database template %q does not exist", value.ResourceTemplates.Database)
	}
	if _, ok := r.networks[value.ResourceTemplates.Network]; !ok {
		return fmt.Errorf("network template %q does not exist", value.ResourceTemplates.Network)
	}
	if _, ok := r.nginx[value.ResourceTemplates.NginxFragment]; !ok {
		return fmt.Errorf("nginx template %q does not exist", value.ResourceTemplates.NginxFragment)
	}
	if value.ResourceTemplates.Redis != "" {
		if _, ok := r.containers[value.ResourceTemplates.Redis]; !ok {
			return fmt.Errorf("redis template %q does not exist", value.ResourceTemplates.Redis)
		}
	}
	if value.Resources.BaseCPULimitCores <= 0 || value.Resources.MemoryLimitMB <= 0 || value.Resources.PidsLimit <= 0 {
		return errors.New("scenario resource limits must be positive")
	}
	if value.Capacity.BaseCapacity <= 0 || value.Capacity.CapacityWindowMS <= 0 ||
		value.Capacity.MinPerformancePercent <= 0 ||
		value.Capacity.InitialPerformancePercent < value.Capacity.MinPerformancePercent ||
		value.Capacity.InitialPerformancePercent > value.Capacity.MaxPerformancePercent {
		return errors.New("scenario capacity settings are invalid")
	}
	if value.LoadBalancing.InitialWeight <= 0 || value.LoadBalancing.Mode == "" {
		return errors.New("scenario load balancing settings are invalid")
	}
	return nil
}

func resolveContainer(id string, files map[string]containerFile, stack map[string]bool) (Container, error) {
	value, ok := files[id]
	if !ok {
		return Container{}, fmt.Errorf("container template %q does not exist", id)
	}
	if stack == nil {
		stack = make(map[string]bool)
	}
	if stack[id] {
		return Container{}, fmt.Errorf("container template inheritance cycle at %q", id)
	}
	stack[id] = true
	defer delete(stack, id)

	result := Container{TemplateID: value.TemplateID, TemplateVersion: value.TemplateVersion}
	if value.Extends != "" {
		base, err := resolveContainer(value.Extends, files, stack)
		if err != nil {
			return Container{}, err
		}
		result = base
		result.TemplateID = value.TemplateID
		result.TemplateVersion = value.TemplateVersion
	}
	overlayContainer(&result, value)
	if err := validateContainer(result); err != nil {
		return Container{}, fmt.Errorf("validate container template %q: %w", id, err)
	}
	return result, nil
}

func overlayContainer(result *Container, value containerFile) {
	if value.ImageEnv != "" {
		result.ImageEnv = value.ImageEnv
	}
	if value.User != "" {
		result.User = value.User
	}
	if value.Command != nil {
		result.Command = append([]string(nil), value.Command...)
	}
	if value.ReadOnlyRootFilesystem != nil {
		result.ReadOnlyRootFilesystem = *value.ReadOnlyRootFilesystem
	}
	if value.Privileged != nil {
		result.Privileged = *value.Privileged
	}
	if value.PublishPorts != nil {
		result.PublishPorts = *value.PublishPorts
	}
	if value.CapDrop != nil {
		result.CapDrop = append([]string(nil), value.CapDrop...)
	}
	if value.MemoryLimitMB != nil {
		result.MemoryLimitMB = *value.MemoryLimitMB
	}
	if value.PidsLimit != nil {
		result.PidsLimit = *value.PidsLimit
	}
	if value.Healthcheck != nil {
		copyValue := *value.Healthcheck
		result.Healthcheck = &copyValue
	}
	if value.Modules != nil {
		result.Modules = append([]string(nil), value.Modules...)
	}
}

func validateContainer(value Container) error {
	if !validID(value.TemplateID) || value.TemplateVersion <= 0 || value.ImageEnv == "" {
		return errors.New("container identity or image source is invalid")
	}
	if value.User == "" || value.Privileged || value.PublishPorts || !value.ReadOnlyRootFilesystem {
		return errors.New("container security settings are invalid")
	}
	if value.MemoryLimitMB <= 0 || value.PidsLimit <= 0 {
		return errors.New("container limits must be positive")
	}
	if len(value.CapDrop) != 1 || value.CapDrop[0] != "ALL" {
		return errors.New("container must drop all capabilities")
	}
	if value.Healthcheck != nil {
		if value.Healthcheck.Path == "" || value.Healthcheck.Port <= 0 ||
			value.Healthcheck.IntervalSeconds <= 0 || value.Healthcheck.TimeoutSeconds <= 0 ||
			value.Healthcheck.Retries <= 0 {
			return errors.New("container healthcheck is invalid")
		}
	}
	return nil
}

func validateNetwork(value Network) error {
	if !validID(value.TemplateID) || value.TemplateVersion <= 0 || value.Driver != "bridge" ||
		!value.Internal || value.Attachable || !strings.Contains(value.NamePattern, "{labId}") {
		return errors.New("network template is invalid")
	}
	if value.RequiredLabels["platform.managed"] != "true" {
		return errors.New("network template must require the managed label")
	}
	return nil
}

func validateDatabase(value Database) error {
	if !validID(value.TemplateID) || value.TemplateVersion <= 0 ||
		!strings.Contains(value.DatabaseNamePattern, "{normalizedLabId}") ||
		!strings.Contains(value.UserNamePattern, "{normalizedLabId}") ||
		value.ProvisionProcedure != "platform.provision_lab_database" ||
		value.ResetProcedure != "platform.reset_lab_database" ||
		value.DestroyProcedure != "platform.destroy_lab_database" {
		return errors.New("database template is invalid")
	}
	return nil
}

func validateNginx(value Nginx) error {
	if !validID(value.TemplateID) || value.TemplateVersion <= 0 || value.TemplatePath == "" ||
		value.OutputDirectory == "" || !value.TestBeforeReload || !value.AtomicReplace ||
		!value.GracefulReload {
		return errors.New("nginx template is invalid")
	}
	return nil
}

func validID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && (r == '-' || r == '_') {
			continue
		}
		return false
	}
	return true
}

func decodeFile(path string, destination any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return decodeStrict(body, destination)
}

func decodeStrict(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("template contains multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
