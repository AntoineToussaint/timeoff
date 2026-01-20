/*
handlers.go - HTTP API handlers for the resource management system

PURPOSE:
  Exposes the resource management engine via REST API. Handles HTTP
  request/response, JSON serialization, and delegates to domain logic.

ENDPOINTS:
  Employees:
    GET    /api/employees              List all employees
    POST   /api/employees              Create employee
    GET    /api/employees/{id}         Get employee details
    GET    /api/employees/{id}/balance Get balance summary

  Requests:
    POST   /api/employees/{id}/requests Submit time-off/resource request
    GET    /api/employees/{id}/transactions Transaction history

  Policies:
    GET    /api/policies               List all policies
    POST   /api/policies               Create policy from JSON

  Admin:
    POST   /api/admin/rollover         Trigger year-end rollover
    POST   /api/admin/adjustment       Manual balance adjustment

  Scenarios:
    GET    /api/scenarios              List demo scenarios
    POST   /api/scenarios/load         Load a demo scenario

ARCHITECTURE:
  Handler struct holds all dependencies:
  - Store: Database access
  - PolicyFactory: JSON to Policy conversion
  - Cached policies/accruals for performance

REQUEST FLOW:
  1. Parse HTTP request
  2. Validate input
  3. Call domain logic (ledger, projection, etc.)
  4. Serialize response
  5. Handle errors

ERROR HANDLING:
  Errors are returned as JSON with appropriate HTTP status:
  - 400: Validation errors, invalid input
  - 404: Resource not found
  - 409: Conflict (idempotency, duplicate)
  - 500: Internal errors

SECURITY NOTE:
  Currently NO authentication or authorization. All endpoints are public.
  See DEVOPS_SECURITY.md for production requirements.

SEE ALSO:
  - dto.go: Request/response data structures
  - scenarios.go: Demo scenario loaders
  - server.go: Router setup and middleware
*/
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/warp/resource-engine/factory"
	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/store/sqlite"
	"github.com/warp/resource-engine/timeoff"
)

// =============================================================================
// HANDLER CONTEXT
// =============================================================================

// Handler holds all dependencies for HTTP handlers.
type Handler struct {
	Store         *sqlite.Store
	PolicyFactory *factory.PolicyFactory
	
	// Cached policies and accruals for quick lookups
	policies map[generic.PolicyID]*generic.Policy
	accruals map[generic.PolicyID]generic.AccrualSchedule
	
	// Track currently loaded scenario
	currentScenario string
}

// NewHandler creates a new handler with the given store.
func NewHandler(store *sqlite.Store) *Handler {
	return &Handler{
		Store:         store,
		PolicyFactory: factory.NewPolicyFactory(),
		policies:      make(map[generic.PolicyID]*generic.Policy),
		accruals:      make(map[generic.PolicyID]generic.AccrualSchedule),
	}
}

// LoadPolicies loads all policies from the database into cache.
func (h *Handler) LoadPolicies(ctx context.Context) error {
	records, err := h.Store.ListPolicies(ctx)
	if err != nil {
		return err
	}

	for _, r := range records {
		policy, accrual, err := h.PolicyFactory.ParsePolicy(r.ConfigJSON)
		if err != nil {
			continue // Skip invalid policies
		}
		h.policies[policy.ID] = policy
		h.accruals[policy.ID] = accrual
	}
	return nil
}

// =============================================================================
// EMPLOYEE HANDLERS
// =============================================================================

// ListEmployees returns all employees.
func (h *Handler) ListEmployees(w http.ResponseWriter, r *http.Request) {
	employees, err := h.Store.ListEmployees(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list employees", err)
		return
	}

	dtos := make([]EmployeeDTO, len(employees))
	for i, e := range employees {
		dtos[i] = EmployeeDTO{
			ID:        e.ID,
			Name:      e.Name,
			Email:     e.Email,
			HireDate:  e.HireDate.Format("2006-01-02"),
			CreatedAt: e.CreatedAt.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, dtos)
}

// GetEmployee returns a single employee.
func (h *Handler) GetEmployee(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	
	emp, err := h.Store.GetEmployee(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get employee", err)
		return
	}
	if emp == nil {
		writeError(w, http.StatusNotFound, "Employee not found", nil)
		return
	}

	writeJSON(w, http.StatusOK, EmployeeDTO{
		ID:        emp.ID,
		Name:      emp.Name,
		Email:     emp.Email,
		HireDate:  emp.HireDate.Format("2006-01-02"),
		CreatedAt: emp.CreatedAt.Format(time.RFC3339),
	})
}

// CreateEmployee creates a new employee.
func (h *Handler) CreateEmployee(w http.ResponseWriter, r *http.Request) {
	var req CreateEmployeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	hireDate, err := time.Parse("2006-01-02", req.HireDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid hire_date format (use YYYY-MM-DD)", err)
		return
	}

	emp := sqlite.Employee{
		ID:       req.ID,
		Name:     req.Name,
		Email:    req.Email,
		HireDate: hireDate,
	}

	if err := h.Store.SaveEmployee(r.Context(), emp); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create employee", err)
		return
	}

	writeJSON(w, http.StatusCreated, EmployeeDTO{
		ID:       emp.ID,
		Name:     emp.Name,
		Email:    emp.Email,
		HireDate: emp.HireDate.Format("2006-01-02"),
	})
}

// =============================================================================
// BALANCE HANDLERS
// =============================================================================

// GetBalance returns aggregate balance for an employee.
func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	entityID := generic.EntityID(chi.URLParam(r, "id"))
	resourceType := r.URL.Query().Get("resource_type")
	if resourceType == "" {
		resourceType = string(timeoff.ResourcePTO) // Default
	}

	ctx := r.Context()
	asOf := generic.Today()

	// Get assignments for this employee
	assignments, err := h.Store.GetAssignmentsByEntity(ctx, string(entityID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get assignments", err)
		return
	}

	// Filter by resource type and build balance
	var policyBalances []PolicyBalanceDTO
	totalAvailable := 0.0
	totalPending := 0.0

	ledger := generic.NewLedger(h.Store)

	for _, a := range assignments {
		policy, ok := h.policies[generic.PolicyID(a.PolicyID)]
		if !ok || policy.ResourceType.ResourceID() != resourceType {
			continue
		}

		accrual := h.accruals[policy.ID]
		period := policy.PeriodConfig.PeriodFor(asOf)

		// Get balance for this policy
		txs, err := ledger.TransactionsInRange(ctx, entityID, policy.ID, period.Start, period.End)
		if err != nil {
			continue
		}

		balance := calculateBalance(txs, period, policy.Unit, accrual, asOf)
		available, _ := balance.AvailableWithMode(policy.ConsumptionMode).Value.Float64()
		accrued, _ := balance.AccruedToDate.Value.Float64()
		entitlement, _ := balance.TotalEntitlement.Value.Float64()
		adjustments, _ := balance.Adjustments.Value.Float64()
		consumed, _ := balance.TotalConsumed.Value.Float64()
		pending, _ := balance.Pending.Value.Float64()

		// Include adjustments (rollover/carryover) in displayed entitlement
		displayEntitlement := entitlement + adjustments

		policyBalances = append(policyBalances, PolicyBalanceDTO{
			PolicyID:         string(policy.ID),
			PolicyName:       policy.Name,
			Priority:         a.ConsumptionPriority,
			Available:        available,
			AccruedToDate:    accrued,
			TotalEntitlement: displayEntitlement,
			Consumed:         consumed,
			Pending:          pending,
			ConsumptionMode:  string(policy.ConsumptionMode),
			RequiresApproval: a.ApprovalConfigJSON != "",
		})

		totalAvailable += available
		totalPending += pending
	}

	writeJSON(w, http.StatusOK, BalanceDTO{
		EntityID:       string(entityID),
		ResourceType:   resourceType,
		TotalAvailable: totalAvailable,
		TotalPending:   totalPending,
		Policies:       policyBalances,
		AsOf:           asOf.Time.Format("2006-01-02"),
	})
}

func calculateBalance(txs []generic.Transaction, period generic.Period, unit generic.Unit, accrual generic.AccrualSchedule, asOf generic.TimePoint) generic.Balance {
	return calculateBalanceWithHireDate(txs, period, unit, accrual, asOf, period.Start)
}

// calculateBalanceWithHireDate calculates balance with prorating from hire date.
// For mid-period hires, accruals should start from hireDate, not period.Start.
func calculateBalanceWithHireDate(txs []generic.Transaction, period generic.Period, unit generic.Unit, accrual generic.AccrualSchedule, asOf generic.TimePoint, hireDate generic.TimePoint) generic.Balance {
	var (
		actualAccruals = generic.NewAmount(0, unit)
		consumed       = generic.NewAmount(0, unit)
		pending        = generic.NewAmount(0, unit)
		adjustments    = generic.NewAmount(0, unit)
	)

	for _, tx := range txs {
		switch tx.Type {
		case generic.TxGrant:
			// Grants add to balance (bonus days, carryover balance, hours-worked accruals)
			actualAccruals = actualAccruals.Add(tx.Delta)
		case generic.TxConsumption:
			consumed = consumed.Add(tx.Delta.Neg())
		case generic.TxPending:
			pending = pending.Add(tx.Delta.Neg())
		case generic.TxReconciliation, generic.TxAdjustment:
			adjustments = adjustments.Add(tx.Delta)
		case generic.TxReversal:
			consumed = consumed.Sub(tx.Delta)
		}
	}

	accruedToDate := actualAccruals
	totalEntitlement := actualAccruals

	// Determine accrual start: later of period start or hire date
	accrualStart := period.Start
	if hireDate.After(period.Start) {
		accrualStart = hireDate
	}

	if accrual != nil {
		// Accruals from hire date (or period start) to asOf
		accruedEvents := accrual.GenerateAccruals(accrualStart, asOf)
		accruedTotal := generic.NewAmount(0, unit)
		for _, e := range accruedEvents {
			accruedTotal = accruedTotal.Add(e.Amount)
		}
		if accruedTotal.GreaterThan(actualAccruals) {
			accruedToDate = accruedTotal
		}

		// Total entitlement from hire date to period end (prorated)
		allEvents := accrual.GenerateAccruals(accrualStart, period.End)
		entitlementTotal := generic.NewAmount(0, unit)
		for _, e := range allEvents {
			entitlementTotal = entitlementTotal.Add(e.Amount)
		}
		totalEntitlement = entitlementTotal
	}

	return generic.Balance{
		Period:           period,
		AccruedToDate:    accruedToDate,
		TotalEntitlement: totalEntitlement,
		TotalConsumed:    consumed,
		Pending:          pending,
		Adjustments:      adjustments,
	}
}

// GetTransactions returns transaction history for an employee.
func (h *Handler) GetTransactions(w http.ResponseWriter, r *http.Request) {
	entityID := chi.URLParam(r, "id")
	policyID := r.URL.Query().Get("policy_id")

	ctx := r.Context()
	var txs []generic.Transaction
	var err error

	if policyID != "" {
		txs, err = h.Store.Load(ctx, generic.EntityID(entityID), generic.PolicyID(policyID))
	} else {
		// Get all transactions for employee (across all policies)
		assignments, _ := h.Store.GetAssignmentsByEntity(ctx, entityID)
		for _, a := range assignments {
			policyTxs, _ := h.Store.Load(ctx, generic.EntityID(entityID), generic.PolicyID(a.PolicyID))
			txs = append(txs, policyTxs...)
		}
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get transactions", err)
		return
	}

	// Calculate balance at each transaction date
	dtos := h.toTransactionDTOsWithBalance(ctx, txs)
	writeJSON(w, http.StatusOK, dtos)
}

// =============================================================================
// REQUEST HANDLERS
// =============================================================================

// SubmitRequest submits a time-off request.
func (h *Handler) SubmitRequest(w http.ResponseWriter, r *http.Request) {
	entityID := generic.EntityID(chi.URLParam(r, "id"))

	var req TimeOffRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if len(req.Days) == 0 {
		writeError(w, http.StatusBadRequest, "At least one day is required", nil)
		return
	}

	// Parse days
	var days []generic.TimePoint
	for _, d := range req.Days {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid date: %s", d), err)
			return
		}
		tp := generic.TimePoint{Time: t}
		if tp.IsWorkday() {
			days = append(days, tp)
		}
	}

	if len(days) == 0 {
		writeError(w, http.StatusBadRequest, "No workdays selected", nil)
		return
	}

	ctx := r.Context()
	resourceType := req.ResourceType
	if resourceType == "" {
		resourceType = string(timeoff.ResourcePTO)
	}

	// Get assignments and calculate distribution
	assignments, err := h.Store.GetAssignmentsByEntity(ctx, string(entityID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get assignments", err)
		return
	}

	// Calculate balance and distribute
	totalDays := float64(len(days))
	requestAmount := generic.NewAmount(totalDays, generic.UnitDays)
	asOf := days[0]

	ledger := generic.NewLedger(h.Store)
	var allocations []AllocationDTO
	remaining := requestAmount
	requiresApproval := false

	for _, a := range assignments {
		if remaining.IsZero() {
			break
		}

		policy, ok := h.policies[generic.PolicyID(a.PolicyID)]
		if !ok || policy.ResourceType.ResourceID() != resourceType {
			continue
		}

		accrual := h.accruals[policy.ID]
		period := policy.PeriodConfig.PeriodFor(asOf)

		txs, _ := ledger.TransactionsInRange(ctx, entityID, policy.ID, period.Start, period.End)
		balance := calculateBalance(txs, period, policy.Unit, accrual, asOf)
		available := balance.AvailableWithMode(policy.ConsumptionMode)

		if available.IsZero() || available.IsNegative() {
			continue
		}

		toConsume := remaining.Min(available)
		toConsumeF, _ := toConsume.Value.Float64()

		allocations = append(allocations, AllocationDTO{
			PolicyID:   string(policy.ID),
			PolicyName: policy.Name,
			Amount:     toConsumeF,
		})

		remaining = remaining.Sub(toConsume)

		if a.ApprovalConfigJSON != "" {
			requiresApproval = true
		}
	}

	// Check if fully satisfied
	if remaining.IsPositive() {
		writeJSON(w, http.StatusOK, TimeOffResponseDTO{
			Status:          "insufficient_balance",
			ValidationError: strPtr(fmt.Sprintf("Insufficient balance. Short by %.2f days", remaining.Value.InexactFloat64())),
		})
		return
	}

	// Create transactions
	requestID := fmt.Sprintf("req-%d", time.Now().UnixNano())
	var txs []generic.Transaction

	dayIndex := 0 // Track day index across all policies
	for i, alloc := range allocations {
		for j := 0; j < int(alloc.Amount); j++ {
			if dayIndex >= len(days) {
				break
			}
			txType := generic.TxConsumption
			if requiresApproval {
				txType = generic.TxPending
			}

			txs = append(txs, generic.Transaction{
				ID:             generic.TransactionID(fmt.Sprintf("%s-%d-%d", requestID, i, j)),
				EntityID:       entityID,
				PolicyID:       generic.PolicyID(alloc.PolicyID),
				ResourceType:   generic.GetOrCreateResource(resourceType),
				EffectiveAt:    days[dayIndex],
				Delta:          generic.NewAmount(-1, generic.UnitDays),
				Type:           txType,
				ReferenceID:    requestID,
				Reason:         req.Reason,
				IdempotencyKey: fmt.Sprintf("%s-%d-%d", requestID, i, j),
			})
			dayIndex++
		}
	}

	if err := h.Store.AppendBatch(ctx, txs); err != nil {
		if errors.Is(err, generic.ErrDuplicateDayConsumption) {
			writeError(w, http.StatusConflict, "One or more selected dates already have time off scheduled", err)
			return
		}
		writeError(w, http.StatusInternalServerError, "Failed to create request", err)
		return
	}

	status := "approved"
	if requiresApproval {
		status = "pending"
	}

	writeJSON(w, http.StatusCreated, TimeOffResponseDTO{
		RequestID:        requestID,
		Status:           status,
		Distribution:     allocations,
		TotalDays:        totalDays,
		RequiresApproval: requiresApproval,
	})
}

// =============================================================================
// TRANSACTION HANDLERS
// =============================================================================

// CancelTransaction cancels a specific time-off day by creating a reversal transaction.
// This allows users to cancel individual days from a multi-day request.
func (h *Handler) CancelTransaction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	txID := chi.URLParam(r, "id")

	// Get the transaction
	tx, err := h.Store.GetTransaction(ctx, txID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get transaction", err)
		return
	}
	if tx == nil {
		writeError(w, http.StatusNotFound, "Transaction not found", nil)
		return
	}

	// Only allow cancelling consumption or pending transactions
	if tx.Type != generic.TxConsumption && tx.Type != generic.TxPending {
		writeError(w, http.StatusBadRequest, "Can only cancel consumption or pending transactions", nil)
		return
	}

	// Check if already reversed
	isReversed, err := h.Store.IsTransactionReversed(ctx, txID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to check reversal status", err)
		return
	}
	if isReversed {
		writeError(w, http.StatusConflict, "Transaction already cancelled", nil)
		return
	}

	// Create reversal transaction
	reversalTx := generic.Transaction{
		ID:             generic.TransactionID(fmt.Sprintf("reversal-%s", txID)),
		EntityID:       tx.EntityID,
		PolicyID:       tx.PolicyID,
		ResourceType:   tx.ResourceType,
		EffectiveAt:    tx.EffectiveAt,
		Delta:          tx.Delta.Neg(), // Reverse the amount
		Type:           generic.TxReversal,
		ReferenceID:    txID,
		Reason:         "Cancelled by user",
		IdempotencyKey: fmt.Sprintf("reversal-%s", txID),
	}

	if err := h.Store.AppendBatch(ctx, []generic.Transaction{reversalTx}); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to cancel transaction", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "cancelled",
		"transaction_id": txID,
		"reversal_id":   reversalTx.ID,
		"date":          tx.EffectiveAt.Time.Format("2006-01-02"),
		"amount":        tx.Delta.Value.Abs().String(),
	})
}

// =============================================================================
// POLICY HANDLERS
// =============================================================================

// ListPolicies returns all policies.
func (h *Handler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := h.Store.ListPolicies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list policies", err)
		return
	}

	dtos := make([]PolicyDTO, len(policies))
	for i, p := range policies {
		var config factory.PolicyJSON
		json.Unmarshal([]byte(p.ConfigJSON), &config)
		
		dtos[i] = PolicyDTO{
			ID:           p.ID,
			Name:         p.Name,
			ResourceType: p.ResourceType,
			Config:       config,
			Version:      p.Version,
			CreatedAt:    p.CreatedAt.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, dtos)
}

// CreatePolicy creates a new policy.
func (h *Handler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	configJSON, _ := json.Marshal(req.Config)

	// Validate by parsing
	policy, accrual, err := h.PolicyFactory.FromJSON(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid policy configuration", err)
		return
	}

	record := sqlite.PolicyRecord{
		ID:           req.Config.ID,
		Name:         req.Config.Name,
		ResourceType: req.Config.ResourceType,
		ConfigJSON:   string(configJSON),
		Version:      1,
	}

	if err := h.Store.SavePolicy(r.Context(), record); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create policy", err)
		return
	}

	// Update cache
	h.policies[policy.ID] = policy
	h.accruals[policy.ID] = accrual

	writeJSON(w, http.StatusCreated, PolicyDTO{
		ID:           record.ID,
		Name:         record.Name,
		ResourceType: record.ResourceType,
		Config:       req.Config,
		Version:      record.Version,
	})
}

// GetPolicy returns a single policy.
func (h *Handler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	record, err := h.Store.GetPolicy(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get policy", err)
		return
	}
	if record == nil {
		writeError(w, http.StatusNotFound, "Policy not found", nil)
		return
	}

	var config factory.PolicyJSON
	json.Unmarshal([]byte(record.ConfigJSON), &config)

	writeJSON(w, http.StatusOK, PolicyDTO{
		ID:           record.ID,
		Name:         record.Name,
		ResourceType: record.ResourceType,
		Config:       config,
		Version:      record.Version,
		CreatedAt:    record.CreatedAt.Format(time.RFC3339),
	})
}

// =============================================================================
// ASSIGNMENT HANDLERS
// =============================================================================

// GetAssignments returns assignments for an employee.
func (h *Handler) GetAssignments(w http.ResponseWriter, r *http.Request) {
	entityID := chi.URLParam(r, "id")

	assignments, err := h.Store.GetAssignmentsByEntity(r.Context(), entityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get assignments", err)
		return
	}

	dtos := make([]AssignmentDTO, len(assignments))
	for i, a := range assignments {
		dto := AssignmentDTO{
			ID:                  a.ID,
			EntityID:            a.EntityID,
			PolicyID:            a.PolicyID,
			EffectiveFrom:       a.EffectiveFrom.Format("2006-01-02"),
			ConsumptionPriority: a.ConsumptionPriority,
		}
		if a.EffectiveTo != nil {
			s := a.EffectiveTo.Format("2006-01-02")
			dto.EffectiveTo = &s
		}
		if policy, ok := h.policies[generic.PolicyID(a.PolicyID)]; ok {
			dto.PolicyName = policy.Name
		}
		dtos[i] = dto
	}

	writeJSON(w, http.StatusOK, dtos)
}

// CreateAssignment creates a policy assignment.
func (h *Handler) CreateAssignment(w http.ResponseWriter, r *http.Request) {
	var req CreateAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	effectiveFrom, err := time.Parse("2006-01-02", req.EffectiveFrom)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid effective_from date", err)
		return
	}

	var effectiveTo *time.Time
	if req.EffectiveTo != nil {
		t, err := time.Parse("2006-01-02", *req.EffectiveTo)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid effective_to date", err)
			return
		}
		effectiveTo = &t
	}

	var approvalConfig string
	if req.RequiresApproval {
		ac := map[string]any{"requires_approval": true}
		if req.AutoApproveUpTo != nil {
			ac["auto_approve_up_to"] = *req.AutoApproveUpTo
		}
		b, _ := json.Marshal(ac)
		approvalConfig = string(b)
	}

	id := fmt.Sprintf("assign-%s-%s-%d", req.EntityID, req.PolicyID, time.Now().UnixNano())

	record := sqlite.AssignmentRecord{
		ID:                  id,
		EntityID:            req.EntityID,
		PolicyID:            req.PolicyID,
		EffectiveFrom:       effectiveFrom,
		EffectiveTo:         effectiveTo,
		ConsumptionPriority: req.ConsumptionPriority,
		ApprovalConfigJSON:  approvalConfig,
	}

	if err := h.Store.SaveAssignment(r.Context(), record); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create assignment", err)
		return
	}

	writeJSON(w, http.StatusCreated, AssignmentDTO{
		ID:                  id,
		EntityID:            req.EntityID,
		PolicyID:            req.PolicyID,
		EffectiveFrom:       req.EffectiveFrom,
		EffectiveTo:         req.EffectiveTo,
		ConsumptionPriority: req.ConsumptionPriority,
		RequiresApproval:    req.RequiresApproval,
	})
}

// =============================================================================
// ADMIN HANDLERS
// =============================================================================

// TriggerRollover processes period-end rollover.
func (h *Handler) TriggerRollover(w http.ResponseWriter, r *http.Request) {
	var req RolloverRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	periodEnd, err := time.Parse("2006-01-02", req.PeriodEnd)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid period_end date", err)
		return
	}

	ctx := r.Context()
	engine := &generic.ReconciliationEngine{}
	ledger := generic.NewLedger(h.Store)

	var results []RolloverResultDTO

	// Get all assignments to process
	var assignments []sqlite.AssignmentRecord
	if req.EntityID != nil {
		assignments, _ = h.Store.GetAssignmentsByEntity(ctx, *req.EntityID)
	} else {
		// Get all employees and their assignments
		employees, _ := h.Store.ListEmployees(ctx)
		for _, emp := range employees {
			empAssigns, _ := h.Store.GetAssignmentsByEntity(ctx, emp.ID)
			assignments = append(assignments, empAssigns...)
		}
	}

	for _, a := range assignments {
		if req.PolicyID != nil && a.PolicyID != *req.PolicyID {
			continue
		}

		policy, ok := h.policies[generic.PolicyID(a.PolicyID)]
		if !ok {
			continue
		}

		accrual := h.accruals[policy.ID]
		entityID := generic.EntityID(a.EntityID)

		// Get the ending period
		endPoint := generic.TimePoint{Time: periodEnd}
		endingPeriod := policy.PeriodConfig.PeriodFor(endPoint)

		// Get current balance
		txs, _ := ledger.TransactionsInRange(ctx, entityID, policy.ID, endingPeriod.Start, endingPeriod.End)
		balance := calculateBalance(txs, endingPeriod, policy.Unit, accrual, endPoint)
		balance.EntityID = entityID
		balance.PolicyID = policy.ID

		// Process reconciliation
		nextPeriod := endingPeriod.NextPeriod()
		output, err := engine.Process(generic.ReconciliationInput{
			EntityID:       entityID,
			PolicyID:       policy.ID,
			Policy:         *policy,
			CurrentBalance: balance,
			EndingPeriod:   endingPeriod,
			NextPeriod:     nextPeriod,
		})

		if err != nil {
			continue
		}

		// Add idempotency keys and append transactions
		for i := range output.Transactions {
			output.Transactions[i].IdempotencyKey = fmt.Sprintf("rollover-%s-%s-%s-%d",
				a.EntityID, a.PolicyID, req.PeriodEnd, i)
		}

		if len(output.Transactions) > 0 {
			h.Store.AppendBatch(ctx, output.Transactions)
		}

		carriedOver, _ := output.Summary.CarriedOver.Value.Float64()
		expired, _ := output.Summary.Expired.Value.Float64()

		results = append(results, RolloverResultDTO{
			EntityID:     a.EntityID,
			PolicyID:     a.PolicyID,
			CarriedOver:  carriedOver,
			Expired:      expired,
			Transactions: toTransactionDTOs(output.Transactions),
		})
	}

	writeJSON(w, http.StatusOK, results)
}

// CreateAdjustment creates a manual adjustment.
func (h *Handler) CreateAdjustment(w http.ResponseWriter, r *http.Request) {
	var req AdjustmentRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	policy, ok := h.policies[generic.PolicyID(req.PolicyID)]
	if !ok {
		writeError(w, http.StatusBadRequest, "Policy not found", nil)
		return
	}

	tx := generic.Transaction{
		ID:             generic.TransactionID(fmt.Sprintf("adj-%d", time.Now().UnixNano())),
		EntityID:       generic.EntityID(req.EntityID),
		PolicyID:       generic.PolicyID(req.PolicyID),
		ResourceType:   policy.ResourceType,
		EffectiveAt:    generic.Today(),
		Delta:          generic.NewAmount(req.Delta, policy.Unit),
		Type:           generic.TxAdjustment,
		Reason:         req.Reason,
		IdempotencyKey: fmt.Sprintf("adj-%s-%s-%d", req.EntityID, req.PolicyID, time.Now().UnixNano()),
	}

	if err := h.Store.Append(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create adjustment", err)
		return
	}

	writeJSON(w, http.StatusCreated, toTransactionDTO(tx))
}

// ResetDatabase clears all data.
func (h *Handler) ResetDatabase(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.Reset(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to reset database", err)
		return
	}

	// Clear caches
	h.policies = make(map[generic.PolicyID]*generic.Policy)
	h.accruals = make(map[generic.PolicyID]generic.AccrualSchedule)
	h.currentScenario = ""

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// HELPERS
// =============================================================================

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// toTransactionDTOsWithBalance calculates balance at each transaction date
func (h *Handler) toTransactionDTOsWithBalance(ctx context.Context, txs []generic.Transaction) []TransactionDTO {
	if len(txs) == 0 {
		return []TransactionDTO{}
	}

	// Group transactions by policy
	policyTxs := make(map[generic.PolicyID][]generic.Transaction)
	for _, tx := range txs {
		policyTxs[tx.PolicyID] = append(policyTxs[tx.PolicyID], tx)
	}

	// Process each policy separately
	var dtos []TransactionDTO

	for policyID, policyTransactions := range policyTxs {
		// Sort transactions by effective date within this policy
		sort.Slice(policyTransactions, func(i, j int) bool {
			return policyTransactions[i].EffectiveAt.Time.Before(policyTransactions[j].EffectiveAt.Time)
		})

		// Get policy and accrual
		policy, ok := h.policies[policyID]
		if !ok {
			// If policy not found, just convert without balance
			for _, tx := range policyTransactions {
				dto := toTransactionDTO(tx)
				dtos = append(dtos, dto)
			}
			continue
		}

		accrual := h.accruals[policyID]

		// Process transactions chronologically and calculate balance at each point
		for i, tx := range policyTransactions {
			// Get all transactions up to and including this one
			txsUpToNow := policyTransactions[:i+1]

			// Calculate period for this transaction date
			txDate := generic.TimePoint{Time: tx.EffectiveAt.Time}
			period := policy.PeriodConfig.PeriodFor(txDate)

			// Calculate balance using existing logic
			balance := calculateBalance(txsUpToNow, period, policy.Unit, accrual, txDate)

			// Get available balance based on consumption mode
			availableAmount := balance.AvailableWithMode(policy.ConsumptionMode)
			available, ok := availableAmount.Value.Float64()
			if !ok {
				// If conversion fails, skip balance
				dto := toTransactionDTO(tx)
				dtos = append(dtos, dto)
				continue
			}

			// Create DTO with balance
			dto := toTransactionDTO(tx)
			dto.Balance = available
			dtos = append(dtos, dto)
		}
	}

	// Sort all DTOs by effective date (maintain chronological order across policies)
	sort.Slice(dtos, func(i, j int) bool {
		ti, err1 := time.Parse(time.RFC3339, dtos[i].EffectiveAt)
		tj, err2 := time.Parse(time.RFC3339, dtos[j].EffectiveAt)
		if err1 != nil || err2 != nil {
			// If parsing fails, maintain original order
			return false
		}
		return ti.Before(tj)
	})

	// Now detect policy changes and calculate post-balance
	// Policy changes are reconciliation transactions that mention "Policy change"
	for i := range dtos {
		if dtos[i].Type == "reconciliation" && 
		   (strings.Contains(dtos[i].Reason, "Policy change") || strings.Contains(dtos[i].Reason, "policy change")) {
			// This is a policy change transaction
			// Find the first transaction of the new policy (next transaction with different policy_id)
			for j := i + 1; j < len(dtos); j++ {
				if dtos[j].PolicyID != dtos[i].PolicyID && dtos[j].Type == "accrual" {
					// Found first transaction of new policy
					// The balance at this transaction is the "after" balance
					dtos[i].BalanceAfter = dtos[j].Balance
					break
				}
			}
		}
	}

	return dtos
}

func writeError(w http.ResponseWriter, status int, message string, err error) {
	resp := ErrorResponse{Error: message}
	if err != nil {
		resp.Details = err.Error()
	}
	writeJSON(w, status, resp)
}

func strPtr(s string) *string {
	return &s
}

// =============================================================================
// HOLIDAY ENDPOINTS
// =============================================================================

// ListHolidays returns all holidays.
// GET /api/holidays
func (h *Handler) ListHolidays(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	companyID := r.URL.Query().Get("company_id")

	holidays, err := h.Store.GetAllHolidays(ctx, companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get holidays", err)
		return
	}

	type HolidayDTO struct {
		ID        string `json:"id"`
		CompanyID string `json:"company_id"`
		Date      string `json:"date"`
		Name      string `json:"name"`
		Recurring bool   `json:"recurring"`
	}

	dtos := make([]HolidayDTO, 0, len(holidays))
	for _, hol := range holidays {
		dtos = append(dtos, HolidayDTO{
			ID:        hol.ID,
			CompanyID: hol.CompanyID,
			Date:      hol.Date.Time.Format("2006-01-02"),
			Name:      hol.Name,
			Recurring: hol.Recurring,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"holidays": dtos})
}

// CreateHoliday creates a new holiday.
// POST /api/holidays
func (h *Handler) CreateHoliday(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		CompanyID string `json:"company_id"`
		Date      string `json:"date"`
		Name      string `json:"name"`
		Recurring bool   `json:"recurring"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Date == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "Date and name are required", nil)
		return
	}

	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid date format (use YYYY-MM-DD)", err)
		return
	}

	holiday := generic.Holiday{
		ID:        fmt.Sprintf("holiday-%d", time.Now().UnixNano()),
		CompanyID: req.CompanyID,
		Date:      generic.TimePoint{Time: date, Granularity: generic.GranularityDay},
		Name:      req.Name,
		Recurring: req.Recurring,
	}

	if err := h.Store.SaveHoliday(ctx, holiday); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create holiday", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status":  "created",
		"holiday": holiday.ID,
	})
}

// DeleteHoliday deletes a holiday.
// DELETE /api/holidays/{id}
func (h *Handler) DeleteHoliday(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if err := h.Store.DeleteHoliday(ctx, id); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete holiday", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

// AddDefaultHolidays adds common US holidays.
// POST /api/holidays/defaults
func (h *Handler) AddDefaultHolidays(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		CompanyID string `json:"company_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Common US holidays (recurring)
	defaults := []struct {
		month int
		day   int
		name  string
	}{
		{1, 1, "New Year's Day"},
		{7, 4, "Independence Day"},
		{12, 25, "Christmas Day"},
		{12, 31, "New Year's Eve"},
	}

	year := time.Now().Year()
	for _, d := range defaults {
		holiday := generic.Holiday{
			ID:        fmt.Sprintf("holiday-%s-%02d%02d", req.CompanyID, d.month, d.day),
			CompanyID: req.CompanyID,
			Date:      generic.NewTimePoint(year, time.Month(d.month), d.day),
			Name:      d.name,
			Recurring: true,
		}
		h.Store.SaveHoliday(ctx, holiday)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "created",
		"count":  len(defaults),
	})
}

// =============================================================================
// APPROVAL WORKFLOW ENDPOINTS
// =============================================================================

// ListPendingRequests returns all pending requests awaiting approval.
// GET /api/requests/pending
func (h *Handler) ListPendingRequests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	requests, err := h.Store.GetPendingRequests(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get pending requests", err)
		return
	}

	// Enrich with employee names
	type RequestDTO struct {
		ID           string  `json:"id"`
		EntityID     string  `json:"entity_id"`
		EmployeeName string  `json:"employee_name"`
		ResourceType string  `json:"resource_type"`
		EffectiveAt  string  `json:"effective_at"`
		Amount       float64 `json:"amount"`
		Unit         string  `json:"unit"`
		Reason       string  `json:"reason"`
		CreatedAt    string  `json:"created_at"`
	}

	dtos := make([]RequestDTO, 0, len(requests))
	for _, req := range requests {
		emp, _ := h.Store.GetEmployee(ctx, req.EntityID)
		empName := req.EntityID
		if emp != nil {
			empName = emp.Name
		}

		dtos = append(dtos, RequestDTO{
			ID:           req.ID,
			EntityID:     req.EntityID,
			EmployeeName: empName,
			ResourceType: req.ResourceType,
			EffectiveAt:  req.EffectiveAt.Format("2006-01-02"),
			Amount:       req.Amount,
			Unit:         req.Unit,
			Reason:       req.Reason,
			CreatedAt:    req.CreatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"requests": dtos})
}

// ApproveRequest approves a pending request.
// POST /api/requests/{id}/approve
func (h *Handler) ApproveRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var req struct {
		ApproverID string `json:"approver_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.ApproverID == "" {
		req.ApproverID = "admin"
	}

	// Get the request
	request, err := h.Store.GetRequest(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get request", err)
		return
	}
	if request == nil {
		writeError(w, http.StatusNotFound, "Request not found", nil)
		return
	}
	if request.Status != "pending" {
		writeError(w, http.StatusConflict, "Request is not pending", nil)
		return
	}

	// Update request status
	now := time.Now()
	request.Status = "approved"
	request.ApprovedBy = req.ApproverID
	request.ApprovedAt = &now
	request.UpdatedAt = now

	// Convert pending transactions to consumption
	// First, find and reverse pending transactions
	effectiveDate := generic.TimePoint{Time: request.EffectiveAt}

	// Get pending transactions for this request (by reference_id)
	txs, _ := h.Store.LoadRange(ctx, generic.EntityID(request.EntityID), generic.PolicyID(""), effectiveDate, effectiveDate.AddDays(1))
	
	var batchTxs []generic.Transaction
	for _, tx := range txs {
		if tx.ReferenceID == id && tx.Type == generic.TxPending {
			// Create reversal
			batchTxs = append(batchTxs, generic.Transaction{
				ID:             generic.TransactionID(fmt.Sprintf("%s-approve-rev-%d", id, len(batchTxs))),
				EntityID:       tx.EntityID,
				PolicyID:       tx.PolicyID,
				ResourceType:   tx.ResourceType,
				EffectiveAt:    tx.EffectiveAt,
				Delta:          tx.Delta.Neg(),
				Type:           generic.TxReversal,
				ReferenceID:    id,
				Reason:         "Approved",
				IdempotencyKey: fmt.Sprintf("%s-approve-rev-%d", id, len(batchTxs)),
			})
			// Create consumption
			batchTxs = append(batchTxs, generic.Transaction{
				ID:             generic.TransactionID(fmt.Sprintf("%s-approve-cons-%d", id, len(batchTxs))),
				EntityID:       tx.EntityID,
				PolicyID:       tx.PolicyID,
				ResourceType:   tx.ResourceType,
				EffectiveAt:    tx.EffectiveAt,
				Delta:          tx.Delta,
				Type:           generic.TxConsumption,
				ReferenceID:    id,
				Reason:         request.Reason,
				IdempotencyKey: fmt.Sprintf("%s-approve-cons-%d", id, len(batchTxs)),
			})
		}
	}

	if len(batchTxs) > 0 {
		if err := h.Store.AppendBatch(ctx, batchTxs); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to process approval", err)
			return
		}
	}

	// Save updated request
	if err := h.Store.SaveRequest(ctx, *request); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to update request", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "approved",
		"approved_by": req.ApproverID,
	})
}

// RejectRequest rejects a pending request.
// POST /api/requests/{id}/reject
func (h *Handler) RejectRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var req struct {
		RejecterID string `json:"rejecter_id"`
		Reason     string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.RejecterID == "" {
		req.RejecterID = "admin"
	}

	// Get the request
	request, err := h.Store.GetRequest(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get request", err)
		return
	}
	if request == nil {
		writeError(w, http.StatusNotFound, "Request not found", nil)
		return
	}
	if request.Status != "pending" {
		writeError(w, http.StatusConflict, "Request is not pending", nil)
		return
	}

	// Update request status
	now := time.Now()
	request.Status = "rejected"
	request.RejectionReason = req.Reason
	request.UpdatedAt = now

	// Reverse pending transactions
	effectiveDate := generic.TimePoint{Time: request.EffectiveAt}
	txs, _ := h.Store.LoadRange(ctx, generic.EntityID(request.EntityID), generic.PolicyID(""), effectiveDate, effectiveDate.AddDays(1))
	
	var batchTxs []generic.Transaction
	for _, tx := range txs {
		if tx.ReferenceID == id && tx.Type == generic.TxPending {
			batchTxs = append(batchTxs, generic.Transaction{
				ID:             generic.TransactionID(fmt.Sprintf("%s-reject-%d", id, len(batchTxs))),
				EntityID:       tx.EntityID,
				PolicyID:       tx.PolicyID,
				ResourceType:   tx.ResourceType,
				EffectiveAt:    tx.EffectiveAt,
				Delta:          tx.Delta.Neg(),
				Type:           generic.TxReversal,
				ReferenceID:    id,
				Reason:         "Rejected: " + req.Reason,
				IdempotencyKey: fmt.Sprintf("%s-reject-%d", id, len(batchTxs)),
			})
		}
	}

	if len(batchTxs) > 0 {
		if err := h.Store.AppendBatch(ctx, batchTxs); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to process rejection", err)
			return
		}
	}

	// Save updated request
	if err := h.Store.SaveRequest(ctx, *request); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to update request", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "rejected",
		"rejected_by": req.RejecterID,
		"reason":      req.Reason,
	})
}

// =============================================================================
// RECONCILIATION ENDPOINTS
// =============================================================================

// ListReconciliationRuns returns reconciliation run history.
// GET /api/reconciliation/runs
func (h *Handler) ListReconciliationRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := r.URL.Query().Get("status")

	runs, err := h.Store.GetReconciliationRuns(ctx, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get reconciliation runs", err)
		return
	}

	type RunDTO struct {
		ID          string  `json:"id"`
		PolicyID    string  `json:"policy_id"`
		EntityID    string  `json:"entity_id"`
		PeriodStart string  `json:"period_start"`
		PeriodEnd   string  `json:"period_end"`
		Status      string  `json:"status"`
		CarriedOver float64 `json:"carried_over"`
		Expired     float64 `json:"expired"`
		Error       string  `json:"error,omitempty"`
		CompletedAt string  `json:"completed_at,omitempty"`
	}

	dtos := make([]RunDTO, 0, len(runs))
	for _, run := range runs {
		dto := RunDTO{
			ID:          run.ID,
			PolicyID:    run.PolicyID,
			EntityID:    run.EntityID,
			PeriodStart: run.PeriodStart.Format("2006-01-02"),
			PeriodEnd:   run.PeriodEnd.Format("2006-01-02"),
			Status:      run.Status,
			CarriedOver: run.CarriedOver,
			Expired:     run.Expired,
			Error:       run.Error,
		}
		if run.CompletedAt != nil {
			dto.CompletedAt = run.CompletedAt.Format(time.RFC3339)
		}
		dtos = append(dtos, dto)
	}

	writeJSON(w, http.StatusOK, map[string]any{"runs": dtos})
}
