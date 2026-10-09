// Package ms2m provides mathematical models and cutoff threshold calculations
// for the Message-based Stateful Microservice Migration (MS2M) framework.
package ms2m

import (
	"fmt"
	"time"
)

// CutoffResult contains the parameters and derived cutoff threshold for message replay.
type CutoffResult struct {
	Lambda           float64       `json:"lambda"`
	MuTarget         float64       `json:"mu_target"`
	Utilization      float64       `json:"utilization"`
	MaxReplayTime    time.Duration `json:"max_replay_time"`
	CalculatedCutoff time.Duration `json:"calculated_cutoff"`
	IsClamped        bool          `json:"is_clamped"`
	Warning          string        `json:"warning,omitempty"`
}

// CalculateThreshold computes the cutoff threshold (T_cutoff) according to MS2M equation:
//
//	T_cutoff <= T_replay_max * (mu / lambda)
//
// It preserves backward compatibility while enforcing all safety bounds.
func CalculateThreshold(lambda float64, mu float64, tReplayMax time.Duration) time.Duration {
	res := CalculateAdaptiveCutoff(lambda, mu, tReplayMax, 1*time.Second, 30*time.Second)
	return res.CalculatedCutoff
}

// CalculateAdaptiveCutoff computes the dynamically bounded cutoff time using measured arrival
// rate (lambda) and target processing rate (mu), adhering to the MS2M mathematical model:
//
//	N_messages = lambda * T_accum
//	T_replay = (lambda * T_accum) / mu_target <= T_replay_max
//	T_cutoff <= T_replay_max * (mu_target / lambda)
//
// Safeguards:
//   - Avoid divide-by-zero (lambda == 0 or mu == 0)
//   - Handle lambda ≈ 0 (idle queue, allow max cutoff)
//   - Handle mu <= 0 (non-processing target, clamp to min cutoff)
//   - Handle mu <= lambda (system overloaded / congested)
//   - Clamp within [minCutoff, maxCutoff]
func CalculateAdaptiveCutoff(lambda, mu float64, tReplayMax, minCutoff, maxCutoff time.Duration) CutoffResult {
	if minCutoff <= 0 {
		minCutoff = 500 * time.Millisecond
	}
	if maxCutoff <= minCutoff {
		maxCutoff = minCutoff + 30*time.Second
	}
	if tReplayMax <= 0 {
		tReplayMax = 5 * time.Second
	}

	result := CutoffResult{
		Lambda:        lambda,
		MuTarget:      mu,
		MaxReplayTime: tReplayMax,
	}

	// Safeguard 1: Target cannot process messages (mu <= 0)
	if mu <= 0 {
		result.CalculatedCutoff = minCutoff
		result.Utilization = 0
		result.IsClamped = true
		result.Warning = fmt.Sprintf("target processing rate mu (%.2f) is non-positive; clamped to min cutoff", mu)
		return result
	}

	// Safeguard 2: Zero or negative incoming rate (idle traffic)
	if lambda <= 0.0001 {
		result.CalculatedCutoff = maxCutoff
		result.Utilization = 0
		result.IsClamped = true
		result.Warning = "incoming message rate lambda is near zero; traffic is idle, using max cutoff"
		return result
	}

	// Calculate utilization rho = lambda / mu
	utilization := lambda / mu
	result.Utilization = utilization

	// MS2M derivation: T_cutoff = T_replay_max * (mu / lambda)
	ratio := mu / lambda
	rawCutoffSeconds := float64(tReplayMax.Nanoseconds()) / 1e9 * ratio
	rawCutoff := time.Duration(rawCutoffSeconds * 1e9)

	// Safeguard 3: Congested system (mu <= lambda)
	if mu <= lambda {
		result.Warning = fmt.Sprintf("system is congested (lambda=%.2f >= mu=%.2f, utilization=%.2f); replay queue may grow", lambda, mu, utilization)
	}

	// Clamp to [minCutoff, maxCutoff]
	if rawCutoff < minCutoff {
		result.CalculatedCutoff = minCutoff
		result.IsClamped = true
	} else if rawCutoff > maxCutoff {
		result.CalculatedCutoff = maxCutoff
		result.IsClamped = true
	} else {
		result.CalculatedCutoff = rawCutoff
		result.IsClamped = false
	}

	// Safeguard 4: Prevent any negative cutoff
	if result.CalculatedCutoff < 0 {
		result.CalculatedCutoff = minCutoff
		result.IsClamped = true
	}

	return result
}
