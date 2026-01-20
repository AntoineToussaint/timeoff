# Engineering Guide

> **Summary:** This guide covers the internal architecture for engineers working on the codebase. The project follows strict dependency rules: `generic/` has zero knowledge of domains, `timeoff/` and `rewards/` depend only on `generic/` and own their `ResourceType` implementations (registered via `init()`), `api/` orchestrates everything. Data flows through: API → RequestService → TimeOffLedger (if time-off) → Generic Ledger → Store → Database. Rollover is currently manual via `/api/admin/rollover` but should become event-driven. Key invariants: append-only ledger, idempotency via TransactionID, atomic batch operations, day-uniqueness for time-off. When adding a new domain, create a package with `types.go` (implementing `ResourceType` interface + `init()` registration), `policies.go`, `accrual.go`, and tests.

---

## Project Layout

```
warp/
├── generic/                 # CORE ENGINE (domain-agnostic)
│   ├── types.go             # Primitives: Amount, Transaction, TimePoint, IDs
│   ├── policy.go            # Policy definition, reconciliation rules
│   ├── balance.go           # Balance calculation from transactions
│   ├── ledger.go            # Append-only log interface + default impl
│   ├── store.go             # Persistence interfaces
│   ├── assignment.go        # Multi-policy: assignment, distribution
│   ├── request.go           # Request lifecycle: create, approve, reject
│   ├── projection.go        # Future balance validation
│   ├── accrual.go           # AccrualSchedule interface
│   ├── period.go            # Period type (calendar year, fiscal, etc.)
│   ├── time.go              # TimePoint utilities
│   ├── snapshot.go          # Balance snapshot for optimization
│   └── store/
│       └── memory.go        # In-memory store for testing
│
├── timeoff/                 # TIME-OFF DOMAIN
│   ├── types.go             # TimeOffRequest, resource type constants
│   ├── policies.go          # Pre-built: StandardPTO, Sick, Parental
│   ├── accrual.go           # YearlyAccrual, TenureAccrual
│   ├── ledger.go            # Day-uniqueness wrapper
│   ├── request.go           # Time-off specific request handling
│   └── *_test.go            # Integration tests
│
├── rewards/                 # REWARDS DOMAIN
│   ├── types.go             # Resource types, units
│   ├── policies.go          # Wellness, Learning, Recognition
│   ├── accrual.go           # MonthlyPoints, Upfront, EventBased
│   └── *_test.go            # Integration tests
│
├── store/
│   └── sqlite/
│       └── sqlite.go        # SQLite/PostgreSQL implementation
│
├── factory/
│   └── policy.go            # JSON → Policy conversion
│
├── api/
│   ├── server.go            # Router setup, middleware
│   ├── handlers.go          # HTTP handlers
│   ├── dto.go               # Request/response types
│   └── scenarios.go         # Demo data loaders
│
├── cmd/
│   └── server/
│       └── main.go          # Application entry point
│
├── web/                     # React frontend
│   └── src/
│       ├── App.tsx
│       ├── api/client.ts
│       └── components/
│
└── docs/                    # Documentation
    ├── DESIGN.md            # Philosophy, concepts
    ├── IMPLEMENTATION.md    # Technical details
    ├── TESTING.md           # Test philosophy
    ├── PERFORMANCE_QA.md    # Performance analysis
    ├── DEVOPS_SECURITY.md   # Deployment, security
    ├── GETTING_STARTED.md   # Setup guide
    └── ENGINEERING.md       # This file
```

---

## Package Dependency Graph

```
                    ┌─────────────────────────────────────────┐
                    │                  cmd/                   │
                    │               (main.go)                 │
                    └─────────────────┬───────────────────────┘
                                      │
                    ┌─────────────────▼───────────────────────┐
                    │                  api/                   │
                    │     (handlers, server, scenarios)       │
                    └─────────────────┬───────────────────────┘
                                      │
          ┌───────────────────────────┼───────────────────────────┐
          │                           │                           │
          ▼                           ▼                           ▼
    ┌───────────┐             ┌─────────────┐             ┌─────────────┐
    │  factory/ │             │   timeoff/  │             │   rewards/  │
    │           │             │             │             │             │
    └─────┬─────┘             └──────┬──────┘             └──────┬──────┘
          │                          │                           │
          │                          │                           │
          └──────────────────────────┼───────────────────────────┘
                                     │
                    ┌────────────────▼────────────────┐
                    │            generic/             │
                    │                                 │
                    │  types, policy, balance,        │
                    │  ledger, store, assignment,     │
                    │  request, projection            │
                    └────────────────┬────────────────┘
                                     │
                    ┌────────────────▼────────────────┐
                    │          store/sqlite/          │
                    │                                 │
                    │  Implements generic.Store       │
                    └─────────────────────────────────┘
```

**Rules:**
- `generic/` has NO dependencies on `timeoff/` or `rewards/`
- `generic/` defines `ResourceType` as an **interface**, not a string
- `timeoff/` and `rewards/` implement `ResourceType` and register in `init()`
- `api/` orchestrates everything, uses domain types
- `store/sqlite/` implements `generic.Store` interface
- Storage uses `ResourceID()` for serialization, `GetOrCreateResource()` for deserialization

---

## Data Flow

### 1. Transaction Write Flow

```
User Request
     │
     ▼
┌─────────────┐
│   API       │  Parse JSON, validate input
└─────┬───────┘
      │
      ▼
┌─────────────┐
│  Request    │  Calculate distribution across policies
│  Service    │  Create pending transactions
└─────┬───────┘
      │
      ▼
┌─────────────┐
│  TimeOff    │  Check day uniqueness (time-off only)
│  Ledger     │  Validate domain rules
└─────┬───────┘
      │
      ▼
┌─────────────┐
│  Generic    │  Check idempotency
│  Ledger     │  Append to transaction log
└─────┬───────┘
      │
      ▼
┌─────────────┐
│   Store     │  Persist to database
│  (SQLite)   │  Enforce constraints
└─────────────┘
```

### 2. Balance Calculation Flow

```
Balance Request (entityID, resourceType, asOf)
     │
     ▼
┌──────────────────┐
│ ResourceBalance  │  Get all policy assignments for entity
│ Calculator       │
└─────┬────────────┘
      │
      │  For each assignment:
      ▼
┌──────────────────┐
│     Ledger       │  Load transactions for (entity, policy, period)
└─────┬────────────┘
      │
      ▼
┌──────────────────┐
│    Balance       │  Sum by type: accruals, consumption, pending
│   Calculation    │  Apply ConsumptionMode
└─────┬────────────┘
      │
      ▼
┌──────────────────┐
│   Aggregation    │  Sum across policies
│                  │  Sort by priority
└─────┬────────────┘
      │
      ▼
ResourceBalance {
    TotalAvailable: 28 days
    PolicyBalances: [
        {PolicyID: "carryover", Available: 5},
        {PolicyID: "standard", Available: 23},
    ]
}
```

---

## Event System: Rollover & Reconciliation

### How Rollover Works Today

Currently, rollover is **triggered manually** via API:

```
POST /api/admin/rollover
{
    "entity_id": "emp-123",
    "policy_id": "pto-standard",
    "period_end": "2025-12-31"
}
```

**Flow:**

```
Manual Trigger
     │
     ▼
┌───────────────────┐
│ ReconciliationEngine │
│    .Process()        │
└─────┬─────────────┘
      │
      │  1. Get current balance
      │  2. Calculate accrued balance (CurrentAccrued())
      │  3. Get policy rules
      │  4. For each rule:
      ▼
┌───────────────────┐
│  Apply Actions    │
│  (on accrued)     │
│                   │
│  • Carryover      │ → Create TxReconciliation (+X days to next period)
│  • Expire         │ → Create TxReconciliation (-X days, reason: expired)
│  • Cap            │ → Create TxReconciliation (-X days, reason: capped)
└─────┬─────────────┘
      │
      ▼
┌───────────────────┐
│  Ledger.Append    │  Write reconciliation transactions
│  Batch            │  (atomic)
└───────────────────┘
```

**Critical Detail**: Reconciliation uses `CurrentAccrued()` (what was actually earned), not `Current()` (full entitlement). This ensures:
- New hires only reconcile what they earned (e.g., 2 days for December hire, not 24)
- Mid-year policy changes reconcile correctly
- Only earned balance can be carried over or expired

### Reconciliation Output

```go
type ReconciliationOutput struct {
    Transactions []Transaction  // To be appended to ledger
    Summary      ReconciliationSummary
    Errors       []error
}

type ReconciliationSummary struct {
    CarriedOver Amount  // e.g., 5 days
    Expired     Amount  // e.g., 10 days
    Capped      Amount  // e.g., 0 days
    Forfeited   Amount  // e.g., 0 days
}
```

### Example: Year-End Rollover

**Scenario:** Employee has 15 days remaining, max carryover is 5 days

```
Input:
  Balance: 15 days remaining (from CurrentAccrued())
  Policy Rule: Carryover max 5, then expire

Processing:
  1. ActionCarryover: min(15, 5) = 5 days → Create +5 TxReconciliation
  2. ActionExpire: 15 - 5 = 10 days → Create -10 TxReconciliation

Output Transactions:
  [
    {Type: TxReconciliation, Delta: +5, Reason: "Carryover to 2026"},
    {Type: TxReconciliation, Delta: -10, Reason: "Expired unused balance"},
  ]

Result:
  New period starts with 5 days from carryover
```

**Important**: Reconciliation uses `CurrentAccrued()` (what was actually earned), not `Current()` (full entitlement). This ensures:
- New hires only reconcile what they earned (e.g., 2 days for December hire, not 24)
- Mid-year policy changes reconcile correctly
- Only earned balance can be carried over or expired

---

## TODO: Event-Driven Reconciliation

The current manual trigger should be replaced with an event-driven system.

### Proposed Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      EVENT BUS                                  │
│                                                                 │
│  Events:                                                        │
│  • PeriodEndEvent(entityID, policyID, period)                  │
│  • PolicyChangeEvent(entityID, oldPolicy, newPolicy)           │
│  • TerminationEvent(entityID, terminationDate)                 │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
          │                    │                    │
          ▼                    ▼                    ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│ Reconciliation  │  │  Policy Change  │  │   Termination   │
│    Handler      │  │    Handler      │  │    Handler      │
│                 │  │                 │  │                 │
│ Process year-   │  │ Close old       │  │ Calculate final │
│ end rollover    │  │ period, open    │  │ balance, payout │
│                 │  │ new period      │  │ or forfeit      │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

### Event Types Needed

| Event | Trigger | Handler |
|-------|---------|---------|
| `PeriodEndEvent` | Cron job at midnight on period end | Run reconciliation for all entities |
| `PolicyChangeEvent` | HR changes employee's policy | Close current period, start new |
| `TerminationEvent` | Employee leaves company | Calculate final balance, create payout/forfeit |
| `HireEvent` | New employee onboarded | Assign policies, prorate first period |
| `AnniversaryEvent` | Employee tenure milestone | Upgrade to higher accrual tier |

### Implementation Plan

```go
// events/types.go
type Event interface {
    Type() string
    EntityID() EntityID
    OccurredAt() time.Time
}

type PeriodEndEvent struct {
    Entity   EntityID
    PolicyID PolicyID
    Period   Period
}

// events/handler.go
type EventHandler interface {
    Handle(ctx context.Context, event Event) error
}

// events/reconciliation_handler.go
type ReconciliationHandler struct {
    Engine *ReconciliationEngine
    Ledger Ledger
}

func (h *ReconciliationHandler) Handle(ctx context.Context, event Event) error {
    e := event.(*PeriodEndEvent)
    output, err := h.Engine.Process(ReconciliationInput{...})
    if err != nil {
        return err
    }
    return h.Ledger.AppendBatch(ctx, output.Transactions)
}
```

---

## Accrual System

### How Accruals Work

Accruals are **calculated on-demand**, not stored as events:

```go
type AccrualSchedule interface {
    GenerateAccruals(from, to TimePoint) []AccrualEvent
    IsDeterministic() bool
}
```

**On Balance Calculation:**
1. Load existing transactions from ledger
2. Call `accrual.GenerateAccruals(period.Start, period.End)`
3. Compare: which accrual events don't have matching transactions?
4. Include pending accruals in projection (if deterministic)

### Why Not Store Accruals As Events?

**Option A (Current): Calculate on demand**
- Pros: Simple, no background jobs, always consistent
- Cons: Calculation cost on every request

**Option B: Pre-generate accrual transactions**
- Pros: Fast reads, audit trail of exact accrual dates
- Cons: Need background job, what if policy changes mid-year?

**Decision:** We use Option A for simplicity. If performance becomes an issue, add balance snapshots (already supported).

### Accrual Types

```
┌─────────────────────────────────────────────────────────────────┐
│                    ACCRUAL TYPES                                │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  DETERMINISTIC (future is known)                                │
│  ├── YearlyAccrual: 20 days/year, monthly or upfront           │
│  ├── TenureAccrual: Rate based on years of service             │
│  └── MonthlyPointsAccrual: Fixed points per month              │
│                                                                 │
│  NON-DETERMINISTIC (future depends on events)                  │
│  ├── HoursWorkedAccrual: 1 hour PTO per 40 hours worked       │
│  └── EventBasedAccrual: Points from peer recognition          │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**Impact on Balance:**

| Accrual Type | ConsumptionMode | Available Balance |
|--------------|-----------------|-------------------|
| Deterministic | ConsumeAhead | Full year entitlement |
| Deterministic | ConsumeUpToAccrued | Only accrued so far |
| Non-Deterministic | (forced) ConsumeUpToAccrued | Only earned events |

---

## Concurrency & Race Conditions

### Known Race Conditions

1. **Duplicate Day-Off Request**
   - Two requests for same day submitted simultaneously
   - Both pass validation, both write
   - **Solution:** Unique DB constraint + TimeOffLedger check

2. **Balance Overdraft**
   - Two requests consume more than available
   - Both pass validation (see old balance), both write
   - **Solution:** Re-validate in transaction, or allow temporary negative

3. **Concurrent Approval/Rejection**
   - Manager approves while employee cancels
   - Both succeed, inconsistent state
   - **Solution:** Optimistic locking with version field (TODO)

### Current Mitigations

```sql
-- DB constraint prevents duplicate day consumption
CREATE UNIQUE INDEX idx_unique_day_consumption 
ON transactions(entity_id, resource_type, DATE(effective_at))
WHERE tx_type IN ('consumption', 'pending');
```

```go
// TimeOffLedger pre-checks before write
func (l *TimeOffLedger) Append(ctx context.Context, tx Transaction, policy Policy) error {
    if policy.UniquePerTimePoint {
        isConsumed, _, err := l.Store.IsDayConsumed(ctx, tx.EntityID, tx.ResourceType, tx.EffectiveAt)
        if isConsumed {
            return &DuplicateDayError{...}
        }
    }
    return l.Store.Append(ctx, tx)
}
```

---

## Testing Strategy

### Test Pyramid

```
                    ┌─────────────┐
                    │   E2E       │  Few: API + Frontend
                    │   Tests     │  (manual/Playwright)
                    ├─────────────┤
                    │ Integration │  Medium: Domain + Store
                    │   Tests     │  timeoff_test.go, rewards_test.go
                    ├─────────────┤
                    │   Unit      │  Many: Pure logic
                    │   Tests     │  engine_test.go, assignment_test.go
                    └─────────────┘
```

### What Each Test Covers

| Package | Test File | Coverage |
|---------|-----------|----------|
| `generic` | `engine_test.go` | Balance calculation, period, idempotency |
| `generic` | `assignment_test.go` | Multi-policy distribution |
| `timeoff` | `timeoff_test.go` | Multi-policy, rollover, consumption modes |
| `timeoff` | `ledger_test.go` | Day uniqueness invariant |
| `rewards` | `rewards_test.go` | Points, credits, different units |

### Running Tests

```bash
# All tests
make test

# Verbose
go test ./... -v

# Single package
go test ./timeoff/... -v

# Single test
go test ./timeoff/... -v -run TestMultiPolicy

# Race detector
go test ./... -race

# Coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

---

## Adding a New Feature

### Example: Add "Sabbatical" Policy Type

1. **Define policy in `timeoff/policies.go`:**
```go
func SabbaticalPolicy(id PolicyID) PolicyConfig {
    return PolicyConfig{
        Policy: generic.Policy{
            ID:              id,
            Name:            "Sabbatical",
            ResourceType:    ResourceSabbatical,
            ConsumptionMode: generic.ConsumeAhead,
            // ... 
        },
        Accrual: &SabbaticalAccrual{YearsRequired: 7, WeeksGranted: 4},
    }
}
```

2. **Add accrual if needed in `timeoff/accrual.go`:**
```go
type SabbaticalAccrual struct {
    YearsRequired int
    WeeksGranted  int
}

func (s *SabbaticalAccrual) GenerateAccruals(from, to TimePoint) []AccrualEvent {
    // Grant sabbatical on 7th anniversary
}
```

3. **Add to factory if JSON support needed:**
```go
// factory/policy.go
func SabbaticalJSON() string {
    return `{
        "id": "sabbatical",
        "resource_type": "sabbatical",
        ...
    }`
}
```

4. **Write tests:**
```go
// timeoff/timeoff_test.go
func TestSabbatical_GrantedAfter7Years(t *testing.T) {
    // ...
}
```

5. **Add to demo scenario if desired:**
```go
// api/scenarios.go
func loadSabbaticalScenario(ctx context.Context) error {
    // ...
}
```

---

## Document Review: What We Have vs What's Missing

### Documentation Status

| Document | Status | Content |
|----------|--------|---------|
| `README.md` | ✅ Complete | Overview, quick start, architecture |
| `GETTING_STARTED.md` | ✅ Complete | Setup, running, troubleshooting |
| `DESIGN.md` | ✅ Complete | Philosophy, concepts, diagrams |
| `IMPLEMENTATION.md` | ⚠️ Needs Update | TODOs outdated, missing event system |
| `TESTING.md` | ✅ Complete | Test philosophy, coverage |
| `PERFORMANCE_QA.md` | ✅ Complete | Bottlenecks, solutions |
| `DEVOPS_SECURITY.md` | ✅ Complete | Security, deployment, TODOs |
| `ENGINEERING.md` | ✅ New | This file |

### Code Documentation Status

| File | Header | Comments |
|------|--------|----------|
| `generic/types.go` | ✅ | ✅ |
| `generic/policy.go` | ✅ | ✅ |
| `generic/balance.go` | ✅ | ✅ |
| `generic/ledger.go` | ✅ | ✅ |
| `generic/store.go` | ✅ | ✅ |
| `generic/assignment.go` | ✅ | ✅ |
| `generic/request.go` | ✅ | ✅ |
| `generic/projection.go` | ✅ | ✅ |
| `timeoff/policies.go` | ✅ | ✅ |
| `timeoff/ledger.go` | ✅ | ✅ |
| `timeoff/accrual.go` | ✅ | ✅ |
| `rewards/types.go` | ✅ | ✅ |
| `rewards/policies.go` | ✅ | ✅ |
| `rewards/accrual.go` | ✅ | ✅ |
| `store/sqlite/sqlite.go` | ✅ | ⚠️ Inline only |
| `api/handlers.go` | ✅ | ⚠️ Inline only |
| `factory/policy.go` | ✅ | ⚠️ Inline only |

### Missing/TODO

| Item | Priority | Description |
|------|----------|-------------|
| Event System | P1 | Background jobs for reconciliation |
| PostgreSQL Store | P1 | Production database support |
| Authentication | P0 | No auth currently |
| Request Versioning | P2 | Optimistic locking for concurrent updates |
| Balance Snapshots | P2 | Performance optimization |
| Audit API | P2 | Query transaction history with filters |
| Webhook Notifications | P3 | Notify external systems of events |
