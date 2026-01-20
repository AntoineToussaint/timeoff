/*
projection.go - Future balance validation

PURPOSE:
  Validates whether a consumption request is valid against the projected
  balance. This is where ConsumptionMode really matters: can the employee
  use resources they haven't earned yet?

KEY INSIGHT:
  Balance is computed for a PERIOD, not at a point in time. When validating
  a request for March 15, we need to consider:
  - What's already consumed in the period?
  - What's pending approval?
  - What will be accrued by March 15? (for ConsumeUpToAccrued)
  - What's the full entitlement? (for ConsumeAhead)

CONSUMPTION MODES:
  ConsumeAhead:
    Employee can request against full period entitlement.
    January request for 15 days is valid if full year = 20 days.

  ConsumeUpToAccrued:
    Employee can only request what they've earned by the request date.
    January request for 15 days is INVALID if only 1.67 accrued.

VALIDATION PROCESS:
  1. Get existing transactions for the period
  2. Calculate accrued amount (based on accrual schedule)
  3. Determine available based on ConsumptionMode
  4. Check if requested amount <= available
  5. Return ValidationResult with details

PROJECTION vs REAL-TIME:
  The projection engine answers "COULD this request be valid?"
  It doesn't actually create transactions - that's RequestService's job.

EXAMPLE:
  engine := &ProjectionEngine{Ledger: ledger}
  result, _ := engine.Project(ctx, ProjectionInput{
      EntityID:        "emp-123",
      PolicyID:        "policy-001",
      Period:          year2025,
      AsOf:            march15,
      Accruals:        monthlyAccrual,
      RequestedAmount: days(10),
      ConsumptionMode: ConsumeUpToAccrued,
  })

  if !result.IsValid {
      fmt.Println("Request denied:", result.Reason)
  }

SEE ALSO:
  - balance.go: Balance struct and calculation
  - accrual.go: AccrualSchedule interface
  - request.go: Uses projection for validation
*/
package generic

import "context"

// =============================================================================
// PROJECTION ENGINE - Validates consumption against period balance
// =============================================================================

// ProjectionEngine validates requests against period-based balance.
//
// Key insight: Balance is computed for a PERIOD, not at a point in time.
// What's "available" depends on the ConsumptionMode:
//   - ConsumeAhead: Full period entitlement is available
//   - ConsumeUpToAccrued: Only what's accrued so far is available
type ProjectionEngine struct {
	Ledger Ledger
}

// ProjectionInput contains all inputs for validation
type ProjectionInput struct {
	EntityID EntityID
	PolicyID PolicyID
	Unit     Unit

	// The period to validate against
	Period Period

	// When is the request being made? (for accrued-to-date calculation)
	AsOf TimePoint

	// Accrual schedule (nil for non-deterministic)
	Accruals AccrualSchedule

	// Requested consumption
	RequestedAmount Amount

	// Consumption mode: can we use future accruals?
	ConsumptionMode ConsumptionMode

	// Constraints
	AllowNegative bool
	MaxBalance    *Amount
}

// ProjectionResult contains validation result
type ProjectionResult struct {
	// Computed balance for the period
	Balance Balance

	// Is the request valid?
	IsValid bool

	// Remaining balance after request (if valid)
	RemainingBalance Amount

	// Error if not valid
	ValidationError *ValidationError

	// Display version for UI
	Display BalanceDisplay
}

// Project validates a consumption request against period balance.
func (pe *ProjectionEngine) Project(ctx context.Context, input ProjectionInput) (*ProjectionResult, error) {
	// Default AsOf to period end (full entitlement view)
	asOf := input.AsOf
	if asOf.IsZero() {
		asOf = input.Period.End
	}

	// 1. Get all transactions in the period
	txs, err := pe.Ledger.TransactionsInRange(ctx, input.EntityID, input.PolicyID, input.Period.Start, input.Period.End)
	if err != nil {
		return nil, err
	}

	// 2. Calculate balance components
	var (
		actualAccruals = NewAmount(0, input.Unit)
		consumed       = NewAmount(0, input.Unit)
		pending        = NewAmount(0, input.Unit)
		adjustments    = NewAmount(0, input.Unit)
	)

	for _, tx := range txs {
		switch tx.Type {
		case TxGrant:
			actualAccruals = actualAccruals.Add(tx.Delta)
		case TxConsumption:
			consumed = consumed.Add(tx.Delta.Neg())
		case TxPending:
			pending = pending.Add(tx.Delta.Neg())
		case TxReconciliation, TxAdjustment:
			adjustments = adjustments.Add(tx.Delta)
		case TxReversal:
			consumed = consumed.Sub(tx.Delta)
		}
	}

	// 3. Calculate accrued-to-date and total entitlement
	accruedToDate := actualAccruals
	totalEntitlement := actualAccruals

	if input.Accruals != nil {
		// Accrued to date: only accruals up to 'asOf'
		accruedEvents := input.Accruals.GenerateAccruals(input.Period.Start, asOf)
		accruedTotal := NewAmount(0, input.Unit)
		for _, e := range accruedEvents {
			accruedTotal = accruedTotal.Add(e.Amount)
		}
		if accruedTotal.GreaterThan(actualAccruals) {
			accruedToDate = accruedTotal
		}

		// Total entitlement: all accruals for the full period
		allEvents := input.Accruals.GenerateAccruals(input.Period.Start, input.Period.End)
		entitlementTotal := NewAmount(0, input.Unit)
		for _, e := range allEvents {
			entitlementTotal = entitlementTotal.Add(e.Amount)
		}
		totalEntitlement = entitlementTotal
	}

	// 4. Build balance
	balance := Balance{
		EntityID:         input.EntityID,
		PolicyID:         input.PolicyID,
		Period:           input.Period,
		AccruedToDate:    accruedToDate,
		TotalEntitlement: totalEntitlement,
		TotalConsumed:    consumed,
		Pending:          pending,
		Adjustments:      adjustments,
	}

	// 5. Validate based on consumption mode
	mode := input.ConsumptionMode
	if mode == "" {
		mode = ConsumeAhead // Default to optimistic
	}

	available := balance.AvailableWithMode(mode)
	remaining := available.Sub(input.RequestedAmount)

	if !input.AllowNegative && remaining.IsNegative() {
		return &ProjectionResult{
			Balance: balance,
			IsValid: false,
			ValidationError: &ValidationError{
				Type:    "insufficient_balance",
				Balance: available,
			},
			Display: balance.ToDisplay(mode),
		}, nil
	}

	// Check max balance constraint
	if input.MaxBalance != nil && balance.Current().GreaterThan(*input.MaxBalance) {
		return &ProjectionResult{
			Balance: balance,
			IsValid: false,
			ValidationError: &ValidationError{
				Type:    "exceeds_max",
				Balance: balance.Current(),
			},
			Display: balance.ToDisplay(mode),
		}, nil
	}

	return &ProjectionResult{
		Balance:          balance,
		IsValid:          true,
		RemainingBalance: remaining,
		Display:          balance.ToDisplay(mode),
	}, nil
}

// QuickValidate is a convenience method for simple validation
func (pe *ProjectionEngine) QuickValidate(
	ctx context.Context,
	entityID EntityID,
	policyID PolicyID,
	period Period,
	accruals AccrualSchedule,
	requestedAmount Amount,
	allowNegative bool,
) (bool, error) {
	result, err := pe.Project(ctx, ProjectionInput{
		EntityID:        entityID,
		PolicyID:        policyID,
		Unit:            requestedAmount.Unit,
		Period:          period,
		Accruals:        accruals,
		RequestedAmount: requestedAmount,
		AllowNegative:   allowNegative,
		ConsumptionMode: ConsumeAhead, // Default
	})
	if err != nil {
		return false, err
	}
	return result.IsValid, nil
}
