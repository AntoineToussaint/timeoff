/*
policies.go - Pre-built rewards policy configurations

PURPOSE:
  Provides ready-to-use policy configurations for various rewards and
  benefits programs. Each policy includes the generic.Policy definition,
  accrual schedule, and reconciliation rules.

AVAILABLE POLICIES:
  WellnessPointsPolicy:
    - Monthly point accrual (e.g., 100 points/month)
    - Partial carryover to next year
    - ConsumeUpToAccrued (can only spend earned points)

  LearningCreditsPolicy:
    - Annual budget granted upfront (e.g., $2500)
    - Use-it-or-lose-it (no carryover)
    - ConsumeAhead (full budget available immediately)

  RecognitionPointsPolicy:
    - Event-based accrual (peer kudos)
    - Unlimited carryover (points don't expire)
    - ConsumeUpToAccrued (can only redeem received points)

  FlexBenefitsPolicy:
    - Annual budget with partial carryover
    - ConsumeAhead (full budget available)

  RemoteWorkDaysPolicy:
    - Monthly allowance (e.g., 8 days/month)
    - No carryover (use this month or lose it)

  VolunteerHoursPolicy:
    - Annual grant upfront (e.g., 16 hours)
    - No carryover (use this year or lose it)

CONSUMPTION MODES:
  Points programs typically use ConsumeUpToAccrued:
    - You can only spend points you've actually earned
    - Prevents "point debt" scenarios

  Budget programs typically use ConsumeAhead:
    - Full budget available immediately
    - Trust employees to use responsibly

RECONCILIATION PATTERNS:
  Wellness: Carryover up to 200 points, expire the rest
  Learning: Expire everything (fresh budget each year)
  Recognition: Carryover everything (never expire)

EXAMPLE:
  config := rewards.WellnessPointsPolicy("wellness", "Wellness Program", 1200, 200)

  // Use the policy
  policy := config.Policy
  accrual := config.Accrual

  // Check reconciliation rules
  for _, rule := range policy.ReconciliationRules {
      fmt.Println(rule.Trigger.Type, rule.Actions)
  }

SEE ALSO:
  - accrual.go: Accrual schedule implementations
  - types.go: Resource types and units
  - factory/policy.go: JSON-based policy creation
*/
package rewards

import (
	"github.com/warp/resource-engine/generic"
)

// =============================================================================
// POLICY CONFIGURATIONS
// =============================================================================

// PolicyConfig is an alias for generic.PolicyConfig.
type PolicyConfig = generic.PolicyConfig

// =============================================================================
// WELLNESS POINTS POLICY
// =============================================================================

// WellnessPointsPolicy creates a wellness program policy.
// Points are earned through health activities and can be redeemed for rewards.
func WellnessPointsPolicy(id, name string, annualPoints, maxCarryover float64) PolicyConfig {
	maxCarry := generic.NewAmount(maxCarryover, UnitPoints)
	
	return PolicyConfig{
		Policy: generic.Policy{
			ID:           generic.PolicyID(id),
			Name:         name,
			ResourceType: ResourceWellnessPoints,
			Unit:         UnitPoints,
			PeriodConfig: generic.PeriodConfig{Type: generic.PeriodCalendarYear},
			ConsumptionMode: generic.ConsumeUpToAccrued, // Can only spend earned points
			Constraints: generic.Constraints{
				AllowNegative: false,
			},
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "year-end",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionCarryover, Config: generic.ActionConfig{MaxCarryover: &maxCarry}},
					{Type: generic.ActionExpire},
				},
			}},
		},
		Accrual: &MonthlyPointsAccrual{
			MonthlyPoints: annualPoints / 12,
			Unit:          UnitPoints,
		},
	}
}

// =============================================================================
// LEARNING CREDITS POLICY
// =============================================================================

// LearningCreditsPolicy creates a professional development budget policy.
// Full budget available at start of year, use-it-or-lose-it.
func LearningCreditsPolicy(id, name string, annualBudget float64) PolicyConfig {
	return PolicyConfig{
		Policy: generic.Policy{
			ID:           generic.PolicyID(id),
			Name:         name,
			ResourceType: ResourceLearningCredits,
			Unit:         UnitDollars,
			PeriodConfig: generic.PeriodConfig{Type: generic.PeriodCalendarYear},
			ConsumptionMode: generic.ConsumeAhead, // Full budget available immediately
			Constraints: generic.Constraints{
				AllowNegative: false,
			},
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "year-end",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionExpire}, // Use it or lose it
				},
			}},
		},
		Accrual: &UpfrontAccrual{
			Amount: generic.NewAmount(annualBudget, UnitDollars),
		},
	}
}

// =============================================================================
// RECOGNITION POINTS POLICY
// =============================================================================

// RecognitionPointsPolicy creates a peer recognition/kudos policy.
// Points received from colleagues, can be redeemed for rewards.
func RecognitionPointsPolicy(id, name string) PolicyConfig {
	return PolicyConfig{
		Policy: generic.Policy{
			ID:           generic.PolicyID(id),
			Name:         name,
			ResourceType: ResourceRecognitionPoints,
			Unit:         UnitPoints,
			PeriodConfig: generic.PeriodConfig{Type: generic.PeriodCalendarYear},
			ConsumptionMode: generic.ConsumeUpToAccrued,
			Constraints: generic.Constraints{
				AllowNegative: false,
			},
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "year-end",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionExpire}, // Points expire at year end
				},
			}},
		},
		Accrual: nil, // Event-based: points added when kudos received
	}
}

// =============================================================================
// FLEX BENEFITS POLICY
// =============================================================================

// FlexBenefitsPolicy creates a flexible spending account policy.
// Similar to FSA/HSA with limited rollover.
func FlexBenefitsPolicy(id, name string, annualBudget, maxRollover float64) PolicyConfig {
	maxCarry := generic.NewAmount(maxRollover, UnitDollars)
	
	return PolicyConfig{
		Policy: generic.Policy{
			ID:           generic.PolicyID(id),
			Name:         name,
			ResourceType: ResourceFlexBenefits,
			Unit:         UnitDollars,
			PeriodConfig: generic.PeriodConfig{Type: generic.PeriodCalendarYear},
			ConsumptionMode: generic.ConsumeAhead,
			Constraints: generic.Constraints{
				AllowNegative: false,
			},
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "year-end",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionCarryover, Config: generic.ActionConfig{MaxCarryover: &maxCarry}},
					{Type: generic.ActionExpire},
				},
			}},
		},
		Accrual: &UpfrontAccrual{
			Amount: generic.NewAmount(annualBudget, UnitDollars),
		},
	}
}

// =============================================================================
// REMOTE WORK DAYS POLICY
// =============================================================================

// RemoteWorkDaysPolicy creates a WFH allowance policy.
// Monthly allowance, use-it-or-lose-it each month (approximated as year-end).
func RemoteWorkDaysPolicy(id, name string, monthlyDays float64) PolicyConfig {
	return PolicyConfig{
		Policy: generic.Policy{
			ID:           generic.PolicyID(id),
			Name:         name,
			ResourceType: ResourceRemoteDays,
			Unit:         UnitDays,
			PeriodConfig: generic.PeriodConfig{Type: generic.PeriodCalendarYear},
			ConsumptionMode: generic.ConsumeUpToAccrued, // Can only use earned days
			Constraints: generic.Constraints{
				AllowNegative: false,
			},
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "year-end",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionExpire},
				},
			}},
		},
		Accrual: &MonthlyPointsAccrual{
			MonthlyPoints: monthlyDays,
			Unit:          UnitDays,
		},
	}
}

// =============================================================================
// VOLUNTEER HOURS POLICY
// =============================================================================

// VolunteerHoursPolicy creates a paid volunteer time policy.
func VolunteerHoursPolicy(id, name string, annualHours float64) PolicyConfig {
	return PolicyConfig{
		Policy: generic.Policy{
			ID:           generic.PolicyID(id),
			Name:         name,
			ResourceType: ResourceVolunteerHours,
			Unit:         UnitHours,
			PeriodConfig: generic.PeriodConfig{Type: generic.PeriodCalendarYear},
			ConsumptionMode: generic.ConsumeAhead,
			Constraints: generic.Constraints{
				AllowNegative: false,
			},
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "year-end",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionExpire},
				},
			}},
		},
		Accrual: &UpfrontAccrual{
			Amount: generic.NewAmount(annualHours, UnitHours),
		},
	}
}
