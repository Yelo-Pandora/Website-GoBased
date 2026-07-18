package operation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"website-gobased/internal/protocol"
	"website-gobased/services/platform-api/internal/lab"
)

type queue interface {
	Claim(
		ctx context.Context,
		owner string,
		now time.Time,
		leaseExpires time.Time,
	) (Record, error)
	MarkRunning(
		ctx context.Context,
		record Record,
		now time.Time,
		leaseExpires time.Time,
	) error
	CompleteProvision(
		ctx context.Context,
		record Record,
		resultValue ProvisionResult,
		errorCode string,
		errorMessage string,
		now time.Time,
	) error
	CompleteReset(
		ctx context.Context,
		record Record,
		resultValue ProvisionResult,
		errorCode string,
		errorMessage string,
		now time.Time,
	) error
	CompleteDestroy(
		ctx context.Context,
		record Record,
		resultValue any,
		errorCode string,
		errorMessage string,
		now time.Time,
	) error
	CompleteTopology(
		ctx context.Context,
		record Record,
		resultValue TopologyResult,
		errorCode string,
		errorMessage string,
		now time.Time,
	) error
	ValidateAdaptive(
		ctx context.Context,
		record Record,
		topologyFingerprint string,
	) (bool, error)
	CompleteFailure(
		ctx context.Context,
		record Record,
		errorCode string,
		errorMessage string,
		now time.Time,
	) error
}

type commandExecutor interface {
	Execute(
		ctx context.Context,
		command protocol.Command,
	) (protocol.CommandResponse, error)
}

// WorkerConfig controls queue polling and operation leases.
type WorkerConfig struct {
	Owner          string
	PollInterval   time.Duration
	LeaseDuration  time.Duration
	CommandTimeout time.Duration
}

// Worker leases persistent operations and executes trusted orchestrator commands.
type Worker struct {
	logger       *slog.Logger
	queue        queue
	executor     commandExecutor
	config       WorkerConfig
	now          func() time.Time
	newCommandID func() (string, error)
}

// NewWorker returns a persistent operation queue worker.
func NewWorker(
	logger *slog.Logger,
	queue *Repository,
	executor commandExecutor,
	config WorkerConfig,
) (*Worker, error) {
	return newWorker(logger, queue, executor, config)
}

func newWorker(
	logger *slog.Logger,
	queue queue,
	executor commandExecutor,
	config WorkerConfig,
) (*Worker, error) {
	if logger == nil || queue == nil || executor == nil {
		return nil, errors.New("operation worker dependencies are required")
	}
	if config.Owner == "" || config.PollInterval <= 0 ||
		config.LeaseDuration <= 0 || config.CommandTimeout <= 0 {
		return nil, errors.New("operation worker config is invalid")
	}
	if config.LeaseDuration <= config.CommandTimeout {
		return nil, errors.New("operation lease duration must exceed command timeout")
	}
	return &Worker{
		logger:       logger,
		queue:        queue,
		executor:     executor,
		config:       config,
		now:          time.Now,
		newCommandID: newCommandID,
	}, nil
}

// Run drains available operations and waits until the context is canceled.
func (w *Worker) Run(ctx context.Context) {
	for {
		processed, err := w.processOne(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			w.logger.ErrorContext(ctx, "process lab operation", "error", err)
		}
		if ctx.Err() != nil {
			return
		}
		if processed {
			continue
		}
		timer := time.NewTimer(w.config.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) processOne(ctx context.Context) (bool, error) {
	now := w.now().UTC().Truncate(time.Microsecond)
	record, err := w.queue.Claim(
		ctx,
		w.config.Owner,
		now,
		now.Add(w.config.LeaseDuration),
	)
	if errors.Is(err, ErrNoPending) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := w.queue.MarkRunning(
		ctx,
		record,
		now,
		now.Add(w.config.LeaseDuration),
	); err != nil {
		return true, err
	}
	if isTopologyAction(record.Action) {
		return true, w.processTopology(ctx, record)
	}
	commandType, ok := commandTypeForAction(record.Action)
	if !ok {
		return true, w.queue.CompleteFailure(
			ctx,
			record,
			"ACTION_NOT_SUPPORTED",
			"operation action is not supported by this worker",
			w.now().UTC().Truncate(time.Microsecond),
		)
	}
	commandID, err := w.newCommandID()
	if err != nil {
		return true, w.completeFailure(ctx, record, "INTERNAL_ERROR", err.Error())
	}
	command := protocol.Command{
		CommandType: commandType,
		CommandID:   commandID,
		OperationID: record.OperationID,
		LabID:       record.LabID,
		RequestedBy: strconv.FormatUint(record.RequestedBy, 10),
		Payload:     record.Payload,
	}
	commandCtx, cancel := context.WithTimeout(ctx, w.config.CommandTimeout)
	response, err := w.executor.Execute(commandCtx, command)
	cancel()
	if err != nil {
		return true, w.completeFailure(
			ctx,
			record,
			"ORCHESTRATOR_UNAVAILABLE",
			"orchestrator command could not be completed",
		)
	}
	switch response.Status {
	case "succeeded":
		return true, w.completeSuccess(ctx, record, response.Result)
	case "failed", "rejected":
		code, message := commandError(response.Error)
		return true, w.completeFailure(ctx, record, code, message)
	default:
		return true, w.completeFailure(
			ctx,
			record,
			"ORCHESTRATOR_RESPONSE_INCOMPLETE",
			"orchestrator did not return a final command result",
		)
	}
}

type topologyServer struct {
	InstanceName string `json:"instanceName"`
	Weight       int    `json:"weight"`
}

type topologyPayload struct {
	ScenarioTemplateID         string           `json:"scenarioTemplateId"`
	InstanceName               string           `json:"instanceName"`
	PerformancePercent         int              `json:"performancePercent"`
	PreviousPerformancePercent int              `json:"previousPerformancePercent"`
	Servers                    []topologyServer `json:"servers"`
	PreviousServers            []topologyServer `json:"previousServers"`
	ExpectedMode               string           `json:"expectedMode"`
	TopologyFingerprint        string           `json:"topologyFingerprint"`
	Request                    struct {
		Weights       []lab.InstanceWeight `json:"weights"`
		BalancingMode *string              `json:"balancingMode"`
	} `json:"request"`
}

func isTopologyAction(action string) bool {
	return action == ActionAddInstance || action == ActionRemoveInstance ||
		action == ActionSetInstancePerformance || action == ActionSetInstanceWeights ||
		action == ActionSetBalancingMode || action == ActionApplyAdaptiveWeights
}

func (w *Worker) processTopology(ctx context.Context, record Record) error {
	var payload topologyPayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return w.completeTopologyFailure(ctx, record, "INVALID_OPERATION", "topology operation payload is invalid")
	}
	switch record.Action {
	case ActionAddInstance:
		return w.addInstance(ctx, record, payload)
	case ActionRemoveInstance:
		return w.removeInstance(ctx, record, payload)
	case ActionSetInstancePerformance:
		return w.setInstancePerformance(ctx, record, payload)
	case ActionSetInstanceWeights:
		return w.setInstanceWeights(ctx, record, payload)
	case ActionSetBalancingMode:
		return w.setBalancingMode(ctx, record, payload)
	case ActionApplyAdaptiveWeights:
		return w.applyAdaptiveWeights(ctx, record, payload)
	default:
		return w.completeTopologyFailure(ctx, record, "ACTION_NOT_SUPPORTED", "topology action is not supported")
	}
}

func (w *Worker) addInstance(ctx context.Context, record Record, payload topologyPayload) error {
	response, err := w.executeTopologyCommand(ctx, record, "CREATE_APP_INSTANCE", map[string]any{
		"scenarioTemplateId": payload.ScenarioTemplateID,
		"instanceName":       payload.InstanceName,
		"performancePercent": payload.PerformancePercent,
	})
	if err != nil {
		return w.completeTopologyFailure(ctx, record, "ORCHESTRATOR_UNAVAILABLE", "application instance could not be created")
	}
	if response.Status != "succeeded" {
		code, message := commandError(response.Error)
		return w.completeTopologyFailure(ctx, record, code, message)
	}
	instance, err := decodeProvisionInstance(response.Result)
	if err != nil {
		return w.completeTopologyFailure(ctx, record, "ORCHESTRATOR_RESPONSE_INCOMPLETE", "application instance result is invalid")
	}
	if err := w.applyTopologyServers(ctx, record, payload.Servers); err != nil {
		_, _ = w.executeTopologyCommand(ctx, record, "DELETE_APP_INSTANCE", map[string]any{
			"instanceName": payload.InstanceName,
		})
		return w.completeTopologyFailure(ctx, record, "NGINX_CONFIG_INVALID", "application upstream could not be applied")
	}
	return w.queue.CompleteTopology(
		ctx, record, TopologyResult{Instance: &instance}, "", "",
		w.now().UTC().Truncate(time.Microsecond),
	)
}

func (w *Worker) removeInstance(ctx context.Context, record Record, payload topologyPayload) error {
	if err := w.applyTopologyServers(ctx, record, payload.Servers); err != nil {
		return w.completeTopologyFailure(ctx, record, "NGINX_CONFIG_INVALID", "application upstream could not be applied")
	}
	timer := time.NewTimer(200 * time.Millisecond)
	select {
	case <-ctx.Done():
		timer.Stop()
		_ = w.applyTopologyServers(context.WithoutCancel(ctx), record, payload.PreviousServers)
		return ctx.Err()
	case <-timer.C:
	}
	response, err := w.executeTopologyCommand(ctx, record, "DELETE_APP_INSTANCE", map[string]any{
		"instanceName": payload.InstanceName,
	})
	if err != nil || response.Status != "succeeded" {
		_ = w.applyTopologyServers(context.WithoutCancel(ctx), record, payload.PreviousServers)
		if err != nil {
			return w.completeTopologyFailure(ctx, record, "ORCHESTRATOR_UNAVAILABLE", "application instance could not be removed")
		}
		code, message := commandError(response.Error)
		return w.completeTopologyFailure(ctx, record, code, message)
	}
	return w.queue.CompleteTopology(
		ctx, record, TopologyResult{RemovedInstanceID: payload.InstanceName}, "", "",
		w.now().UTC().Truncate(time.Microsecond),
	)
}

func (w *Worker) setInstancePerformance(ctx context.Context, record Record, payload topologyPayload) error {
	response, err := w.executeTopologyCommand(ctx, record, "UPDATE_APP_CAPACITY", map[string]any{
		"scenarioTemplateId":         payload.ScenarioTemplateID,
		"instanceName":               payload.InstanceName,
		"performancePercent":         payload.PerformancePercent,
		"previousPerformancePercent": payload.PreviousPerformancePercent,
	})
	if err != nil {
		return w.completeTopologyFailure(ctx, record, "ORCHESTRATOR_UNAVAILABLE", "application performance could not be updated")
	}
	if response.Status != "succeeded" {
		code, message := commandError(response.Error)
		return w.completeTopologyFailure(ctx, record, code, message)
	}
	instance, err := decodeProvisionInstance(response.Result)
	if err != nil {
		return w.completeTopologyFailure(ctx, record, "ORCHESTRATOR_RESPONSE_INCOMPLETE", "application performance result is invalid")
	}
	if err := w.applyTopologyServers(ctx, record, payload.Servers); err != nil {
		_, _ = w.executeTopologyCommand(context.WithoutCancel(ctx), record, "UPDATE_APP_CAPACITY", map[string]any{
			"scenarioTemplateId":         payload.ScenarioTemplateID,
			"instanceName":               payload.InstanceName,
			"performancePercent":         payload.PreviousPerformancePercent,
			"previousPerformancePercent": payload.PerformancePercent,
		})
		_ = w.applyTopologyServers(context.WithoutCancel(ctx), record, payload.PreviousServers)
		return w.completeTopologyFailure(ctx, record, "NGINX_CONFIG_INVALID", "application upstream could not be refreshed")
	}
	return w.queue.CompleteTopology(
		ctx, record, TopologyResult{Instance: &instance}, "", "",
		w.now().UTC().Truncate(time.Microsecond),
	)
}

func (w *Worker) setInstanceWeights(ctx context.Context, record Record, payload topologyPayload) error {
	if err := w.applyTopologyServers(ctx, record, payload.Servers); err != nil {
		return w.completeTopologyFailure(ctx, record, "NGINX_CONFIG_INVALID", "application weights could not be applied")
	}
	return w.queue.CompleteTopology(
		ctx, record, TopologyResult{Weights: payload.Request.Weights}, "", "",
		w.now().UTC().Truncate(time.Microsecond),
	)
}

func (w *Worker) setBalancingMode(ctx context.Context, record Record, payload topologyPayload) error {
	if payload.Request.BalancingMode == nil {
		return w.completeTopologyFailure(
			ctx,
			record,
			"INVALID_OPERATION",
			"balancing mode operation payload is invalid",
		)
	}
	return w.queue.CompleteTopology(
		ctx,
		record,
		TopologyResult{BalancingMode: *payload.Request.BalancingMode},
		"",
		"",
		w.now().UTC().Truncate(time.Microsecond),
	)
}

func (w *Worker) applyAdaptiveWeights(
	ctx context.Context,
	record Record,
	payload topologyPayload,
) error {
	if payload.ExpectedMode != "adaptive" || payload.TopologyFingerprint == "" ||
		len(payload.Servers) == 0 || len(payload.Request.Weights) != len(payload.Servers) {
		return w.completeTopologyFailure(
			ctx,
			record,
			"INVALID_OPERATION",
			"adaptive weight operation payload is invalid",
		)
	}
	valid, err := w.queue.ValidateAdaptive(ctx, record, payload.TopologyFingerprint)
	if err != nil {
		return w.completeTopologyFailure(
			ctx,
			record,
			"INTERNAL_ERROR",
			"adaptive topology could not be validated",
		)
	}
	if !valid {
		return w.queue.CompleteTopology(
			ctx,
			record,
			TopologyResult{Skipped: true},
			"",
			"",
			w.now().UTC().Truncate(time.Microsecond),
		)
	}
	if err := w.applyTopologyServers(ctx, record, payload.Servers); err != nil {
		return w.completeTopologyFailure(
			ctx,
			record,
			"NGINX_CONFIG_INVALID",
			"adaptive application weights could not be applied",
		)
	}
	return w.queue.CompleteTopology(
		ctx,
		record,
		TopologyResult{Weights: payload.Request.Weights},
		"",
		"",
		w.now().UTC().Truncate(time.Microsecond),
	)
}

func (w *Worker) applyTopologyServers(
	ctx context.Context,
	record Record,
	servers []topologyServer,
) error {
	response, err := w.executeTopologyCommand(ctx, record, "APPLY_LAB_UPSTREAM", map[string]any{
		"servers": servers,
	})
	if err != nil {
		return err
	}
	if response.Status != "succeeded" {
		code, message := commandError(response.Error)
		return fmt.Errorf("%s: %s", code, message)
	}
	return nil
}

func (w *Worker) executeTopologyCommand(
	ctx context.Context,
	record Record,
	commandType string,
	payload any,
) (protocol.CommandResponse, error) {
	commandID, err := w.newCommandID()
	if err != nil {
		return protocol.CommandResponse{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return protocol.CommandResponse{}, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, w.config.CommandTimeout)
	defer cancel()
	return w.executor.Execute(commandCtx, protocol.Command{
		CommandType: commandType,
		CommandID:   commandID,
		OperationID: record.OperationID,
		LabID:       record.LabID,
		RequestedBy: strconv.FormatUint(record.RequestedBy, 10),
		Payload:     body,
	})
}

func (w *Worker) completeTopologyFailure(
	ctx context.Context,
	record Record,
	code string,
	message string,
) error {
	return w.queue.CompleteTopology(
		ctx, record, TopologyResult{}, code, message,
		w.now().UTC().Truncate(time.Microsecond),
	)
}

func decodeProvisionInstance(value any) (ProvisionInstance, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return ProvisionInstance{}, err
	}
	var instance ProvisionInstance
	if err := json.Unmarshal(body, &instance); err != nil {
		return ProvisionInstance{}, err
	}
	if instance.InstanceName == "" || instance.ContainerID == "" ||
		instance.ContainerName == "" || instance.CPULimitCores <= 0 ||
		instance.MemoryLimitMB <= 0 || instance.PerformancePercent <= 0 ||
		instance.ProcessingSpeed <= 0 || instance.MaxLoad <= 0 ||
		instance.CurrentWeight <= 0 {
		return ProvisionInstance{}, errors.New("application instance result is incomplete")
	}
	return instance, nil
}

func (w *Worker) completeFailure(
	ctx context.Context,
	record Record,
	code string,
	message string,
) error {
	now := w.now().UTC().Truncate(time.Microsecond)
	switch record.Action {
	case ActionCreateLab:
		return w.queue.CompleteProvision(ctx, record, ProvisionResult{}, code, message, now)
	case ActionResetLab:
		return w.queue.CompleteReset(ctx, record, ProvisionResult{}, code, message, now)
	case ActionDestroyLab:
		return w.queue.CompleteDestroy(ctx, record, nil, code, message, now)
	default:
		return w.queue.CompleteFailure(ctx, record, code, message, now)
	}
}

func (w *Worker) completeSuccess(
	ctx context.Context,
	record Record,
	result any,
) error {
	now := w.now().UTC().Truncate(time.Microsecond)
	switch record.Action {
	case ActionCreateLab, ActionResetLab:
		provision, err := decodeProvisionResult(result)
		if err == nil {
			err = validateProvisionIdentity(record, provision)
		}
		if err != nil {
			return w.completeFailure(
				ctx,
				record,
				"ORCHESTRATOR_RESPONSE_INCOMPLETE",
				"orchestrator provision result is invalid",
			)
		}
		if record.Action == ActionCreateLab {
			return w.queue.CompleteProvision(ctx, record, provision, "", "", now)
		}
		return w.queue.CompleteReset(ctx, record, provision, "", "", now)
	case ActionDestroyLab:
		return w.queue.CompleteDestroy(ctx, record, result, "", "", now)
	default:
		return w.queue.CompleteFailure(
			ctx,
			record,
			"ACTION_NOT_SUPPORTED",
			"operation action is not supported by this worker",
			now,
		)
	}
}

func commandTypeForAction(action string) (string, bool) {
	switch action {
	case ActionCreateLab:
		return "PROVISION_LAB", true
	case ActionResetLab:
		return "RESET_LAB", true
	case ActionDestroyLab:
		return "DESTROY_LAB", true
	default:
		return "", false
	}
}

func decodeProvisionResult(value any) (ProvisionResult, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ProvisionResult{}, err
	}
	var result ProvisionResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		return ProvisionResult{}, err
	}
	if result.LabID == "" || result.DatabaseName == "" ||
		result.DatabaseUser == "" || result.NetworkID == "" ||
		result.NetworkName == "" || len(result.Instances) == 0 {
		return ProvisionResult{}, errors.New("provision result is incomplete")
	}
	for _, instance := range result.Instances {
		if instance.InstanceName == "" || instance.ContainerID == "" ||
			instance.ContainerName == "" || instance.CPULimitCores <= 0 ||
			instance.MemoryLimitMB <= 0 || instance.PerformancePercent <= 0 ||
			instance.ProcessingSpeed <= 0 || instance.MaxLoad <= 0 ||
			instance.CurrentWeight <= 0 {
			return ProvisionResult{}, errors.New("provision instance result is incomplete")
		}
	}
	return result, nil
}

func validateProvisionIdentity(record Record, result ProvisionResult) error {
	if result.LabID != record.LabID {
		return errors.New("provision result lab id does not match")
	}
	var payload struct {
		ScenarioTemplateID string `json:"scenarioTemplateId"`
	}
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return err
	}
	if payload.ScenarioTemplateID == "" ||
		result.ScenarioTemplateID != payload.ScenarioTemplateID {
		return errors.New("provision result scenario template does not match")
	}
	return nil
}

func newCommandID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate command id: %w", err)
	}
	return "cmd-" + hex.EncodeToString(data), nil
}

func commandError(value any) (string, string) {
	code := "ORCHESTRATOR_COMMAND_FAILED"
	message := "orchestrator command failed"
	fields, ok := value.(map[string]any)
	if !ok {
		return code, message
	}
	if candidate, ok := fields["code"].(string); ok && candidate != "" {
		code = candidate
	}
	if candidate, ok := fields["message"].(string); ok && candidate != "" {
		message = candidate
	}
	return code, message
}
