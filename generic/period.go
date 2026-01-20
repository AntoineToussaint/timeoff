package generic

import "time"

// =============================================================================
// PERIOD - The core concept for balance calculation
// =============================================================================

// Period defines the time boundary for balance calculation.
// Balance is ALWAYS computed for a period, not at a point in time.
//
// Examples:
//   - Calendar year 2025: Jan 1 - Dec 31
//   - Fiscal year 2025: Apr 1 - Mar 31
//   - Anniversary year: Hire date + 1 year
type Period struct {
	Start TimePoint
	End   TimePoint
}

// Contains returns true if the time point is within the period [Start, End]
func (p Period) Contains(t TimePoint) bool {
	return t.AfterOrEqual(p.Start) && t.BeforeOrEqual(p.End)
}

// Days returns all days in the period as a slice of TimePoints.
func (p Period) Days() []TimePoint {
	var days []TimePoint
	current := p.Start
	for current.BeforeOrEqual(p.End) {
		days = append(days, current)
		current = current.AddDays(1)
	}
	return days
}

// String returns a string representation of the period.
func (p Period) String() string {
	return "[" + p.Start.String() + ", " + p.End.String() + "]"
}

// PeriodType defines how periods are calculated
type PeriodType string

const (
	PeriodCalendarYear PeriodType = "calendar_year" // Jan 1 - Dec 31
	PeriodFiscalYear   PeriodType = "fiscal_year"   // Custom start (e.g., Apr 1)
	PeriodAnniversary  PeriodType = "anniversary"   // Based on hire/assignment date
	PeriodRolling      PeriodType = "rolling"       // Rolling 12 months
)

// PeriodConfig defines how to calculate periods for a policy
type PeriodConfig struct {
	Type PeriodType

	// For fiscal year: which month starts the fiscal year (1-12)
	FiscalYearStartMonth time.Month

	// For anniversary: the anchor date (e.g., hire date)
	AnchorDate *TimePoint
}

// =============================================================================
// PERIOD CALCULATOR - Determines which period a date falls into
// =============================================================================

// PeriodFor returns the period that contains the given date
func (pc PeriodConfig) PeriodFor(date TimePoint) Period {
	switch pc.Type {
	case PeriodCalendarYear:
		return Period{
			Start: StartOfYear(date.Year()),
			End:   EndOfYear(date.Year()),
		}

	case PeriodFiscalYear:
		return pc.fiscalYearPeriod(date)

	case PeriodAnniversary:
		if pc.AnchorDate == nil {
			// Fallback to calendar year
			return Period{Start: StartOfYear(date.Year()), End: EndOfYear(date.Year())}
		}
		return pc.anniversaryPeriod(date)

	case PeriodRolling:
		// Rolling 12 months from date
		return Period{
			Start: date.AddYears(-1).AddDays(1),
			End:   date,
		}

	default:
		return Period{Start: StartOfYear(date.Year()), End: EndOfYear(date.Year())}
	}
}

func (pc PeriodConfig) fiscalYearPeriod(date TimePoint) Period {
	year := date.Year()
	fiscalStart := NewTimePoint(year, pc.FiscalYearStartMonth, 1)

	// If date is before fiscal year start, we're in previous fiscal year
	if date.Before(fiscalStart) {
		fiscalStart = NewTimePoint(year-1, pc.FiscalYearStartMonth, 1)
	}

	fiscalEnd := fiscalStart.AddYears(1).AddDays(-1)
	return Period{Start: fiscalStart, End: fiscalEnd}
}

func (pc PeriodConfig) anniversaryPeriod(date TimePoint) Period {
	anchor := *pc.AnchorDate
	
	// Find which anniversary year we're in
	yearsElapsed := date.Year() - anchor.Year()
	
	anniversaryThisYear := NewTimePoint(
		anchor.Year()+yearsElapsed,
		anchor.Month(),
		anchor.Day(),
	)
	
	// If date is before this year's anniversary, we're in previous period
	if date.Before(anniversaryThisYear) {
		yearsElapsed--
		anniversaryThisYear = NewTimePoint(
			anchor.Year()+yearsElapsed,
			anchor.Month(),
			anchor.Day(),
		)
	}
	
	periodEnd := anniversaryThisYear.AddYears(1).AddDays(-1)
	return Period{Start: anniversaryThisYear, End: periodEnd}
}

// NextPeriod returns the period following this one
func (p Period) NextPeriod() Period {
	// Assuming periods are contiguous
	newStart := p.End.AddDays(1)
	duration := DaysBetween(p.Start, p.End)
	newEnd := newStart.AddDays(duration)
	return Period{Start: newStart, End: newEnd}
}

// PreviousPeriod returns the period before this one
func (p Period) PreviousPeriod() Period {
	duration := DaysBetween(p.Start, p.End)
	newEnd := p.Start.AddDays(-1)
	newStart := newEnd.AddDays(-duration)
	return Period{Start: newStart, End: newEnd}
}
