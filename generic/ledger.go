/*
ledger.go - Append-only transaction log

PURPOSE:
  The Ledger is the immutable source of truth for all balance changes.
  Every accrual, consumption, adjustment, and reversal is recorded here.
  Balance is always computed by replaying transactions - there's no
  separate "balance" field that can get out of sync.

CRITICAL INVARIANTS:
  1. APPEND-ONLY: No Update, No Delete. EVER.
  2. IMMUTABLE: Once written, transactions cannot be modified
  3. AUDITABLE: Every balance change is traceable with full context
  4. IDEMPOTENT: Same idempotency key = same transaction (no duplicates)

WHY APPEND-ONLY?
  - Audit trail: You can always explain how balance got to current state
  - Debugging: "Why is balance X?" → Look at transaction history
  - Compliance: SOC2, HIPAA require immutable audit logs
  - Correctness: No risk of partial updates corrupting state

CORRECTIONS:
  If a mistake is made, you don't edit the transaction. Instead:
  1. Create a Reversal transaction (opposite sign)
  2. Both original and reversal remain in the ledger
  3. Net effect is correction, but history is preserved

EXAMPLE FLOW:
  1. Employee granted 20 days: TxGrant +20
  2. Takes 3 days off: TxConsumption -3
  3. Oops, was sick not PTO: TxReversal +3 (undo consumption)
  4. Record as sick: TxConsumption -3 (on sick policy)

  PTO ledger: [+20, -3, +3] = 20 days
  Sick ledger: [-3] = -3 days (or from sick balance)

SEE ALSO:
  - store.go: Low-level persistence interface
  - timeoff/ledger.go: Domain-specific wrapper with day-uniqueness
*/
package generic

import "context"

// =============================================================================
// LEDGER - Append-only transaction log
// =============================================================================

// Ledger is the source of truth for all balance changes.
//
// INVARIANTS:
//   - Append-only: No Update, No Delete. EVER.
//   - Immutable: Once written, transactions cannot be modified.
//   - Auditable: Every balance change is traceable.
//
// Corrections are made via reversal transactions, not edits.
type Ledger interface {
	// Append adds a transaction. Fails if idempotency key exists.
	// This is the ONLY write operation.
	Append(ctx context.Context, tx Transaction) error

	// AppendBatch adds multiple transactions atomically.
	// Used when approving requests (multiple days = multiple transactions).
	AppendBatch(ctx context.Context, txs []Transaction) error

	// Transactions returns all transactions for entity+policy, chronologically.
	// Read-only.
	Transactions(ctx context.Context, entityID EntityID, policyID PolicyID) ([]Transaction, error)

	// TransactionsInRange returns transactions in [from, to].
	// Read-only.
	TransactionsInRange(ctx context.Context, entityID EntityID, policyID PolicyID, from, to TimePoint) ([]Transaction, error)

	// BalanceAt computes balance at a specific time.
	// This is a derived value, computed from transactions.
	BalanceAt(ctx context.Context, entityID EntityID, policyID PolicyID, at TimePoint, unit Unit) (Amount, error)
}

// =============================================================================
// DEFAULT LEDGER - Implementation using Store
// =============================================================================

type DefaultLedger struct {
	Store Store
}

func NewLedger(store Store) *DefaultLedger {
	return &DefaultLedger{Store: store}
}

func (l *DefaultLedger) Append(ctx context.Context, tx Transaction) error {
	if tx.IdempotencyKey != "" {
		exists, err := l.Store.Exists(ctx, tx.IdempotencyKey)
		if err != nil {
			return err
		}
		if exists {
			return ErrDuplicateIdempotencyKey
		}
	}
	return l.Store.Append(ctx, tx)
}

func (l *DefaultLedger) AppendBatch(ctx context.Context, txs []Transaction) error {
	// Check all idempotency keys first
	for _, tx := range txs {
		if tx.IdempotencyKey != "" {
			exists, err := l.Store.Exists(ctx, tx.IdempotencyKey)
			if err != nil {
				return err
			}
			if exists {
				return ErrDuplicateIdempotencyKey
			}
		}
	}
	return l.Store.AppendBatch(ctx, txs)
}

func (l *DefaultLedger) Transactions(ctx context.Context, entityID EntityID, policyID PolicyID) ([]Transaction, error) {
	return l.Store.Load(ctx, entityID, policyID)
}

func (l *DefaultLedger) TransactionsInRange(ctx context.Context, entityID EntityID, policyID PolicyID, from, to TimePoint) ([]Transaction, error) {
	return l.Store.LoadRange(ctx, entityID, policyID, from, to)
}

func (l *DefaultLedger) BalanceAt(ctx context.Context, entityID EntityID, policyID PolicyID, at TimePoint, unit Unit) (Amount, error) {
	txs, err := l.Store.Load(ctx, entityID, policyID)
	if err != nil {
		return Amount{}, err
	}

	balance := NewAmount(0, unit)
	for _, tx := range txs {
		if tx.EffectiveAt.After(at) {
			break
		}
		balance = balance.Add(tx.Delta)
	}
	return balance, nil
}

// =============================================================================
// ERRORS
// =============================================================================
// Error types are defined in errors.go for centralized management.
// Key errors used by this package:
//   - ErrDuplicateIdempotencyKey
//   - ErrDuplicateDayConsumption
//   - ErrTransactionFailed
