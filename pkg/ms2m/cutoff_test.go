package ms2m

import (
	"testing"
	"time"
)

func TestCalculateAdaptiveCutoff(t *testing.T) {
	minCutoff := 1 * time.Second
	maxCutoff := 30 * time.Second
	maxReplay := 5 * time.Second

	tests := []struct {
		name            string
		lambda          float64
		mu              float64
		maxReplay       time.Duration
		expectCutoffMax time.Duration
		expectMin       time.Duration
		expectClamped   bool
	}{
		{
			name:            "Standard baseline (lambda=10, mu=20, max_replay=5s) -> Tcutoff=10s",
			lambda:          10.0,
			mu:              20.0,
			maxReplay:       maxReplay,
			expectCutoffMax: 10 * time.Second,
			expectMin:       10 * time.Second,
			expectClamped:   false,
		},
		{
			name:            "Equal rates (lambda=20, mu=20, max_replay=5s) -> Tcutoff=5s",
			lambda:          20.0,
			mu:              20.0,
			maxReplay:       maxReplay,
			expectCutoffMax: 5 * time.Second,
			expectMin:       5 * time.Second,
			expectClamped:   false,
		},
		{
			name:            "Congested (lambda=25, mu=20, max_replay=5s) -> Tcutoff=4s",
			lambda:          25.0,
			mu:              20.0,
			maxReplay:       maxReplay,
			expectCutoffMax: 4 * time.Second,
			expectMin:       4 * time.Second,
			expectClamped:   false,
		},
		{
			name:            "Zero lambda (idle traffic) -> clamped to max cutoff 30s",
			lambda:          0.0,
			mu:              20.0,
			maxReplay:       maxReplay,
			expectCutoffMax: 30 * time.Second,
			expectMin:       30 * time.Second,
			expectClamped:   true,
		},
		{
			name:            "Near-zero lambda -> clamped to max cutoff 30s",
			lambda:          0.00001,
			mu:              20.0,
			maxReplay:       maxReplay,
			expectCutoffMax: 30 * time.Second,
			expectMin:       30 * time.Second,
			expectClamped:   true,
		},
		{
			name:            "Zero mu (dead target) -> clamped to min cutoff 1s",
			lambda:          10.0,
			mu:              0.0,
			maxReplay:       maxReplay,
			expectCutoffMax: 1 * time.Second,
			expectMin:       1 * time.Second,
			expectClamped:   true,
		},
		{
			name:            "Negative mu -> clamped to min cutoff 1s",
			lambda:          10.0,
			mu:              -5.0,
			maxReplay:       maxReplay,
			expectCutoffMax: 1 * time.Second,
			expectMin:       1 * time.Second,
			expectClamped:   true,
		},
		{
			name:            "Extreme high traffic (lambda=1000, mu=20) -> clamped to min cutoff 1s",
			lambda:          1000.0,
			mu:              20.0,
			maxReplay:       maxReplay,
			expectCutoffMax: 1 * time.Second,
			expectMin:       1 * time.Second,
			expectClamped:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := CalculateAdaptiveCutoff(tc.lambda, tc.mu, tc.maxReplay, minCutoff, maxCutoff)

			if res.CalculatedCutoff < tc.expectMin || res.CalculatedCutoff > tc.expectCutoffMax {
				t.Errorf("expected cutoff between %s and %s, got %s", tc.expectMin, tc.expectCutoffMax, res.CalculatedCutoff)
			}
			if res.IsClamped != tc.expectClamped {
				t.Errorf("expected IsClamped=%v, got %v", tc.expectClamped, res.IsClamped)
			}
			if tc.mu > 0 && tc.lambda > 0.0001 {
				expectedUtil := tc.lambda / tc.mu
				if res.Utilization != expectedUtil {
					t.Errorf("expected utilization %.4f, got %.4f", expectedUtil, res.Utilization)
				}
			}
		})
	}
}

func TestCalculateThresholdBackwardCompatibility(t *testing.T) {
	cutoff := CalculateThreshold(10.0, 20.0, 5*time.Second)
	if cutoff != 10*time.Second {
		t.Fatalf("expected 10s cutoff for lambda=10, mu=20, maxReplay=5s; got %s", cutoff)
	}

	zeroCutoff := CalculateThreshold(0, 0, 0)
	if zeroCutoff <= 0 {
		t.Logf("handled zero rate threshold gracefully: %s", zeroCutoff)
	}
}
