/*
scenarios.go - Demo scenario loaders for testing and demonstrations

PURPOSE:

	Provides pre-built scenarios that populate the database with realistic
	data for testing and demos. Each scenario creates employees, policies,
	assignments, and transactions that demonstrate specific features.

AVAILABLE SCENARIOS:

	new-employee:     Single PTO policy, simple case
	mid-year-hire:    Prorated accruals for employee hired mid-year
	multi-policy:     Multiple PTO sources (carryover, bonus, standard)
	new-parent:       Maternity leave + regular PTO
	rewards-benefits: Wellness points, learning credits

HOW SCENARIOS WORK:
 1. Reset database (clear all data)
 2. Create policies via factory
 3. Create employee
 4. Assign policies with priorities
 5. Add accrual transactions
 6. Optionally add consumption transactions

USAGE VIA API:

	POST /api/scenarios/load
	{"scenario_id": "multi-policy"}

ADDING NEW SCENARIOS:
 1. Add to 'scenarios' slice with ID, name, description
 2. Create loader function: loadXxxScenario(ctx, h)
 3. Add case to LoadScenario handler

NOTE:

	Scenarios reset the database. Only use in development/demo environments.

SEE ALSO:
  - handlers.go: LoadScenario, ListScenarios handlers
  - factory/policy.go: Policy JSON definitions
*/
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/warp/resource-engine/factory"
	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/rewards"
	"github.com/warp/resource-engine/store/sqlite"
	"github.com/warp/resource-engine/timeoff"
)

// =============================================================================
// SCENARIO DEFINITIONS
// =============================================================================

var scenarios = []ScenarioDTO{
	{
		ID:          "new-employee",
		Name:        "New Employee",
		Description: "Simple consume-ahead PTO policy with rollover",
		Category:    "timeoff",
	},
	{
		ID:          "multi-policy",
		Name:        "Multi-Policy",
		Description: "Consume-ahead with multiple PTO policies + sick leave",
		Category:    "timeoff",
	},
	{
		ID:          "year-end-rollover",
		Name:        "Year-End Rollover",
		Description: "Consume-ahead PTO with year-end rollover and expiration",
		Category:    "timeoff",
	},
	{
		ID:          "policy-change",
		Name:        "Mid-Year Policy Change",
		Description: "Policy upgrade mid-year showing reconciliation (same as rollover)",
		Category:    "timeoff",
	},
	{
		ID:          "hourly-worker",
		Name:        "Hourly Worker",
		Description: "Consume-up-to-accrued policy (can only use earned time)",
		Category:    "timeoff",
	},
	{
		ID:          "rewards-benefits",
		Name:        "Rewards & Benefits",
		Description: "Points system: wellness, learning credits, recognition, flex benefits",
		Category:    "rewards",
	},
}

// ListScenarios returns available scenarios.
func (h *Handler) ListScenarios(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, scenarios)
}

// GetCurrentScenario returns the currently loaded scenario, if any.
func (h *Handler) GetCurrentScenario(w http.ResponseWriter, r *http.Request) {
	if h.currentScenario == "" {
		writeJSON(w, http.StatusOK, nil)
		return
	}

	// Find the scenario details
	for _, s := range scenarios {
		if s.ID == h.currentScenario {
			writeJSON(w, http.StatusOK, s)
			return
		}
	}

	// Scenario ID exists but not in list (shouldn't happen)
	writeJSON(w, http.StatusOK, ScenarioDTO{
		ID:          h.currentScenario,
		Name:        h.currentScenario,
		Description: "Currently loaded scenario",
	})
}

// LoadScenario loads a predefined scenario.
func (h *Handler) LoadScenario(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScenarioID string `json:"scenario_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	ctx := r.Context()

	// Reset first
	if err := h.Store.Reset(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to reset database", err)
		return
	}
	h.policies = make(map[generic.PolicyID]*generic.Policy)
	h.accruals = make(map[generic.PolicyID]generic.AccrualSchedule)
	h.currentScenario = "" // Clear current scenario on reset

	var err error
	switch req.ScenarioID {
	case "new-employee":
		err = h.loadNewEmployeeScenario(ctx)
	case "year-end-rollover":
		err = h.loadYearEndRolloverScenario(ctx)
	case "multi-policy":
		err = h.loadMultiPolicyScenario(ctx)
	case "policy-change":
		err = h.loadMidYearPolicyChangeScenario(ctx)
	case "rewards-benefits":
		err = h.loadRewardsBenefitsScenario(ctx)
	case "hourly-worker":
		err = h.loadHourlyWorkerScenario(ctx)
	default:
		writeError(w, http.StatusBadRequest, "Unknown scenario", nil)
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load scenario: %v", err), err)
		return
	}

	// Track the loaded scenario
	h.currentScenario = req.ScenarioID

	writeJSON(w, http.StatusOK, map[string]string{"status": "loaded", "scenario": req.ScenarioID})
}

// =============================================================================
// SCENARIO LOADERS
// =============================================================================

func (h *Handler) loadNewEmployeeScenario(ctx context.Context) error {
	// Create standard PTO policy with rollover (5 days max carryover)
	// Policy: 24 days/year, accrued monthly (2 days/month), max 5 days carryover
	policyJSON := timeoff.StandardPTOJSON("pto-standard", "Standard PTO", 24, 5)
	if err := h.createPolicyFromJSON(ctx, policyJSON); err != nil {
		return err
	}

	// Create employee hired in December 2025 (before year-end rollover)
	currentYear := time.Now().Year()
	hireDate := time.Date(currentYear-1, time.December, 15, 0, 0, 0, 0, time.UTC) // Dec 15, 2025
	emp := sqlite.Employee{
		ID:       "emp-001",
		Name:     "Alice Johnson",
		Email:    "alice@example.com",
		HireDate: hireDate,
	}
	if err := h.Store.SaveEmployee(ctx, emp); err != nil {
		return err
	}

	// Assign policy
	assign := sqlite.AssignmentRecord{
		ID:                  "assign-001",
		EntityID:            "emp-001",
		PolicyID:            "pto-standard",
		EffectiveFrom:       hireDate,
		ConsumptionPriority: 1,
	}
	if err := h.Store.SaveAssignment(ctx, assign); err != nil {
		return err
	}

	// Get policy and accrual schedule for rollover calculation
	policy, ok := h.policies[generic.PolicyID("pto-standard")]
	if !ok {
		return fmt.Errorf("policy not found")
	}
	accrual := h.accruals[policy.ID]

	// Process year-end rollover for 2025
	// Accruals are computed on-demand from AccrualSchedule, not stored as transactions
	engine := &generic.ReconciliationEngine{}
	scenarioPrefix := "new-employee-scenario"

	// Calculate balance at end of 2025 using AccrualSchedule (no stored accrual transactions)
	yearEnd2025 := time.Date(currentYear-1, time.December, 31, 23, 59, 59, 0, time.UTC)
	period2025 := policy.PeriodConfig.PeriodFor(generic.TimePoint{Time: yearEnd2025})

	// Balance calculation uses AccrualSchedule.GenerateAccruals() for computed accruals
	// IMPORTANT: Use hire date for prorating - new employee only accrues from their start date
	hireDateTP := generic.TimePoint{Time: hireDate}
	balance2025 := calculateBalanceWithHireDate(nil, period2025, policy.Unit, accrual, generic.TimePoint{Time: yearEnd2025}, hireDateTP)
	balance2025.EntityID = generic.EntityID("emp-001")
	balance2025.PolicyID = policy.ID

	// Process rollover - creates TxReconciliation transactions (carryover + expire)
	nextPeriod := period2025.NextPeriod()
	rolloverOutput, err := engine.Process(generic.ReconciliationInput{
		EntityID:       generic.EntityID("emp-001"),
		PolicyID:       policy.ID,
		Policy:         *policy,
		CurrentBalance: balance2025,
		EndingPeriod:   period2025,
		NextPeriod:     nextPeriod,
	})
	if err != nil {
		return err
	}

	// Add rollover transactions (carryover + expire)
	for i := range rolloverOutput.Transactions {
		tx := &rolloverOutput.Transactions[i]
		if tx.ID == "" {
			txType := "carryover"
			if tx.Delta.IsNegative() {
				txType = "expire"
			}
			tx.ID = generic.TransactionID(fmt.Sprintf("tx-rollover-2025-%s-%d", txType, i))
		}
		txDate := tx.EffectiveAt.Time.Format("20060102")
		txType := "carryover"
		if tx.Delta.IsNegative() {
			txType = "expire"
		}
		tx.IdempotencyKey = fmt.Sprintf("%s-rollover-emp-001-%s-%s-%s-%d-%s",
			scenarioPrefix, string(tx.PolicyID), txType, txDate, currentYear-1, string(tx.ID))
	}
	if len(rolloverOutput.Transactions) > 0 {
		if err := h.Store.AppendBatch(ctx, rolloverOutput.Transactions); err != nil {
			return err
		}
	}

	// No accrual transactions needed - accruals are computed on-demand from AccrualSchedule
	return nil
}

func (h *Handler) loadYearEndRolloverScenario(ctx context.Context) error {
	// Create policy with rollover: 24 days/year, max 10 days carryover
	maxCarry := 10.0
	policyConfig := factory.PolicyJSON{
		ID:              "pto-rollover",
		Name:            "PTO with Rollover",
		ResourceType:    timeoff.ResourcePTO.ResourceID(),
		Unit:            "days",
		PeriodType:      "calendar_year",
		ConsumptionMode: "consume_ahead",
		Accrual: &factory.AccrualJSON{
			Type:       "yearly",
			AnnualDays: 24,
			Frequency:  "monthly",
		},
		Reconciliation: []factory.ReconciliationJSON{{
			Trigger: "period_end",
			Actions: []factory.ActionJSON{
				{Type: "carryover", MaxCarryover: &maxCarry},
				{Type: "expire"},
			},
		}},
	}
	configJSON, _ := json.Marshal(policyConfig)
	if err := h.createPolicyFromJSON(ctx, string(configJSON)); err != nil {
		return err
	}

	// Create employee hired last year
	hireDate := time.Date(time.Now().Year()-1, time.January, 1, 0, 0, 0, 0, time.UTC)
	emp := sqlite.Employee{
		ID:       "emp-003",
		Name:     "Carol Davis",
		Email:    "carol@example.com",
		HireDate: hireDate,
	}
	if err := h.Store.SaveEmployee(ctx, emp); err != nil {
		return err
	}

	// Assign policy
	assign := sqlite.AssignmentRecord{
		ID:                  "assign-003",
		EntityID:            "emp-003",
		PolicyID:            "pto-rollover",
		EffectiveFrom:       hireDate,
		ConsumptionPriority: 1,
	}
	if err := h.Store.SaveAssignment(ctx, assign); err != nil {
		return err
	}

	// Get policy and accrual schedule
	policy, ok := h.policies[generic.PolicyID("pto-rollover")]
	if !ok {
		return fmt.Errorf("policy not found after creation")
	}
	accrual := h.accruals[policy.ID]

	// Last year: Only consumption transactions (accruals are computed on-demand)
	lastYear := time.Now().Year() - 1
	scenarioPrefix := "year-end-rollover-scenario"
	lastYearEnd := generic.TimePoint{Time: time.Date(lastYear, time.December, 31, 23, 59, 59, 0, time.UTC)}

	// Last year consumption: 5 days in summer
	// Accrued balance: 24 days (computed from AccrualSchedule)
	// After consumption: 24 - 5 = 19 days remaining
	var txs []generic.Transaction
	for i := 0; i < 5; i++ {
		consumeDate := time.Date(lastYear, time.July, 10+i, 0, 0, 0, 0, time.UTC)
		txs = append(txs, generic.Transaction{
			ID:             generic.TransactionID(fmt.Sprintf("tx-consume-%d-%d", lastYear, i)),
			EntityID:       "emp-003",
			PolicyID:       "pto-rollover",
			ResourceType:   timeoff.ResourcePTO,
			EffectiveAt:    generic.TimePoint{Time: consumeDate},
			Delta:          generic.NewAmount(-1, generic.UnitDays),
			Type:           generic.TxConsumption,
			Reason:         "Summer vacation",
			IdempotencyKey: fmt.Sprintf("%s-consume-emp-003-july-%d-%d", scenarioPrefix, lastYear, i),
		})
	}

	// Save last year's consumption transactions
	if err := h.Store.AppendBatch(ctx, txs); err != nil {
		return err
	}

	// Process rollover from last year to this year
	// Balance: 24 accrued - 5 consumed = 19 remaining
	// Rollover: min(19, 10) = 10 carryover, 9 expire
	ledger := generic.NewLedger(h.Store)
	engine := &generic.ReconciliationEngine{}

	lastYearPeriod := policy.PeriodConfig.PeriodFor(lastYearEnd)
	lastYearTxs, err := ledger.TransactionsInRange(ctx, generic.EntityID("emp-003"), policy.ID, lastYearPeriod.Start, lastYearPeriod.End)
	if err != nil {
		return err
	}

	// Balance calculation uses AccrualSchedule for accruals + stored transactions for consumption
	balance := calculateBalance(lastYearTxs, lastYearPeriod, policy.Unit, accrual, lastYearEnd)
	balance.EntityID = generic.EntityID("emp-003")
	balance.PolicyID = policy.ID

	// Process rollover - creates TxReconciliation transactions
	nextPeriod := lastYearPeriod.NextPeriod()
	output, err := engine.Process(generic.ReconciliationInput{
		EntityID:       generic.EntityID("emp-003"),
		PolicyID:       policy.ID,
		Policy:         *policy,
		CurrentBalance: balance,
		EndingPeriod:   lastYearPeriod,
		NextPeriod:     nextPeriod,
	})
	if err != nil {
		return err
	}

	// Add rollover transactions (carryover + expire)
	for i := range output.Transactions {
		tx := &output.Transactions[i]
		if tx.ID == "" {
			txType := "carryover"
			if tx.Delta.IsNegative() {
				txType = "expire"
			}
			tx.ID = generic.TransactionID(fmt.Sprintf("tx-rollover-emp-003-%s-%d-%d", txType, lastYear, i))
		}
		txType := "carryover"
		if tx.Delta.IsNegative() {
			txType = "expire"
		}
		txDate := tx.EffectiveAt.Time.Format("20060102")
		tx.IdempotencyKey = fmt.Sprintf("%s-rollover-emp-003-%s-%s-%s-%d-%s",
			scenarioPrefix, string(tx.PolicyID), txType, txDate, lastYear, string(tx.ID))
	}
	if len(output.Transactions) > 0 {
		if err := h.Store.AppendBatch(ctx, output.Transactions); err != nil {
			return err
		}
	}

	// No accrual transactions needed - accruals are computed on-demand from AccrualSchedule
	return nil
}

func (h *Handler) loadMultiPolicyScenario(ctx context.Context) error {
	// Create multiple policies demonstrating different PTO types
	policies := []string{
		timeoff.StandardPTOJSON("pto-standard", "Standard PTO 2025", 24, 10),
		timeoff.UseItOrLoseItJSON("pto-bonus", "Tenure Bonus PTO", 12),
		timeoff.CarryoverBonusJSON("pto-carryover", "Carryover from 2024", 3),
		timeoff.SickLeaveJSON("sick-standard", "Sick Leave", 12),
	}

	for _, pj := range policies {
		if err := h.createPolicyFromJSON(ctx, pj); err != nil {
			return err
		}
	}

	// Create employee (3+ years tenure)
	hireDate := time.Date(time.Now().Year()-3, time.March, 15, 0, 0, 0, 0, time.UTC)
	emp := sqlite.Employee{
		ID:       "emp-004",
		Name:     "David Wilson",
		Email:    "david@example.com",
		HireDate: hireDate,
	}
	if err := h.Store.SaveEmployee(ctx, emp); err != nil {
		return err
	}

	// Assign all policies with different consumption priorities
	// Priority determines which policy to draw from first when taking time off
	assignments := []sqlite.AssignmentRecord{
		{ID: "assign-004-1", EntityID: "emp-004", PolicyID: "pto-carryover", EffectiveFrom: time.Date(time.Now().Year(), 1, 1, 0, 0, 0, 0, time.UTC), ConsumptionPriority: 1},
		{ID: "assign-004-2", EntityID: "emp-004", PolicyID: "pto-bonus", EffectiveFrom: hireDate, ConsumptionPriority: 2},
		{ID: "assign-004-3", EntityID: "emp-004", PolicyID: "pto-standard", EffectiveFrom: hireDate, ConsumptionPriority: 3},
		{ID: "assign-004-4", EntityID: "emp-004", PolicyID: "sick-standard", EffectiveFrom: hireDate, ConsumptionPriority: 1},
	}

	for _, a := range assignments {
		if err := h.Store.SaveAssignment(ctx, a); err != nil {
			return err
		}
	}

	// Add grant and consumption transactions only
	// Accruals for pto-standard and sick-standard are computed on-demand from AccrualSchedule
	var txs []generic.Transaction
	year := time.Now().Year()

	// Carryover grant from last year (Jan 1) - this is a one-time grant, not a computed accrual
	txs = append(txs, generic.Transaction{
		ID: "tx-carryover-grant", EntityID: "emp-004", PolicyID: "pto-carryover", ResourceType: timeoff.ResourcePTO,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(3, generic.UnitDays), Type: generic.TxGrant,
		Reason: fmt.Sprintf("Carryover from %d", year-1), IdempotencyKey: fmt.Sprintf("multi-policy-carryover-emp-004-%d", year),
	})

	// Tenure bonus grant (Jan 1) - one-time grant for 3+ years tenure
	txs = append(txs, generic.Transaction{
		ID: "tx-bonus-grant", EntityID: "emp-004", PolicyID: "pto-bonus", ResourceType: timeoff.ResourcePTO,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(5, generic.UnitDays), Type: generic.TxGrant,
		Reason: "Tenure bonus (3+ years)", IdempotencyKey: fmt.Sprintf("multi-policy-bonus-emp-004-%d", year),
	})

	// Consumption from carryover (2 days in Feb) - uses carryover first due to priority
	for i := 0; i < 2; i++ {
		consumeDate := time.Date(year, time.February, 14+i, 0, 0, 0, 0, time.UTC)
		txs = append(txs, generic.Transaction{
			ID:       generic.TransactionID(fmt.Sprintf("tx-consume-carryover-%d-%d", year, i)),
			EntityID: "emp-004", PolicyID: "pto-carryover", ResourceType: timeoff.ResourcePTO,
			EffectiveAt: generic.TimePoint{Time: consumeDate},
			Delta:       generic.NewAmount(-1, generic.UnitDays), Type: generic.TxConsumption,
			Reason: "Valentine's getaway", IdempotencyKey: fmt.Sprintf("multi-policy-consume-carryover-emp-004-feb-%d-%d", year, i),
		})
	}

	// No accrual transactions needed - accruals are computed on-demand from AccrualSchedule
	return h.Store.AppendBatch(ctx, txs)
}

func (h *Handler) loadRewardsBenefitsScenario(ctx context.Context) error {
	// Create diverse benefit policies demonstrating system versatility
	policies := []string{
		// Wellness points - earned monthly, some carryover allowed
		rewards.WellnessPointsJSON("wellness-program", "Wellness Points", 1200, 200),
		// Learning credits - full budget upfront, use it or lose it
		rewards.LearningCreditsJSON("learning-budget", "Learning & Development", 2500),
		// Recognition points - earn from peers, spend on rewards
		rewards.RecognitionPointsJSON("peer-kudos", "Recognition Points", 100),
		// Flex benefits - HSA-style with some rollover
		rewards.FlexBenefitsBudgetJSON("flex-spending", "Flexible Benefits Account", 1500, 500),
		// Remote work days - monthly allowance
		rewards.RemoteWorkDaysJSON("wfh-allowance", "Remote Work Days", 8),
		// Volunteer hours - annual allowance
		rewards.VolunteerHoursJSON("volunteer-time", "Paid Volunteer Hours", 16),
	}

	for _, pj := range policies {
		if err := h.createPolicyFromJSON(ctx, pj); err != nil {
			return err
		}
	}

	// Create employee - Alex, a software engineer
	hireDate := time.Date(time.Now().Year()-1, time.June, 1, 0, 0, 0, 0, time.UTC)
	emp := sqlite.Employee{
		ID:       "emp-alex",
		Name:     "Alex Rivera",
		Email:    "alex.rivera@example.com",
		HireDate: hireDate,
	}
	if err := h.Store.SaveEmployee(ctx, emp); err != nil {
		return err
	}

	// Assign all policies
	assignments := []sqlite.AssignmentRecord{
		{ID: "assign-alex-wellness", EntityID: "emp-alex", PolicyID: "wellness-program",
			EffectiveFrom: hireDate, ConsumptionPriority: 1},
		{ID: "assign-alex-learning", EntityID: "emp-alex", PolicyID: "learning-budget",
			EffectiveFrom: hireDate, ConsumptionPriority: 1},
		{ID: "assign-alex-kudos", EntityID: "emp-alex", PolicyID: "peer-kudos",
			EffectiveFrom: hireDate, ConsumptionPriority: 1},
		{ID: "assign-alex-flex", EntityID: "emp-alex", PolicyID: "flex-spending",
			EffectiveFrom: hireDate, ConsumptionPriority: 1},
		{ID: "assign-alex-wfh", EntityID: "emp-alex", PolicyID: "wfh-allowance",
			EffectiveFrom: hireDate, ConsumptionPriority: 1},
		{ID: "assign-alex-volunteer", EntityID: "emp-alex", PolicyID: "volunteer-time",
			EffectiveFrom: hireDate, ConsumptionPriority: 1},
	}

	for _, a := range assignments {
		if err := h.Store.SaveAssignment(ctx, a); err != nil {
			return err
		}
	}

	// Add transactions to simulate usage
	var txs []generic.Transaction
	year := time.Now().Year()

	// Generate wellness points accruals from hire date to end of current year
	// Employee hired June 2025, so generate from June 2025 to Dec 2026 (or current year end)
	hireYear := hireDate.Year()
	hireMonth := int(hireDate.Month())

	// Generate accruals for hire year (from hire month to Dec)
	for month := hireMonth; month <= 12; month++ {
		accrualDate := time.Date(hireYear, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		txs = append(txs, generic.Transaction{
			ID:       generic.TransactionID(fmt.Sprintf("tx-wellness-accrual-%d-%02d", hireYear, month)),
			EntityID: "emp-alex", PolicyID: "wellness-program", ResourceType: rewards.ResourceWellnessPoints,
			EffectiveAt: generic.TimePoint{Time: accrualDate},
		Delta:       generic.NewAmount(100, "points"), Type: generic.TxGrant,
		Reason: "Monthly wellness points", IdempotencyKey: fmt.Sprintf("rewards-wellness-grant-%d-%02d", hireYear, month),
		})
	}

	// Generate accruals for current year (all months)
	for month := 1; month <= 12; month++ {
		accrualDate := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		txs = append(txs, generic.Transaction{
			ID:       generic.TransactionID(fmt.Sprintf("tx-wellness-accrual-%d-%02d", year, month)),
			EntityID: "emp-alex", PolicyID: "wellness-program", ResourceType: rewards.ResourceWellnessPoints,
			EffectiveAt: generic.TimePoint{Time: accrualDate},
		Delta:       generic.NewAmount(100, "points"), Type: generic.TxGrant,
		Reason: "Monthly wellness points", IdempotencyKey: fmt.Sprintf("rewards-wellness-grant-%d-%02d", year, month),
		})
	}
	// Redeemed some points for gym membership discount
	txs = append(txs, generic.Transaction{
		ID: "tx-wellness-redeem-1", EntityID: "emp-alex", PolicyID: "wellness-program", ResourceType: rewards.ResourceWellnessPoints,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, time.February, 15, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(-150, "points"), Type: generic.TxConsumption,
		Reason: "Gym membership discount", IdempotencyKey: fmt.Sprintf("rewards-wellness-redeem-gym-%d", year),
	})

	// === LEARNING BUDGET ===
	// Full budget granted at start of year
	txs = append(txs, generic.Transaction{
		ID: "tx-learning-grant", EntityID: "emp-alex", PolicyID: "learning-budget", ResourceType: rewards.ResourceLearningCredits,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(2500, "dollars"), Type: generic.TxGrant,
		Reason: "Annual learning budget", IdempotencyKey: fmt.Sprintf("rewards-learning-grant-%d", year),
	})
	// Used for online course
	txs = append(txs, generic.Transaction{
		ID: "tx-learning-course", EntityID: "emp-alex", PolicyID: "learning-budget", ResourceType: rewards.ResourceLearningCredits,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, time.January, 20, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(-399, "dollars"), Type: generic.TxConsumption,
		Reason: "Udemy course bundle", IdempotencyKey: fmt.Sprintf("rewards-learning-udemy-%d", year),
	})
	// Used for conference
	txs = append(txs, generic.Transaction{
		ID: "tx-learning-conf", EntityID: "emp-alex", PolicyID: "learning-budget", ResourceType: rewards.ResourceLearningCredits,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, time.March, 5, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(-800, "dollars"), Type: generic.TxConsumption,
		Reason: "Tech conference registration", IdempotencyKey: fmt.Sprintf("rewards-learning-conf-%d", year),
	})

	// === RECOGNITION POINTS ===
	// Points received from colleagues
	txs = append(txs, generic.Transaction{
		ID: "tx-kudos-1", EntityID: "emp-alex", PolicyID: "peer-kudos", ResourceType: rewards.ResourceRecognitionPoints,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, time.January, 15, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(50, "points"), Type: generic.TxGrant,
		Reason: "Kudos from @maria: Great job on the API redesign!", IdempotencyKey: fmt.Sprintf("rewards-kudos-maria-jan-%d", year),
	})
	txs = append(txs, generic.Transaction{
		ID: "tx-kudos-2", EntityID: "emp-alex", PolicyID: "peer-kudos", ResourceType: rewards.ResourceRecognitionPoints,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, time.February, 3, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(25, "points"), Type: generic.TxGrant,
		Reason: "Kudos from @james: Helped debug production issue", IdempotencyKey: fmt.Sprintf("rewards-kudos-james-feb-%d", year),
	})
	txs = append(txs, generic.Transaction{
		ID: "tx-kudos-3", EntityID: "emp-alex", PolicyID: "peer-kudos", ResourceType: rewards.ResourceRecognitionPoints,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, time.February, 20, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(100, "points"), Type: generic.TxGrant,
		Reason: "Manager bonus: Q4 performance excellence", IdempotencyKey: fmt.Sprintf("rewards-kudos-manager-q4-%d", year),
	})
	// Redeemed for gift card
	txs = append(txs, generic.Transaction{
		ID: "tx-kudos-redeem", EntityID: "emp-alex", PolicyID: "peer-kudos", ResourceType: rewards.ResourceRecognitionPoints,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, time.March, 1, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(-100, "points"), Type: generic.TxConsumption,
		Reason: "Redeemed for $50 Amazon gift card", IdempotencyKey: fmt.Sprintf("rewards-kudos-redeem-amazon-%d", year),
	})

	// === FLEX BENEFITS ===
	// Full budget at start of year
	txs = append(txs, generic.Transaction{
		ID: "tx-flex-grant", EntityID: "emp-alex", PolicyID: "flex-spending", ResourceType: rewards.ResourceFlexBenefits,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(1500, "dollars"), Type: generic.TxGrant,
		Reason: "Annual flex benefits budget", IdempotencyKey: fmt.Sprintf("rewards-flex-grant-%d", year),
	})
	// Plus carryover from last year
	txs = append(txs, generic.Transaction{
		ID: "tx-flex-carryover", EntityID: "emp-alex", PolicyID: "flex-spending", ResourceType: rewards.ResourceFlexBenefits,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(350, "dollars"), Type: generic.TxReconciliation,
		Reason: fmt.Sprintf("Carryover from %d", year-1), IdempotencyKey: fmt.Sprintf("rewards-flex-carryover-%d", year-1),
	})
	// Used for glasses
	txs = append(txs, generic.Transaction{
		ID: "tx-flex-glasses", EntityID: "emp-alex", PolicyID: "flex-spending", ResourceType: rewards.ResourceFlexBenefits,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, time.February, 10, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(-275, "dollars"), Type: generic.TxConsumption,
		Reason: "Prescription glasses", IdempotencyKey: fmt.Sprintf("rewards-flex-glasses-%d", year),
	})

	// === REMOTE WORK DAYS ===
	// Monthly accruals (8 days/month) from hire date to end of current year
	// Generate for hire year (from hire month to Dec)
	for month := hireMonth; month <= 12; month++ {
		accrualDate := time.Date(hireYear, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		txs = append(txs, generic.Transaction{
			ID:       generic.TransactionID(fmt.Sprintf("tx-wfh-accrual-%d-%02d", hireYear, month)),
			EntityID: "emp-alex", PolicyID: "wfh-allowance", ResourceType: rewards.ResourceRemoteDays,
			EffectiveAt: generic.TimePoint{Time: accrualDate},
			Delta:       generic.NewAmount(8, "days"), Type: generic.TxGrant,
			Reason: "Monthly WFH allowance", IdempotencyKey: fmt.Sprintf("rewards-wfh-accrual-%d-%02d", hireYear, month),
		})
	}
	// Generate for current year (all months)
	for month := 1; month <= 12; month++ {
		accrualDate := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		txs = append(txs, generic.Transaction{
			ID:       generic.TransactionID(fmt.Sprintf("tx-wfh-accrual-%d-%02d", year, month)),
			EntityID: "emp-alex", PolicyID: "wfh-allowance", ResourceType: rewards.ResourceRemoteDays,
			EffectiveAt: generic.TimePoint{Time: accrualDate},
			Delta:       generic.NewAmount(8, "days"), Type: generic.TxGrant,
			Reason: "Monthly WFH allowance", IdempotencyKey: fmt.Sprintf("rewards-wfh-accrual-%d-%02d", year, month),
		})
	}
	// Used some WFH days
	txs = append(txs, generic.Transaction{
		ID: "tx-wfh-jan", EntityID: "emp-alex", PolicyID: "wfh-allowance", ResourceType: rewards.ResourceRemoteDays,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, time.January, 31, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(-6, "days"), Type: generic.TxConsumption,
		Reason: "January WFH usage", IdempotencyKey: fmt.Sprintf("rewards-wfh-use-jan-%d", year),
	})

	// === VOLUNTEER HOURS ===
	// Full hours granted at start of current year
	txs = append(txs, generic.Transaction{
		ID: "tx-volunteer-grant", EntityID: "emp-alex", PolicyID: "volunteer-time", ResourceType: rewards.ResourceVolunteerHours,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(16, "hours"), Type: generic.TxGrant,
		Reason: "Annual volunteer time", IdempotencyKey: fmt.Sprintf("rewards-volunteer-grant-%d", year),
	})
	// Used 4 hours
	txs = append(txs, generic.Transaction{
		ID: "tx-volunteer-use", EntityID: "emp-alex", PolicyID: "volunteer-time", ResourceType: rewards.ResourceVolunteerHours,
		EffectiveAt: generic.TimePoint{Time: time.Date(year, time.February, 14, 0, 0, 0, 0, time.UTC)},
		Delta:       generic.NewAmount(-4, "hours"), Type: generic.TxConsumption,
		Reason: "Food bank volunteering", IdempotencyKey: fmt.Sprintf("rewards-volunteer-foodbank-%d", year),
	})

	return h.Store.AppendBatch(ctx, txs)
}

func (h *Handler) loadHourlyWorkerScenario(ctx context.Context) error {
	// Create consume-up-to-accrued policy: 12 days/year (1 day/month)
	// Worker can only use time they've actually accrued - perfect for hourly workers
	policyJSON := timeoff.HourlyWorkerJSON("pto-hourly", "Hourly Worker PTO", 12)
	if err := h.createPolicyFromJSON(ctx, policyJSON); err != nil {
		return err
	}

	// Create employee hired mid-January
	hireDate := time.Date(time.Now().Year(), time.January, 15, 0, 0, 0, 0, time.UTC)
	emp := sqlite.Employee{
		ID:       "emp-005",
		Name:     "Eve Martinez",
		Email:    "eve@example.com",
		HireDate: hireDate,
	}
	if err := h.Store.SaveEmployee(ctx, emp); err != nil {
		return err
	}

	// Assign policy with approval config (auto-approve up to 1 day)
	approvalConfig, _ := json.Marshal(map[string]any{
		"requires_approval":  true,
		"auto_approve_up_to": 1.0,
	})
	assign := sqlite.AssignmentRecord{
		ID:                  "assign-005",
		EntityID:            "emp-005",
		PolicyID:            "pto-hourly",
		EffectiveFrom:       hireDate,
		ConsumptionPriority: 1,
		ApprovalConfigJSON:  string(approvalConfig),
	}
	if err := h.Store.SaveAssignment(ctx, assign); err != nil {
		return err
	}

	// No accrual transactions needed - accruals are computed on-demand from AccrualSchedule
	// For consume-up-to-accrued, the available balance is limited to AccruedToDate
	return nil
}

// =============================================================================
// MID-YEAR POLICY CHANGE SCENARIO
// =============================================================================
// Demonstrates that policy change and year-end rollover use the same mechanism:
// 1. Close the current period (compute accrued balance)
// 2. Apply carryover/expiration rules via ReconciliationEngine
// 3. Remaining balance carries to the next period/policy

func (h *Handler) loadMidYearPolicyChangeScenario(ctx context.Context) error {
	// Create initial policy: 12 days/year = 1 day/month, max 5 days carryover
	maxCarry := 5.0
	policy1Config := factory.PolicyJSON{
		ID:              "pto-initial",
		Name:            "PTO Initial Rate (12 days/year)",
		ResourceType:    timeoff.ResourcePTO.ResourceID(),
		Unit:            "days",
		PeriodType:      "calendar_year",
		ConsumptionMode: "consume_ahead",
		Accrual: &factory.AccrualJSON{
			Type:       "yearly",
			AnnualDays: 12, // 1 day/month
			Frequency:  "monthly",
		},
		Reconciliation: []factory.ReconciliationJSON{{
			Trigger: "period_end",
			Actions: []factory.ActionJSON{
				{Type: "carryover", MaxCarryover: &maxCarry},
				{Type: "expire"},
			},
		}},
	}
	configJSON1, _ := json.Marshal(policy1Config)
	if err := h.createPolicyFromJSON(ctx, string(configJSON1)); err != nil {
		return err
	}

	// Create employee hired at start of year
	currentYear := time.Now().Year()
	hireDate := time.Date(currentYear, time.January, 1, 0, 0, 0, 0, time.UTC)
	emp := sqlite.Employee{
		ID:       "emp-policy-change",
		Name:     "Emma Thompson",
		Email:    "emma@example.com",
		HireDate: hireDate,
	}
	if err := h.Store.SaveEmployee(ctx, emp); err != nil {
		return err
	}

	// Assign initial policy
	assign := sqlite.AssignmentRecord{
		ID:                  "assign-policy-change",
		EntityID:            "emp-policy-change",
		PolicyID:            "pto-initial",
		EffectiveFrom:       hireDate,
		ConsumptionPriority: 1,
	}
	if err := h.Store.SaveAssignment(ctx, assign); err != nil {
		return err
	}

	// Get initial policy and accrual schedule
	policy1, ok := h.policies[generic.PolicyID("pto-initial")]
	if !ok {
		return fmt.Errorf("initial policy not found")
	}
	accrual1 := h.accruals[policy1.ID]

	scenarioPrefix := "policy-change-scenario"

	// Add consumption in April (2 days)
	// At this point: 4 months accrued (4 days) - 2 consumed = 2 days remaining
	var consumptionTxs []generic.Transaction
	for i := 0; i < 2; i++ {
		consumeDate := time.Date(currentYear, time.April, 10+i, 0, 0, 0, 0, time.UTC)
		consumptionTxs = append(consumptionTxs, generic.Transaction{
			ID:             generic.TransactionID(fmt.Sprintf("tx-consume-april-%d", i)),
			EntityID:       "emp-policy-change",
			PolicyID:       "pto-initial",
			ResourceType:   timeoff.ResourcePTO,
			EffectiveAt:    generic.TimePoint{Time: consumeDate},
			Delta:          generic.NewAmount(-1.0, generic.UnitDays),
			Type:           generic.TxConsumption,
			Reason:         "Spring vacation",
			IdempotencyKey: fmt.Sprintf("%s-consume-april-%d-%d", scenarioPrefix, currentYear, i),
		})
	}
	if err := h.Store.AppendBatch(ctx, consumptionTxs); err != nil {
		return err
	}

	// === POLICY CHANGE ON JULY 1 ===
	// This uses the SAME ReconciliationEngine as year-end rollover!
	// Balance at end of June: 6 accrued (Jan-Jun) - 2 consumed = 4 days
	// Carryover: min(4, 5) = 4 days carry to new policy

	policyChangeDate := time.Date(currentYear, time.July, 1, 0, 0, 0, 0, time.UTC)
	juneEnd := generic.TimePoint{Time: time.Date(currentYear, time.June, 30, 23, 59, 59, 0, time.UTC)}

	// Calculate balance at end of June (accruals computed from schedule, not stored)
	ledger := generic.NewLedger(h.Store)
	junePeriod := generic.Period{
		Start: generic.TimePoint{Time: time.Date(currentYear, time.January, 1, 0, 0, 0, 0, time.UTC)},
		End:   juneEnd,
	}
	juneTxs, err := ledger.TransactionsInRange(ctx, generic.EntityID("emp-policy-change"), policy1.ID, junePeriod.Start, junePeriod.End)
	if err != nil {
		return err
	}
	juneBalance := calculateBalance(juneTxs, junePeriod, policy1.Unit, accrual1, juneEnd)
	juneBalance.EntityID = generic.EntityID("emp-policy-change")
	juneBalance.PolicyID = policy1.ID

	// Use ReconciliationEngine to process the policy change (same as rollover!)
	engine := &generic.ReconciliationEngine{}
	nextPeriod := generic.Period{
		Start: generic.TimePoint{Time: policyChangeDate},
		End:   generic.TimePoint{Time: time.Date(currentYear, time.December, 31, 23, 59, 59, 0, time.UTC)},
	}

	reconcileOutput, err := engine.Process(generic.ReconciliationInput{
		EntityID:       generic.EntityID("emp-policy-change"),
		PolicyID:       policy1.ID,
		Policy:         *policy1,
		CurrentBalance: juneBalance,
		EndingPeriod:   junePeriod,
		NextPeriod:     nextPeriod,
	})
	if err != nil {
		return err
	}

	// Add reconciliation transactions (carryover + expire if any)
	for i := range reconcileOutput.Transactions {
		tx := &reconcileOutput.Transactions[i]
		if tx.ID == "" {
			txType := "carryover"
			if tx.Delta.IsNegative() {
				txType = "expire"
			}
			tx.ID = generic.TransactionID(fmt.Sprintf("tx-policy-change-%s-%d-%d", txType, currentYear, i))
		}
		txType := "carryover"
		if tx.Delta.IsNegative() {
			txType = "expire"
		}
		tx.IdempotencyKey = fmt.Sprintf("%s-reconcile-%s-%d-%d", scenarioPrefix, txType, currentYear, i)
		tx.Reason = fmt.Sprintf("Policy change (Jun 30): %s", tx.Reason)
	}
	if len(reconcileOutput.Transactions) > 0 {
		if err := h.Store.AppendBatch(ctx, reconcileOutput.Transactions); err != nil {
			return err
		}
	}

	// Create new policy: 24 days/year = 2 days/month (effective July 1)
	policy2Config := factory.PolicyJSON{
		ID:              "pto-upgraded",
		Name:            "PTO Upgraded Rate (24 days/year)",
		ResourceType:    timeoff.ResourcePTO.ResourceID(),
		Unit:            "days",
		PeriodType:      "calendar_year",
		ConsumptionMode: "consume_ahead",
		Accrual: &factory.AccrualJSON{
			Type:       "yearly",
			AnnualDays: 24, // 2 days/month
			Frequency:  "monthly",
		},
		Reconciliation: []factory.ReconciliationJSON{{
			Trigger: "period_end",
			Actions: []factory.ActionJSON{
				{Type: "carryover", MaxCarryover: &maxCarry},
				{Type: "expire"},
			},
		}},
	}
	configJSON2, _ := json.Marshal(policy2Config)
	if err := h.createPolicyFromJSON(ctx, string(configJSON2)); err != nil {
		return err
	}

	// End the old assignment
	assign.EffectiveTo = &policyChangeDate
	if err := h.Store.SaveAssignment(ctx, assign); err != nil {
		return err
	}

	// Create new assignment with upgraded policy
	newAssign := sqlite.AssignmentRecord{
		ID:                  "assign-policy-change-new",
		EntityID:            "emp-policy-change",
		PolicyID:            "pto-upgraded",
		EffectiveFrom:       policyChangeDate,
		ConsumptionPriority: 1,
	}
	if err := h.Store.SaveAssignment(ctx, newAssign); err != nil {
		return err
	}

	// Transfer carryover balance to new policy as a grant
	// This represents the balance carried from the old policy
	// Use the maxCarry limit from the policy config (5 days)
	carryoverAmount := juneBalance.CurrentAccrued()
	maxCarryAmount := generic.NewAmount(maxCarry, generic.UnitDays)
	if carryoverAmount.Value.Cmp(maxCarryAmount.Value) > 0 {
		carryoverAmount = maxCarryAmount
	}

	carryoverGrant := generic.Transaction{
		ID:             generic.TransactionID(fmt.Sprintf("tx-carryover-grant-%d", currentYear)),
		EntityID:       "emp-policy-change",
		PolicyID:       "pto-upgraded",
		ResourceType:   timeoff.ResourcePTO,
		EffectiveAt:    generic.TimePoint{Time: policyChangeDate},
		Delta:          carryoverAmount,
		Type:           generic.TxGrant,
		Reason:         fmt.Sprintf("Balance carried from previous policy (%s days)", carryoverAmount.Value.StringFixed(2)),
		IdempotencyKey: fmt.Sprintf("%s-carryover-grant-%d", scenarioPrefix, currentYear),
	}

	return h.Store.AppendBatch(ctx, []generic.Transaction{carryoverGrant})
}

func (h *Handler) createPolicyFromJSON(ctx context.Context, jsonStr string) error {
	policy, accrual, err := h.PolicyFactory.ParsePolicy(jsonStr)
	if err != nil {
		return err
	}

	record := sqlite.PolicyRecord{
		ID:           string(policy.ID),
		Name:         policy.Name,
		ResourceType: policy.ResourceType.ResourceID(),
		ConfigJSON:   jsonStr,
		Version:      1,
	}

	if err := h.Store.SavePolicy(ctx, record); err != nil {
		return err
	}

	h.policies[policy.ID] = policy
	h.accruals[policy.ID] = accrual
	return nil
}
