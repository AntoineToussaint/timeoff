# Code Review Guide

> **Purpose:** This guide helps you understand the codebase structure, test the frontend, and explore the implementation.

---

## 🧪 Step 1: Test the Frontend

### Start the Application

```bash
# Make sure you're in the project root
cd /Users/antoine/Development/warp

# Start both backend and frontend
make dev

# Or if already running, restart:
# Press Ctrl+C to stop, then run make dev again
```

### Test Each Scenario

1. **Open** http://localhost:5173
2. **Navigate** to "Scenarios" in the sidebar
3. **Load each scenario** and verify:
   - ✅ **new-employee**: Shows Alice Johnson with PTO tab
   - ✅ **multi-policy**: Shows David Wilson with PTO and Sick tabs
   - ✅ **year-end-rollover**: Shows Carol Davis with rollover transactions
   - ✅ **hourly-worker**: Shows Eve Martinez with consume-up-to-accrued
   - ✅ **rewards-benefits**: Redirects to Rewards dashboard, shows Alex Rivera with multiple reward types

### What to Check

- **Time Off Dashboard** (`/`):
  - Employee list loads
  - Tabs show only for resource types the employee has
  - Balance displays correctly
  - Transaction history shows balances at each date
  - Can request time off

- **Rewards Dashboard** (`/rewards`):
  - Shows only reward resource types
  - Wellness Points, Learning Credits, etc. display correctly
  - No time-off tabs appear

- **Scenarios** (`/scenarios`):
  - All 5 scenarios listed (4 time-off + 1 rewards)
  - Loading a scenario redirects correctly
  - "Reset All" clears data

---

## 📚 Step 2: Understand the Documentation Structure

### Start Here (in order):

1. **README.md** - High-level overview, quick start
2. **docs/GETTING_STARTED.md** - Setup and first steps
3. **docs/DESIGN.md** - Core philosophy and concepts
4. **docs/ENGINEERING.md** - Architecture and code structure
5. **docs/TESTING.md** - Test philosophy and coverage

### Key Documentation Files

| File | Purpose | When to Read |
|------|---------|--------------|
| `DESIGN.md` | Core concepts, why things work this way | First - understand the "why" |
| `ENGINEERING.md` | Code structure, dependencies, patterns | Second - understand the "how" |
| `TESTING.md` | Test coverage, scenario-to-test mapping | Third - understand test strategy |
| `IMPLEMENTATION.md` | Technical details, TODOs | When diving deep |
| `PERFORMANCE_QA.md` | Performance analysis | When optimizing |

---

## 🔍 Step 3: Understand Key Implementation Details

### Key Files to Review

#### Backend Implementation

1. **`api/scenarios.go`** (1408 lines)
   - **What it does**: 
     - Defines 4 time-off scenarios + 1 rewards scenario
     - Creates employees, policies, assignments, and transactions
     - Uses reconciliation with `CurrentAccrued()` for accurate rollover
     - Accruals computed on-demand from `AccrualSchedule`, not stored as transactions
   - **Key functions**:
     - `loadNewEmployeeScenario()` - Simple PTO with rollover
     - `loadMultiPolicyScenario()` - Multiple policies with priority
     - `loadYearEndRolloverScenario()` - Rollover demonstration
     - `loadMidYearPolicyChangeScenario()` - Policy change (same as rollover)
     - `loadHourlyWorkerScenario()` - Consume-up-to-accrued mode
     - `loadRewardsBenefitsScenario()` - Rewards domain (uses TxGrant for non-deterministic accruals)

2. **`generic/policy.go`** (Reconciliation Engine)
   - **What it does**: 
     - `carryover()` and `expire()` use `CurrentAccrued()` for reconciliation
   - **Why**: Reconciliation reconciles what was actually earned, not full entitlement
   - **Impact**: Ensures correct expiration for new hires and mid-year changes

3. **`api/scenarios_test.go`**
   - **What**: Unit tests for all scenarios
   - **Purpose**: Scenarios are testable and verified
   - **Tests**: Each scenario has a test verifying employees, policies, assignments, transactions

#### Frontend Implementation

1. **`web/src/App.tsx`**
   - **Routes**: `/` (Time Off), `/rewards` (Rewards), `/scenarios` (Scenarios)
   - **Navigation**: Sidebar with links to each section

2. **`web/src/components/EmployeeDashboard.tsx`**
   - **Features**: 
     - Tabs filtered based on actual transactions
     - Shows message if no policies found
     - Handles rewards scenario gracefully
   - **Key logic**: `availableResourceTypes` filters based on `transactions` data

3. **`web/src/components/RewardsDashboard.tsx`**
   - **Purpose**: Separate dashboard for rewards domain
   - **Why**: Rewards have different resource types than time-off

4. **`web/src/components/ScenarioLoader.tsx`**
   - **Features**: Auto-redirects based on scenario category
   - **Logic**: Rewards scenarios → `/rewards`, Time-off → `/`

---

## 🗺️ Step 4: Code Exploration Path

### Path 1: Understanding Core Engine (Start Here)

```
1. generic/types.go          # Core types: Amount, Transaction, TimePoint
2. generic/policy.go         # Policy definition, ReconciliationEngine
3. generic/balance.go       # Balance calculation logic
4. generic/ledger.go         # Append-only ledger interface
5. generic/assignment.go    # Multi-policy distribution
```

**Key concepts to understand:**
- `Balance` is period-based, not point-in-time
- `Current()` vs `CurrentAccrued()` - full entitlement vs actual accrued
- Reconciliation uses `CurrentAccrued()` for rollover/expire

### Path 2: Understanding Domains

```
1. timeoff/types.go         # Resource types: PTO, Sick, Parental
2. timeoff/policies.go      # Pre-built policy configs
3. timeoff/accrual.go      # Accrual schedules
4. timeoff/ledger.go       # Day-uniqueness enforcement

5. rewards/types.go        # Resource types: Points, Credits
6. rewards/policies.go     # Pre-built policy configs
7. rewards/accrual.go     # Accrual schedules
```

**Key concepts:**
- Domains implement `ResourceType` interface
- Domains register types via `init()` functions
- Generic engine has zero knowledge of domain types

### Path 3: Understanding API Layer

```
1. api/handlers.go         # HTTP handlers
2. api/dto.go              # Request/response types
3. api/scenarios.go        # Demo scenario loaders
4. api/scenarios_test.go  # Scenario tests
```

**Key concepts:**
- REST API for all operations
- Scenarios create realistic test data
- All scenarios have corresponding unit tests

### Path 4: Understanding Frontend

```
1. web/src/App.tsx                    # Routing, navigation
2. web/src/components/EmployeeDashboard.tsx  # Time-off UI
3. web/src/components/RewardsDashboard.tsx     # Rewards UI
4. web/src/components/ScenarioLoader.tsx       # Scenario loading
5. web/src/api/client.ts              # API client
```

**Key concepts:**
- React with TypeScript
- TanStack Query for data fetching
- Dynamic tab filtering based on data
- Auto-redirect based on scenario category

---

## 🔎 Step 5: Review Implementation Details

### Implementation Detail 1: Reconciliation Logic

**Location**: `generic/policy.go`

**How it works**:
- Reconciliation uses `CurrentAccrued()` to reconcile only earned balance
- Ensures new hires only reconcile what they earned (e.g., 2 days for December hire, not 24)
- Handles mid-year policy changes correctly

**Code to review**:
```go
// In carryover() and expire() functions
remaining := input.CurrentBalance.CurrentAccrued()  // Uses actual accrued
```

### Implementation Detail 2: Scenario Structure

**Location**: `api/scenarios.go`

**How scenarios work**:
- Each scenario creates employees, policies, assignments, and transactions
- Accruals are computed on-demand from `AccrualSchedule`, NOT stored as transactions
- Only grants (TxGrant), consumption (TxConsumption), and reconciliation (TxReconciliation) are stored
- Reconciliation transactions use `CurrentAccrued()` for accuracy
- Mid-year policy change uses the SAME `ReconciliationEngine` as year-end rollover

**Scenarios available**:
- `new-employee`: Single PTO policy, basic accrual with rollover
- `multi-policy`: Multiple policies with priority-based consumption
- `year-end-rollover`: Rollover demonstration with carryover cap
- `policy-change`: Mid-year policy upgrade (demonstrates reconciliation = rollover)
- `hourly-worker`: Consume-up-to-accrued mode
- `rewards-benefits`: Rewards domain with non-deterministic grants

### Implementation Detail 3: Frontend Tab Filtering

**Location**: `web/src/components/EmployeeDashboard.tsx`

**How it works**:
- Frontend fetches transactions for an employee
- Filters resource types based on actual transaction data
- Only shows tabs for resource types that have transactions
- Prevents showing empty tabs

**Code to review**:
```typescript
const availableResourceTypes = useMemo(() => {
  // Filter based on transactions
  const types = new Set<string>();
  transactions.forEach(tx => {
    if (tx.resource_type) types.add(tx.resource_type);
  });
  return Array.from(types);
}, [transactions]);
```

---

## 🧪 Step 6: Run Tests

### Run All Tests

```bash
make test
```

### Run Specific Test Suites

```bash
# Generic engine tests
go test ./generic/... -v

# Time-off domain tests
go test ./timeoff/... -v

# Rewards domain tests
go test ./rewards/... -v

# API/scenario tests
go test ./api/... -v
```

### Run Scenario Tests

```bash
go test ./api -v -run TestScenario
```

---

## 📖 Step 7: Key Questions to Answer

After reviewing the code, you should be able to answer:

1. **Architecture**:
   - How does the generic engine stay domain-agnostic?
   - How do domains register their resource types?
   - What's the difference between `Current()` and `CurrentAccrued()`?

2. **Reconciliation**:
   - Why does reconciliation use `CurrentAccrued()`?
   - How does rollover work for new hires?
   - What happens to unused balance at year-end?

3. **Multi-Policy**:
   - How does priority-based consumption work?
   - How are multiple policies distributed?
   - What happens when a policy is exhausted?

4. **Scenarios**:
   - How do scenarios create test data?
   - Why do scenarios generate accruals up to year-end?
   - How are idempotency keys structured?

5. **Frontend**:
   - How does tab filtering work?
   - Why are there separate dashboards for time-off and rewards?
   - How does scenario loading redirect work?

---

## 🐛 Step 8: Debugging Tips

### Common Issues

1. **"No time-off policies found"**
   - Check if scenario loaded correctly
   - Verify transactions exist for the employee
   - Check browser console for API errors

2. **Balance incorrect**
   - Verify reconciliation uses `CurrentAccrued()`
   - Check transaction history for missing accruals
   - Verify period boundaries are correct

3. **Duplicate idempotency key**
   - Check idempotency key format includes policy ID and index
   - Verify year is dynamic, not hardcoded
   - Check for duplicate scenario loads

4. **Frontend not updating**
   - Check browser console for errors
   - Verify API is running
   - Check React Query cache

### Debugging Commands

```bash
# Check backend logs
# (logs appear in terminal where you ran make dev)

# Check frontend logs
# (open browser DevTools console)

# Test API directly
curl http://localhost:8080/api/scenarios/list
curl -X POST http://localhost:8080/api/scenarios/load \
  -H "Content-Type: application/json" \
  -d '{"scenario": "new-employee"}'
```

---

## ✅ Review Checklist

- [ ] **Frontend works** - All scenarios load, tabs display correctly
- [ ] **Documentation read** - Understand design philosophy and architecture
- [ ] **Code explored** - Reviewed key files and functions
- [ ] **Tests pass** - All tests run successfully
- [ ] **Questions answered** - Can explain how key features work
- [ ] **Ready to contribute** - Understand codebase structure and patterns

---

**Happy reviewing!** 🎉
