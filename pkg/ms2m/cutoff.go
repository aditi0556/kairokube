package ms2m

import "time"

// CalculateThreshold computes the cutoff threshold (T_cutoff) for the message replay phase.
// This mechanism is used to handle high incoming message rates during migration.
// It uses an M/M/1 queuing theory model where:
//   - lambda: The incoming message rate at the source (messages per second).
//   - mu: The message processing rate at the target (messages per second).
//   - tReplayMax: The maximum acceptable time spent replaying messages.
func CalculateThreshold(lambda float64, mu float64, tReplayMax time.Duration) time.Duration {
	// Prevent division by zero or negative values
	if lambda <= 0 || mu <= 0 || tReplayMax <= 0 {
		return 0
	}
	
	// According to paper equation (5): T_cutoff <= T_replay_max * (mu / lambda)
	// This calculates how long we can allow messages to accumulate in the queue 
	// while ensuring the target pod can still replay them within the tReplayMax limit.
	ratio := mu / lambda
	tCutoff := time.Duration(float64(tReplayMax.Nanoseconds()) * ratio)
	
	return tCutoff
}
