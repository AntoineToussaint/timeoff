/*
Package factory provides JSON to Go policy conversion.

PURPOSE:
  Converts JSON policy definitions into generic.Policy and AccrualSchedule
  objects. This enables policy configuration without code changes - HR
  can define policies in JSON, and the factory creates the proper Go structs.

WHY JSON?
  - Non-developers can modify policies
  - Easy integration with admin UI
  - Version control for policy definitions
  - Database storage of policy configs

JSON SCHEMA:
  {
    "id": "pto-standard",
    "name": "Standard PTO",
    "resource_type": "pto",
    "unit": "days",
    "period_type": "calendar_year",
    "consumption_mode": "consume_ahead",
    "accrual": {
      "type": "yearly",
      "annual_amount": 20,
      "frequency": "monthly"
    },
    "reconciliation_rules": [
      {
        "trigger": "period_end",
        "actions": [
          {"type": "carryover", "max_carryover": 5},
          {"type": "expire"}
        ]
      }
    ]
  }

KEY FEATURES:
  - Validates JSON structure
  - Sets sensible defaults
  - Creates matching AccrualSchedule
  - Handles reconciliation rules
  - Sets UniquePerTimePoint based on resource type

USAGE:
  factory := NewPolicyFactory()

  // From JSON string
  policy, accrual, err := factory.ParsePolicy(jsonString)

  // From domain-specific preset (recommended)
  import "github.com/warp/resource-engine/timeoff"
  jsonStr := timeoff.StandardPTOJSON("pto-standard", "Standard PTO", 24, 5)
  policy, accrual, err := factory.ParsePolicy(jsonStr)

  // Use in system
  ledger.Append(ctx, Transaction{PolicyID: policy.ID, ...})

SEE ALSO:
  - generic/policy.go: Policy type definition
  - timeoff/policies.go: Go-based policy configurations
  - rewards/policies.go: Rewards policy configurations
*/
package factory

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/warp/resource-engine/generic"
	"github.com/warp/resource-engine/timeoff"
)

// =============================================================================
// JSON SCHEMA TYPES
// =============================================================================

// PolicyJSON is the JSON representation of a policy.
type PolicyJSON struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	ResourceType    string              `json:"resource_type"`
	Unit            string              `json:"unit"`
	PeriodType      string              `json:"period_type"`
	FiscalYearStart int                 `json:"fiscal_year_start,omitempty"` // Month 1-12
	ConsumptionMode string              `json:"consumption_mode,omitempty"`
	IsUnlimited     bool                `json:"is_unlimited,omitempty"`
	UniquePerDay    *bool               `json:"unique_per_day,omitempty"`    // Default true for time-off, false for rewards
	Accrual         *AccrualJSON        `json:"accrual,omitempty"`
	Constraints     *ConstraintsJSON    `json:"constraints,omitempty"`
	Reconciliation  []ReconciliationJSON `json:"reconciliation_rules,omitempty"`
}

// AccrualJSON represents accrual configuration.
type AccrualJSON struct {
	Type       string        `json:"type"` // yearly, hours_worked, tenure
	AnnualDays float64       `json:"annual_days,omitempty"`
	Frequency  string        `json:"frequency,omitempty"` // upfront, monthly, daily
	Tiers      []TenureTier  `json:"tiers,omitempty"`     // For tenure-based
	HireDate   string        `json:"hire_date,omitempty"` // For tenure-based
}

// TenureTier represents a tenure-based accrual tier.
type TenureTier struct {
	AfterYears int     `json:"after_years"`
	AnnualDays float64 `json:"annual_days"`
}

// ConstraintsJSON represents policy constraints.
type ConstraintsJSON struct {
	AllowNegative  bool     `json:"allow_negative,omitempty"`
	MaxBalance     *float64 `json:"max_balance,omitempty"`
	MinBalance     *float64 `json:"min_balance,omitempty"`
	MaxRequestSize *float64 `json:"max_request_size,omitempty"`
}

// ReconciliationJSON represents reconciliation rules.
type ReconciliationJSON struct {
	Trigger string       `json:"trigger"` // period_end, policy_change
	Actions []ActionJSON `json:"actions"`
}

// ActionJSON represents a reconciliation action.
type ActionJSON struct {
	Type         string   `json:"type"` // carryover, expire, cap, prorate
	MaxCarryover *float64 `json:"max_carryover,omitempty"`
}

// =============================================================================
// POLICY FACTORY
// =============================================================================

// PolicyFactory converts JSON policies to Go structs.
type PolicyFactory struct{}

// NewPolicyFactory creates a new policy factory.
func NewPolicyFactory() *PolicyFactory {
	return &PolicyFactory{}
}

// ParsePolicy parses a JSON string into a Policy and AccrualSchedule.
func (f *PolicyFactory) ParsePolicy(jsonStr string) (*generic.Policy, generic.AccrualSchedule, error) {
	var pj PolicyJSON
	if err := json.Unmarshal([]byte(jsonStr), &pj); err != nil {
		return nil, nil, fmt.Errorf("failed to parse policy JSON: %w", err)
	}

	return f.FromJSON(pj)
}

// FromJSON converts PolicyJSON to generic.Policy and AccrualSchedule.
func (f *PolicyFactory) FromJSON(pj PolicyJSON) (*generic.Policy, generic.AccrualSchedule, error) {
	// Look up resource type from registry (domain packages register on init)
	resourceType := generic.GetOrCreateResource(pj.ResourceType)

	// Build Policy
	policy := &generic.Policy{
		ID:           generic.PolicyID(pj.ID),
		Name:         pj.Name,
		ResourceType: resourceType,
		Unit:         parseUnit(pj.Unit),
		IsUnlimited:  pj.IsUnlimited,
		PeriodConfig: parsePeriodConfig(pj.PeriodType, pj.FiscalYearStart),
		ConsumptionMode: parseConsumptionMode(pj.ConsumptionMode),
	}

	// Set UniquePerTimePoint based on explicit config or resource type default
	if pj.UniquePerDay != nil {
		policy.UniquePerTimePoint = *pj.UniquePerDay
	} else {
		// Default: time-off resources are unique per day, rewards are not
		policy.UniquePerTimePoint = isTimeOffResource(pj.ResourceType)
	}

	// Parse constraints
	if pj.Constraints != nil {
		policy.Constraints = parseConstraints(*pj.Constraints, policy.Unit)
	}

	// Parse reconciliation rules
	for _, rj := range pj.Reconciliation {
		rule := parseReconciliationRule(rj, policy.Unit)
		policy.ReconciliationRules = append(policy.ReconciliationRules, rule)
	}

	// Build AccrualSchedule
	var accrual generic.AccrualSchedule
	if pj.Accrual != nil && !pj.IsUnlimited {
		var err error
		accrual, err = parseAccrualSchedule(*pj.Accrual)
		if err != nil {
			return nil, nil, err
		}
	}

	return policy, accrual, nil
}

// isTimeOffResource returns true if the resource type represents time-off
// which has the uniqueness constraint (can't take same day off twice).
func isTimeOffResource(resourceType string) bool {
	switch resourceType {
	case "pto", "sick", "parental", "bereavement", "jury_duty", 
	     "floating_holiday", "vacation", "personal", "time_off":
		return true
	default:
		// Points, credits, dollars, etc. are not unique per day
		return false
	}
}

// ToJSON converts a Policy to PolicyJSON.
func (f *PolicyFactory) ToJSON(policy *generic.Policy, accrual generic.AccrualSchedule) PolicyJSON {
	pj := PolicyJSON{
		ID:              string(policy.ID),
		Name:            policy.Name,
		ResourceType:    policy.ResourceType.ResourceID(),
		Unit:            string(policy.Unit),
		PeriodType:      string(policy.PeriodConfig.Type),
		FiscalYearStart: int(policy.PeriodConfig.FiscalYearStartMonth),
		ConsumptionMode: string(policy.ConsumptionMode),
		IsUnlimited:     policy.IsUnlimited,
	}

	// Constraints
	if policy.Constraints.AllowNegative || policy.Constraints.MaxBalance != nil {
		pj.Constraints = &ConstraintsJSON{
			AllowNegative: policy.Constraints.AllowNegative,
		}
		if policy.Constraints.MaxBalance != nil {
			v, _ := policy.Constraints.MaxBalance.Value.Float64()
			pj.Constraints.MaxBalance = &v
		}
	}

	// Reconciliation rules
	for _, rule := range policy.ReconciliationRules {
		rj := ReconciliationJSON{
			Trigger: string(rule.Trigger.Type),
		}
		for _, action := range rule.Actions {
			aj := ActionJSON{Type: string(action.Type)}
			if action.Config.MaxCarryover != nil {
				v, _ := action.Config.MaxCarryover.Value.Float64()
				aj.MaxCarryover = &v
			}
			rj.Actions = append(rj.Actions, aj)
		}
		pj.Reconciliation = append(pj.Reconciliation, rj)
	}

	// Accrual (if YearlyAccrual)
	if ya, ok := accrual.(*timeoff.YearlyAccrual); ok {
		pj.Accrual = &AccrualJSON{
			Type:       "yearly",
			AnnualDays: ya.AnnualDays,
			Frequency:  string(ya.Frequency),
		}
	}

	return pj
}

// =============================================================================
// PARSING HELPERS
// =============================================================================

func parseUnit(s string) generic.Unit {
	switch s {
	case "hours":
		return generic.UnitHours
	case "minutes":
		return generic.UnitMinutes
	case "points":
		return generic.Unit("points")
	case "dollars":
		return generic.Unit("dollars")
	default:
		return generic.UnitDays
	}
}

func parsePeriodConfig(periodType string, fiscalMonth int) generic.PeriodConfig {
	pc := generic.PeriodConfig{}
	switch periodType {
	case "calendar_year":
		pc.Type = generic.PeriodCalendarYear
	case "fiscal_year":
		pc.Type = generic.PeriodFiscalYear
		if fiscalMonth >= 1 && fiscalMonth <= 12 {
			pc.FiscalYearStartMonth = time.Month(fiscalMonth)
		} else {
			pc.FiscalYearStartMonth = time.January
		}
	case "anniversary":
		pc.Type = generic.PeriodAnniversary
	case "rolling":
		pc.Type = generic.PeriodRolling
	default:
		pc.Type = generic.PeriodCalendarYear
	}
	return pc
}

func parseConsumptionMode(s string) generic.ConsumptionMode {
	switch s {
	case "consume_up_to_accrued":
		return generic.ConsumeUpToAccrued
	default:
		return generic.ConsumeAhead
	}
}

func parseConstraints(cj ConstraintsJSON, unit generic.Unit) generic.Constraints {
	c := generic.Constraints{
		AllowNegative: cj.AllowNegative,
	}
	if cj.MaxBalance != nil {
		max := generic.NewAmount(*cj.MaxBalance, unit)
		c.MaxBalance = &max
	}
	if cj.MinBalance != nil {
		min := generic.NewAmount(*cj.MinBalance, unit)
		c.MinBalance = &min
	}
	if cj.MaxRequestSize != nil {
		maxReq := generic.NewAmount(*cj.MaxRequestSize, unit)
		c.MaxRequestSize = &maxReq
	}
	return c
}

func parseReconciliationRule(rj ReconciliationJSON, unit generic.Unit) generic.ReconciliationRule {
	rule := generic.ReconciliationRule{
		ID:      fmt.Sprintf("rule-%s", rj.Trigger),
		Trigger: generic.ReconciliationTrigger{Type: parseTriggerType(rj.Trigger)},
	}

	for _, aj := range rj.Actions {
		action := generic.ReconciliationAction{
			Type: parseActionType(aj.Type),
		}
		if aj.MaxCarryover != nil {
			max := generic.NewAmount(*aj.MaxCarryover, unit)
			action.Config.MaxCarryover = &max
		}
		rule.Actions = append(rule.Actions, action)
	}

	return rule
}

func parseTriggerType(s string) generic.TriggerType {
	switch s {
	case "policy_change":
		return generic.TriggerPolicyChange
	case "entity_join":
		return generic.TriggerEntityJoin
	case "manual":
		return generic.TriggerManual
	default:
		return generic.TriggerPeriodEnd
	}
}

func parseActionType(s string) generic.ActionType {
	switch s {
	case "expire":
		return generic.ActionExpire
	case "cap":
		return generic.ActionCap
	case "prorate":
		return generic.ActionProrate
	default:
		return generic.ActionCarryover
	}
}

func parseAccrualSchedule(aj AccrualJSON) (generic.AccrualSchedule, error) {
	switch aj.Type {
	case "yearly":
		return &timeoff.YearlyAccrual{
			AnnualDays: aj.AnnualDays,
			Frequency:  parseAccrualFrequency(aj.Frequency),
		}, nil

	case "tenure":
		if aj.HireDate == "" {
			return nil, fmt.Errorf("tenure accrual requires hire_date")
		}
		hireDate, err := time.Parse("2006-01-02", aj.HireDate)
		if err != nil {
			return nil, fmt.Errorf("invalid hire_date format: %w", err)
		}

		var tiers []timeoff.TenureTier
		for _, t := range aj.Tiers {
			tiers = append(tiers, timeoff.TenureTier{
				AfterYears: t.AfterYears,
				AnnualDays: t.AnnualDays,
			})
		}

		return &timeoff.TenureAccrual{
			HireDate:  generic.TimePoint{Time: hireDate},
			Tiers:     tiers,
			Frequency: parseAccrualFrequency(aj.Frequency),
		}, nil

	default:
		return nil, fmt.Errorf("unknown accrual type: %s", aj.Type)
	}
}

func parseAccrualFrequency(s string) generic.AccrualFrequency {
	switch s {
	case "upfront":
		return generic.FreqUpfront
	case "daily":
		return generic.FreqDaily
	case "biweekly":
		return generic.FreqBiweekly
	default:
		return generic.FreqMonthly
	}
}

// =============================================================================
// PRESET POLICIES
// =============================================================================
//
// NOTE: Domain-specific preset policy functions have been moved to their
// respective domain packages for better encapsulation:
//
//   - timeoff/ package: StandardPTOJSON, SickLeaveJSON, MaternityLeaveJSON, etc.
//   - rewards/ package: WellnessPointsJSON, LearningCreditsJSON, etc.
//
// The factory package now only contains the generic JSON parser/converter.
// Use domain-specific factories for convenience functions.
