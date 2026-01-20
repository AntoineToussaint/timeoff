# Generic Timed Resource Management System

> **Summary:** This presentation guide explains why we built a generic engine instead of a time-off-specific system: every company ends up building separate systems for PTO, rewards points, learning credits, and expense budgets—all with duplicated balance tracking, accrual calculations, and audit logic. Our solution: one core engine (`generic/`) that handles the math, with domain packages (`timeoff/`, `rewards/`) adding constraints and semantics. Key architectural decisions: append-only ledger for audit, `ResourceType` as an interface (not string) for type safety, period-based balance calculation, policy-driven reconciliation. Demo includes 7 scenarios showcasing multi-policy consumption, rollover with caps, maternity leave, and rewards points. 108 tests validate all invariants.

---

## Presentation & Discussion Guide

---

## 1. Executive Summary

### The Problem
Every company builds time-off management. Then they build rewards points. Then expense credits. Then learning hours. Each is a separate system with duplicated logic for:
- Balance tracking
- Accrual calculations
- Period management
- Approval workflows
- Audit trails

### The Solution
**One engine, multiple domains.**

A generic resource management system where:
- Core math is domain-agnostic
- Domain packages add constraints and semantics
- Storage is pluggable
- Audit is built-in

### Key Insight
> "All timed resources share the same fundamental operations: accrue, consume, reconcile. The differences are in constraints and semantics, not math."

---

## 2. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        API / UI Layer                           │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│                     Domain Packages                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │   timeoff/  │  │   rewards/  │  │   expense/  │  ...        │
│  │ - PTO, Sick │  │ - Points    │  │ - Credits   │             │
│  │ - Day unique│  │ - Multi-tx  │  │ - Approvals │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│                     generic/ - Core Engine                      │
│  Types │ Policy │ Balance │ Ledger │ Projection │ Reconciliation│
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│                      Storage Layer                              │
│          PostgreSQL │ SQLite │ In-Memory (testing)              │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. Key Design Decisions & Trade-offs

### Decision 1: Append-Only Ledger

**Choice:** Transactions are immutable. No UPDATE, no DELETE.

**Trade-off:**

| Benefit | Cost |
|---------|------|
| ✅ Complete audit trail | ❌ Storage grows forever |
| ✅ Point-in-time reconstruction | ❌ Corrections require reversal transactions |
| ✅ No race conditions on updates | ❌ More complex queries |
| ✅ Regulatory compliance ready | ❌ Cannot "fix" data mistakes easily |

**Discussion Point:** *"Would you accept reversal transactions for corrections, or do you need hard deletes for GDPR?"*

---

### Decision 2: Period-Based Balance (Not Point-in-Time)

**Choice:** Balance is always relative to a period (calendar year, fiscal year, anniversary).

**Trade-off:**

| Benefit | Cost |
|---------|------|
| ✅ Matches business reality | ❌ More complex than running total |
| ✅ Natural rollover handling | ❌ Period boundaries require reconciliation |
| ✅ Supports policy changes mid-period | ❌ Must define period for every policy |

**Discussion Point:** *"How do you handle employees who switch from calendar year to fiscal year mid-employment?"*

---

### Decision 3: Deterministic vs Non-Deterministic Accruals

**Choice:** Accruals declare whether future values are predictable.

```go
type AccrualSchedule interface {
    GenerateAccruals(from, to TimePoint) []AccrualEvent
    IsDeterministic() bool  // <-- Key method
}
```

**Impact:**
- **Deterministic** (20 days/year): Full year available immediately (ConsumeAhead)
- **Non-Deterministic** (hours worked): Only accrued-to-date available

**Trade-off:**

| Benefit | Cost |
|---------|------|
| ✅ Supports both PTO models | ❌ Two code paths for balance calculation |
| ✅ Flexible for different industries | ❌ Must classify every accrual type |
| ✅ Explicit about assumptions | ❌ Wrong classification = wrong balance |

**Discussion Point:** *"Is 'ConsumeAhead' (optimistic) or 'ConsumeUpToAccrued' (conservative) the default for new policies?"*

---

### Decision 4: Domain Packages Wrap Generic (Not Extend)

**Choice:** `timeoff.TimeOffLedger` wraps `generic.Ledger`, doesn't inherit.

```go
type TimeOffLedger struct {
    inner       generic.Ledger
    store       generic.Store
    entityStore generic.EntityStore
}
```

**Trade-off:**

| Benefit | Cost |
|---------|------|
| ✅ Generic stays pure | ❌ Some method forwarding |
| ✅ Domain constraints enforced | ❌ Can't use generic ledger directly for time-off |
| ✅ Clear separation | ❌ Two types to understand |
| ✅ Easy to add new domains | ❌ Some duplication in wrapper methods |

**Discussion Point:** *"Is the overhead of wrapper methods worth the architectural purity?"*

---

### Decision 5: Database-Level Constraints as Backup

**Choice:** Uniqueness enforced at both application AND database level.

```sql
CREATE UNIQUE INDEX idx_unique_day_consumption 
ON transactions(entity_id, resource_type, date(effective_at))
WHERE tx_type IN ('consumption', 'pending');
```

**Trade-off:**

| Benefit | Cost |
|---------|------|
| ✅ Defense in depth | ❌ Constraint logic in two places |
| ✅ Handles race conditions | ❌ Database-specific SQL |
| ✅ Data integrity guaranteed | ❌ Different error messages |

**Discussion Point:** *"Should we trust the application layer alone, or always have DB constraints?"*

---

### Decision 6: Multi-Policy with Priority Distribution

**Choice:** When consuming, iterate through policies by priority until satisfied.

```go
for _, policy := range sortedByPriority {
    available := policy.Balance.Available()
    take := min(remaining, available)
    allocations = append(allocations, Allocation{Policy: policy.ID, Amount: take})
    remaining -= take
}
```

**Trade-off:**

| Benefit | Cost |
|---------|------|
| ✅ "Use carryover first" is natural | ❌ Priority must be managed |
| ✅ Flexible allocation strategies | ❌ User might not understand which pool was used |
| ✅ Handles multiple PTO buckets | ❌ Audit trail shows multiple transactions |

**Discussion Point:** *"Should employees be able to choose which pool to draw from, or is it always automatic?"*

---

## 4. What's Implemented vs Proposed

### ✅ Implemented (POC Quality)

| Component | Status | Tests |
|-----------|--------|-------|
| Core types (Amount, Transaction, TimePoint) | ✅ Complete | 59 |
| Period-based balance calculation | ✅ Complete | Covered |
| Deterministic/Non-deterministic accruals | ✅ Complete | Covered |
| Reconciliation engine (carryover, expire, cap) | ✅ Complete | Covered |
| Multi-policy distribution | ✅ Complete | Covered |
| Time-off day uniqueness constraint | ✅ Complete | 36 |
| Rewards points (different unit) | ✅ Complete | 13 |
| SQLite storage | ✅ Complete | Integration |
| In-memory storage (testing) | ✅ Complete | Used in tests |
| Projection engine | ✅ Complete | Covered |
| **Total Tests** | | **108** |

### 🚧 Proposed / TODO

| Component | Status | Effort |
|-----------|--------|--------|
| Background reconciliation events | 📋 Designed | Medium |
| Approval workflow | 📋 Designed | Medium |
| PostgreSQL storage | 🔲 Not started | Low |
| Row-Level Security (RLS) | 🔲 Not started | Medium |
| Real-time balance caching | 🔲 Not started | High |
| Calendar integration (holidays) | 🔲 Not started | Medium |
| Manager delegation | 🔲 Not started | Medium |

---

## 5. Risks & Mitigations

### Risk 1: Performance at Scale

**Concern:** Calculating balance requires scanning all transactions.

**Mitigation:**
1. Periodic snapshots (already designed)
2. Materialized views in PostgreSQL
3. Read replicas for balance queries

**Metrics Needed:** Transactions per employee per year, query latency at 10K/100K/1M employees

---

### Risk 2: Complexity for Simple Use Cases

**Concern:** "We just need PTO tracking, this is overkill."

**Mitigation:**
1. Provide pre-built domain packages
2. Factory functions hide complexity
3. Default policies for common scenarios

**Counter-argument:** "The complexity exists in the domain. We're making it explicit, not creating it."

---

### Risk 3: Migration from Existing Systems

**Concern:** "We have 5 years of PTO data in the old system."

**Mitigation:**
1. Import historical transactions as "migration" type
2. Snapshot existing balances, start fresh
3. Run parallel for one period

---

### Risk 4: Edge Cases in Period Transitions

**Concern:** "What happens when an employee's anniversary changes?"

**Mitigation:**
1. Policy changes close old period, open new one
2. Reconciliation handles the transition
3. Explicit handling in tests

---

## 6. Discussion Questions for Technical Review

### Architecture
1. Is the generic/domain split at the right level of abstraction?
2. Should we use event sourcing instead of traditional append-only ledger?
3. Is Go the right language choice for this system?

### Domain Model
1. Are there resource types that don't fit this model?
2. How do we handle resources without periods (e.g., one-time grants)?
3. Should "approval required" be per-policy or per-request?

### Operations
1. What's the disaster recovery story for the ledger?
2. How do we handle timezone edge cases?
3. What monitoring/alerting do we need?

### Integration
1. How does this integrate with HRIS systems?
2. What's the API contract for external consumers?
3. Do we need GraphQL or is REST sufficient?

---

## 7. Demo Scenarios

### Scenario 1: Standard PTO
- 20 days/year, 5 day rollover cap
- Employee takes 3 days in March
- Shows: accrual, consumption, balance

### Scenario 2: Multiple Policies
- Carryover (5 days from last year, priority 1)
- Current year (20 days, priority 2)
- Shows: priority-based allocation

### Scenario 3: Maternity Leave
- Special grant, no accrual
- Different from regular PTO
- Shows: multiple resource types

### Scenario 4: Rewards Points
- Monthly accrual (100 points)
- Can spend multiple times per day
- Shows: different constraints from time-off

---

## 8. Next Steps

### If Approved

1. **Week 1-2:** PostgreSQL storage implementation
2. **Week 3-4:** Background reconciliation events
3. **Week 5-6:** Approval workflow
4. **Week 7-8:** Integration testing with real data

### If Changes Requested

Document specific concerns and re-present with revisions.

### If Rejected

Document learnings for future reference.

---

## Appendix: Key Code Patterns

### Creating a Policy

```go
policy := generic.Policy{
    ID:           "pto-2025",
    Name:         "Standard PTO",
    ResourceType: timeoff.ResourcePTO,
    Unit:         generic.UnitDays,
    Constraints:  generic.Constraints{AllowNegative: false},
    ConsumptionMode: generic.ConsumeAhead,
    PeriodConfig: generic.PeriodConfig{
        Type:       generic.PeriodCalendarYear,
        StartMonth: time.January,
    },
    ReconciliationRules: []generic.ReconciliationRule{{
        Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
        Actions: []generic.ReconciliationAction{
            {Type: generic.ActionCarryover, Config: generic.ActionConfig{MaxCarryover: &fiveDays}},
            {Type: generic.ActionExpire},
        },
    }},
}
```

### Recording Consumption

```go
tx := generic.Transaction{
    ID:            "tx-123",
    EntityID:      "emp-456",
    PolicyID:      "pto-2025",
    ResourceType:  timeoff.ResourcePTO,
    EffectiveAt:   generic.Date(2025, 3, 15),
    Delta:         generic.NewAmount(-1, generic.UnitDays),
    Type:          generic.TxConsumption,
    Reason:        "Vacation day",
    IdempotencyKey: "request-789",
}

err := ledger.Append(ctx, tx)
```

### Checking Balance

```go
balance, err := engine.CalculateBalance(ctx, entityID, policyID, period, asOf)
// balance.TotalEntitlement = 20 days
// balance.Consumed = 5 days
// balance.Available() = 15 days
```

---

*Document Version: 1.0*  
