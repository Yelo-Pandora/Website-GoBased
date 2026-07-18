package operation

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"website-gobased/internal/protocol"
)

type queueStub struct {
	record          Record
	claimErr        error
	markedRunning   bool
	completedCode   string
	completedResult any
	completedAction string
	adaptiveValid   bool
}

func (s *queueStub) Claim(
	_ context.Context,
	owner string,
	_ time.Time,
	leaseExpires time.Time,
) (Record, error) {
	if s.claimErr != nil {
		return Record{}, s.claimErr
	}
	s.record.LeaseOwner = owner
	s.record.LeaseExpires = leaseExpires
	return s.record, nil
}

func (s *queueStub) MarkRunning(
	_ context.Context,
	_ Record,
	_ time.Time,
	_ time.Time,
) error {
	s.markedRunning = true
	return nil
}

func (s *queueStub) CompleteProvision(
	_ context.Context,
	_ Record,
	resultValue ProvisionResult,
	errorCode string,
	_ string,
	_ time.Time,
) error {
	s.completedCode = errorCode
	s.completedResult = resultValue
	s.completedAction = ActionCreateLab
	return nil
}

func (s *queueStub) CompleteReset(
	_ context.Context,
	_ Record,
	resultValue ProvisionResult,
	errorCode string,
	_ string,
	_ time.Time,
) error {
	s.completedCode = errorCode
	s.completedResult = resultValue
	s.completedAction = ActionResetLab
	return nil
}

func (s *queueStub) CompleteDestroy(
	_ context.Context,
	_ Record,
	resultValue any,
	errorCode string,
	_ string,
	_ time.Time,
) error {
	s.completedCode = errorCode
	s.completedResult = resultValue
	s.completedAction = ActionDestroyLab
	return nil
}

func (s *queueStub) CompleteTopology(
	_ context.Context,
	record Record,
	resultValue TopologyResult,
	errorCode string,
	_ string,
	_ time.Time,
) error {
	s.completedCode = errorCode
	s.completedResult = resultValue
	s.completedAction = record.Action
	return nil
}

func (s *queueStub) CompleteCache(
	_ context.Context,
	record Record,
	resultValue CacheResult,
	errorCode string,
	_ string,
	_ time.Time,
) error {
	s.completedCode = errorCode
	s.completedResult = resultValue
	s.completedAction = record.Action
	return nil
}

func (s *queueStub) ValidateAdaptive(
	_ context.Context,
	_ Record,
	_ string,
) (bool, error) {
	return s.adaptiveValid, nil
}

func (s *queueStub) CompleteFailure(
	_ context.Context,
	_ Record,
	errorCode string,
	_ string,
	_ time.Time,
) error {
	s.completedCode = errorCode
	return nil
}

type executorStub struct {
	command  protocol.Command
	response protocol.CommandResponse
	err      error
}

type cacheRuntimeStub struct {
	labID      string
	instanceID string
	enabled    bool
}

func (s *cacheRuntimeStub) SetL1(
	_ context.Context,
	labID string,
	instanceID string,
	enabled bool,
) (protocol.CacheInstanceState, error) {
	s.labID = labID
	s.instanceID = instanceID
	s.enabled = enabled
	return protocol.CacheInstanceState{InstanceID: instanceID, Status: "live"}, nil
}

type sequenceExecutor struct {
	commands  []protocol.Command
	responses []protocol.CommandResponse
}

func (s *sequenceExecutor) Execute(
	_ context.Context,
	command protocol.Command,
) (protocol.CommandResponse, error) {
	s.commands = append(s.commands, command)
	response := s.responses[0]
	s.responses = s.responses[1:]
	return response, nil
}

func (s *executorStub) Execute(
	_ context.Context,
	command protocol.Command,
) (protocol.CommandResponse, error) {
	s.command = command
	return s.response, s.err
}

func TestWorkerProcessesCreateLab(t *testing.T) {
	t.Parallel()

	queue := &queueStub{record: Record{
		ID:          3,
		OperationID: "operation-1",
		LabID:       "lab-test",
		RequestedBy: 7,
		Action:      ActionCreateLab,
		Payload: json.RawMessage(`{
			"courseId":3,
			"scenarioTemplateId":"scenario-1"
		}`),
	}}
	executor := &executorStub{response: protocol.CommandResponse{
		CommandID:   "command-1",
		OperationID: "operation-1",
		Status:      "succeeded",
		Result: map[string]any{
			"labId": "lab-test", "scenarioTemplateId": "scenario-1",
			"databaseName": "lab_test", "databaseUser": "lab_test_user",
			"networkId": "network-1", "networkName": "lab-test-net",
			"instances": []map[string]any{{
				"instanceName": "app-1", "containerId": "container-1",
				"containerName": "lab-test-app-1", "status": "running",
				"cpuLimitCores": 0.1, "memoryLimitMb": 128,
				"performancePercent": 100, "processingSpeed": 20, "maxLoad": 100,
				"currentWeight": 100,
			}},
		},
	}}
	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		queue,
		executor,
		WorkerConfig{
			Owner:          "worker-1",
			PollInterval:   time.Second,
			LeaseDuration:  time.Minute,
			CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	worker.newCommandID = func() (string, error) { return "command-1", nil }
	processed, err := worker.processOne(context.Background())
	if err != nil {
		t.Fatalf("processOne() error = %v", err)
	}
	if !processed || !queue.markedRunning || queue.completedCode != "" ||
		queue.completedAction != ActionCreateLab {
		t.Fatalf("worker state: processed=%t queue=%#v", processed, queue)
	}
	if executor.command.CommandType != "PROVISION_LAB" ||
		executor.command.RequestedBy != "7" {
		t.Fatalf("command = %#v", executor.command)
	}
}

func TestWorkerAddsInstanceAndAppliesUpstream(t *testing.T) {
	queue := &queueStub{record: Record{
		ID: 4, OperationID: "operation-add", LabID: "lab-test", RequestedBy: 7,
		Action: ActionAddInstance,
		Payload: json.RawMessage(`{
			"scenarioTemplateId":"application_cluster_scenario_v1",
			"instanceName":"app-2",
			"performancePercent":100,
			"servers":[
				{"instanceName":"app-1","weight":100},
				{"instanceName":"app-2","weight":100}
			]
		}`),
	}}
	executor := &sequenceExecutor{responses: []protocol.CommandResponse{
		{Status: "succeeded", Result: map[string]any{
			"instanceName": "app-2", "containerId": "container-2",
			"containerName": "lab-test-app-2", "status": "running",
			"cpuLimitCores": 0.1, "memoryLimitMb": 128,
			"performancePercent": 100, "processingSpeed": 20, "maxLoad": 100,
			"currentWeight": 100,
		}},
		{Status: "succeeded", Result: map[string]any{"applied": true}},
	}}
	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		queue,
		executor,
		WorkerConfig{
			Owner: "worker-1", PollInterval: time.Second,
			LeaseDuration: time.Minute, CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	worker.newCommandID = func() (string, error) { return "command-test", nil }
	processed, err := worker.processOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("processOne() = %t, %v", processed, err)
	}
	if len(executor.commands) != 2 ||
		executor.commands[0].CommandType != "CREATE_APP_INSTANCE" ||
		executor.commands[1].CommandType != "APPLY_LAB_UPSTREAM" {
		t.Fatalf("commands = %#v", executor.commands)
	}
	result, ok := queue.completedResult.(TopologyResult)
	if !ok || result.Instance == nil || result.Instance.InstanceName != "app-2" {
		t.Fatalf("completed result = %#v", queue.completedResult)
	}
}

func TestWorkerPersistsBalancingModeWithoutOrchestratorCommand(t *testing.T) {
	queue := &queueStub{record: Record{
		ID: 5, OperationID: "operation-mode", LabID: "lab-test", RequestedBy: 7,
		Action: ActionSetBalancingMode,
		Payload: json.RawMessage(`{
			"request":{"balancingMode":"adaptive"}
		}`),
	}}
	executor := &executorStub{}
	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		queue,
		executor,
		WorkerConfig{
			Owner: "worker-1", PollInterval: time.Second,
			LeaseDuration: time.Minute, CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	processed, err := worker.processOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("processOne() = %t, %v", processed, err)
	}
	result, ok := queue.completedResult.(TopologyResult)
	if !ok || result.BalancingMode != "adaptive" {
		t.Fatalf("completed result = %#v", queue.completedResult)
	}
	if executor.command.CommandType != "" {
		t.Fatalf("unexpected orchestrator command = %#v", executor.command)
	}
}

func TestWorkerAppliesValidatedAdaptiveWeights(t *testing.T) {
	queue := &queueStub{
		adaptiveValid: true,
		record: Record{
			ID: 6, OperationID: "balancer-test", LabID: "lab-test", RequestedBy: 7,
			Action: ActionApplyAdaptiveWeights,
			Payload: json.RawMessage(`{
				"expectedMode":"adaptive",
				"topologyFingerprint":"fingerprint-test",
				"servers":[
					{"instanceName":"app-1","weight":10},
					{"instanceName":"app-2","weight":3}
				],
				"request":{"weights":[
					{"instanceId":"app-1","weight":10},
					{"instanceId":"app-2","weight":3}
				]}
			}`),
		},
	}
	executor := &executorStub{response: protocol.CommandResponse{
		Status: "succeeded", Result: map[string]any{"applied": true},
	}}
	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		queue,
		executor,
		WorkerConfig{
			Owner: "worker-1", PollInterval: time.Second,
			LeaseDuration: time.Minute, CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	worker.newCommandID = func() (string, error) { return "command-adaptive", nil }
	if _, err := worker.processOne(context.Background()); err != nil {
		t.Fatalf("processOne() error = %v", err)
	}
	if executor.command.CommandType != "APPLY_LAB_UPSTREAM" {
		t.Fatalf("command = %#v", executor.command)
	}
	result, ok := queue.completedResult.(TopologyResult)
	if !ok || len(result.Weights) != 2 || result.Weights[1].Weight != 3 {
		t.Fatalf("completed result = %#v", queue.completedResult)
	}
}

func TestWorkerSkipsStaleAdaptiveWeights(t *testing.T) {
	queue := &queueStub{record: Record{
		ID: 7, OperationID: "balancer-stale", LabID: "lab-test", RequestedBy: 7,
		Action: ActionApplyAdaptiveWeights,
		Payload: json.RawMessage(`{
			"expectedMode":"adaptive",
			"topologyFingerprint":"fingerprint-stale",
			"servers":[{"instanceName":"app-1","weight":1}],
			"request":{"weights":[{"instanceId":"app-1","weight":1}]}
		}`),
	}}
	executor := &executorStub{}
	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		queue,
		executor,
		WorkerConfig{
			Owner: "worker-1", PollInterval: time.Second,
			LeaseDuration: time.Minute, CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	if _, err := worker.processOne(context.Background()); err != nil {
		t.Fatalf("processOne() error = %v", err)
	}
	result, ok := queue.completedResult.(TopologyResult)
	if !ok || !result.Skipped {
		t.Fatalf("completed result = %#v", queue.completedResult)
	}
	if executor.command.CommandType != "" {
		t.Fatalf("unexpected orchestrator command = %#v", executor.command)
	}
}

func TestWorkerMapsResetAndDestroyCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		action      string
		commandType string
		result      any
	}{
		{
			name: "reset", action: ActionResetLab, commandType: "RESET_LAB",
			result: map[string]any{
				"labId": "lab-test", "scenarioTemplateId": "scenario-1",
				"databaseName": "lab_test", "databaseUser": "lab_test_user",
				"networkId": "network-1", "networkName": "lab-test-net",
				"instances": []map[string]any{{
					"instanceName": "app-1", "containerId": "container-1",
					"containerName": "lab-test-app-1", "cpuLimitCores": 0.1,
					"memoryLimitMb":      128,
					"performancePercent": 100, "processingSpeed": 20, "maxLoad": 100,
					"currentWeight": 100,
				}},
			},
		},
		{
			name: "destroy", action: ActionDestroyLab, commandType: "DESTROY_LAB",
			result: map[string]any{"destroyed": true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queue := &queueStub{record: Record{
				ID: 3, OperationID: "operation-1", LabID: "lab-test",
				RequestedBy: 7, Action: test.action,
				Payload: json.RawMessage(`{"scenarioTemplateId":"scenario-1"}`),
			}}
			executor := &executorStub{response: protocol.CommandResponse{
				Status: "succeeded", Result: test.result,
			}}
			worker, err := newWorker(
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				queue,
				executor,
				WorkerConfig{
					Owner: "worker-1", PollInterval: time.Second,
					LeaseDuration: time.Minute, CommandTimeout: 10 * time.Second,
				},
			)
			if err != nil {
				t.Fatalf("newWorker() error = %v", err)
			}
			worker.newCommandID = func() (string, error) { return "command-1", nil }
			if _, err := worker.processOne(context.Background()); err != nil {
				t.Fatalf("processOne() error = %v", err)
			}
			if executor.command.CommandType != test.commandType ||
				queue.completedAction != test.action || queue.completedCode != "" {
				t.Fatalf("command=%#v queue=%#v", executor.command, queue)
			}
		})
	}
}

func TestWorkerPersistsRejectedCommand(t *testing.T) {
	t.Parallel()

	queue := &queueStub{record: Record{
		OperationID: "operation-1",
		LabID:       "lab-test",
		RequestedBy: 7,
		Action:      ActionCreateLab,
	}}
	executor := &executorStub{response: protocol.CommandResponse{
		Status: "rejected",
		Error: map[string]any{
			"code":    "COMMAND_NOT_IMPLEMENTED",
			"message": "not implemented",
		},
	}}
	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		queue,
		executor,
		WorkerConfig{
			Owner:          "worker-1",
			PollInterval:   time.Second,
			LeaseDuration:  time.Minute,
			CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	worker.newCommandID = func() (string, error) { return "command-1", nil }
	if _, err := worker.processOne(context.Background()); err != nil {
		t.Fatalf("processOne() error = %v", err)
	}
	if queue.completedCode != "COMMAND_NOT_IMPLEMENTED" {
		t.Fatalf("completed code = %q", queue.completedCode)
	}
}

func TestWorkerTargetsInstanceL1WithoutOrchestratorCommand(t *testing.T) {
	queue := &queueStub{record: Record{
		ID: 8, OperationID: "operation-l1", LabID: "lab-test", RequestedBy: 7,
		Action: ActionRemoveInstanceL1,
		Payload: json.RawMessage(`{
			"scenarioTemplateId":"multi_level_cache_scenario_v1",
			"request":{"targetInstanceId":"app-2"}
		}`),
	}}
	executor := &executorStub{}
	runtime := &cacheRuntimeStub{}
	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)), queue, executor,
		WorkerConfig{
			Owner: "worker-1", PollInterval: time.Second,
			LeaseDuration: time.Minute, CommandTimeout: 10 * time.Second,
		},
		runtime,
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	if _, err := worker.processOne(context.Background()); err != nil {
		t.Fatalf("processOne() error = %v", err)
	}
	if runtime.labID != "lab-test" || runtime.instanceID != "app-2" || runtime.enabled ||
		executor.command.CommandType != "" || queue.completedAction != ActionRemoveInstanceL1 {
		t.Fatalf("runtime=%#v command=%#v queue=%#v", runtime, executor.command, queue)
	}
}

func TestWorkerMapsRedisLayerActions(t *testing.T) {
	tests := []struct {
		name        string
		action      string
		commandType string
		result      map[string]any
	}{
		{name: "remove", action: ActionRemoveSessionRedis, commandType: "DELETE_SESSION_REDIS", result: map[string]any{"deleted": true}},
		{name: "add", action: ActionAddSessionRedis, commandType: "CREATE_SESSION_REDIS", result: map[string]any{
			"containerId": "redis-1", "containerName": "lab-test-redis",
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queue := &queueStub{record: Record{
				ID: 9, OperationID: "operation-redis", LabID: "lab-test", RequestedBy: 7,
				Action: test.action,
				Payload: json.RawMessage(`{
					"scenarioTemplateId":"multi_level_cache_scenario_v1",
					"request":{}
				}`),
			}}
			executor := &executorStub{response: protocol.CommandResponse{
				Status: "succeeded", Result: test.result,
			}}
			worker, err := newWorker(
				slog.New(slog.NewTextHandler(io.Discard, nil)), queue, executor,
				WorkerConfig{
					Owner: "worker-1", PollInterval: time.Second,
					LeaseDuration: time.Minute, CommandTimeout: 10 * time.Second,
				},
			)
			if err != nil {
				t.Fatalf("newWorker() error = %v", err)
			}
			worker.newCommandID = func() (string, error) { return "command-redis", nil }
			if _, err := worker.processOne(context.Background()); err != nil {
				t.Fatalf("processOne() error = %v", err)
			}
			if executor.command.CommandType != test.commandType || queue.completedAction != test.action {
				t.Fatalf("command=%#v queue=%#v", executor.command, queue)
			}
			if test.action == ActionAddSessionRedis {
				result, ok := queue.completedResult.(CacheResult)
				if !ok || result.Redis == nil || result.Redis.Status != "ready" {
					t.Fatalf("cache result = %#v", queue.completedResult)
				}
			}
		})
	}
}

func TestWorkerNoPendingOperation(t *testing.T) {
	t.Parallel()

	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		&queueStub{claimErr: ErrNoPending},
		&executorStub{},
		WorkerConfig{
			Owner:          "worker-1",
			PollInterval:   time.Second,
			LeaseDuration:  time.Minute,
			CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	processed, err := worker.processOne(context.Background())
	if err != nil || processed {
		t.Fatalf("processOne() = %t, %v", processed, err)
	}
}

func TestWorkerRejectsUnsupportedActionWithoutProvisionTransition(t *testing.T) {
	t.Parallel()

	queue := &queueStub{record: Record{
		OperationID: "operation-1",
		LabID:       "lab-test",
		RequestedBy: 7,
		Action:      "UNKNOWN_ACTION",
	}}
	worker, err := newWorker(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		queue,
		&executorStub{},
		WorkerConfig{
			Owner:          "worker-1",
			PollInterval:   time.Second,
			LeaseDuration:  time.Minute,
			CommandTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("newWorker() error = %v", err)
	}
	if _, err := worker.processOne(context.Background()); err != nil {
		t.Fatalf("processOne() error = %v", err)
	}
	if queue.completedCode != "ACTION_NOT_SUPPORTED" ||
		queue.completedResult != nil {
		t.Fatalf("unsupported action completion = %#v", queue)
	}
}
