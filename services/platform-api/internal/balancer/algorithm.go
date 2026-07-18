package balancer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	loadBalanceThreshold = 0.05
	minimumCorrection    = 0.25
	maximumCorrection    = 1.75
	weightShareThreshold = 0.03
)

// TargetWeights calculates processing-speed weights with proportional load feedback.
func TargetWeights(instances []Instance, loadRatios map[string]float64) ([]Weight, error) {
	if len(instances) == 0 {
		return nil, errors.New("adaptive instances are required")
	}
	values := append([]Instance(nil), instances...)
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	minimumRatio := 1.0
	maximumRatio := 0.0
	totalRatio := 0.0
	for _, instance := range values {
		if instance.ID == "" || instance.ProcessingSpeed <= 0 || instance.MaxLoad <= 0 {
			return nil, errors.New("adaptive instance load model is invalid")
		}
		ratio := loadRatios[instance.ID]
		if ratio < 0 || ratio > 1 {
			return nil, errors.New("adaptive instance load ratio is invalid")
		}
		minimumRatio = min(minimumRatio, ratio)
		maximumRatio = max(maximumRatio, ratio)
		totalRatio += ratio
	}
	averageRatio := totalRatio / float64(len(values))
	balanced := maximumRatio-minimumRatio <= loadBalanceThreshold
	scores := make([]float64, len(values))
	maximumScore := 0.0
	for index, instance := range values {
		correction := 1.0
		if !balanced {
			correction = min(
				maximumCorrection,
				max(minimumCorrection, 1+averageRatio-loadRatios[instance.ID]),
			)
		}
		scores[index] = float64(instance.ProcessingSpeed) * correction
		maximumScore = max(maximumScore, scores[index])
	}
	weights := make([]Weight, 0, len(values))
	divisor := 0
	for index, instance := range values {
		weight := int(math.Round(scores[index] / maximumScore * 100))
		weight = min(100, max(1, weight))
		divisor = greatestCommonDivisor(divisor, weight)
		weights = append(weights, Weight{InstanceID: instance.ID, Weight: weight})
	}
	for index := range weights {
		weights[index].Weight /= divisor
	}
	return weights, nil
}

// ConfigurationFingerprint identifies the sampled instance set and load model.
func ConfigurationFingerprint(instances []Instance) string {
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

// WeightsWithinShareThreshold reports whether routing shares differ materially.
func WeightsWithinShareThreshold(instances []Instance, weights []Weight) bool {
	if len(instances) == 0 || len(instances) != len(weights) {
		return false
	}
	targets := make(map[string]int, len(weights))
	totalCurrent := 0
	totalTarget := 0
	for _, weight := range weights {
		if weight.InstanceID == "" || weight.Weight <= 0 || targets[weight.InstanceID] != 0 {
			return false
		}
		targets[weight.InstanceID] = weight.Weight
		totalTarget += weight.Weight
	}
	for _, instance := range instances {
		if instance.CurrentWeight <= 0 || targets[instance.ID] == 0 {
			return false
		}
		totalCurrent += instance.CurrentWeight
	}
	for _, instance := range instances {
		currentShare := float64(instance.CurrentWeight) / float64(totalCurrent)
		targetShare := float64(targets[instance.ID]) / float64(totalTarget)
		if math.Abs(currentShare-targetShare) >= weightShareThreshold {
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
		body.WriteString(instance.ContainerID)
		body.WriteByte(':')
		body.WriteString(instance.Status)
		body.WriteByte(':')
		body.WriteString(strconv.Itoa(instance.ProcessingSpeed))
		body.WriteByte(':')
		body.WriteString(strconv.Itoa(instance.MaxLoad))
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
