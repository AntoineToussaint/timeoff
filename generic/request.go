/*
request.go - Resource consumption request lifecycle

PURPOSE:
  Handles the full lifecycle of resource consumption requests:
  1. Creation: Validate and compute distribution across policies
  2. Pending: Record tentative transactions (holds balance)
  3. Approval: Convert pending to confirmed consumption
  4. Rejection/Cancellation: Reverse pending transactions

REQUEST FLOW:
  ┌─────────────────────────────────────────────────────────────────┐
  │                                                                 │
  │  User submits     Calculate        Create pending     Approval  │
  │  request    ──▶  distribution ──▶  transactions  ──▶  workflow  │
  │                                                                 │
  │                                         │                       │
  │                                         ▼                       │
  │                                   ┌──────────┐                  │
  │                                   │ Approved │──▶ TxConsumption │
  │                                   └──────────┘                  │
  │                                         │                       │
  │                                   ┌──────────┐                  │
  │                                   │ Rejected │──▶ TxReversal    │
  │                                   └──────────┘                  │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘

PENDING vs CONFIRMED:
  When a request is created, we immediately create TxPending transactions.
  This "holds" the balance so other requests can't overdraw.

  On approval:
  - TxReversal transactions undo the pending
  - TxConsumption transactions record the actual consumption

  On rejection:
  - TxReversal transactions release the held balance

APPROVAL CONFIG:
  Policies can specify:
  - RequiresApproval: All requests need manager approval
  - AutoApproveUpTo: Requests under X days are auto-approved
  - ApproverRoles: Who can approve (manager, HR, etc.)

KEY COMPONENTS:
  Request:        The request entity with status and distribution
  RequestService: Orchestrates the request lifecycle
  BalanceView:    User-facing balance summary

EXAMPLE:
  svc := &RequestService{Ledger: ledger, AssignmentStore: store, ...}

  // Create request
  request, err := svc.CreateRequest(ctx, "emp-123", "policy-001", march10, days(5), "leave request")

  // Manager approves
  approved, err := svc.Approve(ctx, request, "manager-456")

SEE ALSO:
  - assignment.go: ConsumptionDistributor used for allocation
  - projection.go: Validates request against projected balance
*/
package generic

import (
	"context"
	"fmt"
	"time"
)

// =============================================================================
// REQUEST - A request to consume resources
// =============================================================================

type RequestID string

type RequestStatus string

const (
	RequestPending   RequestStatus = "pending"
	RequestApproved  RequestStatus = "approved"
	RequestRejected  RequestStatus = "rejected"
	RequestCancelled RequestStatus = "cancelled"
)

// Request represents a request to consume resources of a specific type.
// The request is against a ResourceType, and the system distributes
// consumption across the entity's policies for that resource.
type Request struct {
	ID           RequestID
	EntityID     EntityID
	ResourceType ResourceType

	// When the resource is being consumed
	EffectiveAt TimePoint

	// Amount requested
	RequestedAmount Amount

	// Current status
	Status RequestStatus

	// Distribution across policies (computed when request is created)
	Distribution *ConsumptionDistribution

	// Approval tracking
	RequiresApproval bool
	ApprovedBy       *string
	ApprovedAt       *time.Time
	RejectionReason  *string

	// Metadata
	Reason    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// =============================================================================
// REQUEST SERVICE - Handles request lifecycle with multi-policy support
// =============================================================================

type RequestService struct {
	Ledger          Ledger
	AssignmentStore AssignmentStore
	BalanceCalc     *ResourceBalanceCalculator
	Distributor     *ConsumptionDistributor
}

// CreateRequest creates a new request and validates it against available balance.
// Returns the request with distribution computed.
func (rs *RequestService) CreateRequest(
	ctx context.Context,
	entityID EntityID,
	resourceType ResourceType,
	effectiveAt TimePoint,
	amount Amount,
	reason string,
) (*Request, error) {
	// 1. Calculate aggregate balance for this resource type
	resourceBalance, err := rs.BalanceCalc.Calculate(ctx, entityID, resourceType, effectiveAt)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate balance: %w", err)
	}

	// 2. Compute distribution across policies
	// First pass: determine if ANY policy allows negative
	allowNegative := false
	for _, pb := range resourceBalance.PolicyBalances {
		if pb.Assignment.Policy.Constraints.AllowNegative {
			allowNegative = true
			break
		}
	}

	distribution := rs.Distributor.Distribute(resourceBalance, amount, allowNegative)

	if !distribution.IsSatisfiable {
		return nil, &ValidationError{
			Type:    "insufficient_balance",
			Balance: resourceBalance.TotalAvailable,
		}
	}

	// 3. Determine if approval is required
	requiresApproval := false
	for _, alloc := range distribution.Allocations {
		if alloc.RequiresApproval {
			requiresApproval = true
			break
		}
	}

	// 4. Create request
	requestID := RequestID(fmt.Sprintf("req-%d", time.Now().UnixNano()))
	now := time.Now()

	request := &Request{
		ID:               requestID,
		EntityID:         entityID,
		ResourceType:     resourceType,
		EffectiveAt:      effectiveAt,
		RequestedAmount:  amount,
		Status:           RequestPending,
		Distribution:     distribution,
		RequiresApproval: requiresApproval,
		Reason:           reason,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	// 5. Create pending transactions in ledger
	pendingTxs := distribution.ToTransactions(
		entityID,
		string(requestID),
		effectiveAt,
		TxPending,
	)

	if err := rs.Ledger.AppendBatch(ctx, pendingTxs); err != nil {
		return nil, fmt.Errorf("failed to record pending transactions: %w", err)
	}

	// 6. Auto-approve if no approval required
	if !requiresApproval {
		return rs.approve(ctx, request, "system", now)
	}

	return request, nil
}

// Approve approves a pending request and converts pending to consumption transactions.
func (rs *RequestService) Approve(
	ctx context.Context,
	request *Request,
	approverID string,
) (*Request, error) {
	if request.Status != RequestPending {
		return nil, fmt.Errorf("can only approve pending requests, current status: %s", request.Status)
	}

	return rs.approve(ctx, request, approverID, time.Now())
}

func (rs *RequestService) approve(
	ctx context.Context,
	request *Request,
	approverID string,
	at time.Time,
) (*Request, error) {
	// Convert pending transactions to consumption transactions
	// This is done atomically:
	// 1. Reverse the pending transactions
	// 2. Create consumption transactions

	var txs []Transaction

	// Reverse pending transactions
	for i, alloc := range request.Distribution.Allocations {
		txs = append(txs, Transaction{
			ID:             TransactionID(fmt.Sprintf("%s-reverse-%d", request.ID, i)),
			EntityID:       request.EntityID,
			PolicyID:       alloc.PolicyID,
			ResourceType:   request.ResourceType,
			EffectiveAt:    request.EffectiveAt,
			Delta:          alloc.Amount, // Positive (reversing the negative pending)
			Type:           TxReversal,
			ReferenceID:    string(request.ID),
			Reason:         "pending reversed on approval",
			IdempotencyKey: fmt.Sprintf("%s-reverse-%d", request.ID, i),
		})
	}

	// Create consumption transactions
	for i, alloc := range request.Distribution.Allocations {
		txs = append(txs, Transaction{
			ID:             TransactionID(fmt.Sprintf("%s-consume-%d", request.ID, i)),
			EntityID:       request.EntityID,
			PolicyID:       alloc.PolicyID,
			ResourceType:   request.ResourceType,
			EffectiveAt:    request.EffectiveAt,
			Delta:          alloc.Amount.Neg(), // Negative (consumption)
			Type:           TxConsumption,
			ReferenceID:    string(request.ID),
			Reason:         request.Reason,
			IdempotencyKey: fmt.Sprintf("%s-consume-%d", request.ID, i),
		})
	}

	// Append atomically
	if err := rs.Ledger.AppendBatch(ctx, txs); err != nil {
		return nil, fmt.Errorf("failed to record approval transactions: %w", err)
	}

	// Update request
	request.Status = RequestApproved
	request.ApprovedBy = &approverID
	request.ApprovedAt = &at
	request.UpdatedAt = at

	return request, nil
}

// Reject rejects a pending request and removes pending transactions.
func (rs *RequestService) Reject(
	ctx context.Context,
	request *Request,
	rejecterID string,
	reason string,
) (*Request, error) {
	if request.Status != RequestPending {
		return nil, fmt.Errorf("can only reject pending requests, current status: %s", request.Status)
	}

	// Reverse pending transactions
	var txs []Transaction
	for i, alloc := range request.Distribution.Allocations {
		txs = append(txs, Transaction{
			ID:             TransactionID(fmt.Sprintf("%s-reject-%d", request.ID, i)),
			EntityID:       request.EntityID,
			PolicyID:       alloc.PolicyID,
			ResourceType:   request.ResourceType,
			EffectiveAt:    request.EffectiveAt,
			Delta:          alloc.Amount, // Positive (reversing the negative pending)
			Type:           TxReversal,
			ReferenceID:    string(request.ID),
			Reason:         "pending reversed on rejection: " + reason,
			IdempotencyKey: fmt.Sprintf("%s-reject-%d", request.ID, i),
		})
	}

	if err := rs.Ledger.AppendBatch(ctx, txs); err != nil {
		return nil, fmt.Errorf("failed to record rejection transactions: %w", err)
	}

	// Update request
	now := time.Now()
	request.Status = RequestRejected
	request.RejectionReason = &reason
	request.UpdatedAt = now

	return request, nil
}

// Cancel cancels a pending request.
func (rs *RequestService) Cancel(ctx context.Context, request *Request) (*Request, error) {
	if request.Status != RequestPending {
		return nil, fmt.Errorf("can only cancel pending requests, current status: %s", request.Status)
	}

	// Same as reject but different status
	var txs []Transaction
	for i, alloc := range request.Distribution.Allocations {
		txs = append(txs, Transaction{
			ID:             TransactionID(fmt.Sprintf("%s-cancel-%d", request.ID, i)),
			EntityID:       request.EntityID,
			PolicyID:       alloc.PolicyID,
			ResourceType:   request.ResourceType,
			EffectiveAt:    request.EffectiveAt,
			Delta:          alloc.Amount, // Positive (reversing the negative pending)
			Type:           TxReversal,
			ReferenceID:    string(request.ID),
			Reason:         "request cancelled by user",
			IdempotencyKey: fmt.Sprintf("%s-cancel-%d", request.ID, i),
		})
	}

	if err := rs.Ledger.AppendBatch(ctx, txs); err != nil {
		return nil, fmt.Errorf("failed to record cancellation transactions: %w", err)
	}

	request.Status = RequestCancelled
	request.UpdatedAt = time.Now()

	return request, nil
}

// =============================================================================
// BALANCE VIEW - What the user sees
// =============================================================================

// GetBalanceView returns a user-friendly view of available balance for a resource type.
func (rs *RequestService) GetBalanceView(
	ctx context.Context,
	entityID EntityID,
	resourceType ResourceType,
	asOf TimePoint,
) (*BalanceView, error) {
	resourceBalance, err := rs.BalanceCalc.Calculate(ctx, entityID, resourceType, asOf)
	if err != nil {
		return nil, err
	}

	var policyBreakdown []PolicyBreakdownItem
	for _, pb := range resourceBalance.PolicyBalances {
		policyBreakdown = append(policyBreakdown, PolicyBreakdownItem{
			PolicyName:       pb.Assignment.Policy.Name,
			Available:        pb.Balance.Available(),
			Total:            pb.Balance.TotalEntitlement,
			Consumed:         pb.Balance.TotalConsumed,
			Pending:          pb.Balance.Pending,
			RequiresApproval: pb.Assignment.ApprovalConfig.RequiresApproval,
		})
	}

	return &BalanceView{
		EntityID:        entityID,
		ResourceType:    resourceType,
		TotalAvailable:  resourceBalance.TotalAvailable,
		TotalPending:    resourceBalance.TotalPending,
		PolicyBreakdown: policyBreakdown,
	}, nil
}

// BalanceView is the user-facing balance summary
type BalanceView struct {
	EntityID       EntityID
	ResourceType   ResourceType
	TotalAvailable Amount // What can be requested
	TotalPending   Amount // Currently pending approval
	PolicyBreakdown []PolicyBreakdownItem
}

// PolicyBreakdownItem shows balance for a single policy
type PolicyBreakdownItem struct {
	PolicyName       string
	Available        Amount
	Total            Amount
	Consumed         Amount
	Pending          Amount
	RequiresApproval bool
}
