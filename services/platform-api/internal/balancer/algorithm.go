package balancer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// TargetWeights reduces effective capacities to their smallest integer ratio.
func TargetWeights(instances []Instance) ([]Weight, error) {
	if len(instances) == 0 {
		return nil, errors.New("adaptive instances are required")
	}
	values := append([]Instance(nil), instances...)
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	divisor := 0
	for _, instance := range values {
		if instance.ID == "" || instance.EffectiveCapacity <= 0 {
			return nil, errors.New("adaptive instance capacity is invalid")
		}
		divisor = greatestCommonDivisor(divisor, instance.EffectiveCapacity)
	}
	weights := make([]Weight, 0, len(values))
	for _, instance := range values {
		weight := instance.EffectiveCapacity / divisor
		if weight < 1 || weight > 100 {
			return nil, errors.New("adaptive target weight is outside the allowed range")
		}
		weights = append(weights, Weight{InstanceID: instance.ID, Weight: weight})
	}
	return weights, nil
}

// CapacityFingerprint identifies the sampled instance set and capacities.
func CapacityFingerprint(instances []Instance) string {
	return fingerprint(instances, false)
}

// TopologyFingerprint identifies the exact topology used by an adjustment.
func TopologyFingerprint(instances []Instance) string {
	return fingerprint(instances, true)
}

// WeightsEqual reports whether persisted instance weights match a target set.
func WeightsEqual(instances []Instance, weights []Weight) bool {
	if len(instances) != len(weights) {
		return false
	}
	values := make(map[string]int, len(weights))
	for _, weight := range weights {
		if weight.InstanceID == "" || values[weight.InstanceID] != 0 {
			return false
		}
		values[weight.InstanceID] = weight.Weight
	}
	for _, instance := range instances {
		if values[instance.ID] != instance.CurrentWeight {
			return false
		}
	}
	return true
}

func fingerprint(instances []Instance, includeWeight bool) string {
	values := append([]Instance(nil), instances...)
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	var body strings.Builder
	for _, instance := range values {
		body.WriteString(instance.ID)
		body.WriteByte(':')
		body.WriteString(instance.Status)
		body.WriteByte(':')
		body.WriteString(strconv.Itoa(instance.EffectiveCapacity))
		if includeWeight {
			body.WriteByte(':')
			body.WriteString(strconv.Itoa(instance.CurrentWeight))
		}
		body.WriteByte('|')
	}
	sum := sha256.Sum256([]byte(body.String()))
	return hex.EncodeToString(sum[:])
}

func greatestCommonDivisor(left, right int) int {
	if left < 0 {
		left = -left
	}
	if right < 0 {
		right = -right
	}
	for right != 0 {
		left, right = right, left%right
	}
	return left
}
