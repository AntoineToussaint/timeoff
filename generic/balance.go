/*
balance.go - Balance calculation and availability

PURPOSE:
  Computes resource balance from transactions within a period. This is
  the central calculation that answers "how much does this employee have?"

KEY INSIGHT:
  Balance is computed for a PERIOD, not at a point in time. This is crucial
  because deterministic accruals (like 20 days/year) should show the full
  entitlement, not just what's accrued so far.

BALANCE COMPONENTS:
  AccruedToDate:    What has actually been earned/accrued
  TotalEntitlement: Full period entitlement (may include future accruals)
  TotalConsumed:    Approved consumption
  Pending:          Submitted but not yet approved
  Adjustments:      Manual corrections, reconciliations, carryover

AVAILABILITY CALCULATION:
  The key question: "How much can I request?"

  If ConsumptionMode == ConsumeAhead:
    Available = TotalEntitlement - TotalConsumed - Pending + Adjustments

  If ConsumptionMode == ConsumeUpToAccrued:
    Available = AccruedToDate - TotalConsumed - Pending + Adjustments

EXAMPLE:
  Employee has 20 days/year PTO, it's March, 5 days accrued, 2 days used:

  ConsumeAhead:      Available = 20 - 2 = 18 days
  ConsumeUpToAccrued: Available = 5 - 2 = 3 days

VALIDATION:
  CanConsume(amount) checks:
  1. Amount is positive
  2. Available() >= amount (unless AllowNegative)
  3. Returns ValidationError with details if invalid

SEE ALSO:
  - projection.go: Validates future requests against projected balance
  - assignment.go: Aggregates balance across multiple policies
*/
package generic

import "context"

// =============================================================================
// BALANCE - Computed for a PERIOD, not at a point in time
// =============================================================================

// Balance represents the resource balance for a specific period.
// This is the CORE concept: balance is always period-based.
//
// Two key amounts:
//   - AccruedToDate: What has actually been accrued so far
//   - TotalEntitlement: Full period entitlement (for deterministic policies)
//
// Which one is "available" depends on ConsumptionMode:
//   - ConsumeAhead: Available = TotalEntitlement - consumed - pending
//   - ConsumeUpToAccrued: Available = AccruedToDate - consumed - pending
type Balance struct {
	EntityID EntityID
	PolicyID PolicyID
	Period   Period

	// What has actually accrued up to the calculation date
	AccruedToDate Amount

	// Full entitlement for the period (same as AccruedToDate for non-deterministic)
	TotalEntitlement Amount

	// Total consumed in the period (approved requests)
	TotalConsumed Amount

	// Pending requests (submitted but not yet approved)
	Pending Amount

	// Adjustments (manual corrections, reconciliations, carryover)
	Adjustments Amount
}

// TotalAccruals returns the full entitlement (for backwards compatibility)
func (b Balance) TotalAccruals() Amount {
	return b.TotalEntitlement
}

// Current returns the current balance based on full entitlement
// (entitlement - consumed + adjustments)
func (b Balance) Current() Amount {
	return b.TotalEntitlement.Sub(b.TotalConsumed).Add(b.Adjustments)
}

// CurrentAccrued returns balance based only on what's accrued so far
// (accrued - consumed + adjustments)
func (b Balance) CurrentAccrued() Amount {
	return b.AccruedToDate.Sub(b.TotalConsumed).Add(b.Adjustments)
}

// Available returns what can be requested based on consumption mode
func (b Balance) Available() Amount {
	return b.Current().Sub(b.Pending)
}

// AvailableWithMode returns what can be requested based on consumption mode
func (b Balance) AvailableWithMode(mode ConsumptionMode) Amount {
	switch mode {
	case ConsumeUpToAccrued:
		return b.CurrentAccrued().Sub(b.Pending)
	default: // ConsumeAhead or unset
		return b.Current().Sub(b.Pending)
	}
}

// CanConsume checks if the given amount can be consumed
func (b Balance) CanConsume(amount Amount, allowNegative bool) bool {
	remaining := b.Available().Sub(amount)
	if allowNegative {
		return true
	}
	return !remaining.IsNegative()
}

// CanConsumeWithMode checks consumption with specific mode
func (b Balance) CanConsumeWithMode(amount Amount, mode ConsumptionMode, allowNegative bool) bool {
	remaining := b.AvailableWithMode(mode).Sub(amount)
	if allowNegative {
		return true
	}
	return !remaining.IsNegative()
}

// =============================================================================
// BALANCE CALCULATOR - Computes balance for a period
// =============================================================================

// BalanceCalculator computes balance from ledger + accrual schedule
type BalanceCalculator struct {
	Ledger Ledger
}

// CalculateBalance computes the balance for an entity in a period.
//
// Key insight: We compute BOTH:
//   - AccruedToDate: what has actually accrued up to 'asOf'
//   - TotalEntitlement: full period entitlement (for deterministic policies)
//
// This allows the UI to show both and the policy to decide which is "available".
func (bc *BalanceCalculator) CalculateBalance(
	ctx context.Context,
	entityID EntityID,
	policyID PolicyID,
	period Period,
	accruals AccrualSchedule, // nil for non-deterministic
	unit Unit,
	asOf TimePoint, // When to calculate "accrued to date"
) (Balance, error) {

	// 1. Get all transactions in the period
	txs, err := bc.Ledger.TransactionsInRange(ctx, entityID, policyID, period.Start, period.End)
	if err != nil {
		return Balance{}, err
	}

	// 2. Sum transactions by type
	var (
		actualAccruals = NewAmount(0, unit)
		consumed       = NewAmount(0, unit)
		pending        = NewAmount(0, unit)
		adjustments    = NewAmount(0, unit)
	)

	for _, tx := range txs {
		switch tx.Type {
		case TxGrant:
			// Grants add to balance (bonus days, carryover balance, hours-worked accruals)
			actualAccruals = actualAccruals.Add(tx.Delta)
		case TxConsumption:
			consumed = consumed.Add(tx.Delta.Neg()) // Store as positive
		case TxPending:
			pending = pending.Add(tx.Delta.Neg()) // Store as positive
		case TxReconciliation, TxAdjustment:
			adjustments = adjustments.Add(tx.Delta)
		case TxReversal:
			// Reversals restore balance
			consumed = consumed.Sub(tx.Delta)
		}
	}

	// 3. Calculate accrued-to-date and total entitlement
	accruedToDate := actualAccruals
	totalEntitlement := actualAccruals

	if accruals != nil {
		// Accrued to date: only accruals up to 'asOf'
		accruedEvents := accruals.GenerateAccruals(period.Start, asOf)
		accruedTotal := NewAmount(0, unit)
		for _, e := range accruedEvents {
			accruedTotal = accruedTotal.Add(e.Amount)
		}
		// Use max of actual transactions and projected (in case accruals recorded early)
		if accruedTotal.GreaterThan(actualAccruals) {
			accruedToDate = accruedTotal
		}

		// Total entitlement: all accruals for the full period
		allEvents := accruals.GenerateAccruals(period.Start, period.End)
		entitlementTotal := NewAmount(0, unit)
		for _, e := range allEvents {
			entitlementTotal = entitlementTotal.Add(e.Amount)
		}
		totalEntitlement = entitlementTotal
	}

	return Balance{
		EntityID:         entityID,
		PolicyID:         policyID,
		Period:           period,
		AccruedToDate:    accruedToDate,
		TotalEntitlement: totalEntitlement,
		TotalConsumed:    consumed,
		Pending:          pending,
		Adjustments:      adjustments,
	}, nil
}

// =============================================================================
// CONSUMPTION VALIDATOR - Can this request be fulfilled?
// =============================================================================

// ConsumptionValidator checks if a consumption can be made
type ConsumptionValidator struct {
	BalanceCalc *BalanceCalculator
}

// ValidateConsumption checks if the requested consumption is valid for the period.
//
// Returns:
//   - valid: true if consumption can be made
//   - balance: the calculated balance
//   - err: validation error if not valid
func (cv *ConsumptionValidator) ValidateConsumption(
	ctx context.Context,
	entityID EntityID,
	policyID PolicyID,
	period Period,
	accruals AccrualSchedule,
	requestedAmount Amount,
	consumptionMode ConsumptionMode,
	allowNegative bool,
	asOf TimePoint,
) (valid bool, balance Balance, err error) {

	balance, err = cv.BalanceCalc.CalculateBalance(
		ctx, entityID, policyID, period, accruals, requestedAmount.Unit, asOf,
	)
	if err != nil {
		return false, Balance{}, err
	}

	if !balance.CanConsumeWithMode(requestedAmount, consumptionMode, allowNegative) {
		return false, balance, &ValidationError{
			Type:    "insufficient_balance",
			Balance: balance.AvailableWithMode(consumptionMode),
		}
	}

	return true, balance, nil
}

// =============================================================================
// USER-FACING BALANCE DISPLAY
// =============================================================================

// BalanceDisplay is what the UI shows to the user.
// It clearly separates "what you can use now" from "what you'll have".
type BalanceDisplay struct {
	// What can be requested RIGHT NOW based on consumption mode
	AvailableNow Amount

	// What has been accrued so far this period
	AccruedToDate Amount

	// What you'll have by end of period (for deterministic)
	WillHaveByPeriodEnd Amount

	// What's already been consumed
	Used Amount

	// What's pending approval
	Pending Amount

	// Period info for display
	PeriodStart TimePoint
	PeriodEnd   TimePoint

	// Policy info
	ConsumptionMode ConsumptionMode
}

// ToDisplay converts a Balance to a user-friendly display format
func (b Balance) ToDisplay(mode ConsumptionMode) BalanceDisplay {
	return BalanceDisplay{
		AvailableNow:        b.AvailableWithMode(mode),
		AccruedToDate:       b.AccruedToDate,
		WillHaveByPeriodEnd: b.TotalEntitlement,
		Used:                b.TotalConsumed,
		Pending:             b.Pending,
		PeriodStart:         b.Period.Start,
		PeriodEnd:           b.Period.End,
		ConsumptionMode:     mode,
	}
}
