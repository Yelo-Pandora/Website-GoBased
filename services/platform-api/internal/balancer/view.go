package balancer

import (
	"math"
	"sort"
	"time"

	"website-gobased/internal/protocol"
	labstate "website-gobased/services/platform-api/internal/lab"
)

// Observe records one already validated synchronous traffic result.
func (c *Controller) Observe(
	labID string,
	instance labstate.Instance,
	result protocol.TrafficBatchResult,
) {
	state := result.InstanceState
	if labID == "" || result.TargetInstanceID == "" || instance.ID != result.TargetInstanceID ||
		instance.ContainerID == "" || instance.ProcessingSpeed != state.ProcessingSpeed ||
		instance.MaxLoad != state.MaxLoad || state.ProcessingSpeed <= 0 ||
		state.MaxLoad <= 0 || state.CurrentLoad < 0 ||
		state.CurrentLoad > float64(state.MaxLoad) || state.ObservedAt.IsZero() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	instances := c.metrics[labID]
	if instances == nil {
		instances = make(map[string]metricState)
		c.metrics[labID] = instances
	}
	metric := instances[result.TargetInstanceID]
	if !metric.observedAt.IsZero() && state.ObservedAt.Before(metric.observedAt) {
		return
	}
	metric.currentLoad = state.CurrentLoad
	metric.containerID = instance.ContainerID
	metric.processingSpeed = state.ProcessingSpeed
	metric.maxLoad = state.MaxLoad
	metric.observedAt = state.ObservedAt.UTC()
	if result.DroppedUnits > 0 {
		metric.lastDropped = state.ObservedAt.UTC()
	}
	instances[result.TargetInstanceID] = metric
}

func (c *Controller) sampleLoadRatios(
	labID string,
	instances []Instance,
	now time.Time,
) map[string]float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	metrics := c.metrics[labID]
	if metrics == nil {
		metrics = make(map[string]metricState)
		c.metrics[labID] = metrics
	}
	ratios := make(map[string]float64, len(instances))
	for _, instance := range instances {
		metric, exists := metrics[instance.ID]
		if !exists || metric.containerID != instance.ContainerID ||
			metric.processingSpeed != instance.ProcessingSpeed ||
			metric.maxLoad != instance.MaxLoad {
			metric = metricState{
				containerID:     instance.ContainerID,
				processingSpeed: instance.ProcessingSpeed,
				maxLoad:         instance.MaxLoad,
				observedAt:      now,
			}
		}
		_, ratio := estimateMetric(metric, now)
		if metric.smoothedAt.IsZero() {
			metric.smoothed = ratio
		} else {
			metric.smoothed = c.config.EWMAAlpha*ratio +
				(1-c.config.EWMAAlpha)*metric.smoothed
		}
		if ratio == 0 && metric.smoothed < 0.0005 {
			metric.smoothed = 0
		}
		metric.smoothedAt = now
		metrics[instance.ID] = metric
		ratios[instance.ID] = metric.smoothed
	}
	return ratios
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
		state.targetWeights, _ = TargetWeights(instances, nil)
	}
	view := View{
		Status:        state.status,
		TargetWeights: append([]Weight(nil), state.targetWeights...),
		SmoothedLoads: make([]SmoothedLoad, 0, len(instances)),
	}
	if view.Status == "" {
		view.Status = StatusConverging
	}
	allSampled := len(instances) > 0
	allUnderused := allSampled
	overloaded := false
	for _, instance := range instances {
		metric, exists := metrics[instance.ID]
		if !exists || metric.smoothedAt.IsZero() ||
			metric.containerID != instance.ContainerID ||
			metric.processingSpeed != instance.ProcessingSpeed ||
			metric.maxLoad != instance.MaxLoad {
			allSampled = false
			allUnderused = false
			continue
		}
		view.SmoothedLoads = append(view.SmoothedLoads, SmoothedLoad{
			InstanceID: instance.ID,
			LoadRatio:  roundThree(metric.smoothed),
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
	} else if allSampled && allUnderused {
		view.CapacityNotice = "cluster_underused"
	}
	if state.lastError != nil {
		value := *state.lastError
		view.LastError = &value
	}
	return view
}

// DecorateSnapshot attaches runtime load and adaptive state to one lab snapshot.
func (c *Controller) DecorateSnapshot(snapshot *labstate.Snapshot) {
	if snapshot == nil {
		return
	}
	now := c.now().UTC().Truncate(time.Microsecond)
	c.mu.RLock()
	metrics := cloneMetrics(c.metrics[snapshot.Lab.ID])
	c.mu.RUnlock()
	for index := range snapshot.Topology.Instances {
		instance := &snapshot.Topology.Instances[index]
		metric, exists := metrics[instance.ID]
		if !exists || metric.containerID != instance.ContainerID ||
			metric.processingSpeed != instance.ProcessingSpeed ||
			metric.maxLoad != instance.MaxLoad {
			continue
		}
		load, ratio := estimateMetric(metric, now)
		instance.CurrentLoad = roundThree(load)
		instance.LoadRatio = roundThree(ratio)
		instance.LoadState = loadState(instance.LoadRatio)
		instance.ObservedAt = now
	}
	if snapshot.Lab.BalancingMode != "adaptive" {
		return
	}
	instances := make([]Instance, 0, len(snapshot.Topology.Instances))
	for _, instance := range snapshot.Topology.Instances {
		instances = append(instances, Instance{
			ID:              instance.ID,
			ContainerID:     instance.ContainerID,
			Status:          instance.Status,
			ProcessingSpeed: instance.ProcessingSpeed,
			MaxLoad:         instance.MaxLoad,
			CurrentWeight:   instance.CurrentWeight,
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

func estimateMetric(metric metricState, now time.Time) (float64, float64) {
	if metric.processingSpeed <= 0 || metric.maxLoad <= 0 || metric.observedAt.IsZero() {
		return 0, 0
	}
	elapsedSeconds := 0.0
	if now.After(metric.observedAt) {
		elapsedSeconds = now.Sub(metric.observedAt).Seconds()
	}
	load := max(
		0,
		metric.currentLoad-float64(metric.processingSpeed)*elapsedSeconds,
	)
	return load, load / float64(metric.maxLoad)
}

func loadState(loadRatio float64) string {
	switch {
	case loadRatio <= 0.3:
		return "idle"
	case loadRatio <= 0.7:
		return "normal"
	case loadRatio < 1:
		return "high"
	default:
		return "overloaded"
	}
}

func roundThree(value float64) float64 {
	return math.Round(value*1000) / 1000
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
