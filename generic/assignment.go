/*
assignment.go - Policy-to-entity mapping and multi-policy balance aggregation

PURPOSE:
  Employees can have MULTIPLE policies for the same resource type. For example,
  an employee might have PTO from three sources:
  - Carryover from last year (5 days)
  - Performance bonus (3 days)
  - Standard annual PTO (20 days)

  This file handles:
  1. Linking employees to policies (PolicyAssignment)
  2. Aggregating balance across policies (ResourceBalance)
  3. Distributing consumption by priority (ConsumptionDistributor)

KEY CONCEPTS:
  PolicyAssignment:
    Links an entity to a policy with:
    - Effective dates (when assignment starts/ends)
    - Consumption priority (which policy to drain first)
    - Approval config (who can approve, auto-approve thresholds)

  ResourceBalance:
    Aggregate balance for a resource type across all policies.
    This is what users see: "You have 28 PTO days total"

  ConsumptionDistributor:
    When requesting 10 days, it allocates consumption:
    - First 5 from carryover (priority 1)
    - Then 3 from bonus (priority 2)
    - Finally 2 from standard (priority 3)

PRIORITY ORDERING:
  Lower priority number = consumed first. Typical setup:
  - Priority 1: Carryover (use first, or it expires)
  - Priority 2: Bonus/Adjustments (use second)
  - Priority 3: Standard annual (use last, most flexible)

EXAMPLE:
  // Get total available for an employee
  calculator := &ResourceBalanceCalculator{Ledger: ledger, AssignmentStore: store}
  balance, _ := calculator.Calculate(ctx, "emp-123", "resource_type", today)
  fmt.Println(balance.TotalAvailable) // 28 days

  // Distribute a request
  distributor := &ConsumptionDistributor{}
  result := distributor.Distribute(balance, days(10), false)
  for _, alloc := range result.Allocations {
      fmt.Printf("%s: %v\n", alloc.PolicyID, alloc.Amount)
  }

SEE ALSO:
  - request.go: Uses distributor for request processing
  - balance.go: Single-policy balance calculation
*/
package generic

import (
	"context"
	"sort"
)

// =============================================================================
// POLICY ASSIGNMENT - Links entity to policy with priority
// =============================================================================

// PolicyAssignment links an entity to a policy with metadata.
// An entity can have MULTIPLE assignments for the same ResourceType.
type PolicyAssignment struct {
	ID       string
	EntityID EntityID
	PolicyID PolicyID
	Policy   Policy // The actual policy

	// When this assignment is effective
	EffectiveFrom TimePoint
	EffectiveTo   *TimePoint // nil = still active

	// Priority for consumption distribution (lower = consume first)
	// Example: Carryover=1, UseItOrLoseIt=2, Standard=3
	ConsumptionPriority int

	// Approval requirements for this policy
	ApprovalConfig ApprovalConfig
}

// ApprovalConfig defines when approval is needed
type ApprovalConfig struct {
	// RequiresApproval if true, all requests need approval
	RequiresApproval bool

	// AutoApproveUpTo: requests up to this amount are auto-approved
	AutoApproveUpTo *Amount

	// ApproverRoles: who can approve (e.g., "manager", "hr")
	ApproverRoles []string
}

// IsActive returns true if the assignment is active at the given time
func (pa PolicyAssignment) IsActive(at TimePoint) bool {
	if at.Before(pa.EffectiveFrom) {
		return false
	}
	if pa.EffectiveTo != nil && at.After(*pa.EffectiveTo) {
		return false
	}
	return true
}

// =============================================================================
// ASSIGNMENT STORE - Persistence for assignments
// =============================================================================

type AssignmentStore interface {
	Save(ctx context.Context, assignment PolicyAssignment) error
	
	// GetByEntity returns all assignments for an entity
	GetByEntity(ctx context.Context, entityID EntityID) ([]PolicyAssignment, error)
	
	// GetByEntityAndResource returns assignments for a specific resource type
	GetByEntityAndResource(ctx context.Context, entityID EntityID, resourceType ResourceType) ([]PolicyAssignment, error)
	
	// GetActive returns only active assignments at a given time
	GetActive(ctx context.Context, entityID EntityID, at TimePoint) ([]PolicyAssignment, error)
}

// =============================================================================
// RESOURCE BALANCE - Aggregates balance across policies
// =============================================================================

// ResourceBalance aggregates balance for a ResourceType across all policies.
// This is what the user sees: "You have 28 PTO days"
type ResourceBalance struct {
	EntityID     EntityID
	ResourceType ResourceType
	Period       Period

	// Total across all policies
	TotalAvailable Amount
	TotalPending   Amount

	// Breakdown by policy (ordered by consumption priority)
	PolicyBalances []PolicyBalance
}

// PolicyBalance is the balance for a single policy
type PolicyBalance struct {
	Assignment PolicyAssignment
	Balance    Balance
	Priority   int // From assignment.ConsumptionPriority
}

// =============================================================================
// RESOURCE BALANCE CALCULATOR
// =============================================================================

type ResourceBalanceCalculator struct {
	Ledger          Ledger
	AssignmentStore AssignmentStore
}

// Calculate computes the aggregate balance for a resource type
func (rbc *ResourceBalanceCalculator) Calculate(
	ctx context.Context,
	entityID EntityID,
	resourceType ResourceType,
	at TimePoint,
) (*ResourceBalance, error) {
	// 1. Get all active assignments for this resource type
	assignments, err := rbc.AssignmentStore.GetByEntityAndResource(ctx, entityID, resourceType)
	if err != nil {
		return nil, err
	}

	// Filter to active assignments
	var activeAssignments []PolicyAssignment
	for _, a := range assignments {
		if a.IsActive(at) && a.Policy.ResourceType == resourceType {
			activeAssignments = append(activeAssignments, a)
		}
	}

	// 2. Calculate balance for each policy
	var policyBalances []PolicyBalance
	totalAvailable := NewAmount(0, UnitDays)
	totalPending := NewAmount(0, UnitDays)

	for _, assignment := range activeAssignments {
		period := assignment.Policy.PeriodConfig.PeriodFor(at)
		
		// Get transactions for this policy
		txs, err := rbc.Ledger.TransactionsInRange(
			ctx, entityID, assignment.PolicyID, period.Start, period.End,
		)
		if err != nil {
			return nil, err
		}

		balance := rbc.calculatePolicyBalance(txs, period, assignment.Policy.Unit)
		
		policyBalances = append(policyBalances, PolicyBalance{
			Assignment: assignment,
			Balance:    balance,
			Priority:   assignment.ConsumptionPriority,
		})

		totalAvailable = totalAvailable.Add(balance.Available())
		totalPending = totalPending.Add(balance.Pending)
	}

	// 3. Sort by priority (lower = first)
	sort.Slice(policyBalances, func(i, j int) bool {
		return policyBalances[i].Priority < policyBalances[j].Priority
	})

	return &ResourceBalance{
		EntityID:       entityID,
		ResourceType:   resourceType,
		Period:         policyBalances[0].Balance.Period, // Use first policy's period
		TotalAvailable: totalAvailable,
		TotalPending:   totalPending,
		PolicyBalances: policyBalances,
	}, nil
}

func (rbc *ResourceBalanceCalculator) calculatePolicyBalance(txs []Transaction, period Period, unit Unit) Balance {
	var (
		accruals    = NewAmount(0, unit)
		consumed    = NewAmount(0, unit)
		pending     = NewAmount(0, unit)
		adjustments = NewAmount(0, unit)
	)

	for _, tx := range txs {
		switch tx.Type {
		case TxGrant:
			accruals = accruals.Add(tx.Delta)
		case TxConsumption:
			consumed = consumed.Add(tx.Delta.Neg())
		case TxPending:
			pending = pending.Add(tx.Delta.Neg())
		case TxReconciliation, TxAdjustment:
			adjustments = adjustments.Add(tx.Delta)
		case TxReversal:
			consumed = consumed.Sub(tx.Delta)
		}
	}

	return Balance{
		Period:           period,
		AccruedToDate:    accruals,
		TotalEntitlement: accruals,
		TotalConsumed:    consumed,
		Pending:          pending,
		Adjustments:      adjustments,
	}
}

// =============================================================================
// CONSUMPTION DISTRIBUTOR - Splits consumption across policies
// =============================================================================

// ConsumptionDistribution describes how a request is split across policies
type ConsumptionDistribution struct {
	TotalRequested Amount
	Allocations    []PolicyAllocation
	
	// Is the request fully satisfiable?
	IsSatisfiable bool
	Shortfall     Amount // How much is missing if not satisfiable
}

// PolicyAllocation is the amount consumed from a specific policy
type PolicyAllocation struct {
	PolicyID   PolicyID
	Assignment PolicyAssignment
	Amount     Amount
	
	// Does this allocation require approval?
	RequiresApproval bool
}

// ConsumptionDistributor determines how to split consumption across policies
type ConsumptionDistributor struct{}

// Distribute splits a consumption request across policies by priority
func (cd *ConsumptionDistributor) Distribute(
	resourceBalance *ResourceBalance,
	requestedAmount Amount,
	allowNegative bool,
) *ConsumptionDistribution {
	var allocations []PolicyAllocation
	remaining := requestedAmount

	// Consume from policies in priority order
	for _, pb := range resourceBalance.PolicyBalances {
		if remaining.IsZero() {
			break
		}

		available := pb.Balance.Available()
		if available.IsZero() || available.IsNegative() {
			continue
		}

		// Take min(remaining, available)
		toConsume := remaining.Min(available)
		
		allocations = append(allocations, PolicyAllocation{
			PolicyID:         pb.Assignment.PolicyID,
			Assignment:       pb.Assignment,
			Amount:           toConsume,
			RequiresApproval: cd.requiresApproval(pb.Assignment, toConsume),
		})

		remaining = remaining.Sub(toConsume)
	}

	// Check if fully satisfiable
	isSatisfiable := remaining.IsZero() || (allowNegative && !remaining.IsPositive())
	
	// If negative allowed and still remaining, allocate to first policy
	if allowNegative && remaining.IsPositive() && len(resourceBalance.PolicyBalances) > 0 {
		firstPolicy := resourceBalance.PolicyBalances[0]
		allocations = append(allocations, PolicyAllocation{
			PolicyID:         firstPolicy.Assignment.PolicyID,
			Assignment:       firstPolicy.Assignment,
			Amount:           remaining,
			RequiresApproval: cd.requiresApproval(firstPolicy.Assignment, remaining),
		})
		remaining = NewAmount(0, remaining.Unit)
		isSatisfiable = true
	}

	return &ConsumptionDistribution{
		TotalRequested: requestedAmount,
		Allocations:    allocations,
		IsSatisfiable:  isSatisfiable,
		Shortfall:      remaining,
	}
}

func (cd *ConsumptionDistributor) requiresApproval(assignment PolicyAssignment, amount Amount) bool {
	if !assignment.ApprovalConfig.RequiresApproval {
		return false
	}
	if assignment.ApprovalConfig.AutoApproveUpTo != nil {
		if amount.LessThan(*assignment.ApprovalConfig.AutoApproveUpTo) || 
		   amount.Value.Equal(assignment.ApprovalConfig.AutoApproveUpTo.Value) {
			return false
		}
	}
	return true
}

// =============================================================================
// HELPER: Create transactions from distribution
// =============================================================================

// ToTransactions converts a distribution to ledger transactions
func (cd *ConsumptionDistribution) ToTransactions(
	entityID EntityID,
	requestID string,
	effectiveAt TimePoint,
	txType TransactionType, // TxConsumption or TxPending
) []Transaction {
	var txs []Transaction
	
	for i, alloc := range cd.Allocations {
		txs = append(txs, Transaction{
			ID:           TransactionID(requestID + "-" + string(rune('A'+i))),
			EntityID:     entityID,
			PolicyID:     alloc.PolicyID,
			ResourceType: alloc.Assignment.Policy.ResourceType,
			EffectiveAt:  effectiveAt,
			Delta:        alloc.Amount.Neg(), // Consumption is negative
			Type:         txType,
			ReferenceID:  requestID,
		})
	}
	
	return txs
}
