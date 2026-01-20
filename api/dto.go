/*
dto.go - Data Transfer Objects for API requests and responses

PURPOSE:
  Defines the JSON structures for API communication. These types decouple
  the internal domain model from the external API contract, allowing:
  - Field renaming without breaking clients
  - API-specific validation
  - Version evolution

NAMING CONVENTION:
  - *DTO: Response types returned to clients
  - *Request: Request body types from clients
  - *Response: Complex response wrappers

TYPES:
  Employee:
    EmployeeDTO, CreateEmployeeRequest

  Balance:
    BalanceSummaryDTO, PolicyBalanceDTO, BalanceDisplayDTO

  Request:
    SubmitRequestDTO, RequestDTO

  Policy:
    PolicyDTO (wraps factory.PolicyJSON)

  Transactions:
    TransactionDTO

  Scenarios:
    ScenarioDTO, LoadScenarioRequest

VALIDATION:
  Validation is done in handlers, not in DTOs. DTOs are pure data carriers.
  Future: Add struct tags for validation library.

SEE ALSO:
  - handlers.go: Uses these types
  - factory/policy.go: PolicyJSON type
*/
package api

import (
	"time"

	"github.com/warp/resource-engine/factory"
	"github.com/warp/resource-engine/generic"
)

// =============================================================================
// REQUEST/RESPONSE TYPES
// =============================================================================

// EmployeeDTO represents an employee in API responses.
type EmployeeDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	HireDate  string `json:"hire_date"`
	CreatedAt string `json:"created_at,omitempty"`
}

// CreateEmployeeRequest is the request to create an employee.
type CreateEmployeeRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	HireDate string `json:"hire_date"`
}

// PolicyDTO represents a policy in API responses.
type PolicyDTO struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	ResourceType string             `json:"resource_type"`
	Config       factory.PolicyJSON `json:"config"`
	Version      int                `json:"version"`
	CreatedAt    string             `json:"created_at,omitempty"`
}

// CreatePolicyRequest is the request to create a policy.
type CreatePolicyRequest struct {
	Config factory.PolicyJSON `json:"config"`
}

// AssignmentDTO represents a policy assignment.
type AssignmentDTO struct {
	ID                  string  `json:"id"`
	EntityID            string  `json:"entity_id"`
	PolicyID            string  `json:"policy_id"`
	PolicyName          string  `json:"policy_name,omitempty"`
	EffectiveFrom       string  `json:"effective_from"`
	EffectiveTo         *string `json:"effective_to,omitempty"`
	ConsumptionPriority int     `json:"consumption_priority"`
	RequiresApproval    bool    `json:"requires_approval"`
	AutoApproveUpTo     *float64 `json:"auto_approve_up_to,omitempty"`
}

// CreateAssignmentRequest is the request to assign a policy.
type CreateAssignmentRequest struct {
	EntityID            string   `json:"entity_id"`
	PolicyID            string   `json:"policy_id"`
	EffectiveFrom       string   `json:"effective_from"`
	EffectiveTo         *string  `json:"effective_to,omitempty"`
	ConsumptionPriority int      `json:"consumption_priority"`
	RequiresApproval    bool     `json:"requires_approval"`
	AutoApproveUpTo     *float64 `json:"auto_approve_up_to,omitempty"`
}

// BalanceDTO represents balance information.
type BalanceDTO struct {
	EntityID       string               `json:"entity_id"`
	ResourceType   string               `json:"resource_type"`
	TotalAvailable float64              `json:"total_available"`
	TotalPending   float64              `json:"total_pending"`
	Policies       []PolicyBalanceDTO   `json:"policies"`
	AsOf           string               `json:"as_of"`
}

// PolicyBalanceDTO represents balance for a single policy.
type PolicyBalanceDTO struct {
	PolicyID         string  `json:"policy_id"`
	PolicyName       string  `json:"policy_name"`
	Priority         int     `json:"priority"`
	Available        float64 `json:"available"`
	AccruedToDate    float64 `json:"accrued_to_date"`
	TotalEntitlement float64 `json:"total_entitlement"`
	Consumed         float64 `json:"consumed"`
	Pending          float64 `json:"pending"`
	ConsumptionMode  string  `json:"consumption_mode"`
	RequiresApproval bool    `json:"requires_approval"`
}

// TransactionDTO represents a ledger transaction.
type TransactionDTO struct {
	ID           string  `json:"id"`
	EntityID     string  `json:"entity_id"`
	PolicyID     string  `json:"policy_id"`
	ResourceType string  `json:"resource_type"`
	EffectiveAt  string  `json:"effective_at"`
	Delta        float64 `json:"delta"`
	Unit         string  `json:"unit"`
	Type         string  `json:"type"`
	ReferenceID  string  `json:"reference_id,omitempty"`
	Reason       string  `json:"reason,omitempty"`
	CreatedAt    string  `json:"created_at,omitempty"`
	Balance      float64 `json:"balance,omitempty"`      // Balance at this transaction date
	BalanceAfter float64 `json:"balance_after,omitempty"` // Balance after this transaction (for policy changes)
}

// TimeOffRequestDTO represents a time-off request.
type TimeOffRequestDTO struct {
	EntityID     string    `json:"entity_id"`
	ResourceType string    `json:"resource_type"`
	Days         []string  `json:"days"` // ISO dates
	Reason       string    `json:"reason,omitempty"`
}

// TimeOffResponseDTO is the response after submitting a request.
type TimeOffResponseDTO struct {
	RequestID    string               `json:"request_id"`
	Status       string               `json:"status"`
	Distribution []AllocationDTO      `json:"distribution"`
	TotalDays    float64              `json:"total_days"`
	RequiresApproval bool             `json:"requires_approval"`
	ValidationError  *string          `json:"validation_error,omitempty"`
}

// AllocationDTO represents allocation from a single policy.
type AllocationDTO struct {
	PolicyID   string  `json:"policy_id"`
	PolicyName string  `json:"policy_name"`
	Amount     float64 `json:"amount"`
}

// RolloverRequestDTO is the request to trigger rollover.
type RolloverRequestDTO struct {
	EntityID   *string `json:"entity_id,omitempty"`   // nil = all entities
	PolicyID   *string `json:"policy_id,omitempty"`   // nil = all policies
	PeriodEnd  string  `json:"period_end"`            // ISO date
}

// RolloverResultDTO is the result of a rollover operation.
type RolloverResultDTO struct {
	EntityID    string  `json:"entity_id"`
	PolicyID    string  `json:"policy_id"`
	CarriedOver float64 `json:"carried_over"`
	Expired     float64 `json:"expired"`
	Transactions []TransactionDTO `json:"transactions"`
}

// AdjustmentRequestDTO is the request to make a manual adjustment.
type AdjustmentRequestDTO struct {
	EntityID string  `json:"entity_id"`
	PolicyID string  `json:"policy_id"`
	Delta    float64 `json:"delta"`
	Reason   string  `json:"reason"`
}

// ScenarioDTO represents a demo scenario.
type ScenarioDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category,omitempty"` // "timeoff" or "rewards"
}

// ErrorResponse is the standard error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details any    `json:"details,omitempty"`
}

// =============================================================================
// CONVERSION HELPERS
// =============================================================================

func toTransactionDTO(tx generic.Transaction) TransactionDTO {
	delta, _ := tx.Delta.Value.Float64()
	return TransactionDTO{
		ID:           string(tx.ID),
		EntityID:     string(tx.EntityID),
		PolicyID:     string(tx.PolicyID),
		ResourceType: tx.ResourceType.ResourceID(),
		EffectiveAt:  tx.EffectiveAt.Time.Format(time.RFC3339),
		Delta:        delta,
		Unit:         string(tx.Delta.Unit),
		Type:         string(tx.Type),
		ReferenceID:  tx.ReferenceID,
		Reason:       tx.Reason,
	}
}

func toTransactionDTOs(txs []generic.Transaction) []TransactionDTO {
	dtos := make([]TransactionDTO, len(txs))
	for i, tx := range txs {
		dtos[i] = toTransactionDTO(tx)
	}
	return dtos
}
