/*
accrual.go - Time-off accrual schedule implementations

PURPOSE:
  Implements generic.AccrualSchedule for time-off specific patterns.
  Accrual schedules define how balance grows over time.

ACCRUAL TYPES:
  YearlyAccrual:
    - "20 days per year" with different distribution frequencies
    - FreqUpfront: All 20 days on January 1
    - FreqMonthly: 1.67 days per month
    - FreqBiweekly: 0.77 days per pay period

  TenureAccrual:
    - Accrual rate increases with years of service
    - Example: 15 days for 0-2 years, 20 days for 3-5 years, 25 days for 5+

  HoursWorkedAccrual:
    - Non-deterministic: accrual depends on hours worked
    - Example: 1 hour PTO per 40 hours worked
    - Balance can't include future accruals (unknown hours)

DETERMINISTIC vs NON-DETERMINISTIC:
  Deterministic (YearlyAccrual):
    Future accruals are known. In January, we know the employee will
    get 20 days total this year. ConsumeAhead can use this.

  Non-Deterministic (HoursWorkedAccrual):
    Future accruals depend on external events. Can only use accrued-to-date.

PRORATION:
  For mid-year hires:
  - Hired June 15 with 20 days/year
  - 6.5 months remaining = 10.8 days prorated
  - YearlyAccrual handles this by counting months from hire date

EXAMPLE:
  // 20 days per year, accrued monthly
  accrual := &YearlyAccrual{
      AnnualDays: 20,
      Frequency:  generic.FreqMonthly,
  }

  // Generate accrual events for the year
  events := accrual.GenerateAccruals(jan1, dec31)
  // Returns 12 events, each with 1.67 days

  // Tenure-based with tiers
  tenureAccrual := &TenureAccrual{
      HireDate:  date(2020, 1, 1),
      Frequency: generic.FreqMonthly,
      Tiers: []TenureTier{
          {AfterYears: 0, AnnualDays: 15},
          {AfterYears: 3, AnnualDays: 20},
          {AfterYears: 5, AnnualDays: 25},
      },
  }

SEE ALSO:
  - generic/accrual.go: AccrualSchedule interface
  - generic/projection.go: Uses accruals for balance projection
  - rewards/accrual.go: Points-based accrual patterns
*/
package timeoff

import (
	"github.com/shopspring/decimal"
	"github.com/warp/resource-engine/generic"
)

// =============================================================================
// TIME-OFF ACCRUAL SCHEDULES
// =============================================================================

// YearlyAccrual implements generic.AccrualSchedule for "X days per year"
type YearlyAccrual struct {
	AnnualDays float64
	Frequency  generic.AccrualFrequency
}

func (ya *YearlyAccrual) GenerateAccruals(from, to generic.TimePoint) []generic.AccrualEvent {
	switch ya.Frequency {
	case generic.FreqUpfront:
		return ya.upfront(from, to)
	case generic.FreqMonthly:
		return ya.monthly(from, to)
	case generic.FreqDaily:
		return ya.daily(from, to)
	default:
		return ya.monthly(from, to)
	}
}

// IsDeterministic returns true - yearly accruals are predictable.
func (ya *YearlyAccrual) IsDeterministic() bool {
	return true
}

func (ya *YearlyAccrual) upfront(from, to generic.TimePoint) []generic.AccrualEvent {
	var events []generic.AccrualEvent
	for year := from.Year(); year <= to.Year(); year++ {
		grantDate := generic.StartOfYear(year)
		if from.BeforeOrEqual(grantDate) && grantDate.BeforeOrEqual(to) {
			events = append(events, generic.AccrualEvent{
				At:     grantDate,
				Amount: generic.NewAmount(ya.AnnualDays, generic.UnitDays),
				Reason: "annual grant",
			})
		}
	}
	return events
}

func (ya *YearlyAccrual) monthly(from, to generic.TimePoint) []generic.AccrualEvent {
	var events []generic.AccrualEvent
	monthly := ya.AnnualDays / 12

	current := generic.StartOfMonth(from.Year(), from.Month())
	end := generic.StartOfMonth(to.Year(), to.Month())

	for current.BeforeOrEqual(end) {
		if from.BeforeOrEqual(current) && current.BeforeOrEqual(to) {
			events = append(events, generic.AccrualEvent{
				At:     current,
				Amount: generic.NewAmount(monthly, generic.UnitDays),
				Reason: "monthly accrual",
			})
		}
		current = current.AddMonths(1)
	}
	return events
}

func (ya *YearlyAccrual) daily(from, to generic.TimePoint) []generic.AccrualEvent {
	var events []generic.AccrualEvent
	daily := ya.AnnualDays / 365

	current := from
	for current.BeforeOrEqual(to) {
		events = append(events, generic.AccrualEvent{
			At:     current,
			Amount: generic.NewAmount(daily, generic.UnitDays),
			Reason: "daily accrual",
		})
		current = current.AddDays(1)
	}
	return events
}

// =============================================================================
// HOURS-WORKED ACCRUAL (for hourly employees)
// =============================================================================

// HoursWorkedAccrual accrues X hours PTO per Y hours worked.
type HoursWorkedAccrual struct {
	PTOHoursEarned  float64 // PTO hours earned
	PerHoursWorked  float64 // per this many hours worked
	PayrollEvents   []PayrollEvent
}

type PayrollEvent struct {
	Date        generic.TimePoint
	HoursWorked float64
}

func (hwa *HoursWorkedAccrual) GenerateAccruals(from, to generic.TimePoint) []generic.AccrualEvent {
	var events []generic.AccrualEvent
	ratio := hwa.PTOHoursEarned / hwa.PerHoursWorked

	for _, pe := range hwa.PayrollEvents {
		if !from.BeforeOrEqual(pe.Date) || !pe.Date.BeforeOrEqual(to) {
			continue
		}
		hoursEarned := pe.HoursWorked * ratio
		daysEarned := hoursEarned / 8 // convert to days

		events = append(events, generic.AccrualEvent{
			At:     pe.Date,
			Amount: generic.NewAmount(daysEarned, generic.UnitDays),
			Reason: "hours worked accrual",
		})
	}
	return events
}

// IsDeterministic returns false - future hours worked are unknown.
func (hwa *HoursWorkedAccrual) IsDeterministic() bool {
	return false
}

// =============================================================================
// TENURE-BASED ACCRUAL
// =============================================================================

// TenureAccrual adjusts accrual rate based on tenure.
type TenureAccrual struct {
	HireDate  generic.TimePoint
	Tiers     []TenureTier
	Frequency generic.AccrualFrequency
}

type TenureTier struct {
	AfterYears int
	AnnualDays float64
}

func (ta *TenureAccrual) GenerateAccruals(from, to generic.TimePoint) []generic.AccrualEvent {
	var events []generic.AccrualEvent

	current := generic.StartOfMonth(from.Year(), from.Month())
	end := generic.StartOfMonth(to.Year(), to.Month())

	for current.BeforeOrEqual(end) {
		if !from.BeforeOrEqual(current) || !current.BeforeOrEqual(to) {
			current = current.AddMonths(1)
			continue
		}

		// Calculate tenure
		yearsOfTenure := current.Year() - ta.HireDate.Year()
		if current.Month() < ta.HireDate.Month() {
			yearsOfTenure--
		}

		// Find applicable tier
		var annualDays float64
		for _, tier := range ta.Tiers {
			if yearsOfTenure >= tier.AfterYears {
				annualDays = tier.AnnualDays
			}
		}

		if annualDays > 0 {
			monthly := annualDays / 12
			events = append(events, generic.AccrualEvent{
				At:     current,
				Amount: generic.NewAmount(monthly, generic.UnitDays),
				Reason: "tenure-based accrual",
			})
		}

		current = current.AddMonths(1)
	}
	return events
}

// IsDeterministic returns true - tenure progression is predictable.
func (ta *TenureAccrual) IsDeterministic() bool {
	return true
}

// Helper
var twelve = decimal.NewFromInt(12)
