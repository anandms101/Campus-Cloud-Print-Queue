package main

import (
	"errors"
	"time"

	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
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
//
// IsSuccessful excludes DynamoDB ConditionalCheckFailedException from failure
// counts — these are expected business-logic errors (e.g., 409 Conflict on
// concurrent release) and should not trip the breaker.
func newCircuitBreaker(name string) *gobreaker.CircuitBreaker[[]byte] {
	return gobreaker.NewCircuitBreaker[[]byte](gobreaker.Settings{
		Name:        name,
		MaxRequests: 3,
		Interval:    30 * time.Second,
		Timeout:     15 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
		IsSuccessful: func(err error) bool {
			if err == nil {
				return true
			}
			// ConditionalCheckFailedException is expected business logic,
			// not an infrastructure failure — don't count it.
			var cce *dbtypes.ConditionalCheckFailedException
			return errors.As(err, &cce)
		},
	})
}
