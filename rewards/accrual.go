/*
accrual.go - Rewards-specific accrual schedule implementations

PURPOSE:
  Implements generic.AccrualSchedule for rewards-specific patterns.
  Unlike time-off, rewards often use points, dollars, and event-based
  accruals rather than calendar-based day grants.

ACCRUAL TYPES:
  MonthlyPointsAccrual:
    - Fixed points per month (e.g., 100 wellness points)
    - Used for wellness, remote work allowances

  UpfrontAccrual:
    - Full amount granted at period start
    - Used for learning budgets, volunteer hours

  EventBasedAccrual:
    - No scheduled accruals - triggered by external events
    - Used for recognition points (peer kudos)
    - Non-deterministic: future balance unknown

DETERMINISTIC vs EVENT-BASED:
  MonthlyPointsAccrual and UpfrontAccrual are deterministic:
    - Future accruals are known
    - Balance projection can include future grants

  EventBasedAccrual is non-deterministic:
    - Accruals depend on external events (kudos received)
    - Can only use what's actually been earned
    - ConsumeUpToAccrued is required

UNIT HANDLING:
  Each accrual is configured with a Unit:
  - UnitPoints for wellness, recognition
  - UnitDollars for learning credits
  - UnitHours for volunteer time
  - UnitDays for remote work

EXAMPLE:
  // 100 wellness points per month
  accrual := &MonthlyPointsAccrual{
      MonthlyPoints: 100,
      Unit:          rewards.UnitPoints,
  }

  events := accrual.GenerateAccruals(jan1, dec31)
  // Returns 12 events, each with 100 points

  // $2500 learning budget upfront
  learningAccrual := &UpfrontAccrual{
      Amount: 2500,
      Unit:   rewards.UnitDollars,
  }

  events := learningAccrual.GenerateAccruals(jan1, dec31)
  // Returns 1 event with $2500 on jan1

SEE ALSO:
  - generic/accrual.go: AccrualSchedule interface
  - timeoff/accrual.go: Day-based accrual patterns
  - policies.go: Uses these accruals in policy configs
*/
package rewards

import (
	"time"

	"github.com/warp/resource-engine/generic"
)

// =============================================================================
// MONTHLY POINTS ACCRUAL
// =============================================================================

// MonthlyPointsAccrual accrues a fixed amount of points each month.
type MonthlyPointsAccrual struct {
	MonthlyPoints float64
	Unit          generic.Unit
}

func (a *MonthlyPointsAccrual) GenerateAccruals(from, to generic.TimePoint) []generic.AccrualEvent {
	var events []generic.AccrualEvent

	// Start from first of the month of 'from'
	current := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	endDate := to.Time

	for !current.After(endDate) {
		// Only include if within range
		if !current.Before(from.Time) {
			events = append(events, generic.AccrualEvent{
				At:     generic.TimePoint{Time: current},
				Amount: generic.NewAmount(a.MonthlyPoints, a.Unit),
				Reason: "Monthly accrual",
			})
		}
		current = current.AddDate(0, 1, 0)
	}

	return events
}

func (a *MonthlyPointsAccrual) IsDeterministic() bool {
	return true // Monthly accruals are predictable
}

// =============================================================================
// UPFRONT ACCRUAL
// =============================================================================

// UpfrontAccrual grants the full amount at the start of the period.
type UpfrontAccrual struct {
	Amount generic.Amount
}

func (a *UpfrontAccrual) GenerateAccruals(from, to generic.TimePoint) []generic.AccrualEvent {
	return []generic.AccrualEvent{{
		At:     from,
		Amount: a.Amount,
		Reason: "Annual grant",
	}}
}

func (a *UpfrontAccrual) IsDeterministic() bool {
	return true
}

// =============================================================================
// EVENT-BASED ACCRUAL (for wellness activities, kudos, etc.)
// =============================================================================

// EventBasedAccrual doesn't generate automatic accruals.
// Points are added manually when events occur (gym visits, kudos received, etc.)
type EventBasedAccrual struct{}

func (a *EventBasedAccrual) GenerateAccruals(from, to generic.TimePoint) []generic.AccrualEvent {
	return nil // No automatic accruals
}

func (a *EventBasedAccrual) IsDeterministic() bool {
	return false // Cannot predict future events
}

// =============================================================================
// ACTIVITY-BASED ACCRUAL
// =============================================================================

// ActivityAccrual generates accruals based on tracked activities.
type ActivityAccrual struct {
	Activities []TrackedActivity
	Unit       generic.Unit
}

// TrackedActivity represents an activity that was completed
type TrackedActivity struct {
	Activity WellnessActivity
	Date     generic.TimePoint
	Count    int
}

func (a *ActivityAccrual) GenerateAccruals(from, to generic.TimePoint) []generic.AccrualEvent {
	var events []generic.AccrualEvent

	for _, tracked := range a.Activities {
		// Only include activities within the date range
		if tracked.Date.Before(from) || tracked.Date.After(to) {
			continue
		}

		points := tracked.Activity.Points * float64(tracked.Count)
		events = append(events, generic.AccrualEvent{
			At:     tracked.Date,
			Amount: generic.NewAmount(points, a.Unit),
			Reason: tracked.Activity.Name,
		})
	}

	return events
}

func (a *ActivityAccrual) IsDeterministic() bool {
	return false // Activities are event-based
}
