# Performance & Quality Engineering Review

> **Summary:** This document identifies performance bottlenecks and quality risks. Critical issues: balance calculation is O(n) on transaction count (needs snapshots for scale), race conditions possible without proper locking (needs optimistic concurrency), missing database indexes (added). Correctness invariants enforced: append-only ledger (no UPDATE/DELETE), idempotency (duplicate keys rejected), atomic batches (all-or-nothing), day-uniqueness for time-off (DB constraint + application check), non-negative balance (when configured). QA priorities: P0 race conditions, P1 balance calculation optimization, P1 index coverage, P2 audit completeness. Recommendations: implement balance snapshots, add optimistic locking with version column, use read replicas for balance queries, implement caching layer for hot paths.

---

## Executive Summary

This document provides a detailed analysis of the system from a **Performance Engineering** and **Quality Assurance** perspective, identifying critical paths, potential bottlenecks, correctness invariants, and recommendations.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  RISK MATRIX                                                                │
├─────────────────────┬───────────────┬───────────────┬───────────────────────┤
│  Issue              │  Impact       │  Likelihood   │  Priority             │
├─────────────────────┼───────────────┼───────────────┼───────────────────────┤
│  Race conditions    │  HIGH         │  MEDIUM       │  P0 - Critical        │
│  O(n) balance calc  │  HIGH         │  HIGH         │  P1 - High            │
│  Missing indexes    │  MEDIUM       │  HIGH         │  P1 - High            │
│  Audit gaps         │  MEDIUM       │  MEDIUM       │  P2 - Medium          │
│  Memory store sort  │  LOW          │  LOW          │  P3 - Low             │
└─────────────────────┴───────────────┴───────────────┴───────────────────────┘
```

---

## Part 1: Quality Assurance

### 1.1 Correctness Invariants

These properties must ALWAYS be true. Any violation is a critical bug.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  INVARIANT 1: Ledger Immutability                                          │
│                                                                             │
│  Once a transaction is written, it CANNOT be modified or deleted.          │
│  Corrections are made via Reversal transactions only.                      │
│                                                                             │
│  Verification: No UPDATE or DELETE SQL statements on transactions table    │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│  INVARIANT 2: Idempotency                                                  │
│                                                                             │
│  A transaction with a given IdempotencyKey can only be written ONCE.       │
│  Duplicate attempts must fail with ErrDuplicateIdempotencyKey.             │
│                                                                             │
│  Verification: UNIQUE constraint on idempotency_key column                 │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│  INVARIANT 3: Day Uniqueness (Time-Off Only)                               │
│                                                                             │
│  For time-off resources, an entity cannot have TWO consumption             │
│  transactions for the same (Date, ResourceType) combination.               │
│                                                                             │
│  Verification: TimeOffLedger.validateDayUniqueness()                       │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│  INVARIANT 4: Balance Consistency                                          │
│                                                                             │
│  Balance = Sum(Accruals) - Sum(Consumption) + Sum(Adjustments)             │
│  This derivation must always produce the same result for the same inputs.  │
│                                                                             │
│  Verification: Property-based tests with random transaction sequences      │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│  INVARIANT 5: Atomicity of Batch Operations                                │
│                                                                             │
│  AppendBatch either writes ALL transactions or NONE.                       │
│  Partial writes must never occur.                                          │
│                                                                             │
│  Verification: Database transaction wrapping                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 Race Condition Analysis

#### Critical Race: Concurrent Time-Off Requests

```
Timeline:
────────────────────────────────────────────────────────────────────────►
     │                    │                    │
     │ Request A          │ Request B          │
     │ (March 10)         │ (March 10)         │
     ▼                    ▼                    │
┌─────────┐          ┌─────────┐              │
│ Check   │          │ Check   │              │
│ if day  │          │ if day  │              │
│ is free │          │ is free │              │
└────┬────┘          └────┬────┘              │
     │ Yes!               │ Yes!              │  ← BOTH see day as free
     ▼                    ▼                   │
┌─────────┐          ┌─────────┐              │
│ Write   │          │ Write   │              │
│ tx A    │          │ tx B    │              │
└─────────┘          └─────────┘              │
                                              │
     RESULT: Two transactions for same day!   │
                                              ▼
```

**Current Status:** ⚠️ VULNERABLE

**Solution: Optimistic Locking with Database Constraint**

```sql
-- Add unique constraint for day uniqueness
CREATE UNIQUE INDEX idx_unique_consumption_day 
ON transactions(entity_id, resource_type, DATE(effective_at))
WHERE tx_type IN ('consumption', 'pending');
```

```go
// In TimeOffLedger.Append, rely on DB constraint as final check
func (l *TimeOffLedger) Append(ctx context.Context, tx Transaction) error {
    // Pre-check (reduces contention)
    if err := l.validateDayUniqueness(ctx, tx); err != nil {
        return err
    }
    
    // Attempt write - DB constraint is authoritative
    err := l.inner.Append(ctx, tx)
    if isDuplicateDayConstraintError(err) {
        return &DuplicateDayError{...}
    }
    return err
}
```

#### Critical Race: Balance Check vs Consumption Write

```
Timeline:
────────────────────────────────────────────────────────────────────────►
     │                    │                    │
     │ Request A          │ Request B          │
     │ (5 days)           │ (5 days)           │
     ▼                    ▼                    │
┌─────────┐          ┌─────────┐              │
│ Check   │          │ Check   │              │
│ balance │          │ balance │              │
│ = 7 days│          │ = 7 days│              │
└────┬────┘          └────┬────┘              │
     │ OK!                │ OK!               │  ← BOTH see 7 days available
     ▼                    ▼                   │
┌─────────┐          ┌─────────┐              │
│ Write   │          │ Write   │              │
│ -5 days │          │ -5 days │              │
└─────────┘          └─────────┘              │
                                              │
     RESULT: -3 days balance (overdraft!)     │
                                              ▼
```

**Current Status:** ⚠️ VULNERABLE (unless AllowNegative=true)

**Solution: Serializable Transaction + Re-check**

```go
func (l *TimeOffLedger) AppendWithBalanceCheck(ctx context.Context, tx Transaction, maxConsumption Amount) error {
    return l.store.WithTx(ctx, func(s Store) error {
        // Re-calculate balance within transaction (serializable read)
        balance, err := calculateBalanceInTx(ctx, s, tx.EntityID, tx.PolicyID, period)
        if err != nil {
            return err
        }
        
        if balance.Available().LessThan(tx.Delta.Neg()) {
            return ErrInsufficientBalance
        }
        
        return s.Append(ctx, tx)
    })
}
```

### 1.3 Auditability Checklist

| Requirement | Status | Implementation |
|-------------|--------|----------------|
| Who created each transaction | ⚠️ Missing | Add `CreatedBy` field |
| When was transaction created | ✅ Present | `created_at` column |
| Original request reference | ✅ Present | `ReferenceID` field |
| Approval chain | ⚠️ Partial | `ApprovedBy` on Request only |
| IP address / client info | ❌ Missing | Add `Metadata` population |
| Change reason | ✅ Present | `Reason` field |

**Recommendation: Enhanced Audit Trail**

```go
type TransactionAudit struct {
    TransactionID TransactionID
    ActorID       string            // Who performed the action
    ActorType     string            // "employee", "manager", "system", "admin"
    ClientIP      string            // Request origin
    UserAgent     string            // Client identifier
    SessionID     string            // For tracking related actions
    Timestamp     time.Time
    Action        string            // "create", "approve", "reject", "reverse"
}
```

### 1.4 Debuggability

#### Current Debug Points

```go
// Transaction has enough context to debug most issues
type Transaction struct {
    ID             TransactionID  // ✅ Unique identifier
    IdempotencyKey string         // ✅ Duplicate detection
    ReferenceID    string         // ✅ Link to request
    Reason         string         // ✅ Human explanation
    Metadata       map[string]string // ✅ Extensible context
}
```

#### Missing Debug Information

```go
// SUGGESTION: Add these to Transaction or separate audit log
type DebugInfo struct {
    // Calculation context
    BalanceAtRequest   Amount   // What was the balance when this was created?
    PolicyVersionUsed  int      // Which policy version was applied?
    
    // Timing
    ProcessingDuration time.Duration // How long did validation take?
    
    // Error context
    ValidationErrors   []string // What validations were run?
}
```

---

## Part 2: Performance Engineering

### 2.1 Complexity Analysis

#### Balance Calculation: O(T × P)

Where T = transactions in period, P = policies for entity

```
Current Flow:
─────────────────────────────────────────────────────────────────────────

1. GetAssignmentsByEntity(entityID)           O(P) - DB query
2. For each policy P:
   a. TransactionsInRange(entityID, policyID)  O(T) - DB query × P
   b. Sum transactions                         O(T)
3. Sort by priority                            O(P log P)
4. Return aggregated balance

Total: O(P × T) queries + O(P × T) compute = O(P × T)

─────────────────────────────────────────────────────────────────────────
```

**Problem Scenario:**
- Employee with 5 policies (pto, sick, carryover, bonus, floating)
- 3 years of history = ~1000 transactions per policy
- Balance check = 5 DB queries × 1000 rows each = 5000 rows processed

#### Memory Store: O(n log n) per append

```go
func (m *Memory) appendLocked(tx Transaction) error {
    m.transactions[k] = append(m.transactions[k], tx)
    
    // THIS IS THE PROBLEM: Sort on every append
    sort.Slice(m.transactions[k], func(i, j int) bool {  // O(n log n)
        return m.transactions[k][i].EffectiveAt.Before(m.transactions[k][j].EffectiveAt)
    })
}
```

**Fix: Binary insertion**

```go
func (m *Memory) appendLocked(tx Transaction) error {
    k := key{EntityID: tx.EntityID, PolicyID: tx.PolicyID}
    
    // Binary search for insertion point: O(log n)
    txs := m.transactions[k]
    i := sort.Search(len(txs), func(i int) bool {
        return txs[i].EffectiveAt.After(tx.EffectiveAt)
    })
    
    // Insert at position: O(n) but no comparison overhead
    txs = append(txs, Transaction{})
    copy(txs[i+1:], txs[i:])
    txs[i] = tx
    m.transactions[k] = txs
}
```

### 2.2 Database Indexes ✅

All critical indexes have been implemented:

```
Implemented Indexes:
─────────────────────────────────────────────────────────────────────────

Transactions Table:
  • idx_transactions_entity_policy        - (entity_id, policy_id)
  • idx_transactions_effective_at         - (effective_at)
  • idx_transactions_idempotency          - (idempotency_key) partial
  • idx_transactions_entity_resource_date - (entity_id, resource_type, effective_at)
  • idx_transactions_entity_policy_date   - (entity_id, policy_id, effective_at DESC)
  • idx_transactions_reference            - (reference_id) partial
  • idx_transactions_type                 - (tx_type)
  • idx_unique_day_consumption            - UNIQUE constraint for day uniqueness

Assignments Table:
  • idx_assignments_entity                - (entity_id)
  • idx_assignments_policy                - (policy_id)
  • idx_assignments_entity_active         - (entity_id, effective_from, effective_to)

Policies Table:
  • idx_policies_resource_type            - (resource_type)
```

#### Query Performance Notes

All hot-path queries now use index scans:
- Balance calculation uses `idx_transactions_entity_policy_date`
- Day uniqueness checks use `idx_transactions_entity_resource_date`
- Request lookups use `idx_transactions_reference`

### 2.3 Caching Strategy

#### What to Cache

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  CACHE HIERARCHY                                                            │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  L1: Hot Data (in-process, <1ms)                                           │
│  ├── Policies (rarely change)                                               │
│  ├── Assignments (change on HR events)                                      │
│  └── Accrual schedules                                                      │
│                                                                             │
│  L2: Warm Data (Redis, <5ms)                                               │
│  ├── Period snapshots (computed balance at period end)                      │
│  ├── Recent balance calculations (TTL: 5min)                                │
│  └── Days-off bitmap per entity/year                                        │
│                                                                             │
│  L3: Cold Data (Database, <50ms)                                           │
│  └── Full transaction history                                               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Balance Cache Implementation

```go
type BalanceCache struct {
    cache    *lru.Cache // github.com/hashicorp/golang-lru
    ttl      time.Duration
    ledger   Ledger
}

type cachedBalance struct {
    Balance   Balance
    ExpiresAt time.Time
    Version   int64 // Invalidated when new tx written
}

func (bc *BalanceCache) GetBalance(ctx context.Context, entityID EntityID, policyID PolicyID, period Period) (Balance, error) {
    key := fmt.Sprintf("%s:%s:%s", entityID, policyID, period.Start.Format("2006"))
    
    if cached, ok := bc.cache.Get(key); ok {
        cb := cached.(*cachedBalance)
        if time.Now().Before(cb.ExpiresAt) {
            return cb.Balance, nil
        }
    }
    
    // Cache miss - calculate and store
    balance, err := bc.ledger.CalculateBalance(ctx, entityID, policyID, period)
    if err != nil {
        return Balance{}, err
    }
    
    bc.cache.Add(key, &cachedBalance{
        Balance:   balance,
        ExpiresAt: time.Now().Add(bc.ttl),
    })
    
    return balance, nil
}

// InvalidateOnWrite must be called after any ledger write
func (bc *BalanceCache) InvalidateOnWrite(entityID EntityID, policyID PolicyID) {
    // Invalidate all cached balances for this entity+policy
    // In production, use cache tags for efficient invalidation
}
```

### 2.4 Snapshot Strategy

Instead of recalculating balance from all transactions, maintain periodic snapshots.

```
Balance Calculation with Snapshots
─────────────────────────────────────────────────────────────────────────

WITHOUT snapshots (current):
┌─────────────────────────────────────────────────────────────────────┐
│ Jan 1 ─── Feb ─── Mar ─── Apr ─── May ─── Jun ─── TODAY            │
│   │        │       │       │       │       │                        │
│   ▼        ▼       ▼       ▼       ▼       ▼                        │
│  tx1     tx2-5   tx6-10  tx11-15 tx16-20 tx21-25                    │
│                                                                     │
│  Calculate: Sum ALL 25 transactions = O(25)                         │
└─────────────────────────────────────────────────────────────────────┘

WITH monthly snapshots:
┌─────────────────────────────────────────────────────────────────────┐
│ Jan 1 ─── Feb ─── Mar ─── Apr ─── May ─── Jun ─── TODAY            │
│   │        │       │       │       │       │                        │
│   ▼        ▼       ▼       ▼       ▼       ▼                        │
│  [S1]    [S2]    [S3]    [S4]    [S5]   tx21-25                     │
│                                    │                                │
│                                    └── Use snapshot + recent only   │
│                                                                     │
│  Calculate: Snapshot + 5 transactions = O(5)                        │
└─────────────────────────────────────────────────────────────────────┘
```

#### Snapshot Table Schema

```sql
CREATE TABLE balance_snapshots (
    entity_id     TEXT NOT NULL,
    policy_id     TEXT NOT NULL,
    snapshot_date TEXT NOT NULL,  -- First day of month
    period_start  TEXT NOT NULL,
    period_end    TEXT NOT NULL,
    
    -- Denormalized balance components
    accrued_to_date    TEXT NOT NULL,
    total_entitlement  TEXT NOT NULL,
    total_consumed     TEXT NOT NULL,
    pending            TEXT NOT NULL,
    adjustments        TEXT NOT NULL,
    
    -- For validation
    last_tx_id    TEXT,           -- Last transaction included
    tx_count      INTEGER,        -- Number of transactions summarized
    
    created_at    TEXT NOT NULL,
    
    PRIMARY KEY (entity_id, policy_id, snapshot_date)
);
```

### 2.5 Scalability Roadmap

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  SCALE STAGES                                                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Stage 1: Single Instance (Current)                                         │
│  ├── PostgreSQL (managed or self-hosted)                                    │
│  ├── In-process caching                                                     │
│  └── Handles: ~10K employees, ~100 req/sec                                  │
│                                                                             │
│  Stage 2: Read Replicas                                                     │
│  ├── Primary for writes                                                     │
│  ├── Replicas for balance reads                                             │
│  ├── Redis for cache                                                        │
│  └── Handles: ~100K employees, ~1K req/sec                                  │
│                                                                             │
│  Stage 3: Sharded                                                           │
│  ├── Shard by entity_id hash                                                │
│  ├── Each shard is independent                                              │
│  ├── Cross-shard queries rare (admin only)                                  │
│  └── Handles: ~1M employees, ~10K req/sec                                   │
│                                                                             │
│  Stage 4: Event-Sourced                                                     │
│  ├── Kafka for transaction log                                              │
│  ├── Materialized views for balances                                        │
│  ├── CQRS pattern                                                           │
│  └── Handles: Unlimited scale                                               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Part 3: Recommendations

### P0: Critical (Implement Now)

#### 1. Add Database Constraint for Day Uniqueness

```sql
-- Partial unique index (PostgreSQL syntax)
CREATE UNIQUE INDEX idx_unique_day_consumption 
ON transactions(entity_id, resource_type, DATE(effective_at))
WHERE tx_type IN ('consumption', 'pending');
```

#### 2. Add Optimistic Locking for Requests

```go
type Request struct {
    // ... existing fields ...
    Version int64 // Incremented on each update
}

func (rs *RequestService) Approve(ctx context.Context, request *Request) error {
    return rs.store.WithTx(ctx, func(s Store) error {
        // Re-fetch and check version
        current, err := s.GetRequest(ctx, request.ID)
        if err != nil {
            return err
        }
        if current.Version != request.Version {
            return ErrConcurrentModification
        }
        
        // Proceed with approval...
        request.Version++
        return s.UpdateRequest(ctx, request)
    })
}
```

### P1: High Priority (This Sprint)

#### 3. Database Indexes ✅ DONE

The following indexes have been added to optimize query performance:

- `idx_transactions_entity_policy` - Entity + Policy lookups
- `idx_transactions_entity_resource_date` - Day uniqueness validation
- `idx_transactions_entity_policy_date` - Period-based balance queries (hot path)
- `idx_transactions_reference` - Request tracking
- `idx_transactions_type` - Transaction type filtering
- `idx_unique_day_consumption` - Enforces day uniqueness constraint
- `idx_assignments_entity_active` - Active assignment lookups
- `idx_policies_resource_type` - Policy filtering by resource type

#### 4. Implement Balance Snapshots

See section 2.4 for full implementation.

#### 5. Add Actor Tracking

```go
// Extend Transaction
type Transaction struct {
    // ... existing fields ...
    CreatedBy     string    // Actor ID
    CreatedByType string    // "employee", "manager", "system"
    CreatedAt     time.Time // Already exists in DB, add to struct
}
```

### P2: Medium Priority (Next Sprint)

#### 6. Implement Caching Layer

```go
type CachedLedger struct {
    inner  Ledger
    cache  BalanceCache
    policy PolicyCache
}

func (cl *CachedLedger) GetBalance(...) (Balance, error) {
    // Check cache first, fallback to inner
}
```

#### 7. Add Metrics/Observability

```go
type InstrumentedLedger struct {
    inner   Ledger
    metrics *prometheus.Registry
}

func (il *InstrumentedLedger) Append(ctx context.Context, tx Transaction) error {
    timer := prometheus.NewTimer(il.appendDuration)
    defer timer.ObserveDuration()
    
    err := il.inner.Append(ctx, tx)
    
    if err != nil {
        il.appendErrors.Inc()
    }
    return err
}
```

### P3: Low Priority (Backlog)

#### 8. Binary Insertion in Memory Store

See section 2.1 for implementation.

#### 9. Event Sourcing Preparation

Design events for future CQRS migration.

---

## Appendix: Testing Checklist

### Invariant Tests

```go
// TestInvariant_LedgerImmutability
// Verify no UPDATE/DELETE operations succeed

// TestInvariant_Idempotency
// Same key twice must fail

// TestInvariant_DayUniqueness
// Same day twice must fail

// TestInvariant_BalanceConsistency
// Balance = accruals - consumption + adjustments (property test)

// TestInvariant_BatchAtomicity
// Partial batch failure rolls back all
```

### Race Condition Tests

```go
// TestRace_ConcurrentTimeOffRequests
// Two goroutines request same day simultaneously

// TestRace_ConcurrentBalanceOverdraft
// Two goroutines consume more than available simultaneously

// TestRace_ConcurrentApprovalRejection
// Approve and reject same request simultaneously
```

### Performance Benchmarks

```go
// BenchmarkBalanceCalculation_10Transactions
// BenchmarkBalanceCalculation_100Transactions
// BenchmarkBalanceCalculation_1000Transactions

// BenchmarkAppend_MemoryStore
// BenchmarkAppend_PostgreSQLStore

// BenchmarkConcurrentReads_10Goroutines
// BenchmarkConcurrentReads_100Goroutines
```
