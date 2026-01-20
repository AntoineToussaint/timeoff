package generic

import "context"

// =============================================================================
// SNAPSHOT - Frozen balance at period end
// =============================================================================

// Snapshot captures the balance at a specific point (usually period end).
// Used for:
//   - Reconciliation (rollover, expire)
//   - Policy changes (close old period, open new)
//   - Audit trail
//   - Fast reads (avoid recalculating from ledger)
type Snapshot struct {
	ID       string
	EntityID EntityID
	PolicyID PolicyID

	// The period this snapshot represents
	Period Period

	// When the snapshot was taken
	TakenAt TimePoint

	// Balance at snapshot time
	Balance Balance

	// Why was this snapshot taken?
	Reason SnapshotReason
}

type SnapshotReason string

const (
	SnapshotPeriodEnd    SnapshotReason = "period_end"    // Regular period end
	SnapshotPolicyChange SnapshotReason = "policy_change" // Policy changed
	SnapshotManual       SnapshotReason = "manual"        // Admin triggered
)

// =============================================================================
// SNAPSHOT STORE - Persistence for snapshots
// =============================================================================

type SnapshotStore interface {
	Save(ctx context.Context, snapshot Snapshot) error
	Get(ctx context.Context, entityID EntityID, policyID PolicyID, period Period) (*Snapshot, error)
	GetLatest(ctx context.Context, entityID EntityID, policyID PolicyID) (*Snapshot, error)
}

// =============================================================================
// PERIOD MANAGER - Handles period lifecycle
// =============================================================================

// PeriodManager handles period transitions (close/open).
// This is the orchestrator for:
//   - Year-end rollover
//   - Policy changes
//   - Employee onboarding
type PeriodManager struct {
	Ledger        Ledger
	SnapshotStore SnapshotStore
	Reconciler    *ReconciliationEngine
}

// ClosePeriodInput contains inputs for closing a period
type ClosePeriodInput struct {
	EntityID EntityID
	PolicyID PolicyID
	Policy   Policy
	Period   Period
	Accruals AccrualSchedule
	Reason   SnapshotReason
}

// ClosePeriodOutput contains the result of closing a period
type ClosePeriodOutput struct {
	Snapshot     Snapshot
	Transactions []Transaction // Reconciliation transactions
	Summary      ReconciliationSummary
}

// ClosePeriod closes a period by:
// 1. Computing final balance
// 2. Taking a snapshot
// 3. Applying reconciliation rules
// 4. Writing reconciliation transactions to ledger
func (pm *PeriodManager) ClosePeriod(ctx context.Context, input ClosePeriodInput) (*ClosePeriodOutput, error) {
	// 1. Calculate final balance for the period
	txs, err := pm.Ledger.TransactionsInRange(ctx, input.EntityID, input.PolicyID, input.Period.Start, input.Period.End)
	if err != nil {
		return nil, err
	}

	balance := pm.calculateBalance(txs, input.Period, input.Accruals, input.Policy.Unit)

	// 2. Take snapshot
	snapshot := Snapshot{
		ID:       generateSnapshotID(input.EntityID, input.PolicyID, input.Period),
		EntityID: input.EntityID,
		PolicyID: input.PolicyID,
		Period:   input.Period,
		TakenAt:  input.Period.End,
		Balance:  balance,
		Reason:   input.Reason,
	}

	if pm.SnapshotStore != nil {
		if err := pm.SnapshotStore.Save(ctx, snapshot); err != nil {
			return nil, err
		}
	}

	// 3. Apply reconciliation rules
	nextPeriod := input.Period.NextPeriod()
	reconOutput, err := pm.Reconciler.Process(ReconciliationInput{
		EntityID:       input.EntityID,
		PolicyID:       input.PolicyID,
		Policy:         input.Policy,
		Rules:          input.Policy.ReconciliationRules,
		CurrentBalance: balance,
		EndingPeriod:   input.Period,
		NextPeriod:     nextPeriod,
	})
	if err != nil {
		return nil, err
	}

	// 4. Write reconciliation transactions
	if len(reconOutput.Transactions) > 0 {
		if err := pm.Ledger.AppendBatch(ctx, reconOutput.Transactions); err != nil {
			return nil, err
		}
	}

	return &ClosePeriodOutput{
		Snapshot:     snapshot,
		Transactions: reconOutput.Transactions,
		Summary:      reconOutput.Summary,
	}, nil
}

// OpenPeriodInput contains inputs for opening a new period
type OpenPeriodInput struct {
	EntityID      EntityID
	PolicyID      PolicyID
	Policy        Policy
	Period        Period
	InitialGrant  *Amount // Optional initial grant (for new employees)
	CarryoverFrom *Snapshot
}

// OpenPeriod opens a new period, optionally with carryover from previous
func (pm *PeriodManager) OpenPeriod(ctx context.Context, input OpenPeriodInput) error {
	var txs []Transaction

	// Initial grant (for new employees joining mid-year)
	if input.InitialGrant != nil && !input.InitialGrant.IsZero() {
		txs = append(txs, Transaction{
			ID:           TransactionID(generateTxID("grant", input.EntityID, input.Period.Start)),
			EntityID:     input.EntityID,
			PolicyID:     input.PolicyID,
			ResourceType: input.Policy.ResourceType,
			EffectiveAt:  input.Period.Start,
			Delta:        *input.InitialGrant,
			Type:         TxGrant,
			Reason:       "initial period grant",
		})
	}

	if len(txs) > 0 {
		return pm.Ledger.AppendBatch(ctx, txs)
	}
	return nil
}

// ChangePolicyInput contains inputs for policy change
type ChangePolicyInput struct {
	EntityID  EntityID
	OldPolicy Policy
	NewPolicy Policy
	ChangeAt  TimePoint
	Accruals  AccrualSchedule
}

// ChangePolicy handles mid-period policy change by:
// 1. Closing the current period early
// 2. Opening a new period with the new policy
func (pm *PeriodManager) ChangePolicy(ctx context.Context, input ChangePolicyInput) (*ClosePeriodOutput, error) {
	// Determine the period being closed
	oldPeriodConfig := input.OldPolicy.PeriodConfig
	fullPeriod := oldPeriodConfig.PeriodFor(input.ChangeAt)

	// Close early - end at change date
	closingPeriod := Period{
		Start: fullPeriod.Start,
		End:   input.ChangeAt.AddDays(-1), // Day before change
	}

	// Close the old period
	closeOutput, err := pm.ClosePeriod(ctx, ClosePeriodInput{
		EntityID: input.EntityID,
		PolicyID: input.OldPolicy.ID,
		Policy:   input.OldPolicy,
		Period:   closingPeriod,
		Accruals: input.Accruals,
		Reason:   SnapshotPolicyChange,
	})
	if err != nil {
		return nil, err
	}

	// The carryover transaction was already written to ledger by ClosePeriod
	// It's effective at the start of "next period" which we need to update
	// to be effective at the new policy start

	return closeOutput, nil
}

func (pm *PeriodManager) calculateBalance(txs []Transaction, period Period, accruals AccrualSchedule, unit Unit) Balance {
	var (
		actualAccruals = NewAmount(0, unit)
		consumed       = NewAmount(0, unit)
		pending        = NewAmount(0, unit)
		adjustments    = NewAmount(0, unit)
	)

	for _, tx := range txs {
		switch tx.Type {
		case TxGrant:
			actualAccruals = actualAccruals.Add(tx.Delta)
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

	totalAccruals := actualAccruals
	if accruals != nil {
		projectedEvents := accruals.GenerateAccruals(period.Start, period.End)
		projectedTotal := NewAmount(0, unit)
		for _, e := range projectedEvents {
			projectedTotal = projectedTotal.Add(e.Amount)
		}
		totalAccruals = projectedTotal
	}

	return Balance{
		EntityID:         "", // Will be set by caller
		PolicyID:         "", // Will be set by caller
		Period:           period,
		AccruedToDate:    totalAccruals,
		TotalEntitlement: totalAccruals,
		TotalConsumed:    consumed,
		Pending:          pending,
		Adjustments:      adjustments,
	}
}

func generateSnapshotID(entityID EntityID, policyID PolicyID, period Period) string {
	return string(entityID) + "-" + string(policyID) + "-" + period.End.String()
}

func generateTxID(prefix string, entityID EntityID, at TimePoint) string {
	return prefix + "-" + string(entityID) + "-" + at.String()
}
