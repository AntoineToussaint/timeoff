/*
balance_test.go - Unit tests for balance calculation and period-end snapshots

CORE DESIGN:
- Accruals are COMPUTED on-demand from AccrualSchedule, never stored
- At period end, remaining balance is "snapshotted" as a TxGrant for carryover
- Next period starts with that TxGrant + new computed accruals
*/
package api

import (
	"testing"
	"time"

	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/timeoff"
)

// =============================================================================
// BALANCE CALCULATION TESTS
// =============================================================================

func TestCalculateBalance_NewEmployee_MidDecemberHire(t *testing.T) {
	// GIVEN: Employee hired Dec 15, policy with 24 days/year (2/month), monthly accruals on 1st
	// WHEN: Calculating balance at Dec 31
	// THEN: Should have 0 accrued (missed Dec 1 accrual date)

	hireDate := generic.NewTimePoint(2025, time.December, 15)
	periodStart := generic.NewTimePoint(2025, time.January, 1)
	periodEnd := generic.NewTimePoint(2025, time.December, 31)
	period := generic.Period{Start: periodStart, End: periodEnd}

	accrual := &timeoff.YearlyAccrual{
		AnnualDays: 24,
		Frequency:  generic.FreqMonthly,
	}

	// Calculate with hire date prorating
	balance := calculateBalanceWithHireDate(
		nil, // no transactions
		period,
		generic.UnitDays,
		accrual,
		periodEnd, // asOf
		hireDate,  // hire date
	)

	// Dec 15 hire misses Dec 1 accrual, so 0 days accrued
	accruedDays, _ := balance.AccruedToDate.Value.Float64()
	if accruedDays != 0 {
		t.Errorf("Expected 0 days accrued for Dec 15 hire (missed Dec 1 accrual), got %.2f", accruedDays)
	}
}

func TestCalculateBalance_NewEmployee_EarlyDecemberHire(t *testing.T) {
	// GIVEN: Employee hired Dec 1, policy with 24 days/year (2/month)
	// WHEN: Calculating balance at Dec 31
	// THEN: Should have 2 days accrued (Dec 1 accrual)

	hireDate := generic.NewTimePoint(2025, time.December, 1)
	periodStart := generic.NewTimePoint(2025, time.January, 1)
	periodEnd := generic.NewTimePoint(2025, time.December, 31)
	period := generic.Period{Start: periodStart, End: periodEnd}

	accrual := &timeoff.YearlyAccrual{
		AnnualDays: 24,
		Frequency:  generic.FreqMonthly,
	}

	balance := calculateBalanceWithHireDate(
		nil,
		period,
		generic.UnitDays,
		accrual,
		periodEnd,
		hireDate,
	)

	accruedDays, _ := balance.AccruedToDate.Value.Float64()
	if accruedDays != 2 {
		t.Errorf("Expected 2 days accrued for Dec 1 hire, got %.2f", accruedDays)
	}
}

func TestCalculateBalance_NewEmployee_JulyHire(t *testing.T) {
	// GIVEN: Employee hired July 1, policy with 24 days/year (2/month)
	// WHEN: Calculating balance at Dec 31
	// THEN: Should have 12 days accrued (Jul-Dec = 6 months * 2 days)

	hireDate := generic.NewTimePoint(2025, time.July, 1)
	periodStart := generic.NewTimePoint(2025, time.January, 1)
	periodEnd := generic.NewTimePoint(2025, time.December, 31)
	period := generic.Period{Start: periodStart, End: periodEnd}

	accrual := &timeoff.YearlyAccrual{
		AnnualDays: 24,
		Frequency:  generic.FreqMonthly,
	}

	balance := calculateBalanceWithHireDate(
		nil,
		period,
		generic.UnitDays,
		accrual,
		periodEnd,
		hireDate,
	)

	accruedDays, _ := balance.AccruedToDate.Value.Float64()
	if accruedDays != 12 {
		t.Errorf("Expected 12 days accrued for July 1 hire (6 months), got %.2f", accruedDays)
	}
}

func TestCalculateBalance_ExistingEmployee_FullYear(t *testing.T) {
	// GIVEN: Employee hired Jan 1, policy with 24 days/year
	// WHEN: Calculating balance at Dec 31
	// THEN: Should have 24 days accrued (full year)

	hireDate := generic.NewTimePoint(2025, time.January, 1)
	periodStart := generic.NewTimePoint(2025, time.January, 1)
	periodEnd := generic.NewTimePoint(2025, time.December, 31)
	period := generic.Period{Start: periodStart, End: periodEnd}

	accrual := &timeoff.YearlyAccrual{
		AnnualDays: 24,
		Frequency:  generic.FreqMonthly,
	}

	balance := calculateBalanceWithHireDate(
		nil,
		period,
		generic.UnitDays,
		accrual,
		periodEnd,
		hireDate,
	)

	accruedDays, _ := balance.AccruedToDate.Value.Float64()
	if accruedDays != 24 {
		t.Errorf("Expected 24 days accrued for full year, got %.2f", accruedDays)
	}
}

func TestCalculateBalance_WithConsumption(t *testing.T) {
	// GIVEN: Employee with 24 days accrued, used 10 days
	// WHEN: Calculating balance
	// THEN: Remaining should be 14 days

	hireDate := generic.NewTimePoint(2025, time.January, 1)
	periodStart := generic.NewTimePoint(2025, time.January, 1)
	periodEnd := generic.NewTimePoint(2025, time.December, 31)
	period := generic.Period{Start: periodStart, End: periodEnd}

	accrual := &timeoff.YearlyAccrual{
		AnnualDays: 24,
		Frequency:  generic.FreqMonthly,
	}

	// 10 days consumed
	txs := []generic.Transaction{
		{
			Type:  generic.TxConsumption,
			Delta: generic.NewAmount(-10, generic.UnitDays),
		},
	}

	balance := calculateBalanceWithHireDate(
		txs,
		period,
		generic.UnitDays,
		accrual,
		periodEnd,
		hireDate,
	)

	// CurrentAccrued = AccruedToDate - TotalConsumed + Adjustments = 24 - 10 + 0 = 14
	remaining := balance.CurrentAccrued()
	remainingDays, _ := remaining.Value.Float64()
	if remainingDays != 14 {
		t.Errorf("Expected 14 days remaining (24 accrued - 10 consumed), got %.2f", remainingDays)
	}
}

// =============================================================================
// PERIOD-END SNAPSHOT TESTS
// =============================================================================

func TestPeriodEndSnapshot_CarryoverUnderMax(t *testing.T) {
	// GIVEN: 5 days remaining, max carryover 10
	// WHEN: Period ends
	// THEN: All 5 days carry over as TxGrant

	engine := &generic.ReconciliationEngine{}
	maxCarry := generic.NewAmount(10, generic.UnitDays)

	input := generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "pto-1",
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
		CurrentBalance: generic.Balance{
			AccruedToDate: generic.NewAmount(5, generic.UnitDays),
			TotalConsumed: generic.NewAmount(0, generic.UnitDays),
			Adjustments:   generic.NewAmount(0, generic.UnitDays),
		},
		EndingPeriod: generic.Period{
			Start: generic.NewTimePoint(2025, time.January, 1),
			End:   generic.NewTimePoint(2025, time.December, 31),
		},
		NextPeriod: generic.Period{
			Start: generic.NewTimePoint(2026, time.January, 1),
			End:   generic.NewTimePoint(2026, time.December, 31),
		},
	}

	output, err := engine.Process(input)
	if err != nil {
		t.Fatalf("Reconciliation failed: %v", err)
	}

	// Should create 1 transaction: TxGrant for 5 days carryover
	if len(output.Transactions) != 1 {
		t.Errorf("Expected 1 transaction (carryover), got %d", len(output.Transactions))
	}

	if len(output.Transactions) > 0 {
		tx := output.Transactions[0]
		if tx.Type != generic.TxReconciliation {
			t.Errorf("Expected TxReconciliation, got %v", tx.Type)
		}
		carryDays, _ := tx.Delta.Value.Float64()
		if carryDays != 5 {
			t.Errorf("Expected 5 days carryover, got %.2f", carryDays)
		}
	}

	carryoverDays, _ := output.Summary.CarriedOver.Value.Float64()
	if carryoverDays != 5 {
		t.Errorf("Expected 5 days carried over, got %.2f", carryoverDays)
	}
}

func TestPeriodEndSnapshot_CarryoverAtMax(t *testing.T) {
	// GIVEN: 15 days remaining, max carryover 5
	// WHEN: Period ends
	// THEN: 5 days carry over, 10 expire

	engine := &generic.ReconciliationEngine{}
	maxCarry := generic.NewAmount(5, generic.UnitDays)

	input := generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "pto-1",
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
		CurrentBalance: generic.Balance{
			AccruedToDate: generic.NewAmount(20, generic.UnitDays),
			TotalConsumed: generic.NewAmount(5, generic.UnitDays), // 20-5=15 remaining
			Adjustments:   generic.NewAmount(0, generic.UnitDays),
		},
		EndingPeriod: generic.Period{
			Start: generic.NewTimePoint(2025, time.January, 1),
			End:   generic.NewTimePoint(2025, time.December, 31),
		},
		NextPeriod: generic.Period{
			Start: generic.NewTimePoint(2026, time.January, 1),
			End:   generic.NewTimePoint(2026, time.December, 31),
		},
	}

	output, err := engine.Process(input)
	if err != nil {
		t.Fatalf("Reconciliation failed: %v", err)
	}

	carryoverDays, _ := output.Summary.CarriedOver.Value.Float64()
	expiredDays, _ := output.Summary.Expired.Value.Float64()

	if carryoverDays != 5 {
		t.Errorf("Expected 5 days carried over (max), got %.2f", carryoverDays)
	}
	if expiredDays != 10 {
		t.Errorf("Expected 10 days expired (15 remaining - 5 max carryover), got %.2f", expiredDays)
	}
}

func TestPeriodEndSnapshot_ZeroBalance_NoTransactions(t *testing.T) {
	// GIVEN: 0 days remaining (new hire who missed accrual dates)
	// WHEN: Period ends
	// THEN: No transactions created

	engine := &generic.ReconciliationEngine{}
	maxCarry := generic.NewAmount(5, generic.UnitDays)

	input := generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "pto-1",
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
		CurrentBalance: generic.Balance{
			AccruedToDate: generic.NewAmount(0, generic.UnitDays), // Dec 15 hire, 0 accrued
			TotalConsumed: generic.NewAmount(0, generic.UnitDays),
			Adjustments:   generic.NewAmount(0, generic.UnitDays),
		},
		EndingPeriod: generic.Period{
			Start: generic.NewTimePoint(2025, time.January, 1),
			End:   generic.NewTimePoint(2025, time.December, 31),
		},
		NextPeriod: generic.Period{
			Start: generic.NewTimePoint(2026, time.January, 1),
			End:   generic.NewTimePoint(2026, time.December, 31),
		},
	}

	output, err := engine.Process(input)
	if err != nil {
		t.Fatalf("Reconciliation failed: %v", err)
	}

	// 0 balance = nothing to carry over or expire
	if len(output.Transactions) != 0 {
		t.Errorf("Expected 0 transactions for 0 balance, got %d", len(output.Transactions))
	}
}

func TestPeriodEndSnapshot_WithPreviousCarryover(t *testing.T) {
	// GIVEN: Year 2 employee with 5 days carryover from Y1 + 24 days new accrual, used 10
	// WHEN: Y2 period ends
	// THEN: Remaining = 5 + 24 - 10 = 19, carryover max 5, expire 14

	engine := &generic.ReconciliationEngine{}
	maxCarry := generic.NewAmount(5, generic.UnitDays)

	// The 5 days carryover is in Adjustments (from TxGrant at start of Y2)
	input := generic.ReconciliationInput{
		EntityID: "emp-1",
		PolicyID: "pto-1",
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
		CurrentBalance: generic.Balance{
			AccruedToDate: generic.NewAmount(24, generic.UnitDays), // This year's accrual
			TotalConsumed: generic.NewAmount(10, generic.UnitDays),
			Adjustments:   generic.NewAmount(5, generic.UnitDays), // Carryover from last year
		},
		EndingPeriod: generic.Period{
			Start: generic.NewTimePoint(2026, time.January, 1),
			End:   generic.NewTimePoint(2026, time.December, 31),
		},
		NextPeriod: generic.Period{
			Start: generic.NewTimePoint(2027, time.January, 1),
			End:   generic.NewTimePoint(2027, time.December, 31),
		},
	}

	output, err := engine.Process(input)
	if err != nil {
		t.Fatalf("Reconciliation failed: %v", err)
	}

	carryoverDays, _ := output.Summary.CarriedOver.Value.Float64()
	expiredDays, _ := output.Summary.Expired.Value.Float64()

	// Remaining = 24 - 10 + 5 = 19 days
	// Max carryover = 5, so 14 expire
	if carryoverDays != 5 {
		t.Errorf("Expected 5 days carried over (max), got %.2f", carryoverDays)
	}
	if expiredDays != 14 {
		t.Errorf("Expected 14 days expired (19 remaining - 5 max carryover), got %.2f", expiredDays)
	}
}
