package ms2m

import (
	"testing"
)

func TestEvaluateFeasibility(t *testing.T) {
	tests := []struct {
		name          string
		lambda        float64
		mu            float64
		policy        string
		expectStatus  FeasibilityStatus
		expectProceed bool
	}{
		{
			name:          "Low traffic (lambda=4, mu=20) -> FEASIBLE",
			lambda:        4.0,
			mu:            20.0,
			policy:        "reject",
			expectStatus:  StatusFeasible,
			expectProceed: true,
		},
		{
			name:          "High risk traffic (lambda=18.5, mu=20) -> HIGH_RISK",
			lambda:        18.5,
			mu:            20.0,
			policy:        "reject",
			expectStatus:  StatusHighRisk,
			expectProceed: true,
		},
		{
			name:          "Infeasible traffic with reject policy (lambda=25, mu=20) -> REJECTED",
			lambda:        25.0,
			mu:            20.0,
			policy:        "reject",
			expectStatus:  StatusInfeasible,
			expectProceed: false,
		},
		{
			name:          "Infeasible traffic with warn policy (lambda=25, mu=20) -> WARNED BUT PROCEEDS",
			lambda:        25.0,
			mu:            20.0,
			policy:        "warn",
			expectStatus:  StatusInfeasible,
			expectProceed: true,
		},
		{
			name:          "Infeasible traffic with force policy (lambda=25, mu=20) -> FORCED",
			lambda:        25.0,
			mu:            20.0,
			policy:        "force",
			expectStatus:  StatusInfeasible,
			expectProceed: true,
		},
		{
			name:          "Zero arrival rate (lambda=0, mu=20) -> FEASIBLE",
			lambda:        0.0,
			mu:            20.0,
			policy:        "reject",
			expectStatus:  StatusFeasible,
			expectProceed: true,
		},
		{
			name:          "Zero target rate (lambda=10, mu=0) -> INFEASIBLE REJECTED",
			lambda:        10.0,
			mu:            0.0,
			policy:        "reject",
			expectStatus:  StatusInfeasible,
			expectProceed: false,
		},
		{
			name:          "Zero target rate with force policy -> FORCED",
			lambda:        10.0,
			mu:            0.0,
			policy:        "force",
			expectStatus:  StatusInfeasible,
			expectProceed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := EvaluateFeasibility(tc.lambda, tc.mu, tc.policy)
			if res.Status != tc.expectStatus {
				t.Errorf("expected status %s, got %s", tc.expectStatus, res.Status)
			}
			if res.CanProceed != tc.expectProceed {
				t.Errorf("expected CanProceed=%v, got %v (policy=%s)", tc.expectProceed, res.CanProceed, tc.policy)
			}
		})
	}
}
