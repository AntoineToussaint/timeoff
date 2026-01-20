/*
policies.go - Pre-built time-off policy configurations

PURPOSE:
  Provides ready-to-use policy configurations for common time-off types.
  These are convenience functions that set up Policy + Accrual + Reconciliation
  rules according to typical HR patterns.

AVAILABLE POLICIES:
  StandardPTOPolicy:     Typical vacation with monthly accrual and carryover
  SickLeavePolicy:       Sick days with monthly accrual, no rollover
  UnlimitedPTOPolicy:    No balance tracking (just approval workflow)
  ParentalLeavePolicy:   One-time grant for new parents
  FloatingHolidayPolicy: Fixed days granted upfront, expire at year end
  BereavementPolicy:     Small fixed allowance for family emergencies

POLICY COMPONENTS:
  Each policy configuration includes:
  - Policy: Rules (period, constraints, consumption mode)
  - Accrual: How balance grows over time
  - ReconciliationRules: What happens at period end

CUSTOMIZATION:
  These are starting points. Real implementations often need:
  - Tenure-based accrual tiers
  - Different fiscal year boundaries
  - Custom carryover caps
  - Manager-specific approval rules

EXAMPLE:
  // Create a standard PTO policy
  config := timeoff.StandardPTOPolicy("pto-2025", 20.0, 5.0)

  // Access components
  policy := config.Policy       // generic.Policy
  accrual := config.Accrual     // AccrualSchedule
  rules := config.Policy.ReconciliationRules

  // Customize if needed
  config.Policy.ConsumptionMode = generic.ConsumeUpToAccrued

SEE ALSO:
  - accrual.go: Accrual schedule implementations
  - factory/policy.go: JSON-based policy creation
  - generic/policy.go: Policy type definition
*/
package timeoff

import "github.com/warp/resource-engine/generic"

// =============================================================================
// COMMON TIME-OFF POLICIES
// =============================================================================

// StandardPTOPolicy returns a typical PTO policy with rollover.
func StandardPTOPolicy(id generic.PolicyID, annualDays float64, maxRollover float64) PolicyConfig {
	maxRoll := generic.NewAmount(maxRollover, generic.UnitDays)
	return PolicyConfig{
		Policy: generic.Policy{
			ID:           id,
			Name:         "Standard PTO",
			ResourceType: ResourcePTO,
			Unit:         generic.UnitDays,
			Constraints:  generic.Constraints{AllowNegative: false},
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "year-end",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionCarryover, Config: generic.ActionConfig{MaxCarryover: &maxRoll}},
					{Type: generic.ActionExpire},
				},
			}},
		},
		Accrual: &YearlyAccrual{AnnualDays: annualDays, Frequency: generic.FreqMonthly},
	}
}

// UnlimitedPTOPolicy returns an unlimited PTO policy (no balance tracking).
func UnlimitedPTOPolicy(id generic.PolicyID) PolicyConfig {
	return PolicyConfig{
		Policy: generic.Policy{
			ID:           id,
			Name:         "Unlimited PTO",
			ResourceType: ResourcePTO,
			Unit:         generic.UnitDays,
			IsUnlimited:  true,
		},
		Accrual: nil, // no accrual for unlimited
	}
}

// UseItOrLoseItPolicy returns a policy where unused balance expires.
func UseItOrLoseItPolicy(id generic.PolicyID, annualDays float64) PolicyConfig {
	return PolicyConfig{
		Policy: generic.Policy{
			ID:           id,
			Name:         "Use It or Lose It",
			ResourceType: ResourcePTO,
			Unit:         generic.UnitDays,
			Constraints:  generic.Constraints{AllowNegative: false},
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "year-end-expire",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionExpire},
				},
			}},
		},
		Accrual: &YearlyAccrual{AnnualDays: annualDays, Frequency: generic.FreqUpfront},
	}
}

// SickLeavePolicy returns a typical sick leave policy.
func SickLeavePolicy(id generic.PolicyID, annualDays float64) PolicyConfig {
	return PolicyConfig{
		Policy: generic.Policy{
			ID:           id,
			Name:         "Sick Leave",
			ResourceType: ResourceSick,
			Unit:         generic.UnitDays,
			Constraints:  generic.Constraints{AllowNegative: true}, // often allow negative for sick
			ReconciliationRules: []generic.ReconciliationRule{{
				ID:      "year-end-expire",
				Trigger: generic.ReconciliationTrigger{Type: generic.TriggerPeriodEnd},
				Actions: []generic.ReconciliationAction{
					{Type: generic.ActionExpire}, // sick leave typically doesn't roll over
				},
			}},
		},
		Accrual: &YearlyAccrual{AnnualDays: annualDays, Frequency: generic.FreqMonthly},
	}
}

// =============================================================================
// POLICY CONFIG - Uses generic.PolicyConfig
// =============================================================================

// PolicyConfig is an alias for generic.PolicyConfig.
// ReconciliationRules are stored in Policy.ReconciliationRules.
type PolicyConfig = generic.PolicyConfig
