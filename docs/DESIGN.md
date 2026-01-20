# Generic Timed Resource Management System

> **Summary:** This system provides a single, generic engine for tracking any time-bounded resource—PTO, sick leave, wellness points, learning budgets—using an append-only ledger, period-based balance calculation, and policy-driven reconciliation. The core `generic/` package handles all the math with zero domain knowledge, while domain packages (`timeoff/`, `rewards/`) implement the `ResourceType` interface and register their types at startup. Key concepts include Policies (accrual rules, carryover limits), Periods (calendar/fiscal year boundaries), Transactions (immutable ledger entries), and Reconciliation (year-end carryover/expiration). The architecture ensures clean separation of concerns, type safety via interfaces, and 108 unit tests validating all invariants.

---

## Design Philosophy

### The Core Insight

Time-off management, wellness points, learning budgets, and recognition systems all share the same fundamental problem: **tracking quantities that accumulate and deplete over bounded time periods**.

Rather than building separate systems for each use case, we designed a single, mathematically rigorous engine that handles the core mechanics, with domain-specific layers providing the human-friendly abstractions.

```mermaid
graph TB
    subgraph "Domain Layer"
        TO[Time-Off]
        RW[Rewards]
        LIC[Licenses]
        CUSTOM[Your Domain]
    end
    
    subgraph "Generic Engine"
        BAL[Balance Calculator]
        PRJ[Projection Engine]
        REC[Reconciliation]
        LED[Immutable Ledger]
    end
    
    subgraph "Storage"
        MEM[Memory]
        SQL[SQLite]
        PG[PostgreSQL]
    end
    
    TO --> BAL
    RW --> BAL
    LIC --> BAL
    CUSTOM --> BAL
    
    BAL --> LED
    PRJ --> LED
    REC --> LED
    
    LED --> MEM
    LED --> SQL
    LED --> PG
```

---

## Core Concepts

### 1. The Ledger: Source of Truth

The ledger is an **append-only, immutable log** of all changes to any resource balance. Think of it like a bank statement—you never edit past entries, you only add new ones.

**Why append-only?**
- Complete audit trail
- No data loss from bugs or mistakes
- Time-travel queries ("what was the balance on March 15th?")
- Idempotent operations (replaying transactions produces the same result)

```mermaid
sequenceDiagram
    participant User
    participant Engine
    participant Ledger
    
    User->>Engine: Request 5 days PTO
    Engine->>Ledger: Read transactions
    Ledger-->>Engine: Transaction history
    Engine->>Engine: Calculate projected balance
    Engine->>Ledger: Append consumption transaction
    Engine-->>User: Request approved
    
    Note over Ledger: Transactions are never<br/>modified or deleted
```

### 2. Periods: The Balance Boundary

A **Period** defines the time window for balance calculations. Most organizations use calendar years, but the system supports any boundary:

| Period Type | Use Case |
|-------------|----------|
| Calendar Year | Standard PTO, annual budgets |
| Fiscal Year | Companies with non-calendar fiscal years |
| Anniversary | Balance resets on hire date anniversary |
| Rolling | Rolling 12-month window |

**The key insight**: Balance only makes sense within a period. "20 days of PTO" means nothing without knowing *which year*.

```mermaid
gantt
    title Period-Based Balance Lifecycle
    dateFormat YYYY-MM-DD
    
    section 2024
    Period 2024           :2024-01-01, 365d
    Accruals accumulate   :2024-01-01, 365d
    
    section Rollover
    Reconciliation        :crit, 2024-12-31, 1d
    
    section 2025
    Period 2025           :2025-01-01, 365d
    Carryover applied     :milestone, 2025-01-01, 0d
```

### 3. ResourceType: Domain-Owned Interface

**Key Design Decision**: ResourceType is an **interface**, not a string.

```mermaid
classDiagram
    class ResourceType {
        <<interface>>
        +ResourceID() string
        +ResourceDomain() string
    }
    
    class timeoff_Resource {
        +ResourceID() string
        +ResourceDomain() string
    }
    
    class rewards_Resource {
        +ResourceID() string
        +ResourceDomain() string
    }
    
    ResourceType <|.. timeoff_Resource : implements
    ResourceType <|.. rewards_Resource : implements
    
    note for ResourceType "Generic package defines interface"
    note for timeoff_Resource "Domain owns: PTO, Sick, Parental"
    note for rewards_Resource "Domain owns: Points, Credits"
```

**Why an interface?**
- Generic engine has ZERO knowledge of domain-specific resources
- Domain packages own their type definitions
- Type safety at compile time
- Clean serialization via `ResourceID()`

### 4. Consumption Modes

Two fundamentally different approaches to "available balance":

| Mode | Description | Use Case |
|------|-------------|----------|
| **ConsumeAhead** | Full year entitlement available immediately | Salaried PTO, annual budgets |
| **ConsumeUpToAccrued** | Only earned balance available | Hourly workers, points programs |

```mermaid
graph LR
    subgraph "ConsumeAhead (January)"
        A1[20 days/year] --> A2[20 available]
    end
    
    subgraph "ConsumeUpToAccrued (January)"
        B1[20 days/year] --> B2[1.67 available<br/>20÷12 months]
    end
```

### 4a. Accruals: Computed vs Stored

**Key Design Decision**: Deterministic accruals are **computed on-the-fly**, not stored as transactions.

| Category | Storage | Example |
|----------|---------|---------|
| **Deterministic Accruals** | Computed from `AccrualSchedule` | "24 days/year" → 2 days/month |
| **Non-Deterministic Grants** | Stored as `TxGrant` | Bonus days, kudos from peers, hours-worked accruals |
| **Consumption** | Stored as `TxConsumption` | Taking a day off |
| **Cancellation** | Stored as `TxReversal` | Cancelling a day off |
| **Period-End** | Stored as `TxReconciliation` | Carryover, expiration |

**Why computed accruals?**
- No redundant data (accrual schedule + computed transactions would be duplicates)
- Retroactive policy changes automatically recalculate balances
- Simpler: only actual events (consumption, grants) are stored
- Balance calculation combines: computed accruals + stored transactions

```mermaid
flowchart LR
    subgraph stored [Stored in Ledger]
        CONS[TxConsumption]
        REV[TxReversal]
        RECON[TxReconciliation]
        GRANT[TxGrant]
    end
    
    subgraph computed [Computed On-Demand]
        ACC[AccrualSchedule<br/>GenerateAccruals]
    end
    
    ACC --> BAL[Balance<br/>Calculator]
    stored --> BAL
    BAL --> RESULT[Available Balance]
```

**When to use `TxGrant`:**
- One-time bonus days (not from a schedule)
- Peer recognition points (kudos)
- Hours-worked accruals (non-deterministic—depends on actual hours logged)
- Carryover balance transferred from old policy to new policy

### 5. Multi-Policy Distribution

Employees can have multiple policies for the same resource type. The system distributes consumption by priority:

```mermaid
flowchart TD
    REQ[Request: 10 days PTO] --> P1{Carryover<br/>5 days<br/>Priority 1}
    P1 -->|Take 5| P2{Bonus Days<br/>3 days<br/>Priority 2}
    P2 -->|Take 3| P3{Standard PTO<br/>20 days<br/>Priority 3}
    P3 -->|Take 2| DONE[Total: 10 days]
    
    style P1 fill:#22c55e
    style P2 fill:#3b82f6
    style P3 fill:#8b5cf6
```

### 6. Reconciliation (Rollover & Expiration)

At period boundaries, the system processes reconciliation rules:

| Action | Description |
|--------|-------------|
| **Carryover** | Move balance to next period (with optional cap) |
| **Expire** | Remove remaining balance |
| **Cap** | Limit maximum balance |

**Important**: Reconciliation uses **accrued balance** (`CurrentAccrued()`), not full entitlement (`Current()`). This ensures that:
- New hires only reconcile what they actually earned
- Mid-year policy changes reconcile correctly
- Only earned balance can be carried over or expired

```mermaid
stateDiagram-v2
    [*] --> CurrentPeriod
    CurrentPeriod --> Reconciliation: Period End
    Reconciliation --> CalculateAccruedBalance: Use CurrentAccrued()
    CalculateAccruedBalance --> Carryover: If rules allow
    CalculateAccruedBalance --> Expire: Remaining balance
    Carryover --> NextPeriod
    Expire --> NextPeriod
    NextPeriod --> [*]
```

**Example**: Employee hired Dec 15 with 24 days/year policy:
- **Accrued balance** at year-end: ~2 days (prorated for December)
- **Full entitlement**: 24 days (if using `Current()`)
- **Reconciliation**: Uses accrued balance (2 days), so only 2 days can carry over, not 24

---

## Domain Encapsulation

### Package Structure

```
generic/                    # Core engine - NO domain knowledge
├── types.go               # Amount, Transaction, TimePoint
├── resource.go            # ResourceType interface + registry
├── policy.go              # Policy, Reconciliation rules
├── balance.go             # Balance calculation
├── ledger.go              # Ledger interface
├── store.go               # Store interfaces
├── errors.go              # Centralized error types
└── projection.go          # Future balance validation

timeoff/                    # Time-off domain
├── types.go               # Resource type: PTO, Sick, Parental
├── policies.go            # Pre-built policy configs
├── accrual.go             # YearlyAccrual, TenureAccrual
└── ledger.go              # TimeOffLedger (day uniqueness)

rewards/                    # Rewards domain
├── types.go               # Resource type: Points, Credits
├── policies.go            # Pre-built policy configs
└── accrual.go             # MonthlyPointsAccrual, etc.
```

### Resource Type Registration

Domain packages register their types on initialization:

```mermaid
sequenceDiagram
    participant App as Application Start
    participant TO as timeoff/types.go
    participant RW as rewards/types.go
    participant REG as generic/resource.go
    
    App->>TO: import timeoff
    TO->>REG: RegisterResource(ResourcePTO)
    TO->>REG: RegisterResource(ResourceSick)
    TO->>REG: RegisterResource(ResourceParental)
    
    App->>RW: import rewards
    RW->>REG: RegisterResource(ResourceWellnessPoints)
    RW->>REG: RegisterResource(ResourceLearningCredits)
    
    Note over REG: Registry populated<br/>before any queries
```

---

## Correctness Guarantees

### Invariants

| Invariant | Enforcement |
|-----------|-------------|
| **Append-only ledger** | No Update/Delete methods exist |
| **Idempotency** | Duplicate idempotency keys rejected |
| **Atomicity** | Batch operations all-or-nothing |
| **Day uniqueness (time-off)** | TimeOffLedger + DB constraint |
| **Non-negative (when configured)** | Projection engine validates |

### Transaction Types

```mermaid
graph LR
    subgraph "Balance Increases"
        ACC[Accrual<br/>+20 days]
        REC[Reconciliation<br/>+5 carryover]
    end
    
    subgraph "Balance Decreases"
        CON[Consumption<br/>-3 days]
        EXP[Expiration<br/>-2 days]
    end
    
    subgraph "Corrections"
        REV[Reversal<br/>Undo previous]
    end
```

---

## Demo Scenarios

The system includes 6 demo scenarios that exercise all features:

| Scenario | Features Demonstrated |
|----------|----------------------|
| **new-employee** | Single policy, basic accrual, year-end rollover |
| **multi-policy** | Priority distribution, 4 policies |
| **year-end-rollover** | Carryover with cap, expiration |
| **policy-change** | Mid-year policy upgrade (reconciliation = rollover) |
| **hourly-worker** | ConsumeUpToAccrued mode |
| **rewards-benefits** | Points, credits, different units (TxGrant) |

Each scenario has corresponding unit tests that validate the expected behavior.

---

## See Also

- `IMPLEMENTATION.md` - Technical implementation details
- `TESTING.md` - Test strategy and coverage
- `ENGINEERING.md` - Internal engineering guide
- `PRESENTATION.md` - Stakeholder presentation
