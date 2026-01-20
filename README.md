# TimeOff - Generic Timed Resource Management Engine

> **A single, mathematically rigorous engine for tracking any time-bounded resource—PTO, sick leave, wellness points, learning budgets—using an append-only ledger, period-based balance calculation, and policy-driven reconciliation.**

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![React](https://img.shields.io/badge/React-18+-61DAFB?style=flat&logo=react)](https://react.dev/)
[![Tests](https://img.shields.io/badge/Tests-135+-success?style=flat)](./docs/TESTING.md)
[![License](https://img.shields.io/badge/License-MIT-blue?style=flat)](LICENSE)

---

## Screenshots

### Employee Dashboard
![Employee Dashboard](screenshots/dashboard.png)
*Single policy view with balance breakdown, consumption order, and request button*

### Multi-Policy Balance
![Multi-Policy Balance](screenshots/multi-policy.png)
*Multiple PTO policies with priority-based consumption order (Carryover → Bonus → Standard)*

### Date Range Picker
![Date Range Picker](screenshots/date-picker.png)
*Calendar-based date selection with workday calculation and quick-select options*

### Request Confirmation
![Request Confirmation](screenshots/request-confirmation.png)
*Shows exactly how days will be consumed from each policy in priority order*

### Time-Off Calendar
![Calendar View](screenshots/calendar.png)
*Visual calendar showing scheduled time off with transaction history*

### Rewards Dashboard
![Rewards Dashboard](screenshots/rewards.png)
*Generic resource support: wellness points, learning credits, recognition points*

---

## What Problem Does This Solve?

Every company builds separate systems for:
- **Time-off management** (PTO, sick leave, parental)
- **Rewards points** (wellness, recognition, peer bonuses)
- **Learning budgets** (training credits, certifications)
- **Expense credits** (meal allowances, transportation)

**Each system duplicates the same logic:**
- Balance tracking
- Accrual calculations
- Period management (year-end rollovers)
- Audit trails
- Multi-policy support

### The Solution: One Engine, Multiple Domains

```mermaid
graph TB
    subgraph "Domain Layer"
        TO[Time-Off - PTO, Sick, Parental]
        RW[Rewards - Points, Credits]
        LB[Learning - Budgets]
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
    LB --> BAL
    CUSTOM --> BAL
    
    BAL --> LED
    PRJ --> LED
    REC --> LED
    
    LED --> MEM
    LED --> SQL
    LED --> PG
    
```

**Key Insight:** All timed resources share the same fundamental operations: accrue, consume, reconcile. The differences are in constraints and semantics, not math.

---

## ✨ Key Features

| Feature | Description |
|---------|-------------|
| 🎯 **Generic Engine** | Works for PTO, sick leave, rewards points, learning budgets, or any timed resource |
| 📅 **Period-Based Balance** | Tracks resources within fiscal/calendar year boundaries |
| 🔄 **Multi-Policy Support** | Employees can have multiple policies with priority-based consumption |
| 🔁 **Reconciliation Engine** | Handles year-end rollovers, carryovers, and expirations automatically |
| 📜 **Append-Only Ledger** | Immutable transaction log for complete audit compliance |
| ⚡ **Consumption Modes** | "Consume Ahead" (use future accruals) vs "Consume Up To Accrued" (only what's earned) |
| 🧪 **135+ Tests** | Comprehensive test coverage including all demo scenarios |

---

## 🏗️ Architecture Overview

### System Layers

```mermaid
graph TB
    subgraph Frontend["🌐 Frontend Layer"]
        DASH[Dashboard]
        SCEN[Scenarios]
        REQ[Requests]
    end
    
    subgraph API["🔌 API Layer"]
        HAND[Handlers]
        SCENAPI[Scenarios]
        DTO[DTOs]
    end
    
    subgraph Domain["📦 Domain Layer"]
        TO_DOM[timeoff/ - PTO, Sick, Parental]
        RW_DOM[rewards/ - Wellness, Learning]
    end
    
    subgraph Engine["⚙️ Generic Engine"]
        BAL_ENG[Balance - Calculator]
        REC_ENG[Reconciliation - Engine]
        LED_ENG[Ledger - Immutable]
        PROJ_ENG[Projection - Engine]
        POL_ENG[Policy - Rules]
        ASS_ENG[Assignment - Multi-Policy]
    end
    
    subgraph Storage["💾 Storage Layer"]
        SQLITE[SQLite]
        MEM_STORE[Memory]
    end
    
    DASH --> HAND
    SCEN --> SCENAPI
    REQ --> HAND
    
    HAND --> TO_DOM
    HAND --> RW_DOM
    
    TO_DOM --> BAL_ENG
    RW_DOM --> BAL_ENG
    
    BAL_ENG --> LED_ENG
    REC_ENG --> LED_ENG
    PROJ_ENG --> LED_ENG
    
    LED_ENG --> SQLITE
    LED_ENG --> MEM_STORE
    
    POL_ENG --> BAL_ENG
    ASS_ENG --> BAL_ENG
    
```

### Data Flow: Request to Balance

```mermaid
sequenceDiagram
    participant User
    participant Frontend
    participant API
    participant Domain as Domain Layer
    participant Engine as Generic Engine
    participant Store as Storage

    User->>Frontend: Request 5 days PTO
    Frontend->>API: POST /api/requests
    API->>Domain: Validate request
    Domain->>Engine: Calculate balance
    Engine->>Store: Query transactions
    Store-->>Engine: Transaction history
    Engine->>Engine: Calculate balance - (period-based)
    Engine-->>Domain: Balance: 15 days available
    Domain->>Domain: Check day uniqueness - (time-off only)
    Domain->>Engine: Create consumption transaction
    Engine->>Engine: Validate projection - (future balance)
    Engine->>Store: Append transaction - (idempotent)
    Store-->>Engine: Success
    Engine-->>Domain: Transaction created
    Domain-->>API: Request approved
    API-->>Frontend: 200 OK
    Frontend-->>User: Request confirmed
```

### Package Dependency Graph

```mermaid
graph LR
    subgraph External["External"]
        HTTP[HTTP Client]
    end
    
    subgraph App["Application"]
        CMD[cmd/server]
        API_PKG[api/]
    end
    
    subgraph Domain["Domain Packages"]
        TO_PKG[timeoff/]
        RW_PKG[rewards/]
        FACTORY[factory/]
    end
    
    subgraph Core["Core Engine"]
        GENERIC[generic/]
    end
    
    subgraph Persistence["Persistence"]
        SQLITE_PKG[store/sqlite/]
        MEM_PKG[store/memory/]
    end
    
    HTTP --> CMD
    CMD --> API_PKG
    API_PKG --> TO_PKG
    API_PKG --> RW_PKG
    API_PKG --> FACTORY
    TO_PKG --> GENERIC
    RW_PKG --> GENERIC
    FACTORY --> GENERIC
    GENERIC --> SQLITE_PKG
    GENERIC --> MEM_PKG
    
```

---

## 🧩 Core Concepts

### 1. The Ledger: Source of Truth

The ledger is an **append-only, immutable log** of all changes. Think of it like a bank statement—you never edit past entries, you only add new ones.

```mermaid
graph LR
    subgraph Ledger["📜 Transaction Ledger"]
        TX1[2025-01-01 - Accrual +20.00 - Annual grant]
        TX2[2025-02-15 - Consumption -5.00 - Vacation]
        TX3[2025-03-01 - Accrual +1.67 - Monthly]
        TX4[2025-12-31 - Reconciliation +5.00 - Carryover]
        TX5[2025-12-31 - Reconciliation -10.00 - Expired]
    end
    
    TX1 --> TX2
    TX2 --> TX3
    TX3 --> TX4
    TX4 --> TX5
    
```

**Why append-only?**
- ✅ Complete audit trail
- ✅ No data loss from bugs or mistakes
- ✅ Time-travel queries ("what was the balance on March 15th?")
- ✅ Idempotent operations (replaying transactions produces the same result)

### 2. Periods: The Balance Boundary

A **Period** defines the time window for balance calculations. Balance only makes sense within a period.

```mermaid
gantt
    title Period-Based Balance Lifecycle (2025 Calendar Year)
    dateFormat YYYY-MM-DD
    axisFormat %b %d
    
    section Accruals
    Annual Grant (20 days)     :2025-01-01, 1d
    Monthly Accrual (1.67)     :2025-02-01, 1d
    Monthly Accrual (1.67)     :2025-03-01, 1d
    Monthly Accrual (1.67)     :2025-04-01, 1d
    Monthly Accrual (1.67)     :2025-05-01, 1d
    Monthly Accrual (1.67)     :2025-06-01, 1d
    Monthly Accrual (1.67)     :2025-07-01, 1d
    Monthly Accrual (1.67)     :2025-08-01, 1d
    Monthly Accrual (1.67)     :2025-09-01, 1d
    Monthly Accrual (1.67)     :2025-10-01, 1d
    Monthly Accrual (1.67)     :2025-11-01, 1d
    Monthly Accrual (1.67)     :2025-12-01, 1d
    
    section Consumption
    Vacation (5 days)           :2025-02-15, 5d
    
    section Reconciliation
    Year-End Rollover          :crit, 2025-12-31, 1d
    Carryover (5 days)         :milestone, 2026-01-01, 0d
    Expired (10 days)         :2025-12-31, 1d
```

### 3. Balance Calculation: Accrued vs Entitlement

The system tracks two balance concepts:

```mermaid
graph TB
    subgraph Balance["Balance Calculation"]
        ENT[TotalEntitlement - Full period entitlement - 24 days/year]
        ACC[AccruedToDate - What's actually earned - 2 days for Dec hire]
        CONS[TotalConsumed - Days used]
        ADJ[Adjustments - Manual corrections]
    end
    
    subgraph Methods["Balance Methods"]
        CURRENT[Current - ENT - CONS + ADJ - Full entitlement]
        ACCRUED[CurrentAccrued - ACC - CONS + ADJ - Actual earned]
    end
    
    subgraph Usage["Usage"]
        CA[ConsumeAhead - Uses Current - Can use full 24 days]
        CUTA[ConsumeUpToAccrued - Uses CurrentAccrued - Only 2 days available]
        REC[Reconciliation - Uses CurrentAccrued - Only reconcile earned]
    end
    
    ENT --> CURRENT
    ACC --> ACCRUED
    CONS --> CURRENT
    CONS --> ACCRUED
    ADJ --> CURRENT
    ADJ --> ACCRUED
    
    CURRENT --> CA
    ACCRUED --> CUTA
    ACCRUED --> REC
    
```

**Example**: Employee hired Dec 15 with 24 days/year policy:
- **AccruedToDate** (Dec 31): ~2 days (prorated for December)
- **TotalEntitlement**: 24 days (full year)
- **Available** (ConsumeAhead): 24 days (can use full year)
- **Available** (ConsumeUpToAccrued): 2 days (only what's earned)
- **Reconciliation**: Uses 2 days (only earned balance can carry over)

### 4. Consumption Modes

Two fundamentally different approaches:

```mermaid
graph LR
    subgraph CA["ConsumeAhead Mode - Salaried PTO"]
        CA_POL[Policy: 20 days/year]
        CA_AVAIL[Available Jan 1: - 20 days - Full entitlement]
        CA_USE[Can use all 20 days - immediately]
        
        CA_POL --> CA_AVAIL
        CA_AVAIL --> CA_USE
    end
    
    subgraph CUTA["ConsumeUpToAccrued Mode - Hourly Worker"]
        CUTA_POL[Policy: 20 days/year]
        CUTA_AVAIL[Available Jan 1: - 1.67 days - 20 ÷ 12 months]
        CUTA_USE[Can only use - what's earned]
        
        CUTA_POL --> CUTA_AVAIL
        CUTA_AVAIL --> CUTA_USE
    end
    
```

### 5. Multi-Policy Distribution

Employees can have multiple policies for the same resource type. Consumption is distributed by priority:

```mermaid
flowchart TD
    REQ[Request: 10 days PTO] --> CHECK{Check Policies - by Priority}
    
    CHECK --> P1[Priority 1: Carryover - 5 days available]
    P1 -->|Take 5| REMAIN1[Remaining: 5 days]
    
    REMAIN1 --> P2[Priority 2: Bonus Days - 3 days available]
    P2 -->|Take 3| REMAIN2[Remaining: 2 days]
    
    REMAIN2 --> P3[Priority 3: Standard PTO - 20 days available]
    P3 -->|Take 2| DONE[Total: 10 days - consumed]
    
    DONE --> TX[Create Transaction - with distribution]
    
```

### 6. Reconciliation (Rollover & Expiration)

At period boundaries, the system processes reconciliation rules using **accrued balance** (what was actually earned), not full entitlement.

```mermaid
stateDiagram-v2
    [*] --> PeriodActive: Period Start
    
    PeriodActive --> Accruing: Time passes
    Accruing --> Consuming: Employee uses time
    Consuming --> Accruing: More accruals
    
    Accruing --> PeriodEnd: Dec 31
    Consuming --> PeriodEnd: Dec 31
    
    PeriodEnd --> CalculateAccrued: Get CurrentAccrued()
    CalculateAccrued --> CheckRules: Apply policy rules
    
    CheckRules --> Carryover: If carryover allowed
    CheckRules --> Expire: If use-it-or-lose-it
    CheckRules --> Cap: If carryover capped
    
    Carryover --> CreateCarryoverTx: Create +X transaction
    Expire --> CreateExpireTx: Create -X transaction
    Cap --> CreateCapTx: Create -X transaction
    
    CreateCarryoverTx --> NextPeriod
    CreateExpireTx --> NextPeriod
    CreateCapTx --> NextPeriod
    
    NextPeriod --> [*]
    
    note right of CalculateAccrued
        Uses CurrentAccrued()
        not Current()
        Only reconcile earned balance
    end note
```

**Example Flow:**

```mermaid
sequenceDiagram
    participant Policy as Policy Rules
    participant Engine as Reconciliation Engine
    participant Balance as Current Balance
    participant Ledger as Transaction Ledger
    
    Note over Policy,Ledger: Year-End Reconciliation (Dec 31, 2025)
    
    Policy->>Engine: Trigger reconciliation
    Engine->>Balance: Get CurrentAccrued()
    Balance-->>Engine: 2 days (prorated for Dec hire)
    
    Engine->>Engine: Apply carryover rule - maxCarryover = 5 days
    Engine->>Engine: carryover = min(2, 5) = 2 days
    
    Engine->>Engine: Apply expire rule - remaining = 2 - 2 = 0 days
    
    Engine->>Ledger: Create TxReconciliation - +2 days (carryover)
    Engine->>Ledger: No expire transaction - (nothing to expire)
    
    Ledger-->>Engine: Transactions created
    Engine-->>Policy: Reconciliation complete - 2 days carried over
```

**Why CurrentAccrued()?**
- ✅ New hires only reconcile what they earned (e.g., 2 days for December hire, not 24)
- ✅ Mid-year policy changes reconcile correctly
- ✅ Only earned balance can be carried over or expired

### 7. Transaction Lifecycle

```mermaid
stateDiagram-v2
    [*] --> Created: Transaction Created
    
    Created --> Validated: Validate
    Validated --> CheckIdempotency: Check Idempotency Key
    
    CheckIdempotency --> Duplicate: Key exists
    CheckIdempotency --> Unique: Key unique
    
    Duplicate --> [*]: Reject (idempotent)
    
    Unique --> ValidateProjection: Validate Future Balance
    ValidateProjection --> ProjectionValid: Balance OK
    ValidateProjection --> ProjectionInvalid: Would go negative
    
    ProjectionInvalid --> [*]: Reject
    
    ProjectionValid --> AppendLedger: Append to Ledger
    AppendLedger --> Committed: Committed
    
    Committed --> [*]
    
    note right of CheckIdempotency
        Idempotency ensures
        replay safety
    end note
    
    note right of ValidateProjection
        Prevents negative
        balances (if configured)
    end note
```

### 8. Policy Structure

```mermaid
classDiagram
    class Policy {
        +ID PolicyID
        +Name string
        +ResourceType ResourceType
        +Period Period
        +AccrualSchedule AccrualSchedule
        +ConsumptionMode ConsumptionMode
        +ReconciliationRules []ReconciliationRule
        +MaxBalance Amount
        +AllowNegative bool
    }
    
    class AccrualSchedule {
        <<interface>>
        +GenerateAccruals(from, to) []AccrualEvent
        +IsDeterministic() bool
    }
    
    class ReconciliationRule {
        +Trigger ReconciliationTrigger
        +Actions []ReconciliationAction
    }
    
    class ReconciliationAction {
        +Type ActionType
        +Config ActionConfig
    }
    
    class Period {
        +Type PeriodType
        +Start TimePoint
        +End TimePoint
    }
    
    Policy --> AccrualSchedule
    Policy --> Period
    Policy --> ReconciliationRule
    ReconciliationRule --> ReconciliationAction
    
    note for Policy "Defines rules for resource - accrual, consumption, - and reconciliation"
    note for AccrualSchedule "Interface for calculating - accrual events"
    note for ReconciliationRule "What happens at - period boundaries"
```

---

## 🚀 Quick Start

### Prerequisites

- Go 1.21+
- Node.js 18+ (for frontend)
- SQLite (included, no setup needed)

### Installation

```bash
# Clone the repository
git clone <repository-url>
cd warp

# Install Go dependencies
go mod download

# Install frontend dependencies
cd web && npm install && cd ..
```

### Running

```bash
# Run all tests
make test

# Start development servers (backend + frontend with hot-reload)
make dev

# Open http://localhost:5173 in your browser
```

### Demo Scenarios

The system includes 6 demo scenarios that showcase all features:

```mermaid
graph LR
    subgraph Scenarios["📊 Demo Scenarios"]
        S1[new-employee - Single policy - ConsumeAhead]
        S2[multi-policy - Priority distribution - 3 PTO + sick]
        S3[year-end-rollover - Carryover with cap - Reconciliation]
        S4[policy-change - Mid-year upgrade - Reconciliation]
        S5[hourly-worker - ConsumeUpToAccrued - Only earned]
        S6[rewards-benefits - Points system - Multiple types]
    end
    
    S1 --> FEAT1[Basic accrual]
    S2 --> FEAT2[Multi-policy]
    S3 --> FEAT3[Rollover]
    S4 --> FEAT4[Policy change]
    S5 --> FEAT5[Accrued mode]
    S6 --> FEAT6[Rewards]
    
```

Load scenarios via the UI or API:

```bash
# Via API
curl -X POST http://localhost:8080/api/scenarios/load \
  -H "Content-Type: application/json" \
  -d '{"scenario": "new-employee"}'
```

---

## 📊 Example: How It Works

### Scenario: Employee with Multiple Policies

```mermaid
sequenceDiagram
    participant User
    participant System
    participant Policies as Policy Engine
    participant Balance as Balance Calculator
    participant Ledger as Transaction Ledger
    
    User->>System: Request 8 days PTO
    
    System->>Balance: Calculate current balance
    Balance->>Ledger: Query transactions
    Ledger-->>Balance: Transaction history
    Balance-->>System: Balance: 30 days available
    
    System->>Policies: Get policies by priority
    Policies-->>System: 1. Carryover (5 days) - 2. Standard (20 days) - 3. Sick (10 days)
    
    System->>System: Distribute consumption - Priority 1: Take 5 - Priority 2: Take 3
    
    System->>Ledger: Create consumption transaction - Distribution: [5, 3]
    Ledger-->>System: Transaction created
    
    System-->>User: Request approved - 8 days consumed
```

---

## 🧪 Testing

```bash
# Run all tests
make test

# Run with race detector
make test-race

# Run with coverage
make test-cover

# Run specific package tests
go test ./generic/... -v
go test ./timeoff/... -v
go test ./rewards/... -v
go test ./api/... -v
```

**135+ tests** covering:

```mermaid
graph TB
    subgraph Tests["🧪 Test Coverage"]
        GEN[Generic Engine - 24 spec tests - 35 engine tests]
        TO[Time-Off - Multi-policy - Rollover - Day uniqueness]
        RW[Rewards - Wellness points - Learning credits - Recognition]
        API[API Scenarios - 5 scenario tests - Integration tests]
    end
    
    GEN --> COVERAGE[135+ Tests - Comprehensive Coverage]
    TO --> COVERAGE
    RW --> COVERAGE
    API --> COVERAGE
    
```

---

## 📚 Documentation

```mermaid
graph LR
    subgraph Docs["📚 Documentation"]
        DESIGN[DESIGN.md - Philosophy & Concepts]
        ENG[ENGINEERING.md - Architecture & Flow]
        START[GETTING_STARTED.md - Setup & Running]
        IMPL[IMPLEMENTATION.md - Technical Details]
        TEST[TESTING.md - Test Coverage]
        REVIEW[CODE_REVIEW_GUIDE.md - How to Review]
    end
    
    START --> DESIGN
    DESIGN --> ENG
    ENG --> IMPL
    IMPL --> TEST
    TEST --> REVIEW
    
```

| Document | Description |
|----------|-------------|
| [**DESIGN.md**](docs/DESIGN.md) | Philosophy, core concepts, and design decisions |
| [**ENGINEERING.md**](docs/ENGINEERING.md) | Architecture, data flow, and implementation details |
| [**GETTING_STARTED.md**](docs/GETTING_STARTED.md) | Setup, running, and first steps |
| [**IMPLEMENTATION.md**](docs/IMPLEMENTATION.md) | Technical details, workflows, and database schema |
| [**TESTING.md**](docs/TESTING.md) | Test philosophy, coverage, and scenario mapping |
| [**CODE_REVIEW_GUIDE.md**](docs/CODE_REVIEW_GUIDE.md) | How to review the codebase |
| [**PERFORMANCE_QA.md**](docs/PERFORMANCE_QA.md) | Performance analysis and optimization |
| [**DEVOPS_SECURITY.md**](docs/DEVOPS_SECURITY.md) | Deployment and security considerations |

---

## 🏛️ Project Structure

```mermaid
graph TB
    subgraph Root["warp/"]
        subgraph Generic["generic/ - Core Engine"]
            TYPES[types.go - Amount, Transaction]
            POLICY[policy.go - Policy, Reconciliation]
            BALANCE[balance.go - Balance Calculation]
            LEDGER[ledger.go - Immutable Ledger]
        end
        
        subgraph Domain["Domain Packages"]
            TO_DOM[timeoff/ - PTO, Sick, Parental]
            RW_DOM[rewards/ - Points, Credits]
        end
        
        subgraph Infra["Infrastructure"]
            STORE[store/sqlite/ - Persistence]
            API_PKG[api/ - REST Handlers]
            WEB[web/ - React Frontend]
        end
    end
    
    TO_DOM --> Generic
    RW_DOM --> Generic
    API_PKG --> TO_DOM
    API_PKG --> RW_DOM
    Generic --> STORE
    
```

---

## 🎯 Key Design Decisions

### 1. Generic Engine, Not Domain-Specific

**Why?** Every company ends up building separate systems for PTO, rewards, learning budgets—all with duplicated logic. One engine handles the math; domains add constraints.

```mermaid
graph LR
    subgraph Old["❌ Old Approach"]
        O1[PTO System]
        O2[Rewards System]
        O3[Learning System]
        O4[Expense System]
        
        O1 -.->|Duplicated Logic| O2
        O2 -.->|Duplicated Logic| O3
        O3 -.->|Duplicated Logic| O4
    end
    
    subgraph New["✅ New Approach"]
        ENGINE[Generic Engine]
        D1[timeoff/]
        D2[rewards/]
        D3[learning/]
        D4[expense/]
        
        ENGINE --> D1
        ENGINE --> D2
        ENGINE --> D3
        ENGINE --> D4
    end
    
```

### 2. ResourceType as Interface, Not String

**Why?** Type safety at compile time. Generic engine has zero knowledge of domain-specific resources. Domains own their type definitions.

```mermaid
classDiagram
    class ResourceType {
        <<interface>>
        +ResourceID() string
        +ResourceDomain() string
    }
    
    class TimeOffResource {
        +ResourceID() string
        +ResourceDomain() string
    }
    
    class RewardsResource {
        +ResourceID() string
        +ResourceDomain() string
    }
    
    ResourceType <|.. TimeOffResource
    ResourceType <|.. RewardsResource
    
    note for ResourceType "Generic engine only knows - the interface, not - domain types"
```

### 3. Append-Only Ledger

**Why?** Complete audit trail, no data loss, time-travel queries, idempotent operations.

```mermaid
graph LR
    subgraph Ledger["Append-Only Ledger"]
        TX1[Transaction 1]
        TX2[Transaction 2]
        TX3[Transaction 3]
        TX4[Transaction 4]
    end
    
    TX1 --> TX2
    TX2 --> TX3
    TX3 --> TX4
    
    TX1 -.->|Never Modified| TX1
    TX2 -.->|Never Deleted| TX2
    
```

### 4. Period-Based Balance

**Why?** Balance only makes sense within a period. "20 days of PTO" means nothing without knowing which year.

### 5. Reconciliation Uses Accrued Balance

**Why?** New hires should only reconcile what they earned (e.g., 2 days for December hire, not 24). Ensures correctness for mid-year changes.

---

## Requirements Status

This implementation covers **100% of core requirements** from the original specification.

### Core Requirements

| Requirement | Status | Implementation |
|-------------|--------|----------------|
| Policies unlimited or accrual-based | ✅ | `Policy.IsUnlimited`, `AccrualSchedule` interface |
| Accrual per time (20 days/year) | ✅ | `YearlyAccrual` with configurable frequency |
| Accrual per hours worked | ✅ | `HoursWorkedAccrual` + `PayrollEvent` |
| Balance tracking (increase/decrease) | ✅ | Append-only ledger with transaction types |
| Employee time-off requests | ✅ | Date selection UI, calendar view, cancellation |
| Admin approval workflow | ✅ | Full UI with approve/reject (see `/admin/approvals`) |
| Multiple policies per company | ✅ | Policy + Assignment model |
| Flexible employee grouping | ✅ | Assignment-based, not group-based |

### "Consider" Questions

| Question | Status | Implementation |
|----------|--------|----------------|
| Policy accrual update | ✅ | `Policy.Version` + `EffectiveAt` |
| Negative balance allowed? | ✅ | `Constraints.AllowNegative` |
| Mid-year hire prorating | ✅ | `ProrateMethod` + accrual from hire date |

### Bonus Challenges

| Challenge | Status | Implementation |
|-----------|--------|----------------|
| Custom work hours | ✅ | `Amount` with `Unit` (days, hours, minutes) |
| Company holiday calendars | ✅ | Holiday Admin UI (see `/admin/holidays`) |
| Carryover & expiration | ✅ | `ReconciliationEngine` with configurable rules |
| Tenure-based policies | ✅ | `TenureAccrual` with `TenureTier` progression |

### Beyond Requirements

| Feature | Description |
|---------|-------------|
| Generic Resource Engine | Works for PTO, rewards, learning budgets, any timed resource |
| Two Consumption Modes | `ConsumeAhead` vs `ConsumeUpToAccrued` |
| Multi-Policy Distribution | Priority-based consumption across policies |
| Day Uniqueness | `TimeOffLedger` prevents duplicate days off |
| Per-Day Cancellation | Cancel individual days from multi-day requests |
| Automated Reconciliation | Scheduled year-end processing (see `/admin/reconciliation`) |
| Calendar View | Visual time-off calendar with click-to-cancel |

---

## 🔮 Future Enhancements

```mermaid
graph LR
    subgraph Future["🔮 Roadmap"]
        E1[Authentication - & Authorization]
        E2[Cross-Period - Requests]
        E3[Audit - Query API]
        E4[GraphQL - API]
        E5[Webhook - Support]
        E6[Balance - Snapshots]
    end
    
```

- [ ] Authentication and authorization
- [ ] Cross-period requests (spanning year boundaries)
- [ ] Audit query API
- [ ] GraphQL API
- [ ] Webhook support
- [ ] Balance snapshots for performance

---

## 📄 License

MIT

---

## 🙏 Acknowledgments

Built with a focus on:
- **Mathematical rigor**: Every calculation is tested
- **Clean architecture**: Generic engine has zero domain knowledge
- **Extensibility**: Easy to add new domains
- **Auditability**: Append-only ledger ensures complete history

---

**Ready to explore?** Start with [GETTING_STARTED.md](docs/GETTING_STARTED.md) or load a demo scenario in the UI!
