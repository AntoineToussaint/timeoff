/*
store.go - Persistence interface for transactions and related data

PURPOSE:
  Defines the interface between the domain logic and the database.
  The Store handles persistence while maintaining append-only semantics.
  Different implementations can use SQLite, PostgreSQL, or in-memory storage.

KEY INTERFACES:
  Store:           Core transaction persistence (append, load, exists)
  TxStore:         Transactional operations (atomic multi-table writes)
  AssignmentStore: Policy-to-entity mapping
  SnapshotStore:   Balance snapshots for performance optimization

APPEND-ONLY CONTRACT:
  The Store interface enforces append-only semantics:
  - Append(): Single transaction write
  - AppendBatch(): Atomic multi-transaction write
  - NO Update() or Delete() methods exist

IDEMPOTENCY:
  Every write includes an idempotency key. If the key already exists,
  the write is rejected. This prevents duplicate transactions from
  network retries or user double-clicks.

ATOMIC BATCHES:
  AppendBatch() ensures all-or-nothing semantics. When approving a
  5-day PTO request (5 transactions), either all 5 are written or
  none are. This prevents partial state.

IMPLEMENTATIONS:
  - store/sqlite/sqlite.go: Production SQLite/PostgreSQL
  - generic/store/memory.go: In-memory for testing

EXAMPLE:
  store := sqlite.New("./data.db")
  err := store.Append(ctx, transaction)
  if err == ErrDuplicateIdempotencyKey {
      // Already processed, safe to ignore
  }

SEE ALSO:
  - ledger.go: Higher-level interface using Store
  - store/sqlite/sqlite.go: Concrete implementation
*/
package generic

import "context"

// =============================================================================
// STORE - Interface for transaction persistence (append-only)
// =============================================================================

// Store handles persistence of transactions.
// IMPORTANT: Store is APPEND-ONLY. No Update, No Delete. Ever.
// Corrections are made via reversal transactions.
type Store interface {
	// Append persists a transaction. Returns error if idempotency key exists.
	// This is the ONLY write operation.
	Append(ctx context.Context, tx Transaction) error

	// AppendBatch persists multiple transactions atomically.
	// Either all succeed or none do.
	AppendBatch(ctx context.Context, txs []Transaction) error

	// Load returns all transactions for entity+policy, ordered by EffectiveAt.
	Load(ctx context.Context, entityID EntityID, policyID PolicyID) ([]Transaction, error)

	// LoadRange returns transactions in [from, to].
	LoadRange(ctx context.Context, entityID EntityID, policyID PolicyID, from, to TimePoint) ([]Transaction, error)

	// Exists checks if idempotency key already exists.
	Exists(ctx context.Context, idempotencyKey string) (bool, error)
}

// EntityStore extends Store with entity-wide queries.
// Required for time-off uniqueness validation across multiple policies.
type EntityStore interface {
	Store

	// LoadByEntity returns ALL transactions for an entity across all policies.
	// Required for day-uniqueness checks in time-off.
	LoadByEntity(ctx context.Context, entityID EntityID, from, to TimePoint) ([]Transaction, error)

	// IsDayConsumed checks if a specific day is already consumed for a resource type.
	// Returns (consumed, existingTxID, error).
	IsDayConsumed(ctx context.Context, entityID EntityID, resourceType ResourceType, day TimePoint) (bool, TransactionID, error)

	// GetConsumedDays returns all consumed days for an entity+resource in a range.
	GetConsumedDays(ctx context.Context, entityID EntityID, resourceType ResourceType, from, to TimePoint) ([]TimePoint, error)
}

// =============================================================================
// TRANSACTIONAL STORE - For atomic operations across multiple writes
// =============================================================================

// TxStore wraps Store with transaction support.
// Use this when you need atomic operations (e.g., approving a request).
type TxStore interface {
	Store

	// WithTx executes fn within a transaction.
	// If fn returns error, transaction is rolled back.
	// If fn returns nil, transaction is committed.
	WithTx(ctx context.Context, fn func(Store) error) error
}

// =============================================================================
// AUDIT LOG - Separate from ledger, tracks who did what when
// =============================================================================

// AuditEntry records who did what when.
type AuditEntry struct {
	ID         string
	Timestamp  TimePoint
	ActorID    string // who performed the action
	Action     AuditAction
	EntityID   EntityID
	PolicyID   PolicyID
	ResourceType ResourceType
	Payload    map[string]any // action-specific data
}

type AuditAction string

const (
	AuditRequestCreated  AuditAction = "request_created"
	AuditRequestApproved AuditAction = "request_approved"
	AuditRequestRejected AuditAction = "request_rejected"
	AuditRequestCanceled AuditAction = "request_canceled"
	AuditPolicyChanged   AuditAction = "policy_changed"
	AuditManualAdjust    AuditAction = "manual_adjustment"
	AuditReconciliation  AuditAction = "reconciliation"
)

// AuditLog stores audit entries. Also append-only.
type AuditLog interface {
	Append(ctx context.Context, entry AuditEntry) error
	Query(ctx context.Context, filter AuditFilter) ([]AuditEntry, error)
}

type AuditFilter struct {
	EntityID   *EntityID
	PolicyID   *PolicyID
	ActorID    *string
	Actions    []AuditAction
	From       *TimePoint
	To         *TimePoint
}
