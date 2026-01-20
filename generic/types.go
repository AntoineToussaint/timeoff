/*
Package generic provides the core resource management engine.

PURPOSE:
  This package contains domain-agnostic types and algorithms for managing
  time-bounded resources. Whether tracking PTO days, wellness points, or
  learning credits, the same engine handles balance calculation, transaction
  logging, and period reconciliation.

KEY CONCEPTS IN THIS FILE (types.go):
  - Amount: A quantity with a unit (e.g., 5 days, 100 points, $500)
  - Transaction: An immutable ledger entry recording balance changes
  - TimePoint: A specific point in time (used as ledger keys)
  - Entity/Policy IDs: Type-safe identifiers

DESIGN PRINCIPLES:
  1. Immutability: Transactions are never modified, only reversed
  2. Precision: Uses decimal.Decimal to avoid floating-point errors
  3. Type Safety: Strong typing for IDs prevents mixing entity/policy IDs
  4. Auditability: Every transaction has reason, reference, and idempotency key

USAGE:
  amount := generic.NewAmount(5, generic.UnitDays)
  tx := generic.Transaction{
      EntityID: "emp-123",
      PolicyID: "policy-001",
      Delta:    amount,
      Type:     generic.TxGrant,
  }

SEE ALSO:
  - policy.go: Policy definitions and reconciliation rules
  - balance.go: Balance calculation from transactions
  - ledger.go: Transaction persistence interface
*/
package generic

import (
	"github.com/shopspring/decimal"
)

// =============================================================================
// AMOUNT - Quantity with unit (always time-based for this system)
// =============================================================================

type Amount struct {
	Value decimal.Decimal
	Unit  Unit
}

type Unit string

const (
	UnitDays    Unit = "days"
	UnitHours   Unit = "hours"
	UnitMinutes Unit = "minutes"
)

func NewAmount(value float64, unit Unit) Amount {
	return Amount{Value: decimal.NewFromFloat(value), Unit: unit}
}

func NewAmountFromInt(value int, unit Unit) Amount {
	return Amount{Value: decimal.NewFromInt(int64(value)), Unit: unit}
}

func MustParseDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}


func (a Amount) Zero() Amount                    { return Amount{Value: decimal.Zero, Unit: a.Unit} }
func (a Amount) Add(b Amount) Amount             { return Amount{Value: a.Value.Add(b.Value), Unit: a.Unit} }
func (a Amount) Sub(b Amount) Amount             { return Amount{Value: a.Value.Sub(b.Value), Unit: a.Unit} }
func (a Amount) Mul(s decimal.Decimal) Amount    { return Amount{Value: a.Value.Mul(s), Unit: a.Unit} }
func (a Amount) Div(s decimal.Decimal) Amount    { return Amount{Value: a.Value.Div(s), Unit: a.Unit} }
func (a Amount) Neg() Amount                     { return Amount{Value: a.Value.Neg(), Unit: a.Unit} }
func (a Amount) IsNegative() bool                { return a.Value.IsNegative() }
func (a Amount) IsZero() bool                    { return a.Value.IsZero() }
func (a Amount) IsPositive() bool                { return a.Value.IsPositive() }
func (a Amount) GreaterThan(b Amount) bool       { return a.Value.GreaterThan(b.Value) }
func (a Amount) LessThan(b Amount) bool          { return a.Value.LessThan(b.Value) }
func (a Amount) Min(b Amount) Amount             { if a.LessThan(b) { return a }; return b }
func (a Amount) Max(b Amount) Amount             { if a.GreaterThan(b) { return a }; return b }

// =============================================================================
// IDENTIFIERS
// =============================================================================

type EntityID string
type PolicyID string
type TransactionID string

// ResourceType identifies what kind of resource is being tracked.
// This is an interface so domain packages define their own concrete types.
// The generic package has NO knowledge of specific resource types.
//
// Domain packages implement this:
//
//   // In timeoff/types.go
//   type TimeOffResource string
//   func (r TimeOffResource) ResourceID() string { return string(r) }
//   func (r TimeOffResource) ResourceDomain() string { return "timeoff" }
//   const ResourcePTO TimeOffResource = "pto"
//
type ResourceType interface {
	// ResourceID returns the unique identifier for this resource type.
	ResourceID() string

	// ResourceDomain returns which domain this resource belongs to.
	ResourceDomain() string
}

// =============================================================================
// TRANSACTION - Atomic change to resource balance
// =============================================================================

type TransactionType string

const (
	TxGrant          TransactionType = "grant"          // One-time resource grant (bonus, carryover balance, hours-worked accrual)
	TxConsumption    TransactionType = "consumption"    // Resource used (approved request)
	TxPending        TransactionType = "pending"        // Reserved for pending request
	TxReconciliation TransactionType = "reconciliation" // Period-end adjustment (rollover, expire)
	TxAdjustment     TransactionType = "adjustment"     // Manual admin correction
	TxReversal       TransactionType = "reversal"       // Undo a previous transaction
)

type Transaction struct {
	ID             TransactionID
	EntityID       EntityID
	PolicyID       PolicyID
	ResourceType   ResourceType
	EffectiveAt    TimePoint
	Delta          Amount
	Type           TransactionType
	ReferenceID    string
	Reason         string
	IdempotencyKey string
	Metadata       map[string]string

	// Audit fields
	CreatedBy     string // Actor who created this transaction
	CreatedByType string // "employee", "manager", "system", "admin"
	CreatedAt     TimePoint
}

// =============================================================================
// CONSUMPTION EVENT - Single point of resource usage
// =============================================================================

type ConsumptionEvent struct {
	At     TimePoint
	Amount Amount
}

// =============================================================================
// BALANCE SNAPSHOT - Computed state at a point in time
// =============================================================================

type BalanceSnapshot struct {
	AsOf          TimePoint
	EntityID      EntityID
	PolicyID      PolicyID
	Balance       Amount
	TotalAccrued  Amount
	TotalConsumed Amount
	TotalAdjusted Amount
}

// =============================================================================
// TIMELINE - For projection and validation
// =============================================================================

type TimelineEvent struct {
	At    TimePoint
	Delta Amount
	Type  string
	Ref   string
}

type Timeline struct {
	Events []TimelineEvent
}

func (t *Timeline) BalanceAt(at TimePoint, initial Amount) Amount {
	balance := initial
	for _, e := range t.Events {
		if e.At.After(at) {
			break
		}
		balance = balance.Add(e.Delta)
	}
	return balance
}

func (t *Timeline) Validate(initial Amount, allowNegative bool, maxBalance *Amount) *ValidationError {
	balance := initial
	for _, e := range t.Events {
		balance = balance.Add(e.Delta)
		if !allowNegative && balance.IsNegative() {
			return &ValidationError{At: e.At, Balance: balance, Type: "negative_balance"}
		}
		if maxBalance != nil && balance.GreaterThan(*maxBalance) {
			return &ValidationError{At: e.At, Balance: balance, Type: "exceeds_max"}
		}
	}
	return nil
}

type ValidationError struct {
	At      TimePoint
	Balance Amount
	Type    string
}

func (e *ValidationError) Error() string { return e.Type }
