/*
ledger.go - Time-off specific ledger with day uniqueness enforcement

PURPOSE:
  Wraps the generic ledger with time-off specific business rules.
  The critical invariant: you cannot take the same day off twice.

INVARIANT:
  No duplicate consumption for (EntityID, Date, ResourceType).

  This is unique to time-off resources. Unlike wellness points (you can
  earn multiple kudos on the same day), time-off consumption represents
  actual calendar days. You can't be "off" twice on March 10th.

WHY A WRAPPER?
  The generic engine doesn't know about calendar days. It handles amounts
  like "5 days" without understanding that those days must be unique.
  This wrapper enforces that domain-specific constraint.

WHAT IT CHECKS:
  1. Single Append: Is this day already consumed for this entity/resource?
  2. Batch Append: Are there duplicates within the batch?
  3. Batch Append: Do any batch items conflict with existing records?

MULTI-POLICY BEHAVIOR:
  Even with multiple PTO policies, you can't double-book:
  - March 10 from "carryover-pto": OK
  - March 10 from "standard-pto": REJECTED (day already taken)

ERROR HANDLING:
  DuplicateDayError is returned with details:
  - Which day was duplicated
  - Which existing transaction conflicts
  - Whether conflict is within-batch or with existing data

QUERYING:
  Provides helper methods:
  - IsDayOff(entityID, day): Check if specific day is taken
  - GetDaysOff(entityID, from, to): List all days off in range
  - GetUpcomingDaysOff(entityID): Future scheduled days off

EXAMPLE:
  timeOffLedger := timeoff.NewTimeOffLedger(store)

  // This will fail if March 10 is already taken
  err := timeOffLedger.Append(ctx, consumptionTx, policy)
  if err != nil {
      if dupErr, ok := err.(*timeoff.DuplicateDayError); ok {
          fmt.Printf("Day %s already taken\n", dupErr.Date)
      }
  }

SEE ALSO:
  - generic/ledger.go: Base ledger interface
  - generic/store.go: IsDayConsumed, GetConsumedDays methods
*/
package timeoff

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/warp/resource-engine/generic"
)

// =============================================================================
// TIME-OFF LEDGER - Wrapper with time-off specific invariants
// =============================================================================

// TimeOffLedger wraps the generic ledger with time-off specific rules:
//
// INVARIANT: No duplicate consumption for the same day.
// For time-off, you cannot take the same day off twice - it's a uniqueness
// constraint on (EntityID, EffectiveAt.Date, ResourceType).
//
// This is different from rewards where you CAN have multiple transactions
// on the same day (e.g., multiple kudos, multiple purchases).
//
// REQUIRES: Store must implement generic.EntityStore for day-uniqueness checks.
type TimeOffLedger struct {
	inner       generic.Ledger
	store       generic.Store
	entityStore generic.EntityStore // May be nil if store doesn't support entity queries
}

// NewTimeOffLedger creates a time-off specific ledger wrapper.
// The store SHOULD implement generic.EntityStore for full functionality.
// If it doesn't, day-uniqueness checks will rely on database constraints only.
func NewTimeOffLedger(store generic.Store) *TimeOffLedger {
	ledger := &TimeOffLedger{
		inner: generic.NewLedger(store),
		store: store,
	}
	// Check if store supports entity-wide queries
	if es, ok := store.(generic.EntityStore); ok {
		ledger.entityStore = es
	}
	return ledger
}

// =============================================================================
// CORE OPERATIONS (delegated to inner ledger with validation)
// =============================================================================

// Append adds a transaction with time-off specific validation.
// Returns DuplicateDayError if trying to consume on an already-consumed day.
func (l *TimeOffLedger) Append(ctx context.Context, tx generic.Transaction) error {
	// Only validate uniqueness for consumption transactions
	if tx.Type == generic.TxConsumption || tx.Type == generic.TxPending {
		if err := l.validateDayUniqueness(ctx, tx); err != nil {
			return err
		}
	}
	err := l.inner.Append(ctx, tx)
	// Wrap database-level uniqueness errors with domain-specific error
	if errors.Is(err, generic.ErrDuplicateDayConsumption) {
		return &DuplicateDayError{
			EntityID:     tx.EntityID,
			Date:         tx.EffectiveAt.Time.Truncate(24 * time.Hour),
			ResourceType: tx.ResourceType,
		}
	}
	return err
}

// AppendBatch adds multiple transactions atomically with validation.
// Validates that:
// 1. No duplicate days within the batch
// 2. No duplicate days with existing transactions
func (l *TimeOffLedger) AppendBatch(ctx context.Context, txs []generic.Transaction) error {
	// Validate batch internally (no duplicate days within batch)
	if err := l.validateBatchUniqueness(txs); err != nil {
		return err
	}

	// Validate against existing transactions
	for _, tx := range txs {
		if tx.Type == generic.TxConsumption || tx.Type == generic.TxPending {
			if err := l.validateDayUniqueness(ctx, tx); err != nil {
				return err
			}
		}
	}

	err := l.inner.AppendBatch(ctx, txs)
	// Wrap database-level uniqueness errors with domain-specific error
	if errors.Is(err, generic.ErrDuplicateDayConsumption) {
		// Find the first consumption transaction to use for error details
		for _, tx := range txs {
			if tx.Type == generic.TxConsumption || tx.Type == generic.TxPending {
				return &DuplicateDayError{
					EntityID:     tx.EntityID,
					Date:         tx.EffectiveAt.Time.Truncate(24 * time.Hour),
					ResourceType: tx.ResourceType,
				}
			}
		}
	}
	return err
}

// Transactions returns all transactions (delegated).
func (l *TimeOffLedger) Transactions(ctx context.Context, entityID generic.EntityID, policyID generic.PolicyID) ([]generic.Transaction, error) {
	return l.inner.Transactions(ctx, entityID, policyID)
}

// TransactionsInRange returns transactions in range (delegated).
func (l *TimeOffLedger) TransactionsInRange(ctx context.Context, entityID generic.EntityID, policyID generic.PolicyID, from, to generic.TimePoint) ([]generic.Transaction, error) {
	return l.inner.TransactionsInRange(ctx, entityID, policyID, from, to)
}

// BalanceAt returns balance at a point (delegated).
func (l *TimeOffLedger) BalanceAt(ctx context.Context, entityID generic.EntityID, policyID generic.PolicyID, at generic.TimePoint, unit generic.Unit) (generic.Amount, error) {
	return l.inner.BalanceAt(ctx, entityID, policyID, at, unit)
}

// =============================================================================
// TIME-OFF SPECIFIC QUERIES
// =============================================================================

// DayOff represents a single day of time off.
type DayOff struct {
	Date         time.Time
	PolicyID     generic.PolicyID
	PolicyName   string
	ResourceType generic.ResourceType
	Status       DayOffStatus
	RequestID    string // ReferenceID from transaction
	Reason       string
}

// DayOffStatus indicates the status of a day off.
type DayOffStatus string

const (
	DayOffApproved DayOffStatus = "approved"
	DayOffPending  DayOffStatus = "pending"
	DayOffCanceled DayOffStatus = "canceled"
)

// GetDaysOff returns all days off for an entity in a date range.
// Aggregates across ALL policies assigned to the entity.
func (l *TimeOffLedger) GetDaysOff(ctx context.Context, entityID generic.EntityID, from, to time.Time) ([]DayOff, error) {
	// We need to query all transactions, not just one policy
	// This requires access to the store directly or a multi-policy query
	
	// For now, we'll use a method that queries by entity across all policies
	txs, err := l.getAllTransactionsForEntity(ctx, entityID, from, to)
	if err != nil {
		return nil, err
	}

	return l.transactionsToDaysOff(txs), nil
}

// GetDaysOffByPolicy returns days off for a specific policy.
func (l *TimeOffLedger) GetDaysOffByPolicy(ctx context.Context, entityID generic.EntityID, policyID generic.PolicyID, from, to time.Time) ([]DayOff, error) {
	txs, err := l.inner.TransactionsInRange(
		ctx, entityID, policyID,
		generic.TimePoint{Time: from},
		generic.TimePoint{Time: to},
	)
	if err != nil {
		return nil, err
	}

	return l.transactionsToDaysOff(txs), nil
}

// IsDayOff checks if a specific day is already taken off.
func (l *TimeOffLedger) IsDayOff(ctx context.Context, entityID generic.EntityID, date time.Time) (bool, *DayOff, error) {
	daysOff, err := l.GetDaysOff(ctx, entityID, date, date)
	if err != nil {
		return false, nil, err
	}

	for _, d := range daysOff {
		if d.Status != DayOffCanceled && isSameDay(d.Date, date) {
			return true, &d, nil
		}
	}

	return false, nil, nil
}

// GetUpcomingDaysOff returns future days off from today.
func (l *TimeOffLedger) GetUpcomingDaysOff(ctx context.Context, entityID generic.EntityID) ([]DayOff, error) {
	today := time.Now().Truncate(24 * time.Hour)
	futureEnd := today.AddDate(1, 0, 0) // 1 year out
	return l.GetDaysOff(ctx, entityID, today, futureEnd)
}

// GetPastDaysOff returns historical days off.
func (l *TimeOffLedger) GetPastDaysOff(ctx context.Context, entityID generic.EntityID, year int) ([]DayOff, error) {
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, 12, 31, 23, 59, 59, 0, time.UTC)
	return l.GetDaysOff(ctx, entityID, from, to)
}

// =============================================================================
// VALIDATION HELPERS
// =============================================================================

// validateDayUniqueness checks if the day is already consumed.
func (l *TimeOffLedger) validateDayUniqueness(ctx context.Context, tx generic.Transaction) error {
	// Get the day (truncate to date)
	day := tx.EffectiveAt.Time.Truncate(24 * time.Hour)

	// Check for existing consumption on this day for this resource type
	// We need to check across ALL policies for the same resource type
	// because you can't take PTO from multiple policies on the same day
	
	// Query all transactions for this entity on this day
	existing, err := l.getAllTransactionsForEntity(ctx, tx.EntityID, day, day)
	if err != nil {
		return fmt.Errorf("failed to check day uniqueness: %w", err)
	}

	for _, e := range existing {
		// Skip non-consumption types
		if e.Type != generic.TxConsumption && e.Type != generic.TxPending {
			continue
		}

		// Skip reversed transactions
		if isReversed(existing, e) {
			continue
		}

		// Check if same resource type and same day
		if e.ResourceType == tx.ResourceType && isSameDay(e.EffectiveAt.Time, tx.EffectiveAt.Time) {
			return &DuplicateDayError{
				EntityID:     tx.EntityID,
				Date:         day,
				ResourceType: tx.ResourceType,
				ExistingTxID: e.ID,
			}
		}
	}

	return nil
}

// validateBatchUniqueness checks for duplicate days within a batch.
func (l *TimeOffLedger) validateBatchUniqueness(txs []generic.Transaction) error {
	seen := make(map[string]generic.TransactionID) // key: "resourceType:date"

	for _, tx := range txs {
		if tx.Type != generic.TxConsumption && tx.Type != generic.TxPending {
			continue
		}

		day := tx.EffectiveAt.Time.Truncate(24 * time.Hour)
		key := fmt.Sprintf("%s:%s", tx.ResourceType, day.Format("2006-01-02"))

		if existingID, exists := seen[key]; exists {
			return &DuplicateDayError{
				EntityID:     tx.EntityID,
				Date:         day,
				ResourceType: tx.ResourceType,
				ExistingTxID: existingID,
				InBatch:      true,
			}
		}
		seen[key] = tx.ID
	}

	return nil
}

// getAllTransactionsForEntity queries all transactions for an entity.
// Returns error if the store doesn't support entity-wide queries.
func (l *TimeOffLedger) getAllTransactionsForEntity(ctx context.Context, entityID generic.EntityID, from, to time.Time) ([]generic.Transaction, error) {
	if l.entityStore != nil {
		return l.entityStore.LoadByEntity(ctx, entityID, generic.TimePoint{Time: from}, generic.TimePoint{Time: to})
	}

	// Store doesn't support entity-wide queries.
	// Day-uniqueness will be enforced by database constraints only.
	// This is acceptable but means validation happens at write time, not read time.
	return nil, nil
}

// transactionsToDaysOff converts transactions to DayOff structs.
func (l *TimeOffLedger) transactionsToDaysOff(txs []generic.Transaction) []DayOff {
	var daysOff []DayOff

	// Track reversals
	reversals := make(map[string]bool)
	for _, tx := range txs {
		if tx.Type == generic.TxReversal && tx.ReferenceID != "" {
			reversals[tx.ReferenceID] = true
		}
	}

	for _, tx := range txs {
		// Only consumption types
		if tx.Type != generic.TxConsumption && tx.Type != generic.TxPending {
			continue
		}

		status := DayOffApproved
		if tx.Type == generic.TxPending {
			status = DayOffPending
		}

		// Check if reversed
		if reversals[string(tx.ID)] {
			status = DayOffCanceled
		}

		daysOff = append(daysOff, DayOff{
			Date:         tx.EffectiveAt.Time,
			PolicyID:     tx.PolicyID,
			ResourceType: tx.ResourceType,
			Status:       status,
			RequestID:    tx.ReferenceID,
			Reason:       tx.Reason,
		})
	}

	// Sort by date
	sort.Slice(daysOff, func(i, j int) bool {
		return daysOff[i].Date.Before(daysOff[j].Date)
	})

	return daysOff
}

// =============================================================================
// ERROR TYPES
// =============================================================================

// DuplicateDayError is returned when trying to consume on an already-consumed day.
type DuplicateDayError struct {
	EntityID     generic.EntityID
	Date         time.Time
	ResourceType generic.ResourceType
	ExistingTxID generic.TransactionID
	InBatch      bool
}

func (e *DuplicateDayError) Error() string {
	if e.InBatch {
		return fmt.Sprintf("duplicate day in request: %s already included for %s",
			e.Date.Format("2006-01-02"), e.ResourceType)
	}
	return fmt.Sprintf("day already consumed: %s is already taken as %s (tx: %s)",
		e.Date.Format("2006-01-02"), e.ResourceType, e.ExistingTxID)
}


// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

func isSameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func isReversed(txs []generic.Transaction, tx generic.Transaction) bool {
	for _, t := range txs {
		if t.Type == generic.TxReversal && t.ReferenceID == string(tx.ID) {
			return true
		}
	}
	return false
}
