/*
policy.go - Policy definitions and reconciliation rules

PURPOSE:
  Defines the rules that govern how a resource behaves: accrual rates,
  consumption limits, carryover rules, and period boundaries. A Policy
  is the contract between the organization and employees about their
  resource entitlements.

KEY CONCEPTS:
  - Policy: The complete ruleset for a resource type
  - Period: Time boundary for balance calculation (year, quarter, etc.)
  - ReconciliationRule: What happens at period end (carryover, expire, cap)
  - ConsumptionMode: Can employees use future accruals or only earned?
  - UniquePerTimePoint: Prevents double-booking (critical for time-off)

CONSUMPTION MODES:
  ConsumeAhead:
    - Employee can use full year's entitlement immediately
    - Example: 20 days PTO available January 1st
    - Risk: Employee leaves mid-year having used more than earned

  ConsumeUpToAccrued:
    - Employee can only use what they've earned so far
    - Example: After 3 months, only 5 days available (20/12*3)
    - Safer for employer but less flexible for employee

RECONCILIATION:
  At period end (e.g., December 31), the engine processes rules:
  1. Check remaining balance
  2. Apply carryover (up to max limit)
  3. Expire anything above carryover limit
  4. Create transactions for next period

EXAMPLE:
  policy := Policy{
      Name:            "Standard PTO",
      ConsumptionMode: ConsumeAhead,
      ReconciliationRules: []ReconciliationRule{{
          Trigger: ReconciliationTrigger{Type: TriggerPeriodEnd},
          Actions: []ReconciliationAction{
              {Type: ActionCarryover, Config: ActionConfig{MaxCarryover: days(5)}},
              {Type: ActionExpire},
          },
      }},
  }
*/
package generic

// =============================================================================
// POLICY - Rules governing resource behavior within a period
// =============================================================================

// Policy defines how a resource behaves for a group of entities.
type Policy struct {
	ID           PolicyID
	Name         string
	ResourceType ResourceType
	Unit         Unit

	// Period configuration - defines the balance boundary
	PeriodConfig PeriodConfig

	// Is this an unlimited resource (no balance tracking)?
	IsUnlimited bool

	// Accrual type: deterministic (time-based) or non-deterministic (hours-worked)
	AccrualType AccrualType

	// Consumption mode: can employee use future accruals or only what's accrued?
	// Only relevant for deterministic accruals
	ConsumptionMode ConsumptionMode

	// UniquePerTimePoint: Can only have ONE consumption per day per resource type.
	// TRUE for time-off (can't take the same day off twice)
	// FALSE for rewards (can receive multiple kudos on same day)
	UniquePerTimePoint bool

	// Constraints
	Constraints Constraints

	// Reconciliation rules for period transitions
	ReconciliationRules []ReconciliationRule

	// Versioning
	Version     int
	EffectiveAt TimePoint
}

// AccrualType determines how balance is calculated
type AccrualType string

const (
	// AccrualDeterministic: Future accruals are known (e.g., 20 days/year)
	// Balance includes ALL accruals for the period (past + future)
	AccrualDeterministic AccrualType = "deterministic"

	// AccrualNonDeterministic: Future accruals are unknown (e.g., hours-worked)
	// Balance only includes accruals that have occurred
	AccrualNonDeterministic AccrualType = "non_deterministic"
)

// ConsumptionMode determines when accrued balance can be consumed
type ConsumptionMode string

const (
	// ConsumeAhead: Can consume full period entitlement regardless of accrual timing
	// "You get 20 days this year - use them whenever"
	// Available = Total period entitlement - consumed - pending
	ConsumeAhead ConsumptionMode = "consume_ahead"

	// ConsumeUpToAccrued: Can only consume what has actually accrued
	// "You accrue 1.67 days/month - only use what you have"
	// Available = Actually accrued so far - consumed - pending
	ConsumeUpToAccrued ConsumptionMode = "consume_up_to_accrued"
)

// Constraints define limits on resource usage
type Constraints struct {
	AllowNegative  bool
	MaxBalance     *Amount
	MinBalance     *Amount
	MaxRequestSize *Amount
}

// =============================================================================
// RECONCILIATION - Period boundary transitions
// =============================================================================

// ReconciliationRule defines what happens at period boundaries
type ReconciliationRule struct {
	ID      string
	Name    string
	Trigger ReconciliationTrigger
	Actions []ReconciliationAction
}

type ReconciliationTrigger struct {
	Type TriggerType
}

type TriggerType string

const (
	TriggerPeriodEnd    TriggerType = "period_end"    // End of period (rollover/expire)
	TriggerPolicyChange TriggerType = "policy_change" // Policy updated
	TriggerEntityJoin   TriggerType = "entity_join"   // New employee assigned
	TriggerManual       TriggerType = "manual"        // Admin triggered
)

type ReconciliationAction struct {
	Type   ActionType
	Config ActionConfig
}

type ActionType string

const (
	ActionCarryover ActionType = "carryover" // Move balance to next period
	ActionExpire    ActionType = "expire"    // Remove unused balance
	ActionCap       ActionType = "cap"       // Enforce max balance
	ActionProrate   ActionType = "prorate"   // Adjust for partial period
)

type ActionConfig struct {
	MaxCarryover  *Amount
	ProrateMethod *ProrateMethod
}

// ProrateMethod is defined in accrual.go

// =============================================================================
// RECONCILIATION ENGINE
// =============================================================================

type ReconciliationInput struct {
	EntityID       EntityID
	PolicyID       PolicyID
	Policy         Policy
	Rules          []ReconciliationRule
	CurrentBalance Balance // Balance for the ending period
	EndingPeriod   Period
	NextPeriod     Period
}

type ReconciliationOutput struct {
	Transactions []Transaction
	Summary      ReconciliationSummary
}

type ReconciliationSummary struct {
	CarriedOver Amount
	Expired     Amount
	Prorated    Amount
}

type ReconciliationEngine struct{}

func (re *ReconciliationEngine) Process(input ReconciliationInput) (*ReconciliationOutput, error) {
	var transactions []Transaction
	summary := ReconciliationSummary{
		CarriedOver: input.CurrentBalance.TotalEntitlement.Zero(),
		Expired:     input.CurrentBalance.TotalEntitlement.Zero(),
		Prorated:    input.CurrentBalance.TotalEntitlement.Zero(),
	}

	// Use rules from Policy (primary) or input.Rules (override)
	rules := input.Policy.ReconciliationRules
	if len(input.Rules) > 0 {
		rules = input.Rules
	}

	for _, rule := range rules {
		if rule.Trigger.Type != TriggerPeriodEnd {
			continue
		}
		for _, action := range rule.Actions {
			txs := re.applyAction(action, input, &summary)
			transactions = append(transactions, txs...)
		}
	}

	return &ReconciliationOutput{Transactions: transactions, Summary: summary}, nil
}

func (re *ReconciliationEngine) applyAction(action ReconciliationAction, input ReconciliationInput, summary *ReconciliationSummary) []Transaction {
	switch action.Type {
	case ActionCarryover:
		return re.carryover(action, input, summary)
	case ActionExpire:
		return re.expire(action, input, summary)
	case ActionCap:
		return re.cap(action, input, summary)
	default:
		return nil
	}
}

func (re *ReconciliationEngine) carryover(action ReconciliationAction, input ReconciliationInput, summary *ReconciliationSummary) []Transaction {
	// Use CurrentAccrued() for reconciliation - we reconcile what was actually earned,
	// not the full entitlement (which may not have been earned yet for mid-period hires)
	remaining := input.CurrentBalance.CurrentAccrued()
	if remaining.IsNegative() || remaining.IsZero() {
		return nil
	}

	carryAmount := remaining
	if action.Config.MaxCarryover != nil && carryAmount.GreaterThan(*action.Config.MaxCarryover) {
		carryAmount = *action.Config.MaxCarryover
	}
	summary.CarriedOver = carryAmount

	// Create transaction in the NEXT period
	return []Transaction{{
		EntityID:     input.EntityID,
		PolicyID:     input.PolicyID,
		ResourceType: input.Policy.ResourceType,
		EffectiveAt:  input.NextPeriod.Start, // First day of next period
		Delta:        carryAmount,
		Type:         TxReconciliation,
		Reason:       "carryover from previous period",
	}}
}

func (re *ReconciliationEngine) expire(action ReconciliationAction, input ReconciliationInput, summary *ReconciliationSummary) []Transaction {
	// Use CurrentAccrued() for reconciliation - we reconcile what was actually earned,
	// not the full entitlement (which may not have been earned yet for mid-period hires)
	remaining := input.CurrentBalance.CurrentAccrued()
	if remaining.IsNegative() || remaining.IsZero() {
		return nil
	}

	expired := remaining.Sub(summary.CarriedOver)
	if expired.IsNegative() || expired.IsZero() {
		return nil
	}
	summary.Expired = summary.Expired.Add(expired)

	return []Transaction{{
		EntityID:     input.EntityID,
		PolicyID:     input.PolicyID,
		ResourceType: input.Policy.ResourceType,
		EffectiveAt:  input.EndingPeriod.End,
		Delta:        expired.Neg(),
		Type:         TxReconciliation,
		Reason:       "balance expired at period end",
	}}
}

func (re *ReconciliationEngine) cap(action ReconciliationAction, input ReconciliationInput, summary *ReconciliationSummary) []Transaction {
	if input.Policy.Constraints.MaxBalance == nil {
		return nil
	}

	max := *input.Policy.Constraints.MaxBalance
	current := input.CurrentBalance.Current()
	if !current.GreaterThan(max) {
		return nil
	}

	excess := current.Sub(max)
	summary.Expired = summary.Expired.Add(excess)

	return []Transaction{{
		EntityID:     input.EntityID,
		PolicyID:     input.PolicyID,
		ResourceType: input.Policy.ResourceType,
		EffectiveAt:  input.EndingPeriod.End,
		Delta:        excess.Neg(),
		Type:         TxReconciliation,
		Reason:       "balance capped at maximum",
	}}
}

// =============================================================================
// POLICY CONFIG - Bundles policy with accrual schedule
// =============================================================================

// PolicyConfig packages a Policy with its AccrualSchedule.
// This is the standard way to define a complete policy configuration.
// Domain packages (timeoff, rewards) should use this type.
type PolicyConfig struct {
	Policy  Policy
	Accrual AccrualSchedule
}
