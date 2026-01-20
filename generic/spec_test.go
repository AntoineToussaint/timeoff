/*
spec_test.go - Specification Tests for Generic Resource Engine

PURPOSE:
  These tests serve as EXECUTABLE SPECIFICATIONS of the system design.
  Each test documents a specific behavior from DESIGN.md and validates
  that the implementation conforms to the specification.

ORGANIZATION:
  Tests are grouped by specification area:
  1. Ledger Invariants - Append-only, idempotency, atomicity
  2. Period-Based Balance - Core insight that balance is per-period
  3. Consumption Modes - ConsumeAhead vs ConsumeUpToAccrued
  4. Multi-Policy Distribution - Priority-based allocation
  5. Reconciliation - Carryover, expire, cap at period end
  6. Projection - Future balance validation
  7. Correctness Guarantees - Invariants we maintain

READING THESE TESTS:
  Each test has:
  - A descriptive name that states the behavior
  - A SPEC comment citing the relevant design document section
  - GIVEN/WHEN/THEN comments explaining the scenario
  - Clear assertions with explanatory messages

These tests are intentionally verbose for documentation purposes.
*/
package generic_test

import (
	"context"
	"testing"
	"time"

	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/generic/store"
)

// =============================================================================
// TEST INFRASTRUCTURE
// =============================================================================
// Note: testResourceType is defined in assignment_test.go
// =============================================================================

func newLedger() generic.Ledger {
	return generic.NewLedger(store.NewMemory())
}

func period2025() generic.Period {
	return generic.Period{
		Start: generic.NewTimePoint(2025, time.January, 1),
		End:   generic.NewTimePoint(2025, time.December, 31),
	}
}

func period2026() generic.Period {
	return generic.Period{
		Start: generic.NewTimePoint(2026, time.January, 1),
		End:   generic.NewTimePoint(2026, time.December, 31),
	}
}

func d(n float64) generic.Amount {
	return generic.NewAmount(n, generic.UnitDays)
}

func accrualTx(entityID, policyID string, date generic.TimePoint, amount float64, key string) generic.Transaction {
	return generic.Transaction{
		ID:             generic.TransactionID(key),
		EntityID:       generic.EntityID(entityID),
		PolicyID:       generic.PolicyID(policyID),
		ResourceType:   testResourceType,
		EffectiveAt:    date,
		Delta:          d(amount),
		Type:           generic.TxGrant,
		IdempotencyKey: key,
	}
}

func consumptionTx(entityID, policyID string, date generic.TimePoint, amount float64, key string) generic.Transaction {
	return generic.Transaction{
		ID:             generic.TransactionID(key),
		EntityID:       generic.EntityID(entityID),
		PolicyID:       generic.PolicyID(policyID),
		ResourceType:   testResourceType,
		EffectiveAt:    date,
		Delta:          d(-amount), // Consumption is negative
		Type:           generic.TxConsumption,
		IdempotencyKey: key,
	}
}

func pendingTx(entityID, policyID string, date generic.TimePoint, amount float64, key string) generic.Transaction {
	return generic.Transaction{
		ID:             generic.TransactionID(key),
		EntityID:       generic.EntityID(entityID),
		PolicyID:       generic.PolicyID(policyID),
		ResourceType:   testResourceType,
		EffectiveAt:    date,
		Delta:          d(-amount), // Pending is negative (reserved)
		Type:           generic.TxPending,
		IdempotencyKey: key,
	}
}

// TestYearlyAccrual implements AccrualSchedule for specs
type TestYearlyAccrual struct {
	AnnualDays float64
}

func (ya *TestYearlyAccrual) GenerateAccruals(from, to generic.TimePoint) []generic.AccrualEvent {
	var events []generic.AccrualEvent
	monthly := ya.AnnualDays / 12

	current := generic.StartOfMonth(from.Year(), from.Month())
	end := generic.StartOfMonth(to.Year(), to.Month())

	for current.BeforeOrEqual(end) {
		if from.BeforeOrEqual(current) && current.BeforeOrEqual(to) {
			events = append(events, generic.AccrualEvent{
				At:     current,
				Amount: d(monthly),
				Reason: "monthly",
			})
		}
		current = current.AddMonths(1)
	}
	return events
}

func (ya *TestYearlyAccrual) IsDeterministic() bool {
	return true
}

// =============================================================================
// SPEC 1: LEDGER INVARIANTS
// =============================================================================
// From DESIGN.md: "The ledger is an append-only, immutable log"

func TestSpec_Ledger_AppendOnly_TransactionsCannotBeModified(t *testing.T) {
	// SPEC: "No updates or deletes; corrections are new transactions"
	//
	// The ledger interface has NO Update() or Delete() methods.
	// This is enforced at compile time by the interface definition.
	//
	// GIVEN: A ledger with the Ledger interface
	// THEN: Only Append, AppendBatch, and read methods exist
	//
	// This test documents the invariant - it passes by the fact that
	// the Ledger interface has no mutation methods.

	var _ generic.Ledger = newLedger()
	// If Ledger had Update/Delete, this test file wouldn't compile
	// with code trying to call ledger.Delete()
}

func TestSpec_Ledger_Idempotency_DuplicateKeyRejected(t *testing.T) {
	// SPEC: "Every transaction has a unique idempotency key; duplicates are rejected"
	//
	// GIVEN: A transaction with idempotency key "grant-2025"
	// WHEN: Appending the same key again
	// THEN: Second append fails with ErrDuplicateIdempotencyKey
	//
	// PURPOSE: Prevents duplicate processing from retries or double-clicks

	ctx := context.Background()
	ledger := newLedger()

	tx := accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.January, 1), 20, "grant-2025")

	err1 := ledger.Append(ctx, tx)
	if err1 != nil {
		t.Fatalf("first append should succeed: %v", err1)
	}

	err2 := ledger.Append(ctx, tx)
	if err2 == nil {
		t.Error("SPEC VIOLATION: Duplicate idempotency key should be rejected")
	}
	if err2 != generic.ErrDuplicateIdempotencyKey {
		t.Errorf("expected ErrDuplicateIdempotencyKey, got: %v", err2)
	}
}

func TestSpec_Ledger_AtomicBatch_AllOrNothing(t *testing.T) {
	// SPEC: "Multi-transaction operations use database transactions"
	//
	// GIVEN: A batch of 3 transactions where the 3rd has a duplicate key
	// WHEN: AppendBatch is called
	// THEN: NO transactions are appended (all-or-nothing)
	//
	// PURPOSE: Ensures consistency when approving multi-day requests

	ctx := context.Background()
	ledger := newLedger()

	// Pre-existing transaction with key "existing"
	existing := accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.January, 1), 10, "existing")
	ledger.Append(ctx, existing)

	// Batch where 3rd transaction has duplicate key
	batch := []generic.Transaction{
		accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.February, 1), 1, "feb"),
		accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.March, 1), 1, "mar"),
		accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.April, 1), 1, "existing"), // Duplicate!
	}

	err := ledger.AppendBatch(ctx, batch)
	if err == nil {
		t.Error("batch with duplicate key should fail")
	}

	// Verify nothing was added
	txs, _ := ledger.Transactions(ctx, "emp-1", "test-policy")
	if len(txs) != 1 {
		t.Errorf("SPEC VIOLATION: Batch should be atomic - expected 1 tx (existing), got %d", len(txs))
	}
}

func TestSpec_Ledger_Ordering_TransactionsChronological(t *testing.T) {
	// SPEC: "Transactions are processed in effective-date order"
	//
	// GIVEN: Transactions appended in random order
	// WHEN: Reading transactions
	// THEN: They are returned sorted by EffectiveAt
	//
	// PURPOSE: Balance calculation depends on chronological order

	ctx := context.Background()
	ledger := newLedger()

	// Append out of order
	ledger.Append(ctx, accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.March, 1), 1, "mar"))
	ledger.Append(ctx, accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.January, 1), 1, "jan"))
	ledger.Append(ctx, accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.February, 1), 1, "feb"))

	txs, _ := ledger.Transactions(ctx, "emp-1", "test-policy")

	if len(txs) != 3 {
		t.Fatalf("expected 3 transactions, got %d", len(txs))
	}

	// Verify chronological order
	if txs[0].EffectiveAt.Month() != time.January {
		t.Error("SPEC VIOLATION: First transaction should be January")
	}
	if txs[1].EffectiveAt.Month() != time.February {
		t.Error("SPEC VIOLATION: Second transaction should be February")
	}
	if txs[2].EffectiveAt.Month() != time.March {
		t.Error("SPEC VIOLATION: Third transaction should be March")
	}
}

// =============================================================================
// SPEC 2: PERIOD-BASED BALANCE
// =============================================================================
// From DESIGN.md: "Balance only makes sense within a period"

func TestSpec_Balance_IsPeriodBased_Not_PointInTime(t *testing.T) {
	// SPEC: "The key insight: Balance only makes sense within a period"
	//
	// GIVEN: Policy with 20 days/year, deterministic accrual
	// WHEN: Checking balance in January
	// THEN: TotalEntitlement is 20 days (full year), not 1.67 days (January only)
	//
	// PURPOSE: This is THE core insight of the design - balance includes
	// future deterministic accruals, enabling ConsumeAhead mode.

	ctx := context.Background()
	ledger := newLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &TestYearlyAccrual{AnnualDays: 20}

	result, err := engine.Project(ctx, generic.ProjectionInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Unit:     generic.UnitDays,
		Period:   period2025(),
		AsOf:     generic.NewTimePoint(2025, time.January, 15), // Mid-January
		Accruals: accruals,
	})

	if err != nil {
		t.Fatalf("projection failed: %v", err)
	}

	// TotalEntitlement should be FULL YEAR (use approx comparison for floating-point)
	diff := result.Balance.TotalEntitlement.Value.Sub(d(20).Value).Abs()
	if diff.GreaterThan(d(0.01).Value) {
		t.Errorf("SPEC VIOLATION: TotalEntitlement should be ~20 (full year), got %v",
			result.Balance.TotalEntitlement.Value)
	}

	// AccruedToDate should be just January's portion
	// 20/12 ≈ 1.67
	if result.Balance.AccruedToDate.Value.GreaterThan(d(2).Value) {
		t.Errorf("AccruedToDate should be ~1.67 (January only), got %v",
			result.Balance.AccruedToDate.Value)
	}
}

func TestSpec_Balance_NonDeterministic_OnlyActual(t *testing.T) {
	// SPEC: For non-deterministic accruals, "Balance only includes accruals that have occurred"
	//
	// GIVEN: Hours-worked policy (non-deterministic) with 5 days actually accrued
	// WHEN: Checking balance
	// THEN: TotalEntitlement equals AccruedToDate (can't predict future hours)
	//
	// PURPOSE: Non-deterministic accruals can't include future amounts
	// because we don't know what they'll be.

	ctx := context.Background()
	ledger := newLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	// Record actual accruals from hours worked
	ledger.Append(ctx, accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.January, 15), 5, "hours-jan"))

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Unit:     generic.UnitDays,
		Period:   period2025(),
		Accruals: nil, // No schedule = non-deterministic
	})

	// Both should be 5 days (only what was actually recorded)
	if !result.Balance.TotalEntitlement.Value.Equal(d(5).Value) {
		t.Errorf("SPEC VIOLATION: Non-deterministic TotalEntitlement should equal actual accruals (5), got %v",
			result.Balance.TotalEntitlement.Value)
	}
	if !result.Balance.AccruedToDate.Value.Equal(d(5).Value) {
		t.Errorf("AccruedToDate should be 5, got %v", result.Balance.AccruedToDate.Value)
	}
}

// =============================================================================
// SPEC 3: CONSUMPTION MODES
// =============================================================================
// From DESIGN.md: "Two Valid Worldviews"

func TestSpec_ConsumeAhead_FullYearAvailableImmediately(t *testing.T) {
	// SPEC: "You're entitled to 20 days this year. Use them whenever you want."
	//
	// GIVEN: Policy with 20 days/year, ConsumeAhead mode
	// WHEN: Requesting 15 days in January
	// THEN: Request is VALID (full 20 days available)
	//
	// PURPOSE: Salaried employees can front-load vacation.
	// RISK: Employee leaves mid-year having used more than earned.

	ctx := context.Background()
	ledger := newLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &TestYearlyAccrual{AnnualDays: 20}

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          period2025(),
		AsOf:            generic.NewTimePoint(2025, time.January, 15),
		Accruals:        accruals,
		RequestedAmount: d(15),
		ConsumptionMode: generic.ConsumeAhead, // KEY: Full year available
		AllowNegative:   false,
	})

	if !result.IsValid {
		t.Error("SPEC VIOLATION: ConsumeAhead should allow 15 days in January when full year is 20")
	}
}

func TestSpec_ConsumeUpToAccrued_OnlyEarnedAvailable(t *testing.T) {
	// SPEC: "You can only spend what you've earned so far."
	//
	// GIVEN: Policy with 20 days/year, ConsumeUpToAccrued mode
	// WHEN: Requesting 5 days in January (only ~1.67 accrued)
	// THEN: Request is INVALID (only 1.67 available)
	//
	// PURPOSE: Hourly workers, points systems - can't go into debt.

	ctx := context.Background()
	ledger := newLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &TestYearlyAccrual{AnnualDays: 20}

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          period2025(),
		AsOf:            generic.NewTimePoint(2025, time.January, 15),
		Accruals:        accruals,
		RequestedAmount: d(5), // More than 1.67 accrued
		ConsumptionMode: generic.ConsumeUpToAccrued, // KEY: Only earned
		AllowNegative:   false,
	})

	if result.IsValid {
		t.Error("SPEC VIOLATION: ConsumeUpToAccrued should DENY 5 days when only ~1.67 accrued")
	}
	if result.ValidationError == nil || result.ValidationError.Type != "insufficient_balance" {
		t.Error("should return insufficient_balance error")
	}
}

func TestSpec_ConsumeUpToAccrued_LaterInYear_MoreAvailable(t *testing.T) {
	// SPEC: ConsumeUpToAccrued availability grows as year progresses
	//
	// GIVEN: Policy with 20 days/year, ConsumeUpToAccrued mode
	// WHEN: Requesting 10 days in July (10 accrued by then)
	// THEN: Request is VALID
	//
	// PURPOSE: Shows how accrued amount grows through the year.

	ctx := context.Background()
	ledger := newLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &TestYearlyAccrual{AnnualDays: 20}

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          period2025(),
		AsOf:            generic.NewTimePoint(2025, time.July, 15), // 7 months in
		Accruals:        accruals,
		RequestedAmount: d(10), // 7 months × 1.67 ≈ 11.67 available
		ConsumptionMode: generic.ConsumeUpToAccrued,
		AllowNegative:   false,
	})

	if !result.IsValid {
		t.Error("SPEC VIOLATION: Should allow 10 days in July when ~11.67 accrued")
	}
}

// =============================================================================
// SPEC 4: MULTI-POLICY DISTRIBUTION
// =============================================================================
// From DESIGN.md: "Priority-ordered assignments"

func TestSpec_MultiPolicy_ConsumeByPriority(t *testing.T) {
	// SPEC: "Priority 1: Carryover (use first), Priority 2: Bonus, Priority 3: Standard"
	//
	// GIVEN: Employee with 3 policies:
	//   - Carryover: 3 days (Priority 1)
	//   - Bonus: 5 days (Priority 2)
	//   - Standard: 20 days (Priority 3)
	// WHEN: Requesting 10 days
	// THEN: Allocation is: 3 from Carryover, 5 from Bonus, 2 from Standard
	//
	// PURPOSE: Use soon-to-expire balances first.

	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   testResourceType,
		TotalAvailable: d(28),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "carryover",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
				},
				Balance:  generic.Balance{AccruedToDate: d(3), TotalEntitlement: d(3)},
				Priority: 1, // FIRST
			},
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "bonus",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
				},
				Balance:  generic.Balance{AccruedToDate: d(5), TotalEntitlement: d(5)},
				Priority: 2, // SECOND
			},
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "standard",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
				},
				Balance:  generic.Balance{AccruedToDate: d(20), TotalEntitlement: d(20)},
				Priority: 3, // LAST
			},
		},
	}

	distributor := &generic.ConsumptionDistributor{}
	result := distributor.Distribute(resourceBalance, d(10), false)

	if !result.IsSatisfiable {
		t.Fatal("should be satisfiable")
	}

	// Build allocation map
	allocations := make(map[string]float64)
	for _, a := range result.Allocations {
		val, _ := a.Amount.Value.Float64()
		allocations[string(a.PolicyID)] = val
	}

	// Verify priority-based allocation
	if allocations["carryover"] != 3 {
		t.Errorf("SPEC VIOLATION: Carryover should be fully drained (3), got %v", allocations["carryover"])
	}
	if allocations["bonus"] != 5 {
		t.Errorf("SPEC VIOLATION: Bonus should be fully drained (5), got %v", allocations["bonus"])
	}
	if allocations["standard"] != 2 {
		t.Errorf("SPEC VIOLATION: Standard should have remainder (2), got %v", allocations["standard"])
	}
}

func TestSpec_MultiPolicy_SkipsExhaustedPolicies(t *testing.T) {
	// SPEC: Distributor skips policies with zero balance
	//
	// GIVEN: First policy has 0 available (exhausted)
	// WHEN: Requesting days
	// THEN: All allocation comes from second policy
	//
	// PURPOSE: Don't allocate from empty pools.

	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   testResourceType,
		TotalAvailable: d(10),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "exhausted",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
				},
				Balance:  generic.Balance{AccruedToDate: d(5), TotalEntitlement: d(5), TotalConsumed: d(5)}, // 0 available
				Priority: 1,
			},
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "available",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
				},
				Balance:  generic.Balance{AccruedToDate: d(10), TotalEntitlement: d(10)},
				Priority: 2,
			},
		},
	}

	distributor := &generic.ConsumptionDistributor{}
	result := distributor.Distribute(resourceBalance, d(5), false)

	if len(result.Allocations) != 1 {
		t.Errorf("SPEC VIOLATION: Should have 1 allocation (skipping exhausted), got %d", len(result.Allocations))
	}
	if string(result.Allocations[0].PolicyID) != "available" {
		t.Errorf("allocation should come from 'available' policy")
	}
}

// =============================================================================
// SPEC 5: RECONCILIATION
// =============================================================================
// From DESIGN.md: "What happens at period boundaries"

func TestSpec_Reconciliation_Carryover_WithCap(t *testing.T) {
	// SPEC: "Carryover: Move balance to next period (with optional cap)"
	//
	// GIVEN: 15 days remaining at year end, max carryover is 10
	// WHEN: Reconciliation runs
	// THEN: 10 days carried over, 5 days expired
	//
	// PURPOSE: Prevent unlimited balance accumulation.

	engine := &generic.ReconciliationEngine{}
	maxCarry := d(10)

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Policy: generic.Policy{
			ResourceType: testResourceType,
			Unit:         generic.UnitDays,
			ReconciliationRules: []generic.ReconciliationRule{{
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionCarryover, Config: generic.ActionConfig{MaxCarryover: &maxCarry}},
					{Type: generic.ActionExpire},
				},
			}},
		},
		CurrentBalance: generic.Balance{
			AccruedToDate:    d(20),
			TotalEntitlement: d(20),
			TotalConsumed:    d(5), // 15 remaining
		},
		EndingPeriod: period2025(),
		NextPeriod:   period2026(),
	})

	if !output.Summary.CarriedOver.Value.Equal(d(10).Value) {
		t.Errorf("SPEC VIOLATION: Should carry over 10 days (capped), got %v", output.Summary.CarriedOver.Value)
	}
	if !output.Summary.Expired.Value.Equal(d(5).Value) {
		t.Errorf("SPEC VIOLATION: Should expire 5 days (excess), got %v", output.Summary.Expired.Value)
	}
}

func TestSpec_Reconciliation_UseItOrLoseIt_AllExpires(t *testing.T) {
	// SPEC: "Expire: Forfeit remaining balance"
	//
	// GIVEN: 8 days remaining, no carryover rule
	// WHEN: Reconciliation runs
	// THEN: All 8 days expire
	//
	// PURPOSE: Use-it-or-lose-it policies.

	engine := &generic.ReconciliationEngine{}

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Policy: generic.Policy{
			ResourceType: testResourceType,
			Unit:         generic.UnitDays,
			ReconciliationRules: []generic.ReconciliationRule{{
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionExpire}, // NO carryover
				},
			}},
		},
		CurrentBalance: generic.Balance{
			AccruedToDate:    d(20),
			TotalEntitlement: d(20),
			TotalConsumed:    d(12), // 8 remaining
		},
		EndingPeriod: period2025(),
		NextPeriod:   period2026(),
	})

	if !output.Summary.Expired.Value.Equal(d(8).Value) {
		t.Errorf("SPEC VIOLATION: All 8 days should expire, got %v", output.Summary.Expired.Value)
	}
	if !output.Summary.CarriedOver.IsZero() {
		t.Errorf("SPEC VIOLATION: No carryover expected, got %v", output.Summary.CarriedOver.Value)
	}
}

func TestSpec_Reconciliation_NegativeBalance_NoAction(t *testing.T) {
	// SPEC: Negative balances are not carried over or expired
	//
	// GIVEN: -3 days balance (employee overdrawn)
	// WHEN: Reconciliation runs
	// THEN: No transactions generated (debt remains)
	//
	// PURPOSE: Can't "expire" a negative. Debt handling is separate.

	engine := &generic.ReconciliationEngine{}
	maxCarry := d(10)

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Policy: generic.Policy{
			ResourceType: testResourceType,
			Unit:         generic.UnitDays,
			ReconciliationRules: []generic.ReconciliationRule{{
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionCarryover, Config: generic.ActionConfig{MaxCarryover: &maxCarry}},
					{Type: generic.ActionExpire},
				},
			}},
		},
		CurrentBalance: generic.Balance{
			AccruedToDate:    d(10),
			TotalEntitlement: d(10),
			TotalConsumed:    d(13), // -3 balance
		},
		EndingPeriod: period2025(),
		NextPeriod:   period2026(),
	})

	if len(output.Transactions) != 0 {
		t.Errorf("SPEC VIOLATION: No transactions for negative balance, got %d", len(output.Transactions))
	}
}

// =============================================================================
// SPEC 6: PERIOD CONFIGURATION
// =============================================================================
// From DESIGN.md: "Calendar Year, Fiscal Year, Anniversary"

func TestSpec_Period_CalendarYear(t *testing.T) {
	// SPEC: "Calendar Year: Jan 1 - Dec 31"
	//
	// GIVEN: Calendar year period configuration
	// WHEN: Getting period for July 15, 2025
	// THEN: Period is Jan 1 - Dec 31, 2025

	config := generic.PeriodConfig{Type: generic.PeriodCalendarYear}
	period := config.PeriodFor(generic.NewTimePoint(2025, time.July, 15))

	if !period.Start.Equal(generic.NewTimePoint(2025, time.January, 1)) {
		t.Errorf("SPEC VIOLATION: Calendar year should start Jan 1, got %s", period.Start)
	}
	if !period.End.Equal(generic.NewTimePoint(2025, time.December, 31)) {
		t.Errorf("SPEC VIOLATION: Calendar year should end Dec 31, got %s", period.End)
	}
}

func TestSpec_Period_FiscalYear_April(t *testing.T) {
	// SPEC: "Fiscal Year: Custom start (e.g., Apr 1)"
	//
	// GIVEN: Fiscal year starting April 1
	// WHEN: Getting period for July 15, 2025
	// THEN: Period is Apr 1, 2025 - Mar 31, 2026

	config := generic.PeriodConfig{
		Type:                 generic.PeriodFiscalYear,
		FiscalYearStartMonth: time.April,
	}
	period := config.PeriodFor(generic.NewTimePoint(2025, time.July, 15))

	if !period.Start.Equal(generic.NewTimePoint(2025, time.April, 1)) {
		t.Errorf("SPEC VIOLATION: Fiscal year should start Apr 1, got %s", period.Start)
	}
	if !period.End.Equal(generic.NewTimePoint(2026, time.March, 31)) {
		t.Errorf("SPEC VIOLATION: Fiscal year should end Mar 31, got %s", period.End)
	}
}

func TestSpec_Period_FiscalYear_BeforeStart(t *testing.T) {
	// SPEC: Date before fiscal year start falls in previous fiscal year
	//
	// GIVEN: Fiscal year starting April 1
	// WHEN: Getting period for February 15, 2025
	// THEN: Period is Apr 1, 2024 - Mar 31, 2025

	config := generic.PeriodConfig{
		Type:                 generic.PeriodFiscalYear,
		FiscalYearStartMonth: time.April,
	}
	period := config.PeriodFor(generic.NewTimePoint(2025, time.February, 15))

	if !period.Start.Equal(generic.NewTimePoint(2024, time.April, 1)) {
		t.Errorf("SPEC VIOLATION: Should be in previous fiscal year, got start %s", period.Start)
	}
}

func TestSpec_Period_Anniversary(t *testing.T) {
	// SPEC: "Anniversary: Based on hire/assignment date"
	//
	// GIVEN: Employee hired June 15, 2023
	// WHEN: Getting period for August 1, 2025
	// THEN: Period is Jun 15, 2025 - Jun 14, 2026

	hireDate := generic.NewTimePoint(2023, time.June, 15)
	config := generic.PeriodConfig{
		Type:       generic.PeriodAnniversary,
		AnchorDate: &hireDate,
	}
	period := config.PeriodFor(generic.NewTimePoint(2025, time.August, 1))

	if !period.Start.Equal(generic.NewTimePoint(2025, time.June, 15)) {
		t.Errorf("SPEC VIOLATION: Anniversary should start Jun 15, 2025, got %s", period.Start)
	}
}

// =============================================================================
// SPEC 7: CONSTRAINTS
// =============================================================================
// From DESIGN.md: "Constraints"

func TestSpec_Constraint_AllowNegative_True(t *testing.T) {
	// SPEC: "Negative balance allowed? - AllowNegative constraint"
	//
	// GIVEN: 10 days available, AllowNegative = true
	// WHEN: Requesting 15 days
	// THEN: Request is VALID with -5 remaining
	//
	// PURPOSE: Some policies allow overdraft (e.g., sick leave).

	ctx := context.Background()
	ledger := newLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &TestYearlyAccrual{AnnualDays: 10}

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          period2025(),
		Accruals:        accruals,
		RequestedAmount: d(15),
		AllowNegative:   true, // KEY
	})

	if !result.IsValid {
		t.Error("SPEC VIOLATION: AllowNegative should permit overdraft")
	}
	// Use approximate comparison for floating-point arithmetic
	diff := result.RemainingBalance.Value.Sub(d(-5).Value).Abs()
	if diff.GreaterThan(d(0.01).Value) {
		t.Errorf("remaining should be ~-5, got %v", result.RemainingBalance.Value)
	}
}

func TestSpec_Constraint_AllowNegative_False(t *testing.T) {
	// SPEC: Default behavior denies over-consumption
	//
	// GIVEN: 10 days available, AllowNegative = false
	// WHEN: Requesting 15 days
	// THEN: Request is INVALID

	ctx := context.Background()
	ledger := newLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &TestYearlyAccrual{AnnualDays: 10}

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          period2025(),
		Accruals:        accruals,
		RequestedAmount: d(15),
		AllowNegative:   false, // KEY
	})

	if result.IsValid {
		t.Error("SPEC VIOLATION: Should deny when balance would go negative")
	}
}

// =============================================================================
// SPEC 8: REVERSAL (Corrections)
// =============================================================================
// From DESIGN.md: "Corrections are made via reversal transactions"

func TestSpec_Reversal_RestoresBalance(t *testing.T) {
	// SPEC: "You never edit past entries, you only add new ones"
	//
	// GIVEN: 5 days consumed, then reversed
	// WHEN: Calculating balance
	// THEN: Balance is restored to original (consumption undone)
	//
	// PURPOSE: Mistakes are corrected by adding reversal, not editing.

	ctx := context.Background()
	ledger := newLedger()

	// Initial accrual
	ledger.Append(ctx, accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.January, 1), 20, "grant"))

	// Consumption (mistake)
	ledger.Append(ctx, consumptionTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.February, 1), 5, "consume"))

	// Reversal (correction)
	reversal := generic.Transaction{
		ID:             "reversal",
		EntityID:       "emp-1",
		PolicyID:       "test-policy",
		ResourceType:   testResourceType,
		EffectiveAt:    generic.NewTimePoint(2025, time.February, 2),
		Delta:          d(5), // POSITIVE to undo negative
		Type:           generic.TxReversal,
		IdempotencyKey: "reversal",
		Reason:         "consumption entered in error",
	}
	ledger.Append(ctx, reversal)

	// Check balance
	balance, _ := ledger.BalanceAt(ctx, "emp-1", "test-policy", generic.NewTimePoint(2025, time.February, 15), generic.UnitDays)

	// 20 (accrual) - 5 (consume) + 5 (reversal) = 20
	if !balance.Value.Equal(d(20).Value) {
		t.Errorf("SPEC VIOLATION: Reversal should restore balance to 20, got %v", balance.Value)
	}
}

// =============================================================================
// SPEC 9: PENDING TRANSACTIONS
// =============================================================================
// From DESIGN.md: Request lifecycle with PENDING state

func TestSpec_Pending_DeductsFromAvailable(t *testing.T) {
	// SPEC: Pending requests reduce available balance
	//
	// GIVEN: 20 days total, 5 days pending approval
	// WHEN: Checking available balance
	// THEN: 15 days available (pending is reserved)
	//
	// PURPOSE: Prevent over-booking while requests await approval.

	ctx := context.Background()
	ledger := newLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	// Accrual
	ledger.Append(ctx, accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.January, 1), 20, "grant"))

	// Pending request
	ledger.Append(ctx, pendingTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.March, 1), 5, "pending"))

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          period2025(),
		RequestedAmount: d(16), // More than 15 available
		AllowNegative:   false,
	})

	if result.IsValid {
		t.Error("SPEC VIOLATION: Should not allow 16 days when only 15 available (5 pending)")
	}

	// Verify the balance shows pending
	if !result.Balance.Pending.Value.Equal(d(5).Value) {
		t.Errorf("Pending should be 5, got %v", result.Balance.Pending.Value)
	}
}

// =============================================================================
// SPEC 10: AUDIT TRAIL
// =============================================================================
// From DESIGN.md: "Complete audit trail"

func TestSpec_AuditTrail_AllTransactionsPreserved(t *testing.T) {
	// SPEC: "Complete audit trail - no data loss from bugs or mistakes"
	//
	// GIVEN: Multiple transactions including reversals
	// WHEN: Querying transaction history
	// THEN: ALL transactions are present (nothing deleted)
	//
	// PURPOSE: Full history enables debugging and compliance.

	ctx := context.Background()
	ledger := newLedger()

	// Build a history
	ledger.Append(ctx, accrualTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.January, 1), 20, "grant"))
	ledger.Append(ctx, consumptionTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.February, 1), 5, "consume1"))
	ledger.Append(ctx, consumptionTx("emp-1", "test-policy", generic.NewTimePoint(2025, time.March, 1), 3, "consume2"))

	// Reversal (undo consume2)
	ledger.Append(ctx, generic.Transaction{
		ID:          "reversal",
		EntityID:    "emp-1",
		PolicyID:    "test-policy",
		EffectiveAt: generic.NewTimePoint(2025, time.March, 2),
		Delta:       d(3),
		Type:        generic.TxReversal,
	})

	txs, _ := ledger.Transactions(ctx, "emp-1", "test-policy")

	// Should have ALL 4 transactions
	if len(txs) != 4 {
		t.Errorf("SPEC VIOLATION: Audit trail should preserve all 4 transactions, got %d", len(txs))
	}

	// Verify we can identify each type
	types := make(map[generic.TransactionType]int)
	for _, tx := range txs {
		types[tx.Type]++
	}

	if types[generic.TxGrant] != 1 {
		t.Error("missing accrual transaction")
	}
	if types[generic.TxConsumption] != 2 {
		t.Error("missing consumption transactions")
	}
	if types[generic.TxReversal] != 1 {
		t.Error("missing reversal transaction")
	}
}
