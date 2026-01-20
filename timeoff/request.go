package timeoff

import (
	"context"
	"fmt"

	"github.com/warp/resource-engine/generic"
)

// =============================================================================
// REQUEST SERVICE - Handles request lifecycle with transactional guarantees
// =============================================================================

type RequestService struct {
	Ledger     generic.Ledger
	Store      generic.TxStore // transactional store
	Projection *generic.ProjectionEngine
	AuditLog   generic.AuditLog // optional
}

// =============================================================================
// APPROVE REQUEST - The critical transactional operation
// =============================================================================

// ApproveRequest approves a time-off request.
// This is TRANSACTIONAL:
//   - Validates the request can be fulfilled (balance check)
//   - Writes consumption transactions to ledger
//   - Updates request status
//   - Writes audit log entry
//
// If ANY step fails, ALL changes are rolled back.
func (rs *RequestService) ApproveRequest(ctx context.Context, req *TimeOffRequest, approverID string) error {
	if req.Status != StatusPending {
		return fmt.Errorf("request must be pending, got %s", req.Status)
	}

	// Convert request to consumption events
	consumptions := req.ToConsumptionEvents()

	// Build transactions for the ledger (one per day)
	var ledgerTxs []generic.Transaction
	for i, ce := range consumptions {
		ledgerTxs = append(ledgerTxs, generic.Transaction{
			ID:             generic.TransactionID(fmt.Sprintf("%s-%d", req.ID, i)),
			EntityID:       req.EntityID,
			PolicyID:       req.PolicyID,
			ResourceType:   req.Resource,
			EffectiveAt:    ce.At,
			Delta:          ce.Amount.Neg(), // consumption is negative
			Type:           generic.TxConsumption,
			ReferenceID:    req.ID,
			Reason:         req.Reason,
			IdempotencyKey: fmt.Sprintf("request-%s-day-%d", req.ID, i),
		})
	}

	// Execute within transaction
	return rs.Store.WithTx(ctx, func(txStore generic.Store) error {
		// 1. Create ledger using transactional store
		txLedger := generic.NewLedger(txStore)

		// 2. Final balance check (within transaction to avoid race)
		// This ensures no concurrent approval can overdraw
		for _, ce := range consumptions {
			balance, err := txLedger.BalanceAt(ctx, req.EntityID, req.PolicyID, ce.At, generic.UnitDays)
			if err != nil {
				return fmt.Errorf("balance check failed: %w", err)
			}
			if balance.Sub(ce.Amount).IsNegative() {
				return fmt.Errorf("insufficient balance at %s: have %v, need %v",
					ce.At, balance.Value, ce.Amount.Value)
			}
		}

		// 3. Append all transactions atomically
		if err := txLedger.AppendBatch(ctx, ledgerTxs); err != nil {
			return fmt.Errorf("failed to write transactions: %w", err)
		}

		// 4. Update request status
		req.Status = StatusApproved

		// 5. Audit log (if available)
		if rs.AuditLog != nil {
			rs.AuditLog.Append(ctx, generic.AuditEntry{
				ID:           fmt.Sprintf("audit-%s", req.ID),
				Timestamp:    generic.Today(),
				ActorID:      approverID,
				Action:       generic.AuditRequestApproved,
				EntityID:     req.EntityID,
				PolicyID:     req.PolicyID,
				ResourceType: req.Resource,
				Payload: map[string]any{
					"request_id": req.ID,
					"days":       len(req.Days),
				},
			})
		}

		return nil
	})
}

// =============================================================================
// CANCEL APPROVED REQUEST - Creates reversal transactions
// =============================================================================

// CancelApprovedRequest cancels a previously approved request.
// This creates REVERSAL transactions (not deletes - ledger is append-only).
func (rs *RequestService) CancelApprovedRequest(ctx context.Context, req *TimeOffRequest, cancellerID string, reason string) error {
	if req.Status != StatusApproved {
		return fmt.Errorf("can only cancel approved requests, got %s", req.Status)
	}

	consumptions := req.ToConsumptionEvents()

	// Build reversal transactions
	var reversalTxs []generic.Transaction
	for i, ce := range consumptions {
		reversalTxs = append(reversalTxs, generic.Transaction{
			ID:             generic.TransactionID(fmt.Sprintf("%s-%d-reversal", req.ID, i)),
			EntityID:       req.EntityID,
			PolicyID:       req.PolicyID,
			ResourceType:   req.Resource,
			EffectiveAt:    generic.Today(), // reversal effective today
			Delta:          ce.Amount,       // positive to restore balance
			Type:           generic.TxReversal,
			ReferenceID:    req.ID,
			Reason:         fmt.Sprintf("cancellation: %s", reason),
			IdempotencyKey: fmt.Sprintf("cancel-%s-day-%d", req.ID, i),
		})
	}

	return rs.Store.WithTx(ctx, func(txStore generic.Store) error {
		txLedger := generic.NewLedger(txStore)

		if err := txLedger.AppendBatch(ctx, reversalTxs); err != nil {
			return fmt.Errorf("failed to write reversal transactions: %w", err)
		}

		req.Status = StatusCanceled

		if rs.AuditLog != nil {
			rs.AuditLog.Append(ctx, generic.AuditEntry{
				ID:        fmt.Sprintf("audit-cancel-%s", req.ID),
				Timestamp: generic.Today(),
				ActorID:   cancellerID,
				Action:    generic.AuditRequestCanceled,
				EntityID:  req.EntityID,
				PolicyID:  req.PolicyID,
				Payload: map[string]any{
					"request_id": req.ID,
					"reason":     reason,
				},
			})
		}

		return nil
	})
}

// =============================================================================
// VALIDATE REQUEST - Pre-approval check (no side effects)
// =============================================================================

// ValidateRequest checks if a request can be approved without modifying anything.
func (rs *RequestService) ValidateRequest(ctx context.Context, req *TimeOffRequest, policy PolicyConfig) (*generic.ProjectionResult, error) {
	// Get period for the request
	// Use the first day of the request to determine the period
	var referenceDate generic.TimePoint
	if len(req.Days) > 0 {
		referenceDate = req.Days[0]
	} else {
		referenceDate = generic.Today()
	}
	
	period := policy.Policy.PeriodConfig.PeriodFor(referenceDate)
	
	// Calculate total requested amount
	totalDays := generic.NewAmount(float64(len(req.Days)), generic.UnitDays)

	return rs.Projection.Project(ctx, generic.ProjectionInput{
		EntityID:        req.EntityID,
		PolicyID:        req.PolicyID,
		Unit:            generic.UnitDays,
		Period:          period,
		Accruals:        policy.Accrual,
		RequestedAmount: totalDays,
		AllowNegative:   policy.Policy.Constraints.AllowNegative,
		MaxBalance:      policy.Policy.Constraints.MaxBalance,
	})
}
