package generic_test

import (
	"context"
	"testing"
	"time"

	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/generic/store"
)

// =============================================================================
// TEST HELPERS
// =============================================================================
// Note: testResource and testResourceType are defined in assignment_test.go

func newTestLedger() generic.Ledger {
	return generic.NewLedger(store.NewMemory())
}

func year2025() generic.Period {
	return generic.Period{
		Start: generic.NewTimePoint(2025, time.January, 1),
		End:   generic.NewTimePoint(2025, time.December, 31),
	}
}

func days(n float64) generic.Amount {
	return generic.NewAmount(n, generic.UnitDays)
}

func balance(accrued, consumed float64) generic.Balance {
	return generic.Balance{
		AccruedToDate:    days(accrued),
		TotalEntitlement: days(accrued),
		TotalConsumed:    days(consumed),
		Pending:          days(0),
		Adjustments:      days(0),
	}
}

// approxEqual checks if two amounts are approximately equal (for floating point)
func approxEqual(a, b generic.Amount) bool {
	diff := a.Value.Sub(b.Value).Abs()
	return diff.LessThan(generic.NewAmount(0.0001, a.Unit).Value)
}

// YearlyAccrual is a simple deterministic accrual schedule for testing
type YearlyAccrual struct {
	AnnualDays float64
}

func (ya *YearlyAccrual) GenerateAccruals(from, to generic.TimePoint) []generic.AccrualEvent {
	// Monthly accrual
	var events []generic.AccrualEvent
	monthly := ya.AnnualDays / 12

	current := generic.StartOfMonth(from.Year(), from.Month())
	end := generic.StartOfMonth(to.Year(), to.Month())

	for current.BeforeOrEqual(end) {
		if from.BeforeOrEqual(current) && current.BeforeOrEqual(to) {
			events = append(events, generic.AccrualEvent{
				At:     current,
				Amount: days(monthly),
				Reason: "monthly",
			})
		}
		current = current.AddMonths(1)
	}
	return events
}

func (ya *YearlyAccrual) IsDeterministic() bool {
	return true
}

// =============================================================================
// PERIOD-BASED BALANCE TESTS
// =============================================================================

func TestPeriodBalance_DeterministicAccrual_IncludesFutureAccruals(t *testing.T) {
	// GIVEN: Policy with 12 days/year (1 day/month), deterministic accrual
	// WHEN: Checking balance in January
	// THEN: Balance should be 12 days (full year), not 1 day (just January)

	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &YearlyAccrual{AnnualDays: 12}
	period := year2025()

	result, err := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          period,
		Accruals:        accruals,
		RequestedAmount: days(0), // Just checking balance
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 12 days (full year), not just what's accrued so far
	if !result.Balance.TotalEntitlement.Value.Equal(days(12).Value) {
		t.Errorf("expected 12 days total entitlement, got %v", result.Balance.TotalEntitlement.Value)
	}
}

func TestPeriodBalance_NonDeterministicAccrual_OnlyActualAccruals(t *testing.T) {
	// GIVEN: Hours-worked policy (non-deterministic), only Jan accrual recorded
	// WHEN: Checking balance in July
	// THEN: Balance should only include what's actually accrued

	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	// Record 5 days accrued from hours worked
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-1", EntityID: "emp-1", PolicyID: "test-policy",
		EffectiveAt: generic.NewTimePoint(2025, time.January, 15),
		Delta:       days(5),
		Type:        generic.TxGrant,
	})

	result, err := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		Accruals:        nil, // Non-deterministic - no projected accruals
		RequestedAmount: days(0),
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have the 5 days actually accrued
	if !result.Balance.TotalEntitlement.Value.Equal(days(5).Value) {
		t.Errorf("expected 5 days total entitlement, got %v", result.Balance.TotalEntitlement.Value)
	}
}

// =============================================================================
// CONSUMPTION VALIDATION TESTS
// =============================================================================

func TestConsumption_WithinBalance_Approved(t *testing.T) {
	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &YearlyAccrual{AnnualDays: 20}

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		Accruals:        accruals,
		RequestedAmount: days(5),
		AllowNegative:   false,
	})

	if !result.IsValid {
		t.Error("request should be valid (5 days from 20)")
	}
	if !approxEqual(result.RemainingBalance, days(15)) {
		t.Errorf("expected 15 days remaining, got %v", result.RemainingBalance.Value)
	}
}

func TestConsumption_ExceedsBalance_Denied(t *testing.T) {
	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &YearlyAccrual{AnnualDays: 10}

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		Accruals:        accruals,
		RequestedAmount: days(15), // More than available
		AllowNegative:   false,
	})

	if result.IsValid {
		t.Error("request should be denied (15 days from 10)")
	}
	if result.ValidationError == nil || result.ValidationError.Type != "insufficient_balance" {
		t.Error("expected insufficient_balance error")
	}
}

func TestConsumption_ExceedsBalance_AllowedWhenNegativePermitted(t *testing.T) {
	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &YearlyAccrual{AnnualDays: 10}

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		Accruals:        accruals,
		RequestedAmount: days(15),
		AllowNegative:   true, // Key: negative allowed
	})

	if !result.IsValid {
		t.Error("request should be valid when negative allowed")
	}
	if !approxEqual(result.RemainingBalance, days(-5)) {
		t.Errorf("expected -5 days remaining, got %v", result.RemainingBalance.Value)
	}
}

func TestConsumption_WithPriorConsumption_CorrectBalance(t *testing.T) {
	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	// Prior consumption of 8 days
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-1", EntityID: "emp-1", PolicyID: "test-policy",
		EffectiveAt: generic.NewTimePoint(2025, time.February, 10),
		Delta:       days(-8), // Consumption is negative
		Type:        generic.TxConsumption,
	})

	accruals := &YearlyAccrual{AnnualDays: 20}

	// Request 10 more days
	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		Accruals:        accruals,
		RequestedAmount: days(10),
		AllowNegative:   false,
	})

	// 20 - 8 - 10 = 2 remaining
	if !result.IsValid {
		t.Error("request should be valid")
	}
	if !approxEqual(result.RemainingBalance, days(2)) {
		t.Errorf("expected 2 days remaining, got %v", result.RemainingBalance.Value)
	}
}

func TestConsumption_WithPendingRequests_DeductedFromAvailable(t *testing.T) {
	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	// Pending request of 5 days
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-1", EntityID: "emp-1", PolicyID: "test-policy",
		EffectiveAt: generic.NewTimePoint(2025, time.March, 1),
		Delta:       days(-5),
		Type:        generic.TxPending,
	})

	accruals := &YearlyAccrual{AnnualDays: 12}

	// Request 10 more days (12 - 5 pending = 7 available)
	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		Accruals:        accruals,
		RequestedAmount: days(10),
		AllowNegative:   false,
	})

	if result.IsValid {
		t.Error("request should be denied (only 7 available after pending)")
	}
}

// =============================================================================
// ROLLOVER TESTS
// =============================================================================

func TestRollover_FullCarryover(t *testing.T) {
	// GIVEN: 10 days remaining, no cap
	// WHEN: Period ends
	// THEN: 10 days carry over to next period

	engine := &generic.ReconciliationEngine{}

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Policy: generic.Policy{
			ResourceType: testResourceType,
			Unit:         generic.UnitDays,
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "rollover",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionCarryover, Config: generic.ActionConfig{}}, // No cap
				},
			}},
		},
		CurrentBalance: balance(20, 10), // Current = 10
		EndingPeriod: year2025(),
		NextPeriod:   generic.Period{Start: generic.NewTimePoint(2026, time.January, 1), End: generic.NewTimePoint(2026, time.December, 31)},
	})

	if !output.Summary.CarriedOver.Value.Equal(days(10).Value) {
		t.Errorf("expected 10 days carried over, got %v", output.Summary.CarriedOver.Value)
	}
	if len(output.Transactions) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(output.Transactions))
	}
}

func TestRollover_CappedCarryover(t *testing.T) {
	// GIVEN: 15 days remaining, cap at 10 days
	// WHEN: Period ends
	// THEN: 10 days carry over, 5 days expire

	engine := &generic.ReconciliationEngine{}
	maxCarry := days(10)

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Policy: generic.Policy{
			ResourceType: testResourceType,
			Unit:         generic.UnitDays,
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "rollover",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionCarryover, Config: generic.ActionConfig{MaxCarryover: &maxCarry}},
					{Type: generic.ActionExpire},
				},
			}},
		},
		CurrentBalance: balance(20, 5), // Current = 15
		EndingPeriod: year2025(),
		NextPeriod:   generic.Period{Start: generic.NewTimePoint(2026, time.January, 1), End: generic.NewTimePoint(2026, time.December, 31)},
	})

	if !output.Summary.CarriedOver.Value.Equal(days(10).Value) {
		t.Errorf("expected 10 days carried over, got %v", output.Summary.CarriedOver.Value)
	}
	if !output.Summary.Expired.Value.Equal(days(5).Value) {
		t.Errorf("expected 5 days expired, got %v", output.Summary.Expired.Value)
	}
}

func TestRollover_NoCarryover_AllExpires(t *testing.T) {
	// GIVEN: 8 days remaining, use-it-or-lose-it policy
	// WHEN: Period ends
	// THEN: All 8 days expire

	engine := &generic.ReconciliationEngine{}

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Policy: generic.Policy{
			ResourceType: testResourceType,
			Unit:         generic.UnitDays,
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "expire",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionExpire}, // No carryover action
				},
			}},
		},
		CurrentBalance: balance(20, 12), // Current = 8
		EndingPeriod: year2025(),
		NextPeriod:   generic.Period{Start: generic.NewTimePoint(2026, time.January, 1), End: generic.NewTimePoint(2026, time.December, 31)},
	})

	if !output.Summary.CarriedOver.IsZero() {
		t.Errorf("expected 0 days carried over, got %v", output.Summary.CarriedOver.Value)
	}
	if !output.Summary.Expired.Value.Equal(days(8).Value) {
		t.Errorf("expected 8 days expired, got %v", output.Summary.Expired.Value)
	}
}

func TestRollover_NegativeBalance_NoCarryoverOrExpire(t *testing.T) {
	// GIVEN: -3 days balance (overdrawn)
	// WHEN: Period ends
	// THEN: Nothing to carry over or expire, negative remains

	engine := &generic.ReconciliationEngine{}
	maxCarry := days(10)

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Policy: generic.Policy{
			ResourceType: testResourceType,
			Unit:         generic.UnitDays,
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "rollover",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionCarryover, Config: generic.ActionConfig{MaxCarryover: &maxCarry}},
					{Type: generic.ActionExpire},
				},
			}},
		},
		CurrentBalance: balance(10, 13), // Current = -3
		EndingPeriod: year2025(),
		NextPeriod:   generic.Period{Start: generic.NewTimePoint(2026, time.January, 1), End: generic.NewTimePoint(2026, time.December, 31)},
	})

	if len(output.Transactions) != 0 {
		t.Errorf("expected no transactions for negative balance, got %d", len(output.Transactions))
	}
}

// =============================================================================
// BALANCE CAP TESTS
// =============================================================================

func TestBalanceCap_UnderCap_NoChange(t *testing.T) {
	engine := &generic.ReconciliationEngine{}
	maxBalance := days(30)

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Policy: generic.Policy{
			ResourceType: testResourceType,
			Unit:         generic.UnitDays,
			Constraints:  generic.Constraints{MaxBalance: &maxBalance},
			ReconciliationRules: []generic.ReconciliationRule{{
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{{Type: generic.ActionCap}},
			}},
		},
		CurrentBalance: balance(20, 0), // Current = 20, under cap
		EndingPeriod: year2025(),
		NextPeriod:   generic.Period{},
	})

	if len(output.Transactions) != 0 {
		t.Error("expected no transactions when under cap")
	}
}

func TestBalanceCap_OverCap_ExcessRemoved(t *testing.T) {
	engine := &generic.ReconciliationEngine{}
	maxBalance := days(25)

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "test-policy",
		Policy: generic.Policy{
			ResourceType: testResourceType,
			Unit:         generic.UnitDays,
			Constraints:  generic.Constraints{MaxBalance: &maxBalance},
			ReconciliationRules: []generic.ReconciliationRule{{
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{{Type: generic.ActionCap}},
			}},
		},
		CurrentBalance: balance(30, 0), // Current = 30, over cap by 5
		EndingPeriod: year2025(),
		NextPeriod:   generic.Period{},
	})

	if !output.Summary.Expired.Value.Equal(days(5).Value) {
		t.Errorf("expected 5 days capped, got %v", output.Summary.Expired.Value)
	}
}

// =============================================================================
// PERIOD CALCULATION TESTS
// =============================================================================

func TestPeriodConfig_CalendarYear(t *testing.T) {
	config := generic.PeriodConfig{Type: generic.PeriodCalendarYear}

	// July 15, 2025 should be in calendar year 2025
	period := config.PeriodFor(generic.NewTimePoint(2025, time.July, 15))

	if !period.Start.Equal(generic.NewTimePoint(2025, time.January, 1)) {
		t.Errorf("expected Jan 1, got %s", period.Start)
	}
	if !period.End.Equal(generic.NewTimePoint(2025, time.December, 31)) {
		t.Errorf("expected Dec 31, got %s", period.End)
	}
}

func TestPeriodConfig_FiscalYear_April(t *testing.T) {
	config := generic.PeriodConfig{
		Type:                 generic.PeriodFiscalYear,
		FiscalYearStartMonth: time.April,
	}

	// July 15, 2025 should be in fiscal year Apr 1 2025 - Mar 31 2026
	period := config.PeriodFor(generic.NewTimePoint(2025, time.July, 15))

	if !period.Start.Equal(generic.NewTimePoint(2025, time.April, 1)) {
		t.Errorf("expected Apr 1 2025, got %s", period.Start)
	}
	if !period.End.Equal(generic.NewTimePoint(2026, time.March, 31)) {
		t.Errorf("expected Mar 31 2026, got %s", period.End)
	}

	// Feb 15, 2025 should be in fiscal year Apr 1 2024 - Mar 31 2025
	period2 := config.PeriodFor(generic.NewTimePoint(2025, time.February, 15))

	if !period2.Start.Equal(generic.NewTimePoint(2024, time.April, 1)) {
		t.Errorf("expected Apr 1 2024, got %s", period2.Start)
	}
}

func TestPeriodConfig_Anniversary(t *testing.T) {
	hireDate := generic.NewTimePoint(2023, time.June, 15)
	config := generic.PeriodConfig{
		Type:       generic.PeriodAnniversary,
		AnchorDate: &hireDate,
	}

	// Aug 1, 2025 should be in anniversary year Jun 15 2025 - Jun 14 2026
	period := config.PeriodFor(generic.NewTimePoint(2025, time.August, 1))

	if !period.Start.Equal(generic.NewTimePoint(2025, time.June, 15)) {
		t.Errorf("expected Jun 15 2025, got %s", period.Start)
	}

	// May 1, 2025 should be in anniversary year Jun 15 2024 - Jun 14 2025
	period2 := config.PeriodFor(generic.NewTimePoint(2025, time.May, 1))

	if !period2.Start.Equal(generic.NewTimePoint(2024, time.June, 15)) {
		t.Errorf("expected Jun 15 2024, got %s", period2.Start)
	}
}

// =============================================================================
// IDEMPOTENCY TESTS
// =============================================================================

func TestIdempotency_DuplicateTransactionRejected(t *testing.T) {
	ctx := context.Background()
	ledger := newTestLedger()

	tx := generic.Transaction{
		ID: "tx-1", EntityID: "emp-1", PolicyID: "test-policy",
		EffectiveAt:    generic.NewTimePoint(2025, time.January, 1),
		Delta:          days(10),
		Type:           generic.TxGrant,
		IdempotencyKey: "accrual-2025-jan",
	}

	err1 := ledger.Append(ctx, tx)
	err2 := ledger.Append(ctx, tx)

	if err1 != nil {
		t.Error("first append should succeed")
	}
	if err2 == nil {
		t.Error("second append should fail")
	}

	txs, _ := ledger.Transactions(ctx, "emp-1", "test-policy")
	if len(txs) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(txs))
	}
}

func TestBatchAppend_Atomic(t *testing.T) {
	ctx := context.Background()
	ledger := newTestLedger()

	txs := []generic.Transaction{
		{ID: "tx-1", EntityID: "emp-1", PolicyID: "test-policy", EffectiveAt: generic.NewTimePoint(2025, time.January, 1), Delta: days(1), Type: generic.TxGrant},
		{ID: "tx-2", EntityID: "emp-1", PolicyID: "test-policy", EffectiveAt: generic.NewTimePoint(2025, time.January, 2), Delta: days(1), Type: generic.TxGrant},
		{ID: "tx-3", EntityID: "emp-1", PolicyID: "test-policy", EffectiveAt: generic.NewTimePoint(2025, time.January, 3), Delta: days(1), Type: generic.TxGrant},
	}

	err := ledger.AppendBatch(ctx, txs)
	if err != nil {
		t.Errorf("batch append failed: %v", err)
	}

	all, _ := ledger.Transactions(ctx, "emp-1", "test-policy")
	if len(all) != 3 {
		t.Errorf("expected 3 transactions, got %d", len(all))
	}
}

// =============================================================================
// CONSUMPTION MODE TESTS
// =============================================================================

func TestConsumptionMode_ConsumeAhead_FullYearAvailable(t *testing.T) {
	// GIVEN: Policy with 12 days/year, ConsumeAhead mode
	// WHEN: Checking in January (only 1 month accrued)
	// THEN: Full 12 days are available (can use future accruals)

	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &YearlyAccrual{AnnualDays: 12}

	// Check balance in January
	result, err := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		AsOf:            generic.NewTimePoint(2025, time.January, 15), // Mid-January
		Accruals:        accruals,
		RequestedAmount: days(10), // Request 10 days
		ConsumptionMode: generic.ConsumeAhead, // KEY: Can use future accruals
		AllowNegative:   false,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be VALID - full year available in ConsumeAhead mode
	if !result.IsValid {
		t.Error("ConsumeAhead: should allow 10 days in January (full 12 available)")
	}

	// Check display shows both accrued and entitlement
	if !result.Display.AccruedToDate.Value.Equal(days(1).Value) {
		t.Errorf("expected 1 day accrued to date (Jan), got %v", result.Display.AccruedToDate.Value)
	}
	if !result.Display.WillHaveByPeriodEnd.Value.Equal(days(12).Value) {
		t.Errorf("expected 12 days by period end, got %v", result.Display.WillHaveByPeriodEnd.Value)
	}
}

func TestConsumptionMode_ConsumeUpToAccrued_OnlyAccruedAvailable(t *testing.T) {
	// GIVEN: Policy with 12 days/year, ConsumeUpToAccrued mode
	// WHEN: Checking in January (only 1 month accrued)
	// THEN: Only 1 day available (can't use future accruals)

	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &YearlyAccrual{AnnualDays: 12}

	// Try to request 10 days in January
	result, err := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		AsOf:            generic.NewTimePoint(2025, time.January, 15), // Mid-January
		Accruals:        accruals,
		RequestedAmount: days(10), // Request 10 days
		ConsumptionMode: generic.ConsumeUpToAccrued, // KEY: Only accrued available
		AllowNegative:   false,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be INVALID - only 1 day accrued so far
	if result.IsValid {
		t.Error("ConsumeUpToAccrued: should DENY 10 days in January (only 1 accrued)")
	}
	if result.ValidationError.Type != "insufficient_balance" {
		t.Errorf("expected insufficient_balance error, got %s", result.ValidationError.Type)
	}
}

func TestConsumptionMode_ConsumeUpToAccrued_SmallRequestApproved(t *testing.T) {
	// GIVEN: Policy with 12 days/year, ConsumeUpToAccrued mode, 1 day accrued
	// WHEN: Request 1 day in January
	// THEN: Approved (within accrued amount)

	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &YearlyAccrual{AnnualDays: 12}

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		AsOf:            generic.NewTimePoint(2025, time.January, 15),
		Accruals:        accruals,
		RequestedAmount: days(1), // Request exactly what's accrued
		ConsumptionMode: generic.ConsumeUpToAccrued,
		AllowNegative:   false,
	})

	if !result.IsValid {
		t.Error("ConsumeUpToAccrued: should allow 1 day when 1 is accrued")
	}
}

func TestConsumptionMode_LaterInYear_MoreAccrued(t *testing.T) {
	// GIVEN: Policy with 12 days/year, ConsumeUpToAccrued mode
	// WHEN: Checking in June (6 months accrued = 6 days)
	// THEN: 6 days available

	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	accruals := &YearlyAccrual{AnnualDays: 12}

	// Request 5 days in June
	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		AsOf:            generic.NewTimePoint(2025, time.June, 15), // Mid-June
		Accruals:        accruals,
		RequestedAmount: days(5), // Request 5 days
		ConsumptionMode: generic.ConsumeUpToAccrued,
		AllowNegative:   false,
	})

	if !result.IsValid {
		t.Error("ConsumeUpToAccrued: should allow 5 days in June (6 accrued)")
	}

	// Check display
	if !approxEqual(result.Display.AccruedToDate, days(6)) {
		t.Errorf("expected 6 days accrued by June, got %v", result.Display.AccruedToDate.Value)
	}
}

// =============================================================================
// REVERSAL TESTS
// =============================================================================

func TestReversal_RestoresBalance(t *testing.T) {
	ctx := context.Background()
	ledger := newTestLedger()
	engine := &generic.ProjectionEngine{Ledger: ledger}

	// Consumption
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-1", EntityID: "emp-1", PolicyID: "test-policy",
		EffectiveAt: generic.NewTimePoint(2025, time.February, 1),
		Delta:       days(-5),
		Type:        generic.TxConsumption,
	})

	// Reversal
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-2", EntityID: "emp-1", PolicyID: "test-policy",
		EffectiveAt: generic.NewTimePoint(2025, time.February, 15),
		Delta:       days(5), // Positive to restore
		Type:        generic.TxReversal,
	})

	accruals := &YearlyAccrual{AnnualDays: 20}

	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "test-policy",
		Unit:            generic.UnitDays,
		Period:          year2025(),
		Accruals:        accruals,
		RequestedAmount: days(0),
	})

	// Should have full 20 days (consumption was reversed)
	available := result.Balance.Available()
	if !approxEqual(available, days(20)) {
		t.Errorf("expected 20 days available after reversal, got %v", available.Value)
	}
}
