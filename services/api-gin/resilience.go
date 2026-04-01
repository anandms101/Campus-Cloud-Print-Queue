package main

import (
	"time"

	"github.com/sony/gobreaker/v2"
)

// newCircuitBreaker creates a circuit breaker for an AWS service dependency.
//
// Behavior:
//   - Closed (normal): all calls pass through, failures are counted.
//   - Open (tripped): after 5 consecutive failures the breaker opens for 15s.
//     All calls fail immediately with gobreaker.ErrOpenState.
//   - Half-Open: after 15s, 3 probe calls are allowed through.
//     If they succeed, the breaker closes; if they fail, it re-opens.
func newCircuitBreaker(name string) *gobreaker.CircuitBreaker[[]byte] {
	return gobreaker.NewCircuitBreaker[[]byte](gobreaker.Settings{
		Name:        name,
		MaxRequests: 3,
		Interval:    30 * time.Second,
		Timeout:     15 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
	})
}
