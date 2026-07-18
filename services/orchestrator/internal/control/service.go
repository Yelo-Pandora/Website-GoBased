// Package control validates and executes trusted orchestrator commands.
package control

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"website-gobased/internal/protocol"
	"website-gobased/services/orchestrator/internal/dockerapi"
	"website-gobased/services/orchestrator/internal/labdb"
	"website-gobased/services/orchestrator/internal/nginx"
	templates "website-gobased/services/orchestrator/internal/template"
)

const (
	commandProvisionLab       = "PROVISION_LAB"
	commandResetLab           = "RESET_LAB"
	commandDestroyLab         = "DESTROY_LAB"
	commandCreateApp          = "CREATE_APP_INSTANCE"
	commandDeleteApp          = "DELETE_APP_INSTANCE"
	commandUpdateAppCapacity  = "UPDATE_APP_CAPACITY"
	commandCreateRedis        = "CREATE_SESSION_REDIS"
	commandRestartRedis       = "RESTART_SESSION_REDIS"
	commandDeleteRedis        = "DELETE_SESSION_REDIS"
	commandApplyUpstream      = "APPLY_LAB_UPSTREAM"
	commandRemoveUpstream     = "REMOVE_LAB_UPSTREAM"
	commandResetDatabase      = "RESET_LAB_DATABASE"
	commandReconcileResources = "RECONCILE_RESOURCES"
	maxDatabaseIdentityLength = 23
)

var allowedCommands = map[string]struct{}{
	commandProvisionLab: {}, commandResetLab: {}, commandDestroyLab: {},
	commandCreateApp: {}, commandDeleteApp: {}, commandUpdateAppCapacity: {},
	commandCreateRedis: {}, commandRestartRedis: {}, commandDeleteRedis: {},
	commandApplyUpstream: {}, commandRemoveUpstream: {}, commandResetDatabase: {},
	commandReconcileResources: {},
}

var errInvalidUpstream = errors.New("upstream server is invalid")

type dockerOperator interface {
	InspectImage(ctx context.Context, image string) error
	EnsureNetwork(ctx context.Context, spec dockerapi.NetworkSpec) (dockerapi.EnsureResult, error)
	ConnectNetwork(ctx context.Context, networkID, containerID string, aliases []string) error
	DisconnectNetwork(ctx context.Context, networkID, containerID string) error
	RemoveNetwork(ctx context.Context, networkID string) error
	EnsureContainer(ctx context.Context, spec dockerapi.ContainerSpec) (dockerapi.EnsureResult, error)
	RestartContainer(ctx context.Context, containerID string) error
	UpdateContainerCPU(ctx context.Context, containerID string, nanoCPUs int64) error
	RemoveContainer(ctx context.Context, containerID string) error
	FindComposeContainer(ctx context.Context, project, service string) (dockerapi.Resource, error)
	ListManagedContainers(ctx context.Context, labID string) ([]dockerapi.Resource, error)
	ListManagedNetworks(ctx context.Context, labID string) ([]dockerapi.Resource, error)
}

type databaseOperator interface {
	Provision(ctx context.Context, databaseName, userName, password string) error
	SeedProducts(ctx context.Context, databaseName string, products []labdb.ProductSeed) error
	Reset(ctx context.Context, databaseName string) error
	Destroy(ctx context.Context, databaseName, userName string) error
	ListManaged(ctx context.Context) ([]labdb.ManagedDatabase, error)
}

type nginxOperator interface {
	Apply(ctx context.Context, labID string, servers []nginx.Server) error
	Remove(ctx context.Context, labID string) error
	List() ([]string, error)
}

// Config fixes runtime image and Compose identities outside command payloads.
type Config struct {
	ComposeProject string
	GatewayService string
	MySQLService   string
	LabAppImage    string
	RedisImage     string
	DatabaseSecret string
}

// Service validates and synchronously executes one trusted command.
type Service struct {
	logger   *slog.Logger
	registry *templates.Registry
	docker   dockerOperator
	database databaseOperator
	nginx    nginxOperator
	config   Config
}

// NewService returns a command executor with fixed trusted dependencies.
func NewService(
	logger *slog.Logger,
	registry *templates.Registry,
	docker dockerOperator,
	database databaseOperator,
	nginx nginxOperator,
	config Config,
) (*Service, error) {
	if logger == nil || registry == nil || docker == nil || database == nil || nginx == nil {
		return nil, errors.New("control service dependencies are required")
	}
	if config.ComposeProject == "" || config.GatewayService == "" ||
		config.MySQLService == "" || config.LabAppImage == "" ||
		config.RedisImage == "" || len(config.DatabaseSecret) < 16 {
		return nil, errors.New("control service config is incomplete")
	}
	return &Service{
		logger: logger, registry: registry, docker: docker,
		database: database, nginx: nginx, config: config,
	}, nil
}

// Execute validates and executes one allowlisted command.
func (s *Service) Execute(ctx context.Context, command protocol.Command) protocol.CommandResponse {
	if code, message := validateEnvelope(command); code != "" {
		return commandResponse(command, "rejected", nil, code, message)
	}
	var result any
	var commandErr *commandError
	switch command.CommandType {
	case commandProvisionLab:
		result, commandErr = s.provision(ctx, command)
	case commandResetLab:
		result, commandErr = s.resetLab(ctx, command)
	case commandDestroyLab:
		result, commandErr = s.destroy(ctx, command)
	case commandCreateApp:
		result, commandErr = s.createAppCommand(ctx, command)
	case commandDeleteApp:
		result, commandErr = s.deleteAppCommand(ctx, command)
	case commandUpdateAppCapacity:
		result, commandErr = s.updateAppCapacity(ctx, command)
	case commandCreateRedis:
		result, commandErr = s.createRedisCommand(ctx, command)
	case commandRestartRedis:
		result, commandErr = s.restartRedis(ctx, command)
	case commandDeleteRedis:
		result, commandErr = s.deleteRedis(ctx, command)
	case commandApplyUpstream:
		result, commandErr = s.applyUpstream(ctx, command)
	case commandRemoveUpstream:
		result, commandErr = s.removeUpstream(ctx, command)
	case commandResetDatabase:
		result, commandErr = s.resetDatabase(ctx, command)
	case commandReconcileResources:
		result, commandErr = s.reconcile(ctx, command)
	}
	if commandErr != nil {
		if commandErr.cause != nil {
			s.logger.ErrorContext(
				ctx,
				"orchestrator command failed",
				"commandType", command.CommandType,
				"commandId", command.CommandID,
				"operationId", command.OperationID,
				"labId", command.LabID,
				"error", commandErr.cause,
			)
		}
		return commandResponse(
			command,
			commandErr.status,
			nil,
			commandErr.code,
			commandErr.message,
		)
	}
	return commandResponse(command, "succeeded", result, "", "")
}

func (s *Service) provision(
	ctx context.Context,
	command protocol.Command,
) (any, *commandError) {
	var payload provisionPayload
	if err := decodePayload(command.Payload, &payload, true); err != nil {
		return nil, reject("INVALID_COMMAND", "invalid provision payload", err)
	}
	scenario, ok := s.registry.Scenario(payload.ScenarioTemplateID)
	if !ok || scenario.ScenarioType != payload.ScenarioType {
		return nil, reject("TEMPLATE_NOT_FOUND", "scenario template is not available", nil)
	}
	if payload.CourseID == 0 || payload.InitialInstances <= 0 || payload.InitialInstances > 4 ||
		payload.RedisRequired != (scenario.ResourceTemplates.Redis != "") {
		return nil, reject("INVALID_COMMAND", "provision payload does not match scenario", nil)
	}
	names, err := s.names(command.LabID, scenario)
	if err != nil {
		return nil, reject("INVALID_COMMAND", "lab identity is invalid", err)
	}
	appTemplate, ok := s.registry.Container(scenario.ResourceTemplates.Application)
	if !ok {
		return nil, reject("TEMPLATE_NOT_FOUND", "application template is not available", nil)
	}
	appImage, err := s.imageFor(appTemplate.ImageEnv)
	if err != nil {
		return nil, reject("TEMPLATE_NOT_FOUND", "application image is not configured", err)
	}
	if err := s.docker.InspectImage(ctx, appImage); err != nil {
		return nil, fail("DOCKER_UNAVAILABLE", "application image is unavailable", err)
	}
	if payload.RedisRequired {
		redisTemplate, ok := s.registry.Container(scenario.ResourceTemplates.Redis)
		if !ok {
			return nil, reject("TEMPLATE_NOT_FOUND", "redis template is not available", nil)
		}
		redisImage, err := s.imageFor(redisTemplate.ImageEnv)
		if err != nil {
			return nil, reject("TEMPLATE_NOT_FOUND", "redis image is not configured", err)
		}
		if err := s.docker.InspectImage(ctx, redisImage); err != nil {
			return nil, fail("DOCKER_UNAVAILABLE", "redis image is unavailable", err)
		}
	}

	password, err := s.databasePassword(command.LabID)
	if err != nil {
		return nil, fail("INTERNAL_ERROR", "lab database credential could not be derived", err)
	}
	compensate := true
	defer func() {
		if compensate {
			cleanupCtx := context.WithoutCancel(ctx)
			if cleanupErr := s.destroyResources(cleanupCtx, command.LabID, names); cleanupErr != nil {
				s.logger.ErrorContext(cleanupCtx, "compensate provision lab", "labId", command.LabID, "error", cleanupErr)
			}
		}
	}()
	if err := s.database.Provision(ctx, names.database, names.databaseUser, password); err != nil {
		return nil, fail("LAB_DATABASE_UNAVAILABLE", "lab database could not be provisioned", err)
	}
	if payload.RedisRequired {
		seeds := make([]labdb.ProductSeed, 0, len(scenario.ProductSeeds))
		for _, value := range scenario.ProductSeeds {
			seeds = append(seeds, labdb.ProductSeed{
				ID: value.ID, Name: value.Name, Category: value.Category,
				Price: value.Price, Currency: value.Currency, StockLabel: value.StockLabel,
				Description: value.Description, Version: value.Version,
			})
		}
		if err := s.database.SeedProducts(ctx, names.database, seeds); err != nil {
			return nil, fail("LAB_DATABASE_UNAVAILABLE", "lab products could not be seeded", err)
		}
	}

	networkTemplate, _ := s.registry.Network(scenario.ResourceTemplates.Network)
	labels := resourceLabels(command, "lab-network", networkTemplate.TemplateID)
	for key, value := range networkTemplate.RequiredLabels {
		labels[key] = value
	}
	network, err := s.docker.EnsureNetwork(ctx, dockerapi.NetworkSpec{
		Name: names.network, Driver: networkTemplate.Driver,
		Internal: networkTemplate.Internal, Attachable: networkTemplate.Attachable,
		Labels: labels,
	})
	if err != nil {
		return nil, fail("DOCKER_UNAVAILABLE", "lab network could not be provisioned", err)
	}
	if err := s.connectFixedServices(ctx, network.ID); err != nil {
		return nil, fail("DOCKER_UNAVAILABLE", "lab network could not connect fixed services", err)
	}

	redisResult := map[string]any(nil)
	if payload.RedisRequired {
		container, err := s.ensureRedis(ctx, command, scenario, names)
		if err != nil {
			return nil, fail("DOCKER_UNAVAILABLE", "session Redis could not be provisioned", err)
		}
		redisResult = map[string]any{
			"containerId": container.ID, "containerName": container.Name,
			"status": "running",
		}
	}

	instances := make([]map[string]any, 0, payload.InitialInstances)
	servers := make([]nginx.Server, 0, payload.InitialInstances)
	for i := 1; i <= payload.InitialInstances; i++ {
		instanceName := "app-" + strconv.Itoa(i)
		container, err := s.ensureApp(
			ctx,
			command,
			scenario,
			appTemplate,
			appImage,
			names,
			instanceName,
			scenario.LoadModel.InitialPerformancePercent,
		)
		if err != nil {
			return nil, fail("DOCKER_UNAVAILABLE", "application instance could not be provisioned", err)
		}
		instances = append(instances, map[string]any{
			"instanceName":  instanceName,
			"containerId":   container.ID,
			"containerName": container.Name,
			"status":        "running",
			"cpuLimitCores": scenario.Resources.BaseCPULimitCores *
				float64(scenario.LoadModel.InitialPerformancePercent) / 100,
			"memoryLimitMb":      scenario.Resources.MemoryLimitMB,
			"performancePercent": scenario.LoadModel.InitialPerformancePercent,
			"processingSpeed": scenario.LoadModel.BaseProcessingSpeed *
				scenario.LoadModel.InitialPerformancePercent / 100,
			"maxLoad":       scenario.LoadModel.MaxLoad,
			"currentWeight": scenario.LoadBalancing.InitialWeight,
		})
		servers = append(servers, nginx.Server{
			Name: instanceName, Host: names.appContainer(instanceName), Port: 8080,
			Weight: scenario.LoadBalancing.InitialWeight,
		})
	}
	if err := s.nginx.Apply(ctx, command.LabID, servers); err != nil {
		return nil, fail("NGINX_CONFIG_INVALID", "lab gateway configuration could not be applied", err)
	}
	compensate = false
	return map[string]any{
		"labId":              command.LabID,
		"scenarioTemplateId": scenario.TemplateID,
		"databaseName":       names.database,
		"databaseUser":       names.databaseUser,
		"networkId":          network.ID,
		"networkName":        names.network,
		"instances":          instances,
		"redis":              redisResult,
	}, nil
}

func (s *Service) resetLab(ctx context.Context, command protocol.Command) (any, *commandError) {
	var payload scenarioPayload
	if err := decodePayload(command.Payload, &payload, true); err != nil {
		return nil, reject("INVALID_COMMAND", "invalid reset payload", err)
	}
	scenario, ok := s.registry.Scenario(payload.ScenarioTemplateID)
	if !ok {
		return nil, reject("TEMPLATE_NOT_FOUND", "scenario template is not available", nil)
	}
	names, err := s.names(command.LabID, scenario)
	if err != nil {
		return nil, reject("INVALID_COMMAND", "lab identity is invalid", err)
	}
	if err := s.destroyResources(ctx, command.LabID, names); err != nil {
		return nil, fail(
			"RESOURCE_CLEANUP_FAILED",
			"existing lab resources could not be removed for reset",
			err,
		)
	}
	provisionBody, err := json.Marshal(provisionPayload{
		CourseID:           1,
		ScenarioType:       scenario.ScenarioType,
		ScenarioTemplateID: scenario.TemplateID,
		InitialInstances:   max(1, scenario.Topology.InitialInstances),
		RedisRequired:      scenario.ResourceTemplates.Redis != "",
	})
	if err != nil {
		return nil, fail("INTERNAL_ERROR", "reset command could not be prepared", err)
	}
	command.Payload = provisionBody
	return s.provision(ctx, command)
}

func (s *Service) destroy(ctx context.Context, command protocol.Command) (any, *commandError) {
	var payload destroyPayload
	if err := decodePayload(command.Payload, &payload, false); err != nil {
		return nil, reject("INVALID_COMMAND", "invalid destroy payload", err)
	}
	if payload.Reason != "" && payload.Reason != "user_requested" &&
		payload.Reason != "idle_timeout" && payload.Reason != "maximum_duration" {
		return nil, reject("INVALID_COMMAND", "destroy reason is invalid", nil)
	}
	names, err := s.basicNames(command.LabID)
	if err != nil {
		return nil, reject("INVALID_COMMAND", "lab identity is invalid", err)
	}
	if err := s.destroyResources(ctx, command.LabID, names); err != nil {
		return nil, fail("RESOURCE_CLEANUP_FAILED", "lab resources could not be fully removed", err)
	}
	return map[string]any{"labId": command.LabID, "destroyed": true}, nil
}

func (s *Service) createAppCommand(
	ctx context.Context,
	command protocol.Command,
) (any, *commandError) {
	var payload createAppPayload
	if err := decodePayload(command.Payload, &payload, true); err != nil || !validInstanceName(payload.InstanceName) {
		return nil, reject("INVALID_COMMAND", "invalid application instance payload", err)
	}
	scenario, ok := s.registry.Scenario(payload.ScenarioTemplateID)
	if !ok {
		return nil, reject("TEMPLATE_NOT_FOUND", "scenario template is not available", nil)
	}
	performance := payload.PerformancePercent
	if performance == 0 {
		performance = scenario.LoadModel.InitialPerformancePercent
	}
	if !validPerformance(scenario, performance) {
		return nil, reject("INVALID_COMMAND", "performance percentage is outside the scenario range", nil)
	}
	names, err := s.names(command.LabID, scenario)
	if err != nil {
		return nil, reject("INVALID_COMMAND", "lab identity is invalid", err)
	}
	templateValue, _ := s.registry.Container(scenario.ResourceTemplates.Application)
	image, err := s.imageFor(templateValue.ImageEnv)
	if err != nil {
		return nil, reject("TEMPLATE_NOT_FOUND", "application image is not configured", err)
	}
	container, err := s.ensureApp(
		ctx, command, scenario, templateValue, image, names, payload.InstanceName, performance,
	)
	if err != nil {
		return nil, fail("DOCKER_UNAVAILABLE", "application instance could not be created", err)
	}
	return map[string]any{
		"instanceName":       payload.InstanceName,
		"containerId":        container.ID,
		"containerName":      container.Name,
		"status":             "running",
		"cpuLimitCores":      scenario.Resources.BaseCPULimitCores * float64(performance) / 100,
		"memoryLimitMb":      scenario.Resources.MemoryLimitMB,
		"performancePercent": performance,
		"processingSpeed":    scenario.LoadModel.BaseProcessingSpeed * performance / 100,
		"maxLoad":            scenario.LoadModel.MaxLoad,
		"currentWeight":      scenario.LoadBalancing.InitialWeight,
	}, nil
}

func (s *Service) deleteAppCommand(
	ctx context.Context,
	command protocol.Command,
) (any, *commandError) {
	var payload instancePayload
	if err := decodePayload(command.Payload, &payload, true); err != nil || !validInstanceName(payload.InstanceName) {
		return nil, reject("INVALID_COMMAND", "invalid application instance payload", err)
	}
	resource, err := s.findInstance(ctx, command.LabID, payload.InstanceName)
	if err != nil {
		return nil, fail("INSTANCE_NOT_FOUND", "application instance was not found", err)
	}
	if err := s.docker.RemoveContainer(ctx, resource.ID); err != nil {
		return nil, fail("DOCKER_UNAVAILABLE", "application instance could not be removed", err)
	}
	return map[string]any{"instanceName": payload.InstanceName, "deleted": true}, nil
}

func (s *Service) updateAppCapacity(
	ctx context.Context,
	command protocol.Command,
) (any, *commandError) {
	var payload updateCapacityPayload
	if err := decodePayload(command.Payload, &payload, true); err != nil || !validInstanceName(payload.InstanceName) {
		return nil, reject("INVALID_COMMAND", "invalid capacity payload", err)
	}
	scenario, ok := s.registry.Scenario(payload.ScenarioTemplateID)
	if !ok || !validPerformance(scenario, payload.PerformancePercent) ||
		!validPerformance(scenario, payload.PreviousPerformancePercent) {
		return nil, reject("INVALID_COMMAND", "capacity settings are invalid", nil)
	}
	resource, err := s.findInstance(ctx, command.LabID, payload.InstanceName)
	if err != nil {
		return nil, fail("INSTANCE_NOT_FOUND", "application instance was not found", err)
	}
	names, err := s.names(command.LabID, scenario)
	if err != nil {
		return nil, reject("INVALID_COMMAND", "lab identity is invalid", err)
	}
	templateValue, ok := s.registry.Container(scenario.ResourceTemplates.Application)
	if !ok {
		return nil, reject("TEMPLATE_NOT_FOUND", "application template is not available", nil)
	}
	image, err := s.imageFor(templateValue.ImageEnv)
	if err != nil {
		return nil, reject("TEMPLATE_NOT_FOUND", "application image is not configured", err)
	}
	if err := s.docker.RemoveContainer(ctx, resource.ID); err != nil {
		return nil, fail("DOCKER_UNAVAILABLE", "application instance could not be replaced", err)
	}
	container, err := s.ensureApp(
		ctx, command, scenario, templateValue, image, names,
		payload.InstanceName, payload.PerformancePercent,
	)
	if err != nil {
		_, rollbackErr := s.ensureApp(
			context.WithoutCancel(ctx), command, scenario, templateValue, image, names,
			payload.InstanceName, payload.PreviousPerformancePercent,
		)
		if rollbackErr != nil {
			err = errors.Join(err, rollbackErr)
		}
		return nil, fail("DOCKER_UNAVAILABLE", "application instance could not be replaced", err)
	}
	cpuCores := scenario.Resources.BaseCPULimitCores * float64(payload.PerformancePercent) / 100
	return map[string]any{
		"instanceName":       payload.InstanceName,
		"containerId":        container.ID,
		"containerName":      container.Name,
		"status":             "running",
		"performancePercent": payload.PerformancePercent,
		"cpuLimitCores":      cpuCores,
		"memoryLimitMb":      scenario.Resources.MemoryLimitMB,
		"processingSpeed":    scenario.LoadModel.BaseProcessingSpeed * payload.PerformancePercent / 100,
		"maxLoad":            scenario.LoadModel.MaxLoad,
		"currentWeight":      scenario.LoadBalancing.InitialWeight,
	}, nil
}

func (s *Service) createRedisCommand(
	ctx context.Context,
	command protocol.Command,
) (any, *commandError) {
	var payload scenarioPayload
	if err := decodePayload(command.Payload, &payload, true); err != nil {
		return nil, reject("INVALID_COMMAND", "invalid Redis payload", err)
	}
	scenario, ok := s.registry.Scenario(payload.ScenarioTemplateID)
	if !ok || scenario.ResourceTemplates.Redis == "" {
		return nil, reject("TEMPLATE_NOT_FOUND", "Redis is not available for the scenario", nil)
	}
	names, err := s.names(command.LabID, scenario)
	if err != nil {
		return nil, reject("INVALID_COMMAND", "lab identity is invalid", err)
	}
	if existing, findErr := s.findResourceType(ctx, command.LabID, "session-redis"); findErr == nil {
		return map[string]any{"containerId": existing.ID, "containerName": existing.Name}, nil
	} else if !errors.Is(findErr, errResourceNotFound) {
		return nil, fail("DOCKER_UNAVAILABLE", "session Redis could not be inspected", findErr)
	}
	container, err := s.ensureRedis(ctx, command, scenario, names)
	if err != nil {
		return nil, fail("DOCKER_UNAVAILABLE", "session Redis could not be created", err)
	}
	return map[string]any{"containerId": container.ID, "containerName": container.Name}, nil
}

func (s *Service) restartRedis(ctx context.Context, command protocol.Command) (any, *commandError) {
	if err := decodeEmptyPayload(command.Payload); err != nil {
		return nil, reject("INVALID_COMMAND", "restart Redis does not accept parameters", err)
	}
	resource, err := s.findResourceType(ctx, command.LabID, "session-redis")
	if err != nil {
		return nil, fail("REDIS_NOT_FOUND", "session Redis was not found", err)
	}
	if err := s.docker.RestartContainer(ctx, resource.ID); err != nil {
		return nil, fail("DOCKER_UNAVAILABLE", "session Redis could not be restarted", err)
	}
	return map[string]any{"restarted": true}, nil
}

func (s *Service) deleteRedis(ctx context.Context, command protocol.Command) (any, *commandError) {
	if err := decodeEmptyPayload(command.Payload); err != nil {
		return nil, reject("INVALID_COMMAND", "delete Redis does not accept parameters", err)
	}
	resource, err := s.findResourceType(ctx, command.LabID, "session-redis")
	if errors.Is(err, errResourceNotFound) {
		return map[string]any{"deleted": false}, nil
	}
	if err != nil {
		return nil, fail("REDIS_NOT_FOUND", "session Redis was not found", err)
	}
	if err := s.docker.RemoveContainer(ctx, resource.ID); err != nil {
		return nil, fail("DOCKER_UNAVAILABLE", "session Redis could not be removed", err)
	}
	return map[string]any{"deleted": true}, nil
}

func (s *Service) applyUpstream(ctx context.Context, command protocol.Command) (any, *commandError) {
	var payload upstreamPayload
	if err := decodePayload(command.Payload, &payload, true); err != nil {
		return nil, reject("INVALID_COMMAND", "invalid upstream payload", err)
	}
	names, err := s.basicNames(command.LabID)
	if err != nil {
		return nil, reject("INVALID_COMMAND", "lab identity is invalid", err)
	}
	servers, err := payload.nginxServers(names)
	if err != nil {
		return nil, reject("INVALID_COMMAND", "invalid upstream payload", err)
	}
	if err := s.nginx.Apply(ctx, command.LabID, servers); err != nil {
		return nil, fail("NGINX_CONFIG_INVALID", "lab gateway configuration could not be applied", err)
	}
	return map[string]any{"applied": true}, nil
}

func (s *Service) removeUpstream(ctx context.Context, command protocol.Command) (any, *commandError) {
	if err := decodeEmptyPayload(command.Payload); err != nil {
		return nil, reject("INVALID_COMMAND", "remove upstream does not accept parameters", err)
	}
	if err := s.nginx.Remove(ctx, command.LabID); err != nil {
		return nil, fail("NGINX_CONFIG_INVALID", "lab gateway configuration could not be removed", err)
	}
	return map[string]any{"removed": true}, nil
}

func (s *Service) resetDatabase(ctx context.Context, command protocol.Command) (any, *commandError) {
	if err := decodeEmptyPayload(command.Payload); err != nil {
		return nil, reject("INVALID_COMMAND", "reset database does not accept parameters", err)
	}
	names, err := s.basicNames(command.LabID)
	if err != nil {
		return nil, reject("INVALID_COMMAND", "lab identity is invalid", err)
	}
	if err := s.database.Reset(ctx, names.database); err != nil {
		return nil, fail("LAB_DATABASE_UNAVAILABLE", "lab database could not be reset", err)
	}
	return map[string]any{"reset": true}, nil
}

func (s *Service) reconcile(ctx context.Context, command protocol.Command) (any, *commandError) {
	var payload reconcilePayload
	if err := decodePayload(command.Payload, &payload, false); err != nil {
		return nil, reject("INVALID_COMMAND", "invalid reconciliation payload", err)
	}
	if payload.ExpectedLabIDs == nil {
		return nil, reject(
			"INVALID_COMMAND",
			"reconciliation payload must include expectedLabIds",
			nil,
		)
	}
	expected := make(map[string]bool, len(*payload.ExpectedLabIDs))
	for _, labID := range *payload.ExpectedLabIDs {
		if !validLabID(labID) {
			return nil, reject(
				"INVALID_COMMAND",
				"reconciliation payload contains an invalid lab id",
				nil,
			)
		}
		if expected[labID] {
			return nil, reject(
				"INVALID_COMMAND",
				"reconciliation payload contains a duplicate lab id",
				nil,
			)
		}
		expected[labID] = true
	}
	expectedIDs := make([]string, 0, len(expected))
	for labID := range expected {
		expectedIDs = append(expectedIDs, labID)
	}
	sort.Strings(expectedIDs)
	reconnectedNetworks, err := s.reconnectExpectedNetworks(ctx, expected)
	if err != nil {
		return nil, fail("RECONCILIATION_FAILED", "managed lab networks could not be restored", err)
	}
	actual, err := s.actualLabIDs(ctx)
	if err != nil {
		return nil, fail("RECONCILIATION_FAILED", "managed resources could not be inspected", err)
	}
	databases, err := s.database.ListManaged(ctx)
	if err != nil {
		return nil, fail("RECONCILIATION_FAILED", "lab databases could not be inspected", err)
	}
	orphans := make([]string, 0)
	for labID := range actual {
		if !expected[labID] {
			orphans = append(orphans, labID)
		}
	}
	sort.Strings(orphans)
	cleaned := make([]string, 0)
	if payload.Cleanup {
		for _, labID := range orphans {
			names, err := s.basicNames(labID)
			if err != nil {
				return nil, fail("RECONCILIATION_FAILED", "orphan lab identity is invalid", err)
			}
			if err := s.destroyResources(ctx, labID, names); err != nil {
				return nil, fail("RECONCILIATION_FAILED", "orphan resources could not be removed", err)
			}
			cleaned = append(cleaned, labID)
		}
	}
	expectedDatabases := make(map[string]bool, len(expectedIDs))
	for _, labID := range expectedIDs {
		names, err := s.basicNames(labID)
		if err != nil {
			return nil, reject("INVALID_COMMAND", "expected lab identity is invalid", err)
		}
		expectedDatabases[names.database] = true
	}
	databaseOrphans := make([]labdb.ManagedDatabase, 0)
	missingDatabases := make([]string, 0)
	actualDatabases := make(map[string]bool, len(databases))
	for _, database := range databases {
		actualDatabases[database.DatabaseName] = true
		if !expectedDatabases[database.DatabaseName] {
			databaseOrphans = append(databaseOrphans, database)
		}
	}
	for databaseName := range expectedDatabases {
		if !actualDatabases[databaseName] {
			missingDatabases = append(missingDatabases, databaseName)
		}
	}
	sort.Slice(databaseOrphans, func(i, j int) bool {
		return databaseOrphans[i].DatabaseName < databaseOrphans[j].DatabaseName
	})
	sort.Strings(missingDatabases)
	cleanedDatabases := make([]string, 0)
	if payload.Cleanup {
		for _, database := range databaseOrphans {
			if err := s.database.Destroy(ctx, database.DatabaseName, database.UserName); err != nil {
				return nil, fail("RECONCILIATION_FAILED", "orphan lab database could not be removed", err)
			}
			cleanedDatabases = append(cleanedDatabases, database.DatabaseName)
		}
	}
	return map[string]any{
		"expectedLabIds":      expectedIDs,
		"orphanLabIds":        orphans,
		"cleanedLabIds":       cleaned,
		"orphanDatabases":     databaseNames(databaseOrphans),
		"missingDatabases":    missingDatabases,
		"cleanedDatabases":    cleanedDatabases,
		"cleanupRequested":    payload.Cleanup,
		"reconnectedNetworks": reconnectedNetworks,
	}, nil
}

func (s *Service) reconnectExpectedNetworks(
	ctx context.Context,
	expected map[string]bool,
) ([]string, error) {
	networks, err := s.docker.ListManagedNetworks(ctx, "")
	if err != nil {
		return nil, err
	}
	reconnected := make([]string, 0)
	for _, network := range networks {
		labID := network.Labels["platform.labId"]
		if !expected[labID] {
			continue
		}
		if err := s.connectFixedServices(ctx, network.ID); err != nil {
			return nil, fmt.Errorf("reconnect fixed services for %s: %w", labID, err)
		}
		reconnected = append(reconnected, labID)
	}
	sort.Strings(reconnected)
	return reconnected, nil
}

func databaseNames(values []labdb.ManagedDatabase) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.DatabaseName)
	}
	return result
}

func (s *Service) ensureApp(
	ctx context.Context,
	command protocol.Command,
	scenario templates.Scenario,
	templateValue templates.Container,
	image string,
	names resourceNames,
	instanceName string,
	performance int,
) (dockerapi.EnsureResult, error) {
	if err := s.docker.InspectImage(ctx, image); err != nil {
		return dockerapi.EnsureResult{}, err
	}
	cpuCores := scenario.Resources.BaseCPULimitCores * float64(performance) / 100
	labels := resourceLabels(command, "app-container", templateValue.TemplateID)
	labels["platform.instanceName"] = instanceName
	password, err := s.databasePassword(command.LabID)
	if err != nil {
		return dockerapi.EnsureResult{}, err
	}
	return s.docker.EnsureContainer(ctx, dockerapi.ContainerSpec{
		Name: names.appContainer(instanceName), Image: image, User: templateValue.User,
		Environment: map[string]string{
			"LAB_APP_ADDR": ":8080", "LAB_ID": command.LabID,
			"INSTANCE_ID": instanceName, "SCENARIO_TYPE": scenario.ScenarioType,
			"MYSQL_HOST": "lab-db", "MYSQL_PORT": "3306",
			"MYSQL_DATABASE": names.database, "MYSQL_USER": names.databaseUser,
			"MYSQL_PASSWORD":      password,
			"PERFORMANCE_PERCENT": strconv.Itoa(performance),
			"PROCESSING_SPEED": strconv.Itoa(
				scenario.LoadModel.BaseProcessingSpeed * performance / 100,
			),
			"MAX_LOAD":                     strconv.Itoa(scenario.LoadModel.MaxLoad),
			"REDIS_ADDR":                   "lab-redis:6379",
			"CACHE_L1_MAX_PRODUCT_ENTRIES": strconv.Itoa(scenario.Cache.L1MaxProductEntries),
			"CACHE_INSTANCE_COUNT":         strconv.Itoa(max(1, scenario.Topology.InitialInstances)),
			"CACHE_L1_TTL_MS":              strconv.Itoa(scenario.Cache.L1TTLMS),
			"CACHE_L2_TTL_MS":              strconv.Itoa(scenario.Cache.L2TTLMS),
			"CACHE_L2_TTL_JITTER_PERCENT":  strconv.Itoa(scenario.Cache.L2TTLJitterPercent),
			"CACHE_LATENCY_L1_MS":          strconv.Itoa(scenario.Cache.SimulatedLatencyMS.L1),
			"CACHE_LATENCY_REDIS_MS":       strconv.Itoa(scenario.Cache.SimulatedLatencyMS.Redis),
			"CACHE_LATENCY_MYSQL_MS":       strconv.Itoa(scenario.Cache.SimulatedLatencyMS.MySQL),
			"CACHE_LATENCY_DEGRADED_MS":    strconv.Itoa(scenario.Cache.SimulatedLatencyMS.Degraded),
			"TZ":                           "UTC",
		},
		Labels: labels, NetworkName: names.network,
		NetworkAliases: []string{names.appContainer(instanceName)},
		ReadOnlyRootFS: templateValue.ReadOnlyRootFilesystem,
		CapDrop:        append([]string(nil), templateValue.CapDrop...),
		MemoryBytes:    int64(scenario.Resources.MemoryLimitMB) << 20,
		NanoCPUs:       nanoCPUs(cpuCores), PidsLimit: int64(scenario.Resources.PidsLimit),
		Healthcheck: healthcheck(templateValue),
	})
}

func (s *Service) ensureRedis(
	ctx context.Context,
	command protocol.Command,
	scenario templates.Scenario,
	names resourceNames,
) (dockerapi.EnsureResult, error) {
	templateValue, ok := s.registry.Container(scenario.ResourceTemplates.Redis)
	if !ok {
		return dockerapi.EnsureResult{}, errors.New("redis template does not exist")
	}
	image, err := s.imageFor(templateValue.ImageEnv)
	if err != nil {
		return dockerapi.EnsureResult{}, err
	}
	if err := s.docker.InspectImage(ctx, image); err != nil {
		return dockerapi.EnsureResult{}, err
	}
	return s.docker.EnsureContainer(ctx, dockerapi.ContainerSpec{
		Name: names.redisContainer(), Image: image, User: templateValue.User,
		Command:     append([]string(nil), templateValue.Command...),
		Environment: map[string]string{"TZ": "UTC"},
		Labels:      resourceLabels(command, "session-redis", templateValue.TemplateID),
		NetworkName: names.network, NetworkAliases: []string{"lab-redis"},
		ReadOnlyRootFS: templateValue.ReadOnlyRootFilesystem,
		CapDrop:        append([]string(nil), templateValue.CapDrop...),
		MemoryBytes:    int64(templateValue.MemoryLimitMB) << 20,
		NanoCPUs:       50_000_000, PidsLimit: int64(templateValue.PidsLimit),
	})
}

func (s *Service) connectFixedServices(ctx context.Context, networkID string) error {
	gateway, err := s.docker.FindComposeContainer(ctx, s.config.ComposeProject, s.config.GatewayService)
	if err != nil {
		return err
	}
	mysql, err := s.docker.FindComposeContainer(ctx, s.config.ComposeProject, s.config.MySQLService)
	if err != nil {
		return err
	}
	if err := s.docker.ConnectNetwork(ctx, networkID, gateway.ID, []string{"lab-gateway"}); err != nil {
		return err
	}
	return s.docker.ConnectNetwork(ctx, networkID, mysql.ID, []string{"lab-db"})
}

func (s *Service) destroyResources(ctx context.Context, labID string, names resourceNames) error {
	var failures []error
	if err := s.nginx.Remove(ctx, labID); err != nil {
		failures = append(failures, err)
	}
	containers, err := s.docker.ListManagedContainers(ctx, labID)
	if err != nil {
		failures = append(failures, err)
	} else {
		for _, resource := range containers {
			if err := s.docker.RemoveContainer(ctx, resource.ID); err != nil {
				failures = append(failures, err)
			}
		}
	}
	networks, err := s.docker.ListManagedNetworks(ctx, labID)
	if err != nil {
		failures = append(failures, err)
	} else {
		gateway, gatewayErr := s.docker.FindComposeContainer(
			ctx, s.config.ComposeProject, s.config.GatewayService,
		)
		mysql, mysqlErr := s.docker.FindComposeContainer(
			ctx, s.config.ComposeProject, s.config.MySQLService,
		)
		for _, network := range networks {
			if gatewayErr == nil {
				if err := s.docker.DisconnectNetwork(ctx, network.ID, gateway.ID); err != nil {
					failures = append(failures, err)
				}
			}
			if mysqlErr == nil {
				if err := s.docker.DisconnectNetwork(ctx, network.ID, mysql.ID); err != nil {
					failures = append(failures, err)
				}
			}
			if err := s.docker.RemoveNetwork(ctx, network.ID); err != nil {
				failures = append(failures, err)
			}
		}
	}
	if err := s.database.Destroy(ctx, names.database, names.databaseUser); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (s *Service) actualLabIDs(ctx context.Context) (map[string]bool, error) {
	actual := make(map[string]bool)
	containers, err := s.docker.ListManagedContainers(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, resource := range containers {
		if validLabID(resource.Labels["platform.labId"]) {
			actual[resource.Labels["platform.labId"]] = true
		}
	}
	networks, err := s.docker.ListManagedNetworks(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, resource := range networks {
		if validLabID(resource.Labels["platform.labId"]) {
			actual[resource.Labels["platform.labId"]] = true
		}
	}
	fragments, err := s.nginx.List()
	if err != nil {
		return nil, err
	}
	for _, labID := range fragments {
		actual[labID] = true
	}
	return actual, nil
}

func (s *Service) findInstance(
	ctx context.Context,
	labID string,
	instanceName string,
) (dockerapi.Resource, error) {
	resources, err := s.docker.ListManagedContainers(ctx, labID)
	if err != nil {
		return dockerapi.Resource{}, err
	}
	for _, resource := range resources {
		if resource.Labels["platform.resourceType"] == "app-container" &&
			resource.Labels["platform.instanceName"] == instanceName {
			return resource, nil
		}
	}
	return dockerapi.Resource{}, errors.New("instance not found")
}

func (s *Service) findResourceType(
	ctx context.Context,
	labID string,
	resourceType string,
) (dockerapi.Resource, error) {
	resources, err := s.docker.ListManagedContainers(ctx, labID)
	if err != nil {
		return dockerapi.Resource{}, err
	}
	for _, resource := range resources {
		if resource.Labels["platform.resourceType"] == resourceType {
			return resource, nil
		}
	}
	return dockerapi.Resource{}, errResourceNotFound
}

var errResourceNotFound = errors.New("resource not found")

func (s *Service) imageFor(environmentName string) (string, error) {
	switch environmentName {
	case "LAB_APP_IMAGE":
		return s.config.LabAppImage, nil
	case "REDIS_IMAGE":
		return s.config.RedisImage, nil
	default:
		return "", fmt.Errorf("image environment %q is not allowlisted", environmentName)
	}
}

func (s *Service) databasePassword(labID string) (string, error) {
	mac := hmac.New(sha256.New, []byte(s.config.DatabaseSecret))
	if _, err := mac.Write([]byte("lab-database:" + labID)); err != nil {
		return "", fmt.Errorf("derive database credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

type resourceNames struct {
	normalized   string
	database     string
	databaseUser string
	network      string
}

func (s *Service) names(labID string, scenario templates.Scenario) (resourceNames, error) {
	names, err := s.basicNames(labID)
	if err != nil {
		return resourceNames{}, err
	}
	databaseTemplate, ok := s.registry.Database(scenario.ResourceTemplates.Database)
	if !ok {
		return resourceNames{}, errors.New("database template does not exist")
	}
	networkTemplate, ok := s.registry.Network(scenario.ResourceTemplates.Network)
	if !ok {
		return resourceNames{}, errors.New("network template does not exist")
	}
	names.database = strings.ReplaceAll(
		databaseTemplate.DatabaseNamePattern,
		"{normalizedLabId}",
		names.normalized,
	)
	names.databaseUser = strings.ReplaceAll(
		databaseTemplate.UserNamePattern,
		"{normalizedLabId}",
		names.normalized,
	)
	if len(names.database) > 64 || len(names.databaseUser) > 32 {
		return resourceNames{}, errors.New("database resource name exceeds engine limits")
	}
	names.network = strings.ReplaceAll(networkTemplate.NamePattern, "{labId}", labID)
	return names, nil
}

func (s *Service) basicNames(labID string) (resourceNames, error) {
	if !validLabID(labID) {
		return resourceNames{}, errors.New("invalid lab id")
	}
	normalized := strings.ReplaceAll(strings.TrimPrefix(labID, "lab-"), "-", "")
	if len(normalized) < 4 || len(normalized) > 32 {
		return resourceNames{}, errors.New("normalized lab id has invalid length")
	}
	for _, r := range normalized {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				return resourceNames{}, errors.New("normalized lab id is invalid")
			}
		}
	}
	if len(normalized) > maxDatabaseIdentityLength {
		normalized = normalized[:maxDatabaseIdentityLength]
	}
	return resourceNames{
		normalized:   normalized,
		database:     "lab_" + normalized,
		databaseUser: "lab_" + normalized + "_user",
		network:      "lab-" + labID + "-net",
	}, nil
}

func (n resourceNames) appContainer(instanceName string) string {
	return "lab-" + n.normalized + "-" + instanceName
}

func (n resourceNames) redisContainer() string {
	return "lab-" + n.normalized + "-redis"
}

func resourceLabels(
	command protocol.Command,
	resourceType string,
	templateID string,
) map[string]string {
	return map[string]string{
		"platform.managed":      "true",
		"platform.labId":        command.LabID,
		"platform.operationId":  command.OperationID,
		"platform.resourceType": resourceType,
		"platform.templateId":   templateID,
	}
}

func healthcheck(value templates.Container) *dockerapi.Healthcheck {
	if value.Healthcheck == nil {
		return nil
	}
	return &dockerapi.Healthcheck{
		Command:  []string{"/usr/local/bin/lab-app", "healthcheck"},
		Interval: durationSeconds(value.Healthcheck.IntervalSeconds),
		Timeout:  durationSeconds(value.Healthcheck.TimeoutSeconds),
		Retries:  value.Healthcheck.Retries,
	}
}

func durationSeconds(value int) time.Duration {
	return time.Duration(value) * time.Second
}

func nanoCPUs(cores float64) int64 {
	return int64(cores * 1_000_000_000)
}

func validPerformance(scenario templates.Scenario, value int) bool {
	return value >= scenario.LoadModel.MinPerformancePercent &&
		value <= scenario.LoadModel.MaxPerformancePercent && value%10 == 0
}

func validateEnvelope(command protocol.Command) (string, string) {
	if _, ok := allowedCommands[command.CommandType]; !ok {
		return "COMMAND_NOT_ALLOWED", "command type is not allowlisted"
	}
	if !validToken(command.CommandID) || !validToken(command.OperationID) ||
		!validLabID(command.LabID) || !validToken(command.RequestedBy) {
		return "INVALID_COMMAND", "command identity is invalid"
	}
	return "", ""
}

func validToken(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && (r == '-' || r == '_' || r == '.') {
			continue
		}
		return false
	}
	return true
}

func validLabID(value string) bool {
	if len(value) < 4 || len(value) > 64 {
		return false
	}
	for i, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		if i > 0 && r == '-' {
			continue
		}
		return false
	}
	return true
}

func validInstanceName(value string) bool {
	if !strings.HasPrefix(value, "app-") || len(value) > 64 {
		return false
	}
	number, err := strconv.Atoi(strings.TrimPrefix(value, "app-"))
	return err == nil && number > 0 && number <= 4
}

func decodePayload(body json.RawMessage, destination any, required bool) error {
	if len(bytes.TrimSpace(body)) == 0 {
		if required {
			return errors.New("payload is required")
		}
		body = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("payload contains multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func decodeEmptyPayload(body json.RawMessage) error {
	var payload map[string]any
	if err := decodePayload(body, &payload, false); err != nil {
		return err
	}
	if len(payload) != 0 {
		return errors.New("payload must be empty")
	}
	return nil
}

type commandError struct {
	status  string
	code    string
	message string
	cause   error
}

func reject(code, message string, cause error) *commandError {
	return &commandError{status: "rejected", code: code, message: message, cause: cause}
}

func fail(code, message string, cause error) *commandError {
	return &commandError{status: "failed", code: code, message: message, cause: cause}
}

func commandResponse(
	command protocol.Command,
	status string,
	result any,
	code string,
	message string,
) protocol.CommandResponse {
	response := protocol.CommandResponse{
		CommandID: command.CommandID, OperationID: command.OperationID,
		Status: status, Result: result,
	}
	if code != "" {
		response.Error = map[string]string{"code": code, "message": message}
	}
	return response
}
