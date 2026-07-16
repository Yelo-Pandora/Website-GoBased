package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"
)

const (
	applicationClusterScenario = "application_cluster"
	defaultInstanceWeight      = 100
	minimumInstanceWeight      = 1
	maximumInstanceWeight      = 100
)

type topologyServer struct {
	InstanceName string `json:"instanceName"`
	Weight       int    `json:"weight"`
}

type topologyActionPayload struct {
	Request                    ActionInput      `json:"request"`
	ScenarioTemplateID         string           `json:"scenarioTemplateId"`
	InstanceName               string           `json:"instanceName,omitempty"`
	PerformancePercent         int              `json:"performancePercent,omitempty"`
	PreviousPerformancePercent int              `json:"previousPerformancePercent,omitempty"`
	Servers                    []topologyServer `json:"servers,omitempty"`
	PreviousServers            []topologyServer `json:"previousServers,omitempty"`
}

// EnqueueTopologyAction validates and serializes one cluster topology change.
func (r *Repository) EnqueueTopologyAction(
	ctx context.Context,
	userID uint64,
	labID string,
	input ActionInput,
	quota Quota,
	now time.Time,
) (ActionResult, error) {
	connection, err := r.database.Conn(ctx)
	if err != nil {
		return ActionResult{}, fmt.Errorf("reserve topology action connection: %w", err)
	}
	defer connection.Close()
	tx, err := connection.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ActionResult{}, fmt.Errorf("begin topology action transaction: %w", err)
	}
	globalAdmissionLocked := false
	defer func() {
		_ = tx.Rollback()
		if globalAdmissionLocked {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), globalAdmissionReleaseTimeout)
			_ = releaseGlobalAdmission(releaseCtx, connection)
			cancel()
		}
	}()

	if existing, storedInput, findErr := findTopologyAction(ctx, tx, input.OperationID); findErr == nil {
		return matchingTopologyAction(existing, storedInput, userID, labID, input)
	} else if !errors.Is(findErr, sql.ErrNoRows) {
		return ActionResult{}, fmt.Errorf("find existing topology action: %w", findErr)
	}
	session, err := scanSession(tx.QueryRowContext(ctx, `
		SELECT id, user_id, course_id, scenario_type, scenario_template_id,
			status, balancing_mode, redis_enabled, started_at,
			last_effective_action_at, terminated_at, termination_reason,
			created_at, updated_at
		FROM lab_sessions
		WHERE id = ?
		FOR UPDATE`, labID))
	if errors.Is(err, sql.ErrNoRows) {
		return ActionResult{}, ErrNotFound
	}
	if err != nil {
		return ActionResult{}, fmt.Errorf("lock topology lab: %w", err)
	}
	if session.UserID != userID {
		return ActionResult{}, ErrNotOwned
	}
	if session.Status != StatusRunning && session.Status != StatusExpiring {
		return ActionResult{}, ErrNotRunning
	}
	if session.ScenarioType != applicationClusterScenario {
		return ActionResult{}, ErrActionNotAllowed
	}
	if input.ActionType == ActionAddInstance {
		if err := lockGlobalAdmission(ctx, tx); err != nil {
			return ActionResult{}, err
		}
		globalAdmissionLocked = true
	}
	if err := ensureNoPendingTopologyAction(ctx, tx, labID); err != nil {
		return ActionResult{}, err
	}
	instances, err := lockTopologyInstances(ctx, tx, labID)
	if err != nil {
		return ActionResult{}, err
	}
	payload, target, err := buildTopologyPayload(ctx, tx, session, instances, input, quota)
	if err != nil {
		return ActionResult{}, err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return ActionResult{}, fmt.Errorf("encode topology action: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO lab_operations (
			operation_id, lab_id, requested_by, action, target_instance_id,
			status, payload_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?)`,
		input.OperationID,
		labID,
		userID,
		input.ActionType,
		target,
		payloadJSON,
		now,
		now,
	)
	if err != nil {
		if duplicateKey(err) {
			return ActionResult{}, ErrOperationConflict
		}
		return ActionResult{}, fmt.Errorf("insert topology action: %w", err)
	}
	databaseID, err := result.LastInsertId()
	if err != nil {
		return ActionResult{}, fmt.Errorf("read topology action id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ActionResult{}, fmt.Errorf("commit topology action: %w", err)
	}
	if globalAdmissionLocked {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), globalAdmissionReleaseTimeout)
		err = releaseGlobalAdmission(releaseCtx, connection)
		cancel()
		if err != nil {
			return ActionResult{}, err
		}
		globalAdmissionLocked = false
	}
	operation := CreatedOperation{
		ID: uint64(databaseID), OperationID: input.OperationID,
		LabID: labID, RequestedBy: userID, Action: input.ActionType,
		Status: "pending", SubmittedAt: now,
	}
	if target != nil {
		value := *target
		operation.TargetInstanceID = &value
	}
	return ActionResult{Operation: operation}, nil
}

func buildTopologyPayload(
	ctx context.Context,
	tx *sql.Tx,
	session Session,
	instances []Instance,
	input ActionInput,
	quota Quota,
) (topologyActionPayload, *string, error) {
	payload := topologyActionPayload{
		Request: input, ScenarioTemplateID: session.ScenarioTemplateID,
		PreviousServers: topologyServers(instances),
	}
	switch input.ActionType {
	case ActionAddInstance:
		if len(instances) >= quota.MaxInstancesPerLab {
			return payload, nil, ErrMaxInstanceLimit
		}
		if err := admitGlobalTemporaryContainer(ctx, tx, quota); err != nil {
			return payload, nil, err
		}
		payload.InstanceName = nextInstanceName(instances, quota.MaxInstancesPerLab)
		payload.PerformancePercent = 100
		payload.Servers = append(topologyServers(instances), topologyServer{
			InstanceName: payload.InstanceName,
			Weight:       defaultInstanceWeight,
		})
		return payload, nil, nil
	case ActionRemoveInstance:
		if len(instances) <= 1 {
			return payload, input.TargetInstanceID, ErrMinInstanceLimit
		}
		if !containsInstance(instances, *input.TargetInstanceID) {
			return payload, input.TargetInstanceID, ErrInstanceNotFound
		}
		payload.InstanceName = *input.TargetInstanceID
		payload.Servers = topologyServersWithout(instances, payload.InstanceName)
		return payload, input.TargetInstanceID, nil
	case ActionSetInstancePerformance:
		instance, ok := findTopologyInstance(instances, *input.TargetInstanceID)
		if !ok {
			return payload, input.TargetInstanceID, ErrInstanceNotFound
		}
		payload.InstanceName = *input.TargetInstanceID
		payload.PerformancePercent = *input.PerformancePercent
		payload.PreviousPerformancePercent = instance.PerformancePercent
		payload.Servers = topologyServers(instances)
		return payload, input.TargetInstanceID, nil
	case ActionSetInstanceWeights:
		if session.BalancingMode != "fixed" {
			return payload, nil, ErrActionNotAllowed
		}
		servers, err := validateWeights(instances, input.Weights)
		if err != nil {
			return payload, nil, err
		}
		payload.Servers = servers
		return payload, nil, nil
	default:
		return payload, nil, ErrActionNotAllowed
	}
}

func findTopologyInstance(instances []Instance, instanceID string) (Instance, bool) {
	for _, instance := range instances {
		if instance.ID == instanceID || instance.Name == instanceID {
			return instance, true
		}
	}
	return Instance{}, false
}

func ensureNoPendingTopologyAction(ctx context.Context, tx *sql.Tx, labID string) error {
	var id uint64
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM lab_operations
		WHERE lab_id = ? AND status IN ('pending', 'claimed', 'running', 'compensating')
		LIMIT 1`, labID).Scan(&id)
	if err == nil {
		return ErrBusy
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check topology action queue: %w", err)
	}
	return nil
}

func lockTopologyInstances(ctx context.Context, tx *sql.Tx, labID string) ([]Instance, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT instance_name, container_id, status, cpu_limit_cores,
			memory_limit_mb, performance_percent, effective_capacity, current_weight
		FROM lab_instances
		WHERE lab_id = ? AND status = 'running'
		ORDER BY instance_name
		FOR UPDATE`, labID)
	if err != nil {
		return nil, fmt.Errorf("lock topology instances: %w", err)
	}
	defer rows.Close()
	var instances []Instance
	for rows.Next() {
		var instance Instance
		if err := rows.Scan(
			&instance.Name, &instance.ContainerID, &instance.Status,
			&instance.CPULimitCores, &instance.MemoryLimitMB,
			&instance.PerformancePercent, &instance.EffectiveCapacity,
			&instance.CurrentWeight,
		); err != nil {
			return nil, fmt.Errorf("scan topology instance: %w", err)
		}
		instance.ID = instance.Name
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topology instances: %w", err)
	}
	return instances, nil
}

func admitGlobalTemporaryContainer(ctx context.Context, tx *sql.Tx, quota Quota) error {
	var instances int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM lab_instances
		WHERE status NOT IN ('terminated', 'deleted')`).Scan(&instances); err != nil {
		return fmt.Errorf("count temporary instances: %w", err)
	}
	var redis int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM lab_resources
		WHERE resource_type = 'session-redis' AND status NOT IN ('deleted', 'terminated')`).Scan(&redis); err != nil {
		return fmt.Errorf("count temporary redis: %w", err)
	}
	if instances+redis+1 > quota.MaxTemporaryContainers {
		return ErrCapacityExceeded
	}
	return nil
}

func topologyServers(instances []Instance) []topologyServer {
	servers := make([]topologyServer, 0, len(instances))
	for _, instance := range instances {
		servers = append(servers, topologyServer{
			InstanceName: instance.Name,
			Weight:       instance.CurrentWeight,
		})
	}
	return servers
}

func topologyServersWithout(instances []Instance, excluded string) []topologyServer {
	servers := make([]topologyServer, 0, len(instances)-1)
	for _, instance := range instances {
		if instance.Name != excluded {
			servers = append(servers, topologyServer{
				InstanceName: instance.Name,
				Weight:       instance.CurrentWeight,
			})
		}
	}
	return servers
}

func nextInstanceName(instances []Instance, maximum int) string {
	used := make(map[string]bool, len(instances))
	for _, instance := range instances {
		used[instance.Name] = true
	}
	for index := 1; index <= maximum; index++ {
		name := fmt.Sprintf("app-%d", index)
		if !used[name] {
			return name
		}
	}
	return ""
}

func containsInstance(instances []Instance, instanceID string) bool {
	for _, instance := range instances {
		if instance.ID == instanceID || instance.Name == instanceID {
			return true
		}
	}
	return false
}

func validateWeights(instances []Instance, weights []InstanceWeight) ([]topologyServer, error) {
	if len(weights) != len(instances) {
		return nil, ErrWeightInvalid
	}
	values := make(map[string]int, len(weights))
	for _, value := range weights {
		if value.Weight < minimumInstanceWeight || value.Weight > maximumInstanceWeight ||
			values[value.InstanceID] != 0 {
			return nil, ErrWeightInvalid
		}
		values[value.InstanceID] = value.Weight
	}
	servers := make([]topologyServer, 0, len(instances))
	for _, instance := range instances {
		weight := values[instance.ID]
		if weight == 0 {
			return nil, ErrWeightInvalid
		}
		servers = append(servers, topologyServer{InstanceName: instance.Name, Weight: weight})
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].InstanceName < servers[j].InstanceName })
	return servers, nil
}

func findTopologyAction(
	ctx context.Context,
	tx *sql.Tx,
	operationID string,
) (CreatedOperation, ActionInput, error) {
	operation, err := findActionByOperationID(ctx, tx, operationID)
	if err != nil {
		return CreatedOperation{}, ActionInput{}, err
	}
	var body []byte
	if err := tx.QueryRowContext(
		ctx,
		"SELECT payload_json FROM lab_operations WHERE id = ?",
		operation.ID,
	).Scan(&body); err != nil {
		return CreatedOperation{}, ActionInput{}, err
	}
	var payload topologyActionPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return CreatedOperation{}, ActionInput{}, err
	}
	return operation, payload.Request, nil
}

func matchingTopologyAction(
	operation CreatedOperation,
	stored ActionInput,
	userID uint64,
	labID string,
	requested ActionInput,
) (ActionResult, error) {
	if operation.LabID != labID || operation.RequestedBy != userID ||
		operation.Action != requested.ActionType || !reflect.DeepEqual(stored, requested) {
		return ActionResult{}, ErrOperationConflict
	}
	return ActionResult{Operation: operation, Existing: true}, nil
}
