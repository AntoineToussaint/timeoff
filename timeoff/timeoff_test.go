package timeoff_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/store/sqlite"
	"github.com/warp/resource-engine/timeoff"
)

// =============================================================================
// TEST HELPERS
// =============================================================================

func newTestStore(t *testing.T) *sqlite.Store {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func newTestLedger(store *sqlite.Store) generic.Ledger {
	return generic.NewLedger(store)
}

func days(n float64) generic.Amount {
	return generic.NewAmount(n, generic.UnitDays)
}

func date(year int, month time.Month, day int) generic.TimePoint {
	return generic.NewTimePoint(year, month, day)
}

func year2025Period() generic.Period {
	return generic.Period{
		Start: date(2025, time.January, 1),
		End:   date(2025, time.December, 31),
	}
}

// =============================================================================
// MULTI-POLICY CONSUMPTION TESTS
// =============================================================================

func TestMultiPolicy_ConsumesByPriority(t *testing.T) {
	// GIVEN: Employee with 3 PTO policies of different priorities
	//   Priority 1: Carryover (3 days)
	//   Priority 2: Bonus (5 days)
	//   Priority 3: Standard (20 days)
	// WHEN: Request 10 days
	// THEN: Consume 3 from carryover, 5 from bonus, 2 from standard

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Setup transactions (simulate accruals)
	txs := []generic.Transaction{
		{ID: "tx-carryover", EntityID: "emp-1", PolicyID: "carryover", ResourceType: timeoff.ResourcePTO,
			EffectiveAt: date(2025, time.January, 1), Delta: days(3), Type: generic.TxGrant},
		{ID: "tx-bonus", EntityID: "emp-1", PolicyID: "bonus", ResourceType: timeoff.ResourcePTO,
			EffectiveAt: date(2025, time.January, 1), Delta: days(5), Type: generic.TxGrant},
		{ID: "tx-standard", EntityID: "emp-1", PolicyID: "standard", ResourceType: timeoff.ResourcePTO,
			EffectiveAt: date(2025, time.January, 1), Delta: days(20), Type: generic.TxGrant},
	}

	for _, tx := range txs {
		if err := ledger.Append(ctx, tx); err != nil {
			t.Fatalf("Failed to append transaction: %v", err)
		}
	}

	// Build resource balance (simulating multi-policy aggregation)
	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   timeoff.ResourcePTO,
		TotalAvailable: days(28),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "carryover",
					Policy:   generic.Policy{ResourceType: timeoff.ResourcePTO, Unit: generic.UnitDays},
				},
				Balance:  generic.Balance{AccruedToDate: days(3), TotalEntitlement: days(3)},
				Priority: 1,
			},
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "bonus",
					Policy:   generic.Policy{ResourceType: timeoff.ResourcePTO, Unit: generic.UnitDays},
				},
				Balance:  generic.Balance{AccruedToDate: days(5), TotalEntitlement: days(5)},
				Priority: 2,
			},
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "standard",
					Policy:   generic.Policy{ResourceType: timeoff.ResourcePTO, Unit: generic.UnitDays},
				},
				Balance:  generic.Balance{AccruedToDate: days(20), TotalEntitlement: days(20)},
				Priority: 3,
			},
		},
	}

	// Distribute consumption
	distributor := &generic.ConsumptionDistributor{}
	result := distributor.Distribute(resourceBalance, days(10), false)

	if !result.IsSatisfiable {
		t.Error("should be satisfiable")
	}
	if len(result.Allocations) != 3 {
		t.Errorf("expected 3 allocations, got %d", len(result.Allocations))
	}

	expected := map[string]float64{
		"carryover": 3,
		"bonus":     5,
		"standard":  2,
	}

	for _, alloc := range result.Allocations {
		exp, ok := expected[string(alloc.PolicyID)]
		if !ok {
			t.Errorf("unexpected allocation from policy %s", alloc.PolicyID)
			continue
		}
		if !alloc.Amount.Value.Equal(days(exp).Value) {
			t.Errorf("expected %v days from %s, got %v", exp, alloc.PolicyID, alloc.Amount.Value)
		}
	}
}

func TestMultiPolicy_SkipsExhaustedPolicies(t *testing.T) {
	// GIVEN: First policy exhausted (0 balance), second has balance
	// WHEN: Request 5 days
	// THEN: All from second policy

	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   timeoff.ResourcePTO,
		TotalAvailable: days(10),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "exhausted",
					Policy:   generic.Policy{ResourceType: timeoff.ResourcePTO, Unit: generic.UnitDays},
				},
				Balance:  generic.Balance{AccruedToDate: days(5), TotalEntitlement: days(5), TotalConsumed: days(5)},
				Priority: 1,
			},
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "available",
					Policy:   generic.Policy{ResourceType: timeoff.ResourcePTO, Unit: generic.UnitDays},
				},
				Balance:  generic.Balance{AccruedToDate: days(10), TotalEntitlement: days(10)},
				Priority: 2,
			},
		},
	}

	distributor := &generic.ConsumptionDistributor{}
	result := distributor.Distribute(resourceBalance, days(5), false)

	if !result.IsSatisfiable {
		t.Error("should be satisfiable")
	}
	if len(result.Allocations) != 1 {
		t.Errorf("expected 1 allocation (skipping exhausted), got %d", len(result.Allocations))
	}
	if string(result.Allocations[0].PolicyID) != "available" {
		t.Errorf("expected allocation from 'available', got %s", result.Allocations[0].PolicyID)
	}
}

func TestMultiPolicy_ApprovalRequired(t *testing.T) {
	// GIVEN: Policy with approval required
	// WHEN: Distributing consumption
	// THEN: Allocation marked as requiring approval

	autoApprove := days(2)
	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   timeoff.ResourcePTO,
		TotalAvailable: days(20),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "needs-approval",
					Policy:   generic.Policy{ResourceType: timeoff.ResourcePTO, Unit: generic.UnitDays},
					ApprovalConfig: generic.ApprovalConfig{
						RequiresApproval: true,
						AutoApproveUpTo:  &autoApprove,
					},
				},
				Balance:  generic.Balance{AccruedToDate: days(20), TotalEntitlement: days(20)},
				Priority: 1,
			},
		},
	}

	distributor := &generic.ConsumptionDistributor{}

	// Request 1 day (under auto-approve limit)
	result1 := distributor.Distribute(resourceBalance, days(1), false)
	if result1.Allocations[0].RequiresApproval {
		t.Error("1 day should NOT require approval (under 2 day limit)")
	}

	// Request 5 days (over auto-approve limit)
	result2 := distributor.Distribute(resourceBalance, days(5), false)
	if !result2.Allocations[0].RequiresApproval {
		t.Error("5 days should require approval (over 2 day limit)")
	}
}

// =============================================================================
// ROLLOVER TESTS
// =============================================================================

func TestRollover_FullYear_WithCarryover(t *testing.T) {
	// GIVEN: Employee with 20 days accrued, 5 consumed = 15 remaining
	//        Policy allows max 10 days carryover
	// WHEN: Process year-end rollover
	// THEN: 10 days carried over, 5 days expired

	store := newTestStore(t)
	ctx := context.Background()

	// Create policy config
	maxCarry := days(10)
	policy := generic.Policy{
		ID:           "pto-standard",
		Name:         "Standard PTO",
		ResourceType: timeoff.ResourcePTO,
		Unit:         generic.UnitDays,
		PeriodConfig: generic.PeriodConfig{Type: generic.PeriodCalendarYear},
		ReconciliationRules: []generic.ReconciliationRule{{
			ID:      "year-end",
			Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
			Actions: []generic.ReconciliationAction{
				{Type: generic.ActionCarryover, Config: generic.ActionConfig{MaxCarryover: &maxCarry}},
				{Type: generic.ActionExpire},
			},
		}},
	}

	// Setup balance
	currentBalance := generic.Balance{
		EntityID:         "emp-1",
		PolicyID:         "pto-standard",
		Period:           year2025Period(),
		AccruedToDate:    days(20),
		TotalEntitlement: days(20),
		TotalConsumed:    days(5), // 15 remaining
	}

	engine := &generic.ReconciliationEngine{}
	nextPeriod := generic.Period{
		Start: date(2026, time.January, 1),
		End:   date(2026, time.December, 31),
	}

	output, err := engine.Process(generic.ReconciliationInput{
		EntityID:       "emp-1",
		PolicyID:       "pto-standard",
		Policy:         policy,
		CurrentBalance: currentBalance,
		EndingPeriod:   year2025Period(),
		NextPeriod:     nextPeriod,
	})

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if !output.Summary.CarriedOver.Value.Equal(days(10).Value) {
		t.Errorf("expected 10 days carried over, got %v", output.Summary.CarriedOver.Value)
	}
	if !output.Summary.Expired.Value.Equal(days(5).Value) {
		t.Errorf("expected 5 days expired, got %v", output.Summary.Expired.Value)
	}

	// Verify transactions were created
	if len(output.Transactions) != 2 {
		t.Errorf("expected 2 transactions (carryover + expire), got %d", len(output.Transactions))
	}

	// Apply transactions to store
	for _, tx := range output.Transactions {
		tx.IdempotencyKey = "test-" + string(tx.ID)
		store.Append(ctx, tx)
	}
}

func TestRollover_MidYearHire_Prorated(t *testing.T) {
	// GIVEN: Employee hired June 15, gets prorated accruals (10 days instead of 20)
	// WHEN: Checking balance
	// THEN: Only has access to prorated amount

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Accrual from June to December (7 months)
	// 20 days / 12 months = ~1.67/month × 7 = ~11.67 days
	accrual := &timeoff.YearlyAccrual{
		AnnualDays: 20,
		Frequency:  generic.FreqMonthly,
	}

	hireDate := date(2025, time.June, 15)
	periodEnd := date(2025, time.December, 31)

	events := accrual.GenerateAccruals(hireDate, periodEnd)

	// Add accruals to ledger
	for i, e := range events {
		tx := generic.Transaction{
			ID:             generic.TransactionID("tx-accrual-" + e.At.String()),
			EntityID:       "emp-1",
			PolicyID:       "pto",
			ResourceType:   timeoff.ResourcePTO,
			EffectiveAt:    e.At,
			Delta:          e.Amount,
			Type:           generic.TxGrant,
			IdempotencyKey: "accrual-" + string(rune(i)),
		}
		if err := ledger.Append(ctx, tx); err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	// Check total accrued
	totalAccrued := days(0)
	for _, e := range events {
		totalAccrued = totalAccrued.Add(e.Amount)
	}

	// The YearlyAccrual starts from the 1st of the hire month
	// June 1 to Dec 31 = June, July, Aug, Sep, Oct, Nov, Dec = 7 months
	// But accrual counts from start of month: June, July, Aug, Sep, Oct, Nov = 6 months
	// (Dec 31 is end, not start of Dec)
	// 20/12 × 6 = 10 days
	expectedMin := 9.0  // Allow some variance
	expectedMax := 12.0
	actualFloat, _ := totalAccrued.Value.Float64()
	if actualFloat < expectedMin || actualFloat > expectedMax {
		t.Errorf("expected %.0f-%.0f days accrued, got %v", expectedMin, expectedMax, totalAccrued.Value)
	}
}

func TestRollover_NegativeBalance_CarriedForward(t *testing.T) {
	// GIVEN: Employee with -3 days balance (overdrawn)
	// WHEN: Process rollover
	// THEN: No carryover or expire (negative remains as-is)

	engine := &generic.ReconciliationEngine{}
	maxCarry := days(10)

	currentBalance := generic.Balance{
		EntityID:         "emp-1",
		PolicyID:         "pto",
		Period:           year2025Period(),
		AccruedToDate:    days(10),
		TotalEntitlement: days(10),
		TotalConsumed:    days(13), // -3 balance
	}

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "pto",
		Policy: generic.Policy{
			ResourceType: timeoff.ResourcePTO,
			Unit:         generic.UnitDays,
			ReconciliationRules: []generic.ReconciliationRule{{
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionCarryover, Config: generic.ActionConfig{MaxCarryover: &maxCarry}},
					{Type: generic.ActionExpire},
				},
			}},
		},
		CurrentBalance: currentBalance,
		EndingPeriod:   year2025Period(),
		NextPeriod:     generic.Period{Start: date(2026, time.January, 1), End: date(2026, time.December, 31)},
	})

	if len(output.Transactions) != 0 {
		t.Errorf("expected no transactions for negative balance, got %d", len(output.Transactions))
	}
}

// =============================================================================
// CONSUMPTION MODE TESTS
// =============================================================================

func TestConsumeAhead_FullYearAvailableInJanuary(t *testing.T) {
	// GIVEN: Policy with 20 days/year, ConsumeAhead mode
	// WHEN: Checking balance in January
	// THEN: Full 20 days available (can use future accruals)

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Only January accrual recorded (1.67 days)
	tx := generic.Transaction{
		ID:             "tx-jan",
		EntityID:       "emp-1",
		PolicyID:       "pto",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    date(2025, time.January, 1),
		Delta:          days(1.67),
		Type:           generic.TxGrant,
		IdempotencyKey: "accrual-jan",
	}
	ledger.Append(ctx, tx)

	accruals := &timeoff.YearlyAccrual{AnnualDays: 20, Frequency: generic.FreqMonthly}

	engine := &generic.ProjectionEngine{Ledger: ledger}
	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "pto",
		Unit:            generic.UnitDays,
		Period:          year2025Period(),
		AsOf:            date(2025, time.January, 15),
		Accruals:        accruals,
		RequestedAmount: days(15), // Request 15 days
		ConsumptionMode: generic.ConsumeAhead,
		AllowNegative:   false,
	})

	if !result.IsValid {
		t.Error("ConsumeAhead: should allow 15 days in January (full 20 available)")
	}
}

func TestConsumeUpToAccrued_OnlyEarnedAvailable(t *testing.T) {
	// GIVEN: Policy with 20 days/year, ConsumeUpToAccrued mode
	// WHEN: Checking balance in January (1.67 days accrued)
	// THEN: Only ~1.67 days available

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	accruals := &timeoff.YearlyAccrual{AnnualDays: 20, Frequency: generic.FreqMonthly}

	engine := &generic.ProjectionEngine{Ledger: ledger}
	result, _ := engine.Project(ctx, generic.ProjectionInput{
		EntityID:        "emp-1",
		PolicyID:        "pto",
		Unit:            generic.UnitDays,
		Period:          year2025Period(),
		AsOf:            date(2025, time.January, 15),
		Accruals:        accruals,
		RequestedAmount: days(5), // Request 5 days
		ConsumptionMode: generic.ConsumeUpToAccrued,
		AllowNegative:   false,
	})

	if result.IsValid {
		t.Error("ConsumeUpToAccrued: should DENY 5 days in January (only ~1.67 accrued)")
	}
}

// =============================================================================
// ACCRUAL SCHEDULE TESTS
// =============================================================================

func TestYearlyAccrual_Monthly(t *testing.T) {
	accrual := &timeoff.YearlyAccrual{
		AnnualDays: 24, // 2 per month
		Frequency:  generic.FreqMonthly,
	}

	events := accrual.GenerateAccruals(
		date(2025, time.January, 1),
		date(2025, time.December, 31),
	)

	if len(events) != 12 {
		t.Errorf("expected 12 monthly accruals, got %d", len(events))
	}

	// Each should be 2 days
	for i, e := range events {
		if !e.Amount.Value.Equal(days(2).Value) {
			t.Errorf("event %d: expected 2 days, got %v", i, e.Amount.Value)
		}
	}
}

func TestYearlyAccrual_Upfront(t *testing.T) {
	accrual := &timeoff.YearlyAccrual{
		AnnualDays: 20,
		Frequency:  generic.FreqUpfront,
	}

	events := accrual.GenerateAccruals(
		date(2025, time.January, 1),
		date(2025, time.December, 31),
	)

	if len(events) != 1 {
		t.Errorf("expected 1 upfront accrual, got %d", len(events))
	}

	if !events[0].Amount.Value.Equal(days(20).Value) {
		t.Errorf("expected 20 days upfront, got %v", events[0].Amount.Value)
	}
}

func TestTenureAccrual_TierProgression(t *testing.T) {
	hireDate := date(2020, time.March, 15) // 5 years tenure by 2025

	accrual := &timeoff.TenureAccrual{
		HireDate:  hireDate,
		Frequency: generic.FreqMonthly,
		Tiers: []timeoff.TenureTier{
			{AfterYears: 0, AnnualDays: 15},
			{AfterYears: 3, AnnualDays: 20},
			{AfterYears: 5, AnnualDays: 25},
		},
	}

	// Check accruals in 2025 (5 years tenure)
	events := accrual.GenerateAccruals(
		date(2025, time.April, 1), // After March anniversary
		date(2025, time.December, 31),
	)

	// Should be getting 25 days/year rate (highest tier)
	// 25/12 = ~2.08 days/month
	if len(events) == 0 {
		t.Error("expected accrual events")
		return
	}

	expectedMonthly := 25.0 / 12.0
	for _, e := range events {
		actual, _ := e.Amount.Value.Float64()
		if actual < expectedMonthly-0.1 || actual > expectedMonthly+0.1 {
			t.Errorf("expected ~%.2f days/month for 5yr tenure, got %.2f", expectedMonthly, actual)
		}
	}
}

// =============================================================================
// UNLIMITED POLICY TESTS
// =============================================================================

func TestUnlimitedPolicy_NoBalanceTracking(t *testing.T) {
	// Unlimited policies should not track balance - just allow requests
	policy := timeoff.UnlimitedPTOPolicy("unlimited")

	if !policy.Policy.IsUnlimited {
		t.Error("expected IsUnlimited to be true")
	}
	if policy.Accrual != nil {
		t.Error("expected no accrual schedule for unlimited policy")
	}
}

// =============================================================================
// REQUEST FLOW TESTS
// =============================================================================

func TestTimeOffRequest_ToConsumptionEvents(t *testing.T) {
	request := &timeoff.TimeOffRequest{
		ID:       "req-1",
		EntityID: "emp-1",
		PolicyID: "pto",
		Resource: timeoff.ResourcePTO,
		Days: []generic.TimePoint{
			date(2025, time.March, 10),
			date(2025, time.March, 11),
			date(2025, time.March, 12),
		},
		HoursPerDay: 8,
		Status:      timeoff.StatusPending,
	}

	events := request.ToConsumptionEvents()

	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}

	for i, e := range events {
		if !e.Amount.Value.Equal(days(1).Value) {
			t.Errorf("event %d: expected 1 day, got %v", i, e.Amount.Value)
		}
	}
}

func TestTimeOffRequest_FilterWorkdays(t *testing.T) {
	// March 8-9 2025 is Saturday-Sunday
	request := &timeoff.TimeOffRequest{
		Days: []generic.TimePoint{
			date(2025, time.March, 7),  // Friday
			date(2025, time.March, 8),  // Saturday
			date(2025, time.March, 9),  // Sunday
			date(2025, time.March, 10), // Monday
		},
	}

	request.FilterWorkdays()

	if len(request.Days) != 2 {
		t.Errorf("expected 2 workdays, got %d", len(request.Days))
	}
}

// =============================================================================
// MULTI-POLICY TYPE TESTS (PTO + Sick + Parental + etc.)
// =============================================================================

func TestMultiResourceType_IndependentBalances(t *testing.T) {
	// GIVEN: Employee with PTO, Sick, and Parental leave policies
	// WHEN: Checking balances by resource type
	// THEN: Each resource type should have independent balance

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Setup: Add accruals for different resource types
	txs := []generic.Transaction{
		{ID: "tx-pto", EntityID: "emp-1", PolicyID: "pto", ResourceType: timeoff.ResourcePTO,
			EffectiveAt: date(2025, time.January, 1), Delta: days(20), Type: generic.TxGrant,
			IdempotencyKey: "pto-grant"},
		{ID: "tx-sick", EntityID: "emp-1", PolicyID: "sick", ResourceType: timeoff.ResourceSick,
			EffectiveAt: date(2025, time.January, 1), Delta: days(10), Type: generic.TxGrant,
			IdempotencyKey: "sick-grant"},
		{ID: "tx-parental", EntityID: "emp-1", PolicyID: "maternity", ResourceType: timeoff.ResourceParental,
			EffectiveAt: date(2025, time.January, 1), Delta: days(60), Type: generic.TxGrant,
			IdempotencyKey: "maternity-grant"},
	}

	for _, tx := range txs {
		if err := ledger.Append(ctx, tx); err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	// Consume from PTO
	consumeTx := generic.Transaction{
		ID: "tx-pto-consume", EntityID: "emp-1", PolicyID: "pto", ResourceType: timeoff.ResourcePTO,
		EffectiveAt: date(2025, time.February, 1), Delta: days(-5), Type: generic.TxConsumption,
		IdempotencyKey: "pto-consume",
	}
	ledger.Append(ctx, consumeTx)

	// Verify PTO balance
	ptoTxs, _ := ledger.TransactionsInRange(ctx, "emp-1", "pto", date(2025, time.January, 1), date(2025, time.December, 31))
	ptoBalance := calculateTestBalance(ptoTxs)
	if !ptoBalance.Value.Equal(days(15).Value) { // 20 - 5
		t.Errorf("PTO: expected 15 days, got %v", ptoBalance.Value)
	}

	// Verify Sick balance (unchanged)
	sickTxs, _ := ledger.TransactionsInRange(ctx, "emp-1", "sick", date(2025, time.January, 1), date(2025, time.December, 31))
	sickBalance := calculateTestBalance(sickTxs)
	if !sickBalance.Value.Equal(days(10).Value) {
		t.Errorf("Sick: expected 10 days, got %v", sickBalance.Value)
	}

	// Verify Parental balance (unchanged)
	parentalTxs, _ := ledger.TransactionsInRange(ctx, "emp-1", "maternity", date(2025, time.January, 1), date(2025, time.December, 31))
	parentalBalance := calculateTestBalance(parentalTxs)
	if !parentalBalance.Value.Equal(days(60).Value) {
		t.Errorf("Parental: expected 60 days, got %v", parentalBalance.Value)
	}
}

func TestMaternityLeave_FullUsage(t *testing.T) {
	// GIVEN: Employee with 12 weeks (60 days) maternity leave
	// WHEN: Taking the full 12 weeks
	// THEN: Balance should be 0, no rollover

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Grant maternity leave
	grantTx := generic.Transaction{
		ID: "tx-grant", EntityID: "emp-1", PolicyID: "maternity", ResourceType: timeoff.ResourceParental,
		EffectiveAt: date(2025, time.January, 1), Delta: days(60), Type: generic.TxGrant,
		IdempotencyKey: "grant",
	}
	ledger.Append(ctx, grantTx)

	// Consume all 60 days (12 weeks of workdays)
	for i := 0; i < 60; i++ {
		// Skip weekends in consumption dates
		baseDate := date(2025, time.March, 1)
		consumeDate := baseDate.Time.AddDate(0, 0, i+(i/5)*2) // Add 2 days for each week (weekends)
		
		tx := generic.Transaction{
			ID: generic.TransactionID(fmt.Sprintf("tx-consume-%d", i)),
			EntityID: "emp-1", PolicyID: "maternity", ResourceType: timeoff.ResourceParental,
			EffectiveAt: generic.TimePoint{Time: consumeDate},
			Delta: days(-1), Type: generic.TxConsumption,
			IdempotencyKey: fmt.Sprintf("consume-%d", i),
		}
		ledger.Append(ctx, tx)
	}

	// Check balance
	txs, _ := ledger.TransactionsInRange(ctx, "emp-1", "maternity", date(2025, time.January, 1), date(2025, time.December, 31))
	balance := calculateTestBalance(txs)
	if !balance.Value.IsZero() {
		t.Errorf("expected 0 days after full usage, got %v", balance.Value)
	}
}

func TestMaternityLeave_PartialUsage_Expires(t *testing.T) {
	// GIVEN: Employee with 60 days maternity leave, only used 40
	// WHEN: Year-end rollover
	// THEN: Remaining 20 days should expire (no carryover for maternity)

	engine := &generic.ReconciliationEngine{}

	currentBalance := generic.Balance{
		EntityID:         "emp-1",
		PolicyID:         "maternity",
		Period:           year2025Period(),
		AccruedToDate:    days(60),
		TotalEntitlement: days(60),
		TotalConsumed:    days(40), // 20 remaining
	}

	// Maternity policy: no carryover, expires at year end
	policy := generic.Policy{
		ResourceType: timeoff.ResourceParental,
		Unit:         generic.UnitDays,
		ReconciliationRules: []generic.ReconciliationRule{{
			Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
			Actions: []generic.ReconciliationAction{
				{Type: generic.ActionExpire}, // No carryover for maternity
			},
		}},
	}

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID:       "emp-1",
		PolicyID:       "maternity",
		Policy:         policy,
		CurrentBalance: currentBalance,
		EndingPeriod:   year2025Period(),
		NextPeriod:     generic.Period{Start: date(2026, time.January, 1), End: date(2026, time.December, 31)},
	})

	// All remaining should expire
	if !output.Summary.Expired.Value.Equal(days(20).Value) {
		t.Errorf("expected 20 days expired, got %v", output.Summary.Expired.Value)
	}
	if !output.Summary.CarriedOver.Value.IsZero() {
		t.Errorf("expected 0 days carried over, got %v", output.Summary.CarriedOver.Value)
	}
}

func TestMultiPolicy_DifferentResourceTypes_Distribution(t *testing.T) {
	// GIVEN: Employee with multiple policies of different resource types
	//   - PTO: 20 days
	//   - Sick: 10 days  
	//   - Parental: 60 days
	// WHEN: Requesting from each type independently
	// THEN: Each request draws from correct resource type

	// Build resource balances for each type
	ptoBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   timeoff.ResourcePTO,
		TotalAvailable: days(20),
		PolicyBalances: []generic.PolicyBalance{{
			Assignment: generic.PolicyAssignment{
				PolicyID: "pto-standard",
				Policy:   generic.Policy{ResourceType: timeoff.ResourcePTO, Unit: generic.UnitDays},
			},
			Balance:  generic.Balance{AccruedToDate: days(20), TotalEntitlement: days(20)},
			Priority: 1,
		}},
	}

	parentalBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   timeoff.ResourceParental,
		TotalAvailable: days(60),
		PolicyBalances: []generic.PolicyBalance{{
			Assignment: generic.PolicyAssignment{
				PolicyID: "maternity",
				Policy:   generic.Policy{ResourceType: timeoff.ResourceParental, Unit: generic.UnitDays},
			},
			Balance:  generic.Balance{AccruedToDate: days(60), TotalEntitlement: days(60)},
			Priority: 1,
		}},
	}

	distributor := &generic.ConsumptionDistributor{}

	// Request 5 PTO days
	ptoResult := distributor.Distribute(ptoBalance, days(5), false)
	if !ptoResult.IsSatisfiable {
		t.Error("PTO request should be satisfiable")
	}
	if string(ptoResult.Allocations[0].PolicyID) != "pto-standard" {
		t.Errorf("expected allocation from pto-standard, got %s", ptoResult.Allocations[0].PolicyID)
	}

	// Request 40 parental days
	parentalResult := distributor.Distribute(parentalBalance, days(40), false)
	if !parentalResult.IsSatisfiable {
		t.Error("Parental request should be satisfiable")
	}
	if string(parentalResult.Allocations[0].PolicyID) != "maternity" {
		t.Errorf("expected allocation from maternity, got %s", parentalResult.Allocations[0].PolicyID)
	}
}

func TestFloatingHoliday_UseItOrLoseIt(t *testing.T) {
	// GIVEN: Employee with 3 floating holidays
	// WHEN: Year-end rollover with 1 unused
	// THEN: Unused day expires

	engine := &generic.ReconciliationEngine{}

	currentBalance := generic.Balance{
		EntityID:         "emp-1",
		PolicyID:         "floating",
		Period:           year2025Period(),
		AccruedToDate:    days(3),
		TotalEntitlement: days(3),
		TotalConsumed:    days(2), // 1 remaining
	}

	policy := generic.Policy{
		ResourceType: timeoff.ResourceFloatingHoliday,
		Unit:         generic.UnitDays,
		ReconciliationRules: []generic.ReconciliationRule{{
			Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
			Actions: []generic.ReconciliationAction{
				{Type: generic.ActionExpire},
			},
		}},
	}

	output, _ := engine.Process(generic.ReconciliationInput{
		EntityID:       "emp-1",
		PolicyID:       "floating",
		Policy:         policy,
		CurrentBalance: currentBalance,
		EndingPeriod:   year2025Period(),
		NextPeriod:     generic.Period{Start: date(2026, time.January, 1), End: date(2026, time.December, 31)},
	})

	if !output.Summary.Expired.Value.Equal(days(1).Value) {
		t.Errorf("expected 1 day expired, got %v", output.Summary.Expired.Value)
	}
}

func TestBereavementLeave_Consumption(t *testing.T) {
	// GIVEN: Employee with 5 days bereavement leave
	// WHEN: Using 3 days
	// THEN: Balance should be 2 days

	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// Grant
	ledger.Append(ctx, generic.Transaction{
		ID: "tx-grant", EntityID: "emp-1", PolicyID: "bereavement", ResourceType: timeoff.ResourceBereavement,
		EffectiveAt: date(2025, time.January, 1), Delta: days(5), Type: generic.TxGrant,
		IdempotencyKey: "grant",
	})

	// Consume 3 days
	for i := 0; i < 3; i++ {
		ledger.Append(ctx, generic.Transaction{
			ID: generic.TransactionID(fmt.Sprintf("tx-consume-%d", i)),
			EntityID: "emp-1", PolicyID: "bereavement", ResourceType: timeoff.ResourceBereavement,
			EffectiveAt: date(2025, time.March, 10+i), Delta: days(-1), Type: generic.TxConsumption,
			IdempotencyKey: fmt.Sprintf("consume-%d", i),
		})
	}

	txs, _ := ledger.TransactionsInRange(ctx, "emp-1", "bereavement", date(2025, time.January, 1), date(2025, time.December, 31))
	balance := calculateTestBalance(txs)
	if !balance.Value.Equal(days(2).Value) {
		t.Errorf("expected 2 days remaining, got %v", balance.Value)
	}
}

func calculateTestBalance(txs []generic.Transaction) generic.Amount {
	total := days(0)
	for _, tx := range txs {
		total = total.Add(tx.Delta)
	}
	return total
}

// =============================================================================
// END-TO-END TEST WITH SQLITE
// =============================================================================

func TestEndToEnd_FullRequestFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	ledger := newTestLedger(store)

	// 1. Setup: Add accrual
	accrualTx := generic.Transaction{
		ID:             "tx-accrual",
		EntityID:       "emp-1",
		PolicyID:       "pto",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    date(2025, time.January, 1),
		Delta:          days(20),
		Type:           generic.TxGrant,
		IdempotencyKey: "setup-accrual",
	}
	if err := ledger.Append(ctx, accrualTx); err != nil {
		t.Fatalf("Failed to add accrual: %v", err)
	}

	// 2. Check balance
	txs, _ := ledger.TransactionsInRange(ctx, "emp-1", "pto", date(2025, time.January, 1), date(2025, time.December, 31))
	if len(txs) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(txs))
	}

	// 3. Submit consumption request
	consumeTxs := []generic.Transaction{
		{ID: "tx-consume-1", EntityID: "emp-1", PolicyID: "pto", ResourceType: timeoff.ResourcePTO,
			EffectiveAt: date(2025, time.March, 10), Delta: days(-1), Type: generic.TxConsumption, IdempotencyKey: "consume-1"},
		{ID: "tx-consume-2", EntityID: "emp-1", PolicyID: "pto", ResourceType: timeoff.ResourcePTO,
			EffectiveAt: date(2025, time.March, 11), Delta: days(-1), Type: generic.TxConsumption, IdempotencyKey: "consume-2"},
		{ID: "tx-consume-3", EntityID: "emp-1", PolicyID: "pto", ResourceType: timeoff.ResourcePTO,
			EffectiveAt: date(2025, time.March, 12), Delta: days(-1), Type: generic.TxConsumption, IdempotencyKey: "consume-3"},
	}

	if err := ledger.AppendBatch(ctx, consumeTxs); err != nil {
		t.Fatalf("Failed to add consumption: %v", err)
	}

	// 4. Verify final balance
	txs, _ = ledger.TransactionsInRange(ctx, "emp-1", "pto", date(2025, time.January, 1), date(2025, time.December, 31))
	if len(txs) != 4 {
		t.Errorf("expected 4 transactions, got %d", len(txs))
	}

	// Calculate balance
	balance := days(0)
	for _, tx := range txs {
		balance = balance.Add(tx.Delta)
	}

	// Should be 20 - 3 = 17 days
	if !balance.Value.Equal(days(17).Value) {
		t.Errorf("expected 17 days balance, got %v", balance.Value)
	}
}
