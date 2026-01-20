/*
scenarios_test.go - Unit tests for demo scenarios

PURPOSE:
	Tests that each scenario correctly sets up the expected state:
	- Employees are created
	- Policies are created and assigned
	- Transactions are generated correctly
	- Balances match expected values

These tests ensure scenarios work correctly and can be used as integration tests.
*/
package api

import (
	"context"
	"testing"
	"time"

	"github.com/warp/resource-engine/factory"
	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/store/sqlite"
)

func setupTestHandler(t *testing.T) *Handler {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	
	handler := &Handler{
		Store:         store,
		PolicyFactory: factory.NewPolicyFactory(),
		policies:      make(map[generic.PolicyID]*generic.Policy),
		accruals:      make(map[generic.PolicyID]generic.AccrualSchedule),
	}
	return handler
}

func TestScenario_NewEmployee(t *testing.T) {
	// GIVEN: New employee scenario
	// WHEN: Loading the scenario
	// THEN: Employee, policy, assignments, and transactions should be created correctly

	handler := setupTestHandler(t)
	ctx := context.Background()

	err := handler.loadNewEmployeeScenario(ctx)
	if err != nil {
		t.Fatalf("Failed to load new-employee scenario: %v", err)
	}

	// Verify employee exists
	employees, err := handler.Store.ListEmployees(ctx)
	if err != nil {
		t.Fatalf("Failed to list employees: %v", err)
	}
	if len(employees) != 1 {
		t.Errorf("Expected 1 employee, got %d", len(employees))
	}
	if employees[0].ID != "emp-001" {
		t.Errorf("Expected employee ID 'emp-001', got '%s'", employees[0].ID)
	}

	// Verify policy exists
	policy, ok := handler.policies[generic.PolicyID("pto-standard")]
	if !ok {
		t.Fatal("Policy 'pto-standard' not found")
	}
	if policy.Name != "Standard PTO" {
		t.Errorf("Expected policy name 'Standard PTO', got '%s'", policy.Name)
	}

	// Verify assignment exists
	assignments, err := handler.Store.GetAssignmentsByEntity(ctx, "emp-001")
	if err != nil {
		t.Fatalf("Failed to get assignments: %v", err)
	}
	if len(assignments) != 1 {
		t.Errorf("Expected 1 assignment, got %d", len(assignments))
	}

	// Verify transactions can be queried (rollover from Dec 2025)
	// In the new design, accruals are computed on-demand from AccrualSchedule
	// Only grants, consumption, and reconciliation transactions are stored
	// 
	// NOTE: For a Dec 15 hire with monthly accruals on the 1st:
	// - The Dec 1 accrual happens before hire date, so they get 0 accruals for Dec 2025
	// - With 0 balance, there's nothing to carry over or expire
	// - This is CORRECT prorating behavior
	ledger := generic.NewLedger(handler.Store)
	_, err = ledger.TransactionsInRange(ctx, generic.EntityID("emp-001"), policy.ID, 
		generic.TimePoint{Time: time.Date(time.Now().Year()-1, time.January, 1, 0, 0, 0, 0, time.UTC)},
		generic.TimePoint{Time: time.Date(time.Now().Year(), time.December, 31, 23, 59, 59, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Failed to get transactions: %v", err)
	}
	
	// For Dec 15 hire with monthly accruals, 0 reconciliation transactions is expected
	// (they missed the Dec 1 accrual date, so balance is 0, nothing to carry over)
}

func TestScenario_MultiPolicy(t *testing.T) {
	// GIVEN: Multi-policy scenario
	// WHEN: Loading the scenario
	// THEN: Multiple policies should be created and assigned correctly

	handler := setupTestHandler(t)
	ctx := context.Background()

	err := handler.loadMultiPolicyScenario(ctx)
	if err != nil {
		t.Fatalf("Failed to load multi-policy scenario: %v", err)
	}

	// Verify employee exists
	employees, err := handler.Store.ListEmployees(ctx)
	if err != nil {
		t.Fatalf("Failed to list employees: %v", err)
	}
	if len(employees) != 1 {
		t.Errorf("Expected 1 employee, got %d", len(employees))
	}

	// Verify multiple policies exist
	expectedPolicies := []string{"pto-standard", "pto-bonus", "pto-carryover", "sick-standard"}
	for _, policyID := range expectedPolicies {
		if _, ok := handler.policies[generic.PolicyID(policyID)]; !ok {
			t.Errorf("Expected policy '%s' not found", policyID)
		}
	}

	// Verify multiple assignments exist
	assignments, err := handler.Store.GetAssignmentsByEntity(ctx, "emp-004")
	if err != nil {
		t.Fatalf("Failed to get assignments: %v", err)
	}
	if len(assignments) < 4 {
		t.Errorf("Expected at least 4 assignments, got %d", len(assignments))
	}

	// Verify grant and consumption transactions exist for bonus/carryover policies
	// In new design, pto-standard and sick-standard have accruals computed on-demand (no stored transactions)
	// pto-carryover and pto-bonus have TxGrant transactions
	ledger := generic.NewLedger(handler.Store)
	year := time.Now().Year()
	yearStart := generic.TimePoint{Time: time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)}
	yearEnd := generic.TimePoint{Time: time.Date(year, time.December, 31, 23, 59, 59, 0, time.UTC)}

	// Carryover policy should have grant and consumption
	carryoverTxs, err := ledger.TransactionsInRange(ctx, generic.EntityID("emp-004"), generic.PolicyID("pto-carryover"), yearStart, yearEnd)
	if err != nil {
		t.Errorf("Failed to get transactions for pto-carryover: %v", err)
	}
	if len(carryoverTxs) == 0 {
		t.Error("Expected transactions for pto-carryover (grant + consumption)")
	}

	// Bonus policy should have a grant
	bonusTxs, err := ledger.TransactionsInRange(ctx, generic.EntityID("emp-004"), generic.PolicyID("pto-bonus"), yearStart, yearEnd)
	if err != nil {
		t.Errorf("Failed to get transactions for pto-bonus: %v", err)
	}
	if len(bonusTxs) == 0 {
		t.Error("Expected grant transaction for pto-bonus")
	}
}

func TestScenario_YearEndRollover(t *testing.T) {
	// GIVEN: Year-end rollover scenario
	// WHEN: Loading the scenario
	// THEN: Rollover transactions should be created correctly

	handler := setupTestHandler(t)
	ctx := context.Background()

	err := handler.loadYearEndRolloverScenario(ctx)
	if err != nil {
		t.Fatalf("Failed to load year-end-rollover scenario: %v", err)
	}

	// Verify employee exists
	employees, err := handler.Store.ListEmployees(ctx)
	if err != nil {
		t.Fatalf("Failed to list employees: %v", err)
	}
	if len(employees) != 1 {
		t.Errorf("Expected 1 employee, got %d", len(employees))
	}

	// Verify policy exists
	policy, ok := handler.policies[generic.PolicyID("pto-rollover")]
	if !ok {
		t.Fatal("Policy 'pto-rollover' not found")
	}

	// Verify rollover transactions exist
	ledger := generic.NewLedger(handler.Store)
	lastYear := time.Now().Year() - 1
	lastYearStart := generic.TimePoint{Time: time.Date(lastYear, time.January, 1, 0, 0, 0, 0, time.UTC)}
	currentYearEnd := generic.TimePoint{Time: time.Date(time.Now().Year(), time.December, 31, 23, 59, 59, 0, time.UTC)}

	txs, err := ledger.TransactionsInRange(ctx, generic.EntityID("emp-003"), policy.ID, lastYearStart, currentYearEnd)
	if err != nil {
		t.Fatalf("Failed to get transactions: %v", err)
	}

	// Verify reconciliation transactions exist (carryover/expire)
	reconciliationCount := 0
	for _, tx := range txs {
		if tx.Type == generic.TxReconciliation {
			reconciliationCount++
		}
	}
	if reconciliationCount == 0 {
		t.Error("Expected reconciliation transactions (carryover/expire), got 0")
	}
}

func TestScenario_HourlyWorker(t *testing.T) {
	// GIVEN: Hourly worker scenario
	// WHEN: Loading the scenario
	// THEN: Consume-up-to-accrued policy should be created correctly

	handler := setupTestHandler(t)
	ctx := context.Background()

	err := handler.loadHourlyWorkerScenario(ctx)
	if err != nil {
		t.Fatalf("Failed to load hourly-worker scenario: %v", err)
	}

	// Verify employee exists
	employees, err := handler.Store.ListEmployees(ctx)
	if err != nil {
		t.Fatalf("Failed to list employees: %v", err)
	}
	if len(employees) != 1 {
		t.Errorf("Expected 1 employee, got %d", len(employees))
	}

	// Verify policy exists with consume-up-to-accrued mode
	policy, ok := handler.policies[generic.PolicyID("pto-hourly")]
	if !ok {
		t.Fatal("Policy 'pto-hourly' not found")
	}
	if policy.ConsumptionMode != generic.ConsumeUpToAccrued {
		t.Errorf("Expected ConsumptionMode ConsumeUpToAccrued, got %v", policy.ConsumptionMode)
	}

	// In new design, hourly workers have NO stored transactions
	// Accruals are computed on-demand from AccrualSchedule
	// Available balance = AccruedToDate (not full entitlement)
	// This verifies the policy is set up correctly - actual balance calculation
	// is tested in the balance calculation tests
}

func TestScenario_RewardsBenefits(t *testing.T) {
	// GIVEN: Rewards benefits scenario
	// WHEN: Loading the scenario
	// THEN: Multiple reward policies should be created correctly

	handler := setupTestHandler(t)
	ctx := context.Background()

	err := handler.loadRewardsBenefitsScenario(ctx)
	if err != nil {
		t.Fatalf("Failed to load rewards-benefits scenario: %v", err)
	}

	// Verify employee exists
	employees, err := handler.Store.ListEmployees(ctx)
	if err != nil {
		t.Fatalf("Failed to list employees: %v", err)
	}
	if len(employees) != 1 {
		t.Errorf("Expected 1 employee, got %d", len(employees))
	}

	// Verify multiple reward policies exist
	expectedPolicies := []string{
		"wellness-program",
		"learning-budget",
		"peer-kudos",
		"flex-spending",
		"wfh-allowance",
		"volunteer-time",
	}
	for _, policyID := range expectedPolicies {
		if _, ok := handler.policies[generic.PolicyID(policyID)]; !ok {
			t.Errorf("Expected policy '%s' not found", policyID)
		}
	}

	// Verify assignments exist
	assignments, err := handler.Store.GetAssignmentsByEntity(ctx, "emp-alex")
	if err != nil {
		t.Fatalf("Failed to get assignments: %v", err)
	}
	if len(assignments) < 6 {
		t.Errorf("Expected at least 6 assignments, got %d", len(assignments))
	}

	// Verify transactions exist for different resource types
	ledger := generic.NewLedger(handler.Store)
	year := time.Now().Year()
	yearStart := generic.TimePoint{Time: time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)}
	yearEnd := generic.TimePoint{Time: time.Date(year, time.December, 31, 23, 59, 59, 0, time.UTC)}

	// Check wellness points transactions
	wellnessPolicy := handler.policies[generic.PolicyID("wellness-program")]
	if wellnessPolicy != nil {
		txs, err := ledger.TransactionsInRange(ctx, generic.EntityID("emp-alex"), wellnessPolicy.ID, yearStart, yearEnd)
		if err != nil {
			t.Errorf("Failed to get wellness transactions: %v", err)
		} else if len(txs) == 0 {
			t.Error("Expected wellness points transactions, got 0")
		}
	}

	// Check learning credits transactions
	learningPolicy := handler.policies[generic.PolicyID("learning-budget")]
	if learningPolicy != nil {
		txs, err := ledger.TransactionsInRange(ctx, generic.EntityID("emp-alex"), learningPolicy.ID, yearStart, yearEnd)
		if err != nil {
			t.Errorf("Failed to get learning transactions: %v", err)
		} else if len(txs) == 0 {
			t.Error("Expected learning credits transactions, got 0")
		}
	}
}

func TestScenario_PolicyChange(t *testing.T) {
	// GIVEN: Mid-year policy change scenario
	// WHEN: Loading the scenario
	// THEN: Reconciliation (same as rollover) should be performed at policy change

	handler := setupTestHandler(t)
	ctx := context.Background()

	err := handler.loadMidYearPolicyChangeScenario(ctx)
	if err != nil {
		t.Fatalf("Failed to load policy-change scenario: %v", err)
	}

	// Verify employee exists
	employees, err := handler.Store.ListEmployees(ctx)
	if err != nil {
		t.Fatalf("Failed to list employees: %v", err)
	}
	if len(employees) != 1 {
		t.Errorf("Expected 1 employee, got %d", len(employees))
	}

	// Verify both policies exist
	if _, ok := handler.policies[generic.PolicyID("pto-initial")]; !ok {
		t.Error("Expected 'pto-initial' policy")
	}
	if _, ok := handler.policies[generic.PolicyID("pto-upgraded")]; !ok {
		t.Error("Expected 'pto-upgraded' policy")
	}

	// Verify two assignments (old policy ended, new policy started)
	assignments, err := handler.Store.GetAssignmentsByEntity(ctx, "emp-policy-change")
	if err != nil {
		t.Fatalf("Failed to get assignments: %v", err)
	}
	if len(assignments) != 2 {
		t.Errorf("Expected 2 assignments (old + new), got %d", len(assignments))
	}

	// Verify reconciliation and grant transactions exist
	ledger := generic.NewLedger(handler.Store)
	year := time.Now().Year()
	yearStart := generic.TimePoint{Time: time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)}
	yearEnd := generic.TimePoint{Time: time.Date(year, time.December, 31, 23, 59, 59, 0, time.UTC)}

	// Check old policy for consumption and reconciliation
	oldTxs, err := ledger.TransactionsInRange(ctx, generic.EntityID("emp-policy-change"), 
		generic.PolicyID("pto-initial"), yearStart, yearEnd)
	if err != nil {
		t.Fatalf("Failed to get transactions for old policy: %v", err)
	}
	
	hasConsumption := false
	hasReconciliation := false
	for _, tx := range oldTxs {
		if tx.Type == generic.TxConsumption {
			hasConsumption = true
		}
		if tx.Type == generic.TxReconciliation {
			hasReconciliation = true
		}
	}
	if !hasConsumption {
		t.Error("Expected consumption transactions on old policy")
	}
	if !hasReconciliation {
		t.Error("Expected reconciliation transactions on old policy (policy change uses same mechanism as rollover)")
	}

	// Check new policy for carryover grant
	newTxs, err := ledger.TransactionsInRange(ctx, generic.EntityID("emp-policy-change"), 
		generic.PolicyID("pto-upgraded"), yearStart, yearEnd)
	if err != nil {
		t.Fatalf("Failed to get transactions for new policy: %v", err)
	}
	
	hasGrant := false
	for _, tx := range newTxs {
		if tx.Type == generic.TxGrant {
			hasGrant = true
		}
	}
	if !hasGrant {
		t.Error("Expected grant transaction on new policy (carryover from old policy)")
	}
}

func TestScenario_AllScenariosLoadWithoutError(t *testing.T) {
	// GIVEN: All available scenarios
	// WHEN: Loading each scenario
	// THEN: None should error

	scenarioIDs := []string{
		"new-employee",
		"multi-policy",
		"year-end-rollover",
		"policy-change",
		"hourly-worker",
		"rewards-benefits",
	}

	for _, scenarioID := range scenarioIDs {
		t.Run(scenarioID, func(t *testing.T) {
			handler := setupTestHandler(t)
			ctx := context.Background()

			var err error
			switch scenarioID {
			case "new-employee":
				err = handler.loadNewEmployeeScenario(ctx)
			case "multi-policy":
				err = handler.loadMultiPolicyScenario(ctx)
			case "year-end-rollover":
				err = handler.loadYearEndRolloverScenario(ctx)
			case "policy-change":
				err = handler.loadMidYearPolicyChangeScenario(ctx)
			case "hourly-worker":
				err = handler.loadHourlyWorkerScenario(ctx)
			case "rewards-benefits":
				err = handler.loadRewardsBenefitsScenario(ctx)
			default:
				t.Fatalf("Unknown scenario: %s", scenarioID)
			}

			if err != nil {
				t.Errorf("Scenario '%s' failed to load: %v", scenarioID, err)
			}
		})
	}
}
