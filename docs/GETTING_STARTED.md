# Getting Started

> **Summary:** To run the system: install Go 1.21+ and Node 18+, run `go mod download` and `cd web && npm install`, then `make dev` to start both backend (port 8080) and frontend (port 5173) with hot-reload. Load demo scenarios via the UI or `curl -X POST localhost:8080/api/scenarios/load -d '{"scenario_id":"new-employee"}'`. Seven scenarios are available: new-employee, mid-year-hire, year-end-rollover, multi-policy, new-parent, rewards-benefits, hourly-worker. Key files to understand: `generic/types.go` (core types), `generic/resource.go` (ResourceType registry), `generic/policy.go` (policies), `timeoff/types.go` (domain resource types), `api/scenarios.go` (demo data). Run tests with `make test` (135+ tests).

---

## Prerequisites

```bash
# Required
go version    # Go 1.21+
node --version # Node 18+

# Optional (for hot-reload)
brew install air   # or: go install github.com/cosmtrek/air@latest
```

---

## Quick Start

### 1. Clone and Install

```bash
cd /path/to/warp

# Install Go dependencies
go mod download

# Install frontend dependencies
cd web && npm install && cd ..
```

### 2. Run Tests

```bash
# Run all tests
make test

# Expected output:
# ok   github.com/warp/resource-engine/generic
# ok   github.com/warp/resource-engine/rewards
# ok   github.com/warp/resource-engine/timeoff
```

### 3. Start Development Servers

```bash
# Option A: Both servers with hot-reload
make dev

# Option B: Backend only
make backend

# Option C: Frontend only
make frontend
```

### 4. Open the Application

```
Frontend:  http://localhost:5173
API:       http://localhost:8080/api
```

---

## Loading Demo Scenarios

The application comes with pre-built scenarios to demonstrate features.

### Via UI

1. Open http://localhost:5173
2. Click "Scenarios" in the navigation
3. Click any scenario card to load it

### Via API

```bash
# Load "New Employee" scenario
curl -X POST http://localhost:8080/api/scenarios/load \
  -H "Content-Type: application/json" \
  -d '{"scenario_id": "new-employee"}'

# Available scenarios (7 total):
# - new-employee       Basic PTO setup
# - multi-policy       Multiple PTO sources (carryover, bonus, standard)
# - year-end-rollover  Balance carryover with cap
# - hourly-worker      ConsumeUpToAccrued mode (only earned balance)
# - rewards-benefits   Wellness points, learning credits, recognition
```

---

## Understanding the Demo

### Scenario: New Employee

```
Employee: Sarah Chen
Hire Date: 2025-01-15

Policies:
├── Standard PTO: 20 days/year, monthly accrual
├── Sick Leave: 10 days/year
└── Floating Holiday: 3 days

Expected Balance (by March):
├── PTO: ~3.3 days accrued (20/12 × 2 months)
├── Sick: ~1.6 days accrued
└── Floating: 3 days (upfront)
```

### Scenario: Multi-Policy

```
Employee: Alex Rivera
Tenure: 3 years

Policies (consumption priority):
├── Priority 1: Carryover PTO (5 days from last year)
├── Priority 2: Bonus PTO (3 days for Q4 performance)
└── Priority 3: Standard PTO (20 days/year)

When requesting 10 days:
├── First: 5 days from Carryover
├── Then: 3 days from Bonus
└── Finally: 2 days from Standard
```

---

## Project Structure

```
warp/
├── generic/              # Core engine (interfaces, types, algorithms)
│   ├── types.go          # Amount, Transaction, TimePoint
│   ├── policy.go         # Policy definition and reconciliation
│   ├── balance.go        # Balance calculation
│   ├── ledger.go         # Append-only transaction log
│   ├── assignment.go     # Policy-to-entity mapping
│   ├── request.go        # Request service with multi-policy
│   └── *_test.go         # Unit tests
│
├── timeoff/              # Time-off domain implementation
│   ├── policies.go       # PTO, Sick, Parental policies
│   ├── accrual.go        # Yearly, Tenure-based accruals
│   ├── ledger.go         # Day-uniqueness enforcement
│   └── *_test.go         # Integration tests
│
├── rewards/              # Rewards domain implementation
│   ├── policies.go       # Wellness, Learning, Recognition
│   ├── types.go          # Domain-specific types
│   └── *_test.go         # Integration tests
│
├── store/sqlite/         # Database persistence
│   └── sqlite.go         # Full Store implementation
│
├── api/                  # HTTP REST API
│   ├── handlers.go       # Request handlers
│   ├── scenarios.go      # Demo scenario loaders
│   └── dto.go            # Data transfer objects
│
├── factory/              # Policy creation from JSON
│   └── policy.go         # PolicyFactory
│
├── web/                  # React frontend
│   └── src/
│       ├── App.tsx
│       └── components/
│
└── docs/                 # Documentation
```

---

## Key Files to Understand

### Start Here

1. **`generic/types.go`** - Core types: `Amount`, `Transaction`, `TimePoint`, `ResourceType` interface
2. **`generic/resource.go`** - ResourceType registry (domains register their types here)
3. **`generic/policy.go`** - What a `Policy` is and how reconciliation works
4. **`generic/balance.go`** - How balance is calculated from transactions

### Then

5. **`generic/ledger.go`** - The append-only transaction log
6. **`timeoff/types.go`** - Domain-owned resource types (PTO, Sick, Parental)
7. **`timeoff/policies.go`** - Concrete policy implementations
8. **`timeoff/ledger.go`** - Domain-specific constraints (day uniqueness)

### Finally

9. **`api/handlers.go`** - How the API works
10. **`api/scenarios.go`** - How demo data is created

---

## Running Specific Tests

```bash
# All tests
go test ./...

# Verbose output
go test ./... -v

# Specific package
go test ./generic/... -v

# Specific test
go test ./timeoff/... -v -run TestMultiPolicy

# With race detector
go test ./... -race
```

---

## Common Tasks

### Add a New Policy Type

1. Define policy in `timeoff/policies.go` or `rewards/policies.go`
2. Add accrual schedule if needed in `*/accrual.go`
3. Add factory function in `factory/policy.go`
4. Write tests
5. Add to demo scenario if desired

### Debug Balance Calculation

```go
// In code or test
txs, _ := ledger.TransactionsInRange(ctx, entityID, policyID, period.Start, period.End)
for _, tx := range txs {
    fmt.Printf("%s: %s %v\n", tx.Type, tx.EffectiveAt, tx.Delta)
}
```

### Check Day Uniqueness

```go
// Using TimeOffLedger
isOff, dayOff, _ := timeOffLedger.IsDayOff(ctx, entityID, day)
if isOff {
    fmt.Printf("Already off: %s via %s\n", dayOff.Date, dayOff.PolicyID)
}
```

---

## Troubleshooting

### "Failed to create store"

```bash
# Ensure the data directory exists
mkdir -p data

# Or use in-memory database
make dev-memory
```

### "Port already in use"

```bash
# Kill existing processes
lsof -i :8080 | grep LISTEN | awk '{print $2}' | xargs kill -9
lsof -i :5173 | grep LISTEN | awk '{print $2}' | xargs kill -9
```

### Tests failing with race conditions

```bash
# Run with race detector to identify issues
go test ./... -race -count=1
```

---

## Next Steps

1. **Read the Design Doc**: [DESIGN.md](DESIGN.md) explains the philosophy
2. **Explore Tests**: Best way to understand behavior
3. **Try the API**: Use Postman or curl with the scenarios
4. **Add a Feature**: Create a new policy type as practice
