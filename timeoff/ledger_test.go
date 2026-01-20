package timeoff_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/store/sqlite"
	"github.com/warp/resource-engine/timeoff"
)

// =============================================================================
// TEST SETUP
// =============================================================================

func newTestTimeOffLedger(t *testing.T) (*timeoff.TimeOffLedger, *sqlite.Store) {
	store, err := sqlite.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	ledger := timeoff.NewTimeOffLedger(store)
	return ledger, store
}

func ptoTx(entityID string, policyID string, date time.Time, txID string) generic.Transaction {
	return generic.Transaction{
		ID:             generic.TransactionID(txID),
		EntityID:       generic.EntityID(entityID),
		PolicyID:       generic.PolicyID(policyID),
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    generic.TimePoint{Time: date},
		Delta:          generic.NewAmount(-1, generic.UnitDays),
		Type:           generic.TxConsumption,
		IdempotencyKey: txID,
	}
}

func sickTx(entityID string, policyID string, date time.Time, txID string) generic.Transaction {
	return generic.Transaction{
		ID:             generic.TransactionID(txID),
		EntityID:       generic.EntityID(entityID),
		PolicyID:       generic.PolicyID(policyID),
		ResourceType:   timeoff.ResourceSick,
		EffectiveAt:    generic.TimePoint{Time: date},
		Delta:          generic.NewAmount(-1, generic.UnitDays),
		Type:           generic.TxConsumption,
		IdempotencyKey: txID,
	}
}

// =============================================================================
// UNIQUENESS INVARIANT TESTS
// =============================================================================

func TestTimeOffLedger_DuplicateDay_SinglePolicy_Rejected(t *testing.T) {
	// GIVEN: Employee already took March 10 off
	// WHEN: Trying to take March 10 off again (same policy)
	// THEN: Request is rejected with DuplicateDayError

	ledger, _ := newTestTimeOffLedger(t)
	ctx := context.Background()

	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	// First request - should succeed
	tx1 := ptoTx("emp-1", "pto", march10, "tx-1")
	err := ledger.Append(ctx, tx1)
	assert.NoError(t, err, "first request should succeed")

	// Second request for same day - should fail
	tx2 := ptoTx("emp-1", "pto", march10, "tx-2")
	err = ledger.Append(ctx, tx2)

	assert.Error(t, err, "duplicate day should be rejected")
	var dupErr *timeoff.DuplicateDayError
	assert.ErrorAs(t, err, &dupErr, "should be DuplicateDayError")
	assert.Equal(t, march10.Truncate(24*time.Hour), dupErr.Date)
}

func TestTimeOffLedger_DuplicateDay_MultiplePolicy_Rejected(t *testing.T) {
	// GIVEN: Employee took March 10 off from "carryover" policy
	// WHEN: Trying to take March 10 off from "standard" policy
	// THEN: Request is rejected (can't be off twice on same day)

	ledger, _ := newTestTimeOffLedger(t)
	ctx := context.Background()

	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	// First request from carryover policy
	tx1 := ptoTx("emp-1", "carryover", march10, "tx-1")
	err := ledger.Append(ctx, tx1)
	assert.NoError(t, err)

	// Second request from standard policy (same day, different policy)
	tx2 := ptoTx("emp-1", "standard", march10, "tx-2")
	err = ledger.Append(ctx, tx2)

	assert.Error(t, err, "same day from different policy should be rejected")
}

func TestTimeOffLedger_DifferentResourceTypes_Allowed(t *testing.T) {
	// GIVEN: Employee took March 10 as PTO
	// WHEN: Also requesting March 10 as SICK leave (which is weird but technically possible)
	// THEN: Request succeeds (different resource types are independent)
	//
	// NOTE: This tests the design decision that PTO and Sick are separate resources.
	// In some systems, you might want to prevent this. If so, make them same resource type.

	ledger, _ := newTestTimeOffLedger(t)
	ctx := context.Background()

	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	// PTO on March 10
	tx1 := ptoTx("emp-1", "pto", march10, "tx-1")
	err := ledger.Append(ctx, tx1)
	assert.NoError(t, err)

	// Sick on March 10 (different resource type)
	tx2 := sickTx("emp-1", "sick", march10, "tx-2")
	err = ledger.Append(ctx, tx2)

	// This should succeed because they're different resource types
	// If you want to prevent this, both would need to be same resource type (e.g., "time_off")
	assert.NoError(t, err, "different resource types on same day should be allowed")
}

func TestTimeOffLedger_DifferentDays_Allowed(t *testing.T) {
	// GIVEN: Employee took March 10 off
	// WHEN: Requesting March 11 off
	// THEN: Request succeeds (different days)

	ledger, _ := newTestTimeOffLedger(t)
	ctx := context.Background()

	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)
	march11 := time.Date(2025, time.March, 11, 0, 0, 0, 0, time.UTC)

	tx1 := ptoTx("emp-1", "pto", march10, "tx-1")
	tx2 := ptoTx("emp-1", "pto", march11, "tx-2")

	assert.NoError(t, ledger.Append(ctx, tx1))
	assert.NoError(t, ledger.Append(ctx, tx2))
}

func TestTimeOffLedger_DifferentEmployees_Allowed(t *testing.T) {
	// GIVEN: Employee A took March 10 off
	// WHEN: Employee B requests March 10 off
	// THEN: Request succeeds (different employees)

	ledger, _ := newTestTimeOffLedger(t)
	ctx := context.Background()

	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	tx1 := ptoTx("emp-1", "pto", march10, "tx-1")
	tx2 := ptoTx("emp-2", "pto", march10, "tx-2")

	assert.NoError(t, ledger.Append(ctx, tx1))
	assert.NoError(t, ledger.Append(ctx, tx2))
}

// =============================================================================
// BATCH UNIQUENESS TESTS
// =============================================================================

func TestTimeOffLedger_BatchAppend_NoDuplicatesInBatch(t *testing.T) {
	// GIVEN: Empty ledger
	// WHEN: Submitting batch with same day twice
	// THEN: Entire batch is rejected

	ledger, _ := newTestTimeOffLedger(t)
	ctx := context.Background()

	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	txs := []generic.Transaction{
		ptoTx("emp-1", "pto", march10, "tx-1"),
		ptoTx("emp-1", "pto", march10, "tx-2"), // Duplicate day in same batch
	}

	err := ledger.AppendBatch(ctx, txs)

	assert.Error(t, err)
	var dupErr *timeoff.DuplicateDayError
	assert.ErrorAs(t, err, &dupErr)
	assert.True(t, dupErr.InBatch, "should indicate duplicate was in batch")
}

func TestTimeOffLedger_BatchAppend_ValidMultipleDays(t *testing.T) {
	// GIVEN: Empty ledger
	// WHEN: Submitting batch with different days
	// THEN: All succeed

	ledger, _ := newTestTimeOffLedger(t)
	ctx := context.Background()

	txs := []generic.Transaction{
		ptoTx("emp-1", "pto", time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC), "tx-1"),
		ptoTx("emp-1", "pto", time.Date(2025, time.March, 11, 0, 0, 0, 0, time.UTC), "tx-2"),
		ptoTx("emp-1", "pto", time.Date(2025, time.March, 12, 0, 0, 0, 0, time.UTC), "tx-3"),
	}

	err := ledger.AppendBatch(ctx, txs)
	assert.NoError(t, err)
}

func TestTimeOffLedger_BatchAppend_ConflictsWithExisting(t *testing.T) {
	// GIVEN: March 10 already taken
	// WHEN: Submitting batch that includes March 10
	// THEN: Entire batch is rejected

	ledger, _ := newTestTimeOffLedger(t)
	ctx := context.Background()

	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)
	march11 := time.Date(2025, time.March, 11, 0, 0, 0, 0, time.UTC)

	// Pre-existing transaction
	existing := ptoTx("emp-1", "pto", march10, "existing")
	require.NoError(t, ledger.Append(ctx, existing))

	// Try batch that conflicts
	txs := []generic.Transaction{
		ptoTx("emp-1", "pto", march10, "tx-1"), // Conflicts!
		ptoTx("emp-1", "pto", march11, "tx-2"), // Would be OK alone
	}

	err := ledger.AppendBatch(ctx, txs)
	assert.Error(t, err, "batch with conflicting day should fail")

	// Verify nothing was added
	allTxs, _ := ledger.Transactions(ctx, "emp-1", "pto")
	assert.Len(t, allTxs, 1, "batch should be atomic - nothing added on failure")
}

// =============================================================================
// ACCRUAL TRANSACTIONS (no uniqueness constraint)
// =============================================================================

func TestTimeOffLedger_AccrualTransactions_NoUniquenessCheck(t *testing.T) {
	// GIVEN: Multiple accruals on same day
	// WHEN: Appending them
	// THEN: All succeed (accruals don't have uniqueness constraint)

	ledger, _ := newTestTimeOffLedger(t)
	ctx := context.Background()

	jan1 := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)

	accrual1 := generic.Transaction{
		ID:             "tx-accrual-1",
		EntityID:       "emp-1",
		PolicyID:       "pto",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    generic.TimePoint{Time: jan1},
		Delta:          generic.NewAmount(10, generic.UnitDays),
		Type:           generic.TxGrant,
		IdempotencyKey: "accrual-1",
	}

	accrual2 := generic.Transaction{
		ID:             "tx-accrual-2",
		EntityID:       "emp-1",
		PolicyID:       "carryover",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    generic.TimePoint{Time: jan1},
		Delta:          generic.NewAmount(5, generic.UnitDays),
		Type:           generic.TxGrant,
		IdempotencyKey: "accrual-2",
	}

	assert.NoError(t, ledger.Append(ctx, accrual1))
	assert.NoError(t, ledger.Append(ctx, accrual2))
}

// =============================================================================
// PENDING TRANSACTIONS
// =============================================================================

func TestTimeOffLedger_PendingTransaction_BlocksDay(t *testing.T) {
	// GIVEN: Pending request for March 10
	// WHEN: Trying to submit another request for March 10
	// THEN: Request is rejected (pending counts as reserved)

	ledger, _ := newTestTimeOffLedger(t)
	ctx := context.Background()

	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	pending := generic.Transaction{
		ID:             "tx-pending",
		EntityID:       "emp-1",
		PolicyID:       "pto",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    generic.TimePoint{Time: march10},
		Delta:          generic.NewAmount(-1, generic.UnitDays),
		Type:           generic.TxPending, // Pending, not yet approved
		IdempotencyKey: "pending-1",
	}

	require.NoError(t, ledger.Append(ctx, pending))

	// Try another request for same day
	another := ptoTx("emp-1", "pto", march10, "tx-another")
	err := ledger.Append(ctx, another)

	assert.Error(t, err, "pending request should block the day")
}

// =============================================================================
// DAYS OFF QUERY TESTS
// =============================================================================

func TestTimeOffLedger_GetDaysOff_ReturnsAllDays(t *testing.T) {
	ledger, store := newTestTimeOffLedger(t)
	ctx := context.Background()

	// Setup some days off
	days := []time.Time{
		time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2025, time.March, 11, 0, 0, 0, 0, time.UTC),
		time.Date(2025, time.March, 12, 0, 0, 0, 0, time.UTC),
	}

	for i, d := range days {
		tx := ptoTx("emp-1", "pto", d, "tx-"+string(rune('a'+i)))
		require.NoError(t, ledger.Append(ctx, tx))
	}

	// Query days off using direct store query
	from := time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, time.March, 31, 0, 0, 0, 0, time.UTC)

	consumedDays, err := store.GetConsumedDays(ctx, "emp-1", timeoff.ResourcePTO,
		generic.TimePoint{Time: from}, generic.TimePoint{Time: to})
	require.NoError(t, err)

	assert.Len(t, consumedDays, 3)
}

func TestTimeOffLedger_IsDayConsumed_True(t *testing.T) {
	ledger, store := newTestTimeOffLedger(t)
	ctx := context.Background()

	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	tx := ptoTx("emp-1", "pto", march10, "tx-1")
	require.NoError(t, ledger.Append(ctx, tx))

	consumed, txID, err := store.IsDayConsumed(ctx, "emp-1", timeoff.ResourcePTO, generic.TimePoint{Time: march10})
	require.NoError(t, err)

	assert.True(t, consumed, "day should be consumed")
	assert.Equal(t, generic.TransactionID("tx-1"), txID)
}

func TestTimeOffLedger_IsDayConsumed_False(t *testing.T) {
	_, store := newTestTimeOffLedger(t)
	ctx := context.Background()

	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	consumed, txID, err := store.IsDayConsumed(ctx, "emp-1", timeoff.ResourcePTO, generic.TimePoint{Time: march10})
	require.NoError(t, err)

	assert.False(t, consumed, "day should not be consumed")
	assert.Empty(t, txID)
}

// =============================================================================
// ERROR MESSAGE TESTS
// =============================================================================

func TestDuplicateDayError_Message(t *testing.T) {
	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	err := &timeoff.DuplicateDayError{
		EntityID:     "emp-1",
		Date:         march10,
		ResourceType: timeoff.ResourcePTO,
		ExistingTxID: "tx-existing",
	}

	msg := err.Error()
	assert.Contains(t, msg, "2025-03-10")
	assert.Contains(t, msg, "pto")
	assert.Contains(t, msg, "tx-existing")
}

func TestDuplicateDayError_InBatch_Message(t *testing.T) {
	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	err := &timeoff.DuplicateDayError{
		EntityID:     "emp-1",
		Date:         march10,
		ResourceType: timeoff.ResourcePTO,
		InBatch:      true,
	}

	msg := err.Error()
	assert.Contains(t, msg, "duplicate day in request")
}

// =============================================================================
// DATABASE CONSTRAINT TESTS
// =============================================================================

func TestDatabaseConstraint_DuplicateDay_DirectStore(t *testing.T) {
	// This test bypasses the TimeOffLedger validation to verify
	// that the database itself enforces the day uniqueness constraint.
	// This is the "last line of defense" against race conditions.

	store, err := sqlite.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	// Write first consumption directly to store (bypassing TimeOffLedger)
	tx1 := generic.Transaction{
		ID:             "tx-1",
		EntityID:       "emp-1",
		PolicyID:       "pto",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    generic.TimePoint{Time: march10},
		Delta:          generic.NewAmount(-1, generic.UnitDays),
		Type:           generic.TxConsumption,
		IdempotencyKey: "key-1",
	}
	err = store.Append(ctx, tx1)
	require.NoError(t, err, "first transaction should succeed")

	// Try second consumption on same day (different idempotency key)
	tx2 := generic.Transaction{
		ID:             "tx-2",
		EntityID:       "emp-1",
		PolicyID:       "pto",           // Same policy
		ResourceType:   timeoff.ResourcePTO,           // Same resource
		EffectiveAt:    generic.TimePoint{Time: march10}, // Same day!
		Delta:          generic.NewAmount(-1, generic.UnitDays),
		Type:           generic.TxConsumption,
		IdempotencyKey: "key-2",
	}
	err = store.Append(ctx, tx2)

	// Database constraint should catch this
	assert.Error(t, err, "database should reject duplicate day consumption")
	assert.ErrorIs(t, err, generic.ErrDuplicateDayConsumption,
		"should be ErrDuplicateDayConsumption, not idempotency error")
}

func TestDatabaseConstraint_SameDay_DifferentResourceType_Allowed(t *testing.T) {
	// Verify that the constraint allows different resource types on same day

	store, err := sqlite.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	march10 := time.Date(2025, time.March, 10, 0, 0, 0, 0, time.UTC)

	// PTO on March 10
	tx1 := generic.Transaction{
		ID:             "tx-1",
		EntityID:       "emp-1",
		PolicyID:       "pto",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    generic.TimePoint{Time: march10},
		Delta:          generic.NewAmount(-1, generic.UnitDays),
		Type:           generic.TxConsumption,
		IdempotencyKey: "key-1",
	}
	require.NoError(t, store.Append(ctx, tx1))

	// Sick on March 10 (different resource type)
	tx2 := generic.Transaction{
		ID:             "tx-2",
		EntityID:       "emp-1",
		PolicyID:       "sick",
		ResourceType:   timeoff.ResourceSick, // Different resource type
		EffectiveAt:    generic.TimePoint{Time: march10},
		Delta:          generic.NewAmount(-1, generic.UnitDays),
		Type:           generic.TxConsumption,
		IdempotencyKey: "key-2",
	}
	err = store.Append(ctx, tx2)

	// Should succeed - different resource types are independent
	assert.NoError(t, err, "different resource types on same day should be allowed")
}

func TestDatabaseConstraint_Accrual_NotConstrained(t *testing.T) {
	// Verify that accrual transactions are not constrained by day uniqueness

	store, err := sqlite.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	jan1 := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)

	// Two accruals on same day (common for multi-policy setups)
	tx1 := generic.Transaction{
		ID:             "tx-1",
		EntityID:       "emp-1",
		PolicyID:       "pto-standard",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    generic.TimePoint{Time: jan1},
		Delta:          generic.NewAmount(10, generic.UnitDays),
		Type:           generic.TxGrant, // Accrual, not consumption
		IdempotencyKey: "key-1",
	}
	require.NoError(t, store.Append(ctx, tx1))

	tx2 := generic.Transaction{
		ID:             "tx-2",
		EntityID:       "emp-1",
		PolicyID:       "pto-carryover",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    generic.TimePoint{Time: jan1},
		Delta:          generic.NewAmount(5, generic.UnitDays),
		Type:           generic.TxGrant, // Also accrual
		IdempotencyKey: "key-2",
	}
	err = store.Append(ctx, tx2)

	// Should succeed - accruals are not constrained
	assert.NoError(t, err, "multiple accruals on same day should be allowed")
}
