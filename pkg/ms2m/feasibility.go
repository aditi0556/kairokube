package ms2m

import (
	"fmt"
	"strings"
)

// FeasibilityStatus classifies the risk level of starting a migration.
type FeasibilityStatus string

const (
	StatusFeasible   FeasibilityStatus = "FEASIBLE"
	StatusHighRisk   FeasibilityStatus = "HIGH_RISK"
	StatusInfeasible FeasibilityStatus = "INFEASIBLE"
)

// FeasibilityResult encapsulates the feasibility check evaluation.
type FeasibilityResult struct {
	ArrivalRate float64           `json:"arrival_rate"`
	TargetRate  float64           `json:"target_rate"`
	Utilization float64           `json:"utilization"`
	Status      FeasibilityStatus `json:"status"`
	Policy      string            `json:"policy"`
	CanProceed  bool              `json:"can_proceed"`
	Message     string            `json:"message"`
}

// EvaluateFeasibility checks if the target consumer can realistically catch up with accumulated messages.
// Evaluates rho = lambda / mu_target.
// Policies:
//   - "reject": aborts migration if status is INFEASIBLE (rho >= 1.0)
//   - "warn": logs a warning but allows migration to proceed
//   - "force": proceeds unconditionally
func EvaluateFeasibility(lambda, mu float64, policy string) FeasibilityResult {
	pol := strings.ToLower(policy)
	if pol == "" {
		pol = "warn"
	}

	result := FeasibilityResult{
		ArrivalRate: lambda,
		TargetRate:  mu,
		Policy:      pol,
	}

	if mu <= 0 {
		result.Status = StatusInfeasible
		result.Utilization = 999.0
		result.Message = fmt.Sprintf("target processing rate is non-positive (mu=%.2f); target cannot process messages", mu)
		result.CanProceed = (pol == "force")
		return result
	}

	if lambda <= 0.0001 {
		result.Status = StatusFeasible
		result.Utilization = 0.0
		result.CanProceed = true
		result.Message = "idle traffic; migration is completely feasible"
		return result
	}

	rho := lambda / mu
	result.Utilization = rho

	if rho >= 1.0 {
		result.Status = StatusInfeasible
		result.Message = fmt.Sprintf("arrival rate (%.2f msg/s) >= target processing rate (%.2f msg/s); utilization is %.1f%%; target cannot drain accumulated messages in steady state",
			lambda, mu, rho*100)
		result.CanProceed = (pol != "reject")
	} else if rho >= 0.80 {
		result.Status = StatusHighRisk
		result.Message = fmt.Sprintf("arrival rate (%.2f msg/s) is close to target processing rate (%.2f msg/s); utilization is %.1f%%; migration has high risk of long replay",
			lambda, mu, rho*100)
		result.CanProceed = true
	} else {
		result.Status = StatusFeasible
		result.Message = fmt.Sprintf("arrival rate (%.2f msg/s) < target processing rate (%.2f msg/s); utilization is %.1f%%; migration is feasible",
			lambda, mu, rho*100)
		result.CanProceed = true
	}

	return result
}
