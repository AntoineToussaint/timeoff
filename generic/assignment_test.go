package generic_test

import (
	"testing"
	"time"

	"github.com/warp/resource-engine/generic"
)

// =============================================================================
// TEST RESOURCE TYPE - For testing without domain dependencies
// =============================================================================

// testResource is a concrete ResourceType for tests
type testResource string

func (r testResource) ResourceID() string     { return string(r) }
func (r testResource) ResourceDomain() string { return "test" }

const testResourceType testResource = "test_resource"

// =============================================================================
// MULTI-POLICY CONSUMPTION DISTRIBUTION TESTS
// =============================================================================

func TestConsumptionDistribution_SinglePolicy(t *testing.T) {
	// GIVEN: One policy with 20 days available
	// WHEN: Request 5 days
	// THEN: All 5 days from that policy

	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   testResourceType,
		TotalAvailable: days(20),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "policy-a",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
				},
				Balance:  balance(20, 0),
				Priority: 1,
			},
		},
	}

	distributor := &generic.ConsumptionDistributor{}
	result := distributor.Distribute(resourceBalance, days(5), false)

	if !result.IsSatisfiable {
		t.Error("should be satisfiable")
	}
	if len(result.Allocations) != 1 {
		t.Errorf("expected 1 allocation, got %d", len(result.Allocations))
	}
	if !result.Allocations[0].Amount.Value.Equal(days(5).Value) {
		t.Errorf("expected 5 days from policy, got %v", result.Allocations[0].Amount.Value)
	}
}

func TestConsumptionDistribution_MultiplePolices_ByPriority(t *testing.T) {
	// GIVEN: Three policies with different priorities
	//   - Policy A (priority 1): 3 days (carryover - use first)
	//   - Policy B (priority 2): 5 days (bonus - use second)
	//   - Policy C (priority 3): 20 days (standard - use last)
	// WHEN: Request 10 days
	// THEN: 3 from A + 5 from B + 2 from C

	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   testResourceType,
		TotalAvailable: days(28),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID:            "carryover",
					Policy:              generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
					ConsumptionPriority: 1,
				},
				Balance:  balance(3, 0),
				Priority: 1,
			},
			{
				Assignment: generic.PolicyAssignment{
					PolicyID:            "bonus",
					Policy:              generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
					ConsumptionPriority: 2,
				},
				Balance:  balance(5, 0),
				Priority: 2,
			},
			{
				Assignment: generic.PolicyAssignment{
					PolicyID:            "standard",
					Policy:              generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
					ConsumptionPriority: 3,
				},
				Balance:  balance(20, 0),
				Priority: 3,
			},
		},
	}

	distributor := &generic.ConsumptionDistributor{}
	result := distributor.Distribute(resourceBalance, days(10), false)

	if !result.IsSatisfiable {
		t.Error("should be satisfiable")
	}
	if len(result.Allocations) != 3 {
		t.Errorf("expected 3 allocations, got %d", len(result.Allocations))
	}

	// Check allocations in order
	expected := []struct {
		policyID string
		amount   float64
	}{
		{"carryover", 3},
		{"bonus", 5},
		{"standard", 2},
	}

	for i, exp := range expected {
		if string(result.Allocations[i].PolicyID) != exp.policyID {
			t.Errorf("allocation %d: expected policy %s, got %s", i, exp.policyID, result.Allocations[i].PolicyID)
		}
		if !result.Allocations[i].Amount.Value.Equal(days(exp.amount).Value) {
			t.Errorf("allocation %d: expected %v days, got %v", i, exp.amount, result.Allocations[i].Amount.Value)
		}
	}
}

func TestConsumptionDistribution_InsufficientBalance(t *testing.T) {
	// GIVEN: Total 10 days across policies
	// WHEN: Request 15 days (negative not allowed)
	// THEN: Not satisfiable, shortfall of 5 days

	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   testResourceType,
		TotalAvailable: days(10),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "policy-a",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
				},
				Balance:  balance(10, 0),
				Priority: 1,
			},
		},
	}

	distributor := &generic.ConsumptionDistributor{}
	result := distributor.Distribute(resourceBalance, days(15), false)

	if result.IsSatisfiable {
		t.Error("should not be satisfiable")
	}
	if !result.Shortfall.Value.Equal(days(5).Value) {
		t.Errorf("expected shortfall of 5, got %v", result.Shortfall.Value)
	}
}

func TestConsumptionDistribution_NegativeAllowed(t *testing.T) {
	// GIVEN: Total 5 days
	// WHEN: Request 10 days (negative allowed)
	// THEN: Satisfiable, first policy goes negative

	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   testResourceType,
		TotalAvailable: days(5),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "policy-a",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
				},
				Balance:  balance(5, 0),
				Priority: 1,
			},
		},
	}

	distributor := &generic.ConsumptionDistributor{}
	result := distributor.Distribute(resourceBalance, days(10), true) // negative allowed

	if !result.IsSatisfiable {
		t.Error("should be satisfiable with negative allowed")
	}

	// Should have 2 allocations: 5 (available) + 5 (overdraft)
	totalAllocated := days(0)
	for _, alloc := range result.Allocations {
		totalAllocated = totalAllocated.Add(alloc.Amount)
	}
	if !totalAllocated.Value.Equal(days(10).Value) {
		t.Errorf("expected 10 days total allocated, got %v", totalAllocated.Value)
	}
}

func TestConsumptionDistribution_SkipsExhaustedPolicies(t *testing.T) {
	// GIVEN: First policy exhausted (0 balance), second has balance
	// WHEN: Request 5 days
	// THEN: All from second policy (skips first)

	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   testResourceType,
		TotalAvailable: days(20),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "exhausted",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
				},
				Balance:  balance(10, 10), // 10 accrued, 10 consumed = 0 available
				Priority: 1,
			},
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "available",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
				},
				Balance:  balance(20, 0),
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

// =============================================================================
// APPROVAL REQUIREMENT TESTS
// =============================================================================

func TestApprovalRequired_WhenConfigured(t *testing.T) {
	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   testResourceType,
		TotalAvailable: days(20),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "needs-approval",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
					ApprovalConfig: generic.ApprovalConfig{
						RequiresApproval: true,
					},
				},
				Balance:  balance(20, 0),
				Priority: 1,
			},
		},
	}

	distributor := &generic.ConsumptionDistributor{}
	result := distributor.Distribute(resourceBalance, days(5), false)

	if !result.Allocations[0].RequiresApproval {
		t.Error("should require approval")
	}
}

func TestApprovalNotRequired_WhenUnderAutoApproveLimit(t *testing.T) {
	autoApprove := days(5)
	resourceBalance := &generic.ResourceBalance{
		EntityID:       "emp-1",
		ResourceType:   testResourceType,
		TotalAvailable: days(20),
		PolicyBalances: []generic.PolicyBalance{
			{
				Assignment: generic.PolicyAssignment{
					PolicyID: "auto-approve-small",
					Policy:   generic.Policy{ResourceType: testResourceType, Unit: generic.UnitDays},
					ApprovalConfig: generic.ApprovalConfig{
						RequiresApproval: true,
						AutoApproveUpTo:  &autoApprove,
					},
				},
				Balance:  balance(20, 0),
				Priority: 1,
			},
		},
	}

	distributor := &generic.ConsumptionDistributor{}
	
	// Request 3 days (under limit) - should auto-approve
	result := distributor.Distribute(resourceBalance, days(3), false)
	if result.Allocations[0].RequiresApproval {
		t.Error("should NOT require approval for 3 days (under 5 day limit)")
	}

	// Request 7 days (over limit) - should require approval
	result2 := distributor.Distribute(resourceBalance, days(7), false)
	if !result2.Allocations[0].RequiresApproval {
		t.Error("should require approval for 7 days (over 5 day limit)")
	}
}

// =============================================================================
// POLICY ASSIGNMENT TESTS
// =============================================================================

func TestPolicyAssignment_IsActive(t *testing.T) {
	start := generic.NewTimePoint(2025, time.January, 1)
	end := generic.NewTimePoint(2025, time.December, 31)

	assignment := generic.PolicyAssignment{
		EffectiveFrom: start,
		EffectiveTo:   &end,
	}

	// Before start - not active
	if assignment.IsActive(generic.NewTimePoint(2024, time.December, 15)) {
		t.Error("should not be active before start")
	}

	// During - active
	if !assignment.IsActive(generic.NewTimePoint(2025, time.June, 15)) {
		t.Error("should be active during period")
	}

	// After end - not active
	if assignment.IsActive(generic.NewTimePoint(2026, time.January, 15)) {
		t.Error("should not be active after end")
	}
}

func TestPolicyAssignment_NoEndDate_AlwaysActive(t *testing.T) {
	start := generic.NewTimePoint(2025, time.January, 1)

	assignment := generic.PolicyAssignment{
		EffectiveFrom: start,
		EffectiveTo:   nil, // No end date
	}

	// Way in the future - still active
	if !assignment.IsActive(generic.NewTimePoint(2099, time.December, 31)) {
		t.Error("should be active indefinitely when no end date")
	}
}

// =============================================================================
// DISTRIBUTION TO TRANSACTIONS TEST
// =============================================================================

func TestDistributionToTransactions(t *testing.T) {
	distribution := &generic.ConsumptionDistribution{
		TotalRequested: days(10),
		Allocations: []generic.PolicyAllocation{
			{PolicyID: "policy-a", Amount: days(3), Assignment: generic.PolicyAssignment{Policy: generic.Policy{ResourceType: testResourceType}}},
			{PolicyID: "policy-b", Amount: days(7), Assignment: generic.PolicyAssignment{Policy: generic.Policy{ResourceType: testResourceType}}},
		},
		IsSatisfiable: true,
	}

	txs := distribution.ToTransactions(
		"emp-1",
		"request-123",
		generic.NewTimePoint(2025, time.March, 10),
		generic.TxPending,
	)

	if len(txs) != 2 {
		t.Errorf("expected 2 transactions, got %d", len(txs))
	}

	// Check first transaction
	if txs[0].PolicyID != "policy-a" {
		t.Errorf("expected policy-a, got %s", txs[0].PolicyID)
	}
	if !txs[0].Delta.Value.Equal(days(-3).Value) { // Negative (consumption)
		t.Errorf("expected -3 days, got %v", txs[0].Delta.Value)
	}
	if txs[0].Type != generic.TxPending {
		t.Errorf("expected TxPending, got %s", txs[0].Type)
	}
}
