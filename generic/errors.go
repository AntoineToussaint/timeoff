/*
errors.go - Centralized error types for the generic engine

PURPOSE:
  All error types in one place for consistency and discoverability.
  Domain packages should wrap these errors with additional context.

ERROR CATEGORIES:
  1. Ledger errors - Transaction persistence failures
  2. Validation errors - Business rule violations
  3. Store errors - Database-level failures

USAGE:
  Domain packages can wrap generic errors:

    if errors.Is(err, generic.ErrDuplicateDayConsumption) {
        return &DomainSpecificError{...}
    }

SEE ALSO:
  - ledger.go: Uses these errors
  - store.go: Uses these errors
  - timeoff/ledger.go: Wraps these errors with domain context
*/
package generic

import (
	"errors"
	"fmt"
)

// =============================================================================
// SENTINEL ERRORS - Use with errors.Is()
// =============================================================================

var (
	// ErrDuplicateIdempotencyKey is returned when a transaction with the same
	// idempotency key already exists. This is expected behavior for retries.
	ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")

	// ErrDuplicateDayConsumption is returned when trying to consume the same
	// day twice for time-off resources. This enforces the uniqueness invariant.
	ErrDuplicateDayConsumption = errors.New("duplicate consumption on same day")

	// ErrTransactionFailed is returned when a transaction cannot be persisted.
	ErrTransactionFailed = errors.New("transaction failed")

	// ErrInsufficientBalance is returned when consumption exceeds available balance.
	ErrInsufficientBalance = errors.New("insufficient balance")

	// ErrConcurrentModification is returned when optimistic locking detects a conflict.
	ErrConcurrentModification = errors.New("concurrent modification detected")

	// ErrPolicyNotFound is returned when a referenced policy doesn't exist.
	ErrPolicyNotFound = errors.New("policy not found")

	// ErrEntityNotFound is returned when a referenced entity doesn't exist.
	ErrEntityNotFound = errors.New("entity not found")

	// ErrInvalidPeriod is returned when a period is malformed (end before start).
	ErrInvalidPeriod = errors.New("invalid period: end before start")

	// ErrStoreRequired is returned when an operation requires a specific store capability.
	ErrStoreRequired = errors.New("operation requires extended store interface")
)

// =============================================================================
// STRUCTURED ERRORS - Carry additional context
// =============================================================================

// InsufficientBalanceError provides details about a balance shortage.
type InsufficientBalanceError struct {
	EntityID    EntityID
	PolicyID    PolicyID
	Available   Amount
	Requested   Amount
	Shortfall   Amount
}

func (e *InsufficientBalanceError) Error() string {
	return fmt.Sprintf("insufficient balance: available %v, requested %v, shortfall %v",
		e.Available.Value, e.Requested.Value, e.Shortfall.Value)
}

func (e *InsufficientBalanceError) Unwrap() error {
	return ErrInsufficientBalance
}

// DuplicateDayError provides details about a day uniqueness violation.
// This is the generic version; timeoff package may wrap with more context.
type DuplicateDayError struct {
	EntityID     EntityID
	Date         TimePoint
	ResourceType ResourceType
	ExistingTxID TransactionID
}

func (e *DuplicateDayError) Error() string {
	return fmt.Sprintf("day already consumed: %s for %s (tx: %s)",
		e.Date, e.ResourceType, e.ExistingTxID)
}

func (e *DuplicateDayError) Unwrap() error {
	return ErrDuplicateDayConsumption
}

// ValidationError provides details about a validation failure.
// Used by projection and balance calculation.
type ValidationErrorDetail struct {
	Code    string // e.g., "insufficient_balance", "exceeds_max"
	Message string
	At      TimePoint // When the violation occurred
	Balance Amount    // Balance at time of violation
}

func (e *ValidationErrorDetail) Error() string {
	return fmt.Sprintf("%s: %s at %s (balance: %v)",
		e.Code, e.Message, e.At, e.Balance.Value)
}

// =============================================================================
// ERROR HELPERS
// =============================================================================

// IsRetryable returns true if the error might succeed on retry.
func IsRetryable(err error) bool {
	return errors.Is(err, ErrConcurrentModification)
}

// IsClientError returns true if the error is due to invalid client input.
func IsClientError(err error) bool {
	return errors.Is(err, ErrInsufficientBalance) ||
		errors.Is(err, ErrDuplicateDayConsumption) ||
		errors.Is(err, ErrDuplicateIdempotencyKey) ||
		errors.Is(err, ErrInvalidPeriod)
}

// IsNotFound returns true if the error indicates a missing resource.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrPolicyNotFound) ||
		errors.Is(err, ErrEntityNotFound)
}
