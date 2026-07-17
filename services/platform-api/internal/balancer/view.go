package balancer

import (
	"sort"
	"time"

	"website-gobased/internal/protocol"
	labstate "website-gobased/services/platform-api/internal/lab"
)

// Observe records one already validated synchronous traffic result.
func (c *Controller) Observe(labID string, result protocol.TrafficBatchResult) {
	if labID == "" || result.TargetInstanceID == "" || result.InstanceState.LoadRatio < 0 {
		return
	}
	now := c.now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	instances := c.metrics[labID]
	if instances == nil {
		instances = make(map[string]metricState)
		c.metrics[labID] = instances
	}
	metric, exists := instances[result.TargetInstanceID]
	if exists {
		metric.smoothed = c.config.EWMAAlpha*result.InstanceState.LoadRatio +
			(1-c.config.EWMAAlpha)*metric.smoothed
	} else {
		metric.smoothed = result.InstanceState.LoadRatio
	}
	metric.updatedAt = now
	if result.DroppedUnits > 0 {
		metric.lastDropped = now
	}
	instances[result.TargetInstanceID] = metric
}

// View returns the current safe public status for one adaptive lab.
func (c *Controller) View(labID string, instances []Instance) View {
	now := c.now().UTC()
	c.mu.RLock()
	state, ok := c.states[labID]
	metrics := cloneMetrics(c.metrics[labID])
	c.mu.RUnlock()
	if !ok {
		state.status = StatusConverging
		state.targetWeights, _ = TargetWeights(instances)
	}
	view := View{
		Status:        state.status,
		TargetWeights: append([]Weight(nil), state.targetWeights...),
		SmoothedLoads: make([]SmoothedLoad, 0, len(metrics)),
	}
	if view.Status == "" {
		view.Status = StatusConverging
	}
	allFresh := len(instances) > 0
	allUnderused := allFresh
	overloaded := false
	for _, instance := range instances {
		metric, exists := metrics[instance.ID]
		fresh := exists && freshAt(now, metric.updatedAt, c.config.MetricFreshness)
		if !fresh {
			allFresh = false
			allUnderused = false
			continue
		}
		view.SmoothedLoads = append(view.SmoothedLoads, SmoothedLoad{
			InstanceID: instance.ID,
			LoadRatio:  metric.smoothed,
		})
		if metric.smoothed >= 1 ||
			freshAt(now, metric.lastDropped, c.config.MetricFreshness) {
			overloaded = true
		}
		if metric.smoothed >= 0.2 {
			allUnderused = false
		}
	}
	sort.Slice(view.SmoothedLoads, func(i, j int) bool {
		return view.SmoothedLoads[i].InstanceID < view.SmoothedLoads[j].InstanceID
	})
	if overloaded {
		view.CapacityNotice = "cluster_overloaded"
	} else if allFresh && allUnderused {
		view.CapacityNotice = "cluster_underused"
	}
	if state.lastError != nil {
		value := *state.lastError
		view.LastError = &value
	}
	return view
}

// DecorateSnapshot attaches adaptive runtime state to one persisted lab snapshot.
func (c *Controller) DecorateSnapshot(snapshot *labstate.Snapshot) {
	if snapshot == nil || snapshot.Lab.BalancingMode != "adaptive" {
		return
	}
	instances := make([]Instance, 0, len(snapshot.Topology.Instances))
	for _, instance := range snapshot.Topology.Instances {
		instances = append(instances, Instance{
			ID:                instance.ID,
			Status:            instance.Status,
			EffectiveCapacity: instance.EffectiveCapacity,
			CurrentWeight:     instance.CurrentWeight,
		})
	}
	view := c.View(snapshot.Lab.ID, instances)
	result := &labstate.BalancerSnapshot{
		Status:         view.Status,
		TargetWeights:  make([]labstate.InstanceWeight, 0, len(view.TargetWeights)),
		SmoothedLoads:  make([]labstate.InstanceLoad, 0, len(view.SmoothedLoads)),
		CapacityNotice: view.CapacityNotice,
	}
	for _, weight := range view.TargetWeights {
		result.TargetWeights = append(result.TargetWeights, labstate.InstanceWeight{
			InstanceID: weight.InstanceID,
			Weight:     weight.Weight,
		})
	}
	for _, load := range view.SmoothedLoads {
		result.SmoothedLoads = append(result.SmoothedLoads, labstate.InstanceLoad{
			InstanceID: load.InstanceID,
			LoadRatio:  load.LoadRatio,
		})
	}
	if view.LastError != nil {
		result.LastError = &labstate.OperationError{
			Code: view.LastError.Code, Message: view.LastError.Message,
		}
	}
	snapshot.Balancer = result
}

func freshAt(now, value time.Time, freshness time.Duration) bool {
	return !value.IsZero() && !value.After(now) && now.Sub(value) <= freshness
}

func cloneMetrics(values map[string]metricState) map[string]metricState {
	result := make(map[string]metricState, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
