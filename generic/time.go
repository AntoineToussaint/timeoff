package generic

import (
	"time"
)

// =============================================================================
// TIME POINT - Concrete time abstraction (this IS a time resource system)
// =============================================================================

type TimePoint struct {
	Time        time.Time
	Granularity Granularity
}

type Granularity int

const (
	GranularityDay Granularity = iota
	GranularityHour
	GranularityMinute
)

// Constructors
func NewTimePoint(year int, month time.Month, day int) TimePoint {
	return TimePoint{Time: time.Date(year, month, day, 0, 0, 0, 0, time.UTC), Granularity: GranularityDay}
}

func NewTimePointWithHour(year int, month time.Month, day, hour int) TimePoint {
	return TimePoint{Time: time.Date(year, month, day, hour, 0, 0, 0, time.UTC), Granularity: GranularityHour}
}

func Today() TimePoint {
	now := time.Now()
	return NewTimePoint(now.Year(), now.Month(), now.Day())
}

// Comparison
func (tp TimePoint) Before(other TimePoint) bool      { return tp.normalize().Before(other.normalize()) }
func (tp TimePoint) Equal(other TimePoint) bool       { return tp.normalize().Equal(other.normalize()) }
func (tp TimePoint) After(other TimePoint) bool       { return tp.normalize().After(other.normalize()) }
func (tp TimePoint) BeforeOrEqual(other TimePoint) bool { return tp.Before(other) || tp.Equal(other) }
func (tp TimePoint) AfterOrEqual(other TimePoint) bool  { return tp.After(other) || tp.Equal(other) }

func (tp TimePoint) normalize() time.Time {
	switch tp.Granularity {
	case GranularityDay:
		return time.Date(tp.Time.Year(), tp.Time.Month(), tp.Time.Day(), 0, 0, 0, 0, time.UTC)
	case GranularityHour:
		return time.Date(tp.Time.Year(), tp.Time.Month(), tp.Time.Day(), tp.Time.Hour(), 0, 0, 0, time.UTC)
	default:
		return tp.Time
	}
}

// Arithmetic
func (tp TimePoint) AddDays(n int) TimePoint   { return TimePoint{Time: tp.Time.AddDate(0, 0, n), Granularity: tp.Granularity} }
func (tp TimePoint) AddMonths(n int) TimePoint { return TimePoint{Time: tp.Time.AddDate(0, n, 0), Granularity: tp.Granularity} }
func (tp TimePoint) AddYears(n int) TimePoint  { return TimePoint{Time: tp.Time.AddDate(n, 0, 0), Granularity: tp.Granularity} }

// Properties
func (tp TimePoint) Year() int            { return tp.Time.Year() }
func (tp TimePoint) Month() time.Month    { return tp.Time.Month() }
func (tp TimePoint) Day() int             { return tp.Time.Day() }
func (tp TimePoint) Weekday() time.Weekday { return tp.Time.Weekday() }
func (tp TimePoint) IsWeekend() bool      { wd := tp.Weekday(); return wd == time.Saturday || wd == time.Sunday }
func (tp TimePoint) IsWorkday() bool      { return !tp.IsWeekend() }
func (tp TimePoint) IsZero() bool         { return tp.Time.IsZero() }

func (tp TimePoint) String() string {
	switch tp.Granularity {
	case GranularityDay:
		return tp.Time.Format("2006-01-02")
	case GranularityHour:
		return tp.Time.Format("2006-01-02 15:00")
	default:
		return tp.Time.Format(time.RFC3339)
	}
}

// =============================================================================
// HOLIDAY CALENDAR - Company-specific holidays
// =============================================================================

// Holiday represents a company holiday that should not count against time-off.
type Holiday struct {
	ID        string
	CompanyID string    // Empty string = global/default holidays
	Date      TimePoint // The holiday date
	Name      string    // e.g., "Christmas Day", "Independence Day"
	Recurring bool      // true = same month/day every year
}

// HolidayCalendar provides holiday lookup functionality.
type HolidayCalendar interface {
	// IsHoliday checks if a date is a holiday for the given company.
	// Checks company-specific holidays first, then global holidays.
	IsHoliday(companyID string, date TimePoint) bool

	// GetHolidays returns all holidays for a company in a given year.
	// Includes both company-specific and global holidays.
	GetHolidays(companyID string, year int) []Holiday
}

// DefaultHolidayCalendar is a no-op calendar for when holidays are disabled.
type DefaultHolidayCalendar struct{}

func (d *DefaultHolidayCalendar) IsHoliday(companyID string, date TimePoint) bool { return false }
func (d *DefaultHolidayCalendar) GetHolidays(companyID string, year int) []Holiday { return nil }

// IsWorkdayWithHolidays checks if a date is a working day, considering holidays.
func (tp TimePoint) IsWorkdayWithHolidays(calendar HolidayCalendar, companyID string) bool {
	if tp.IsWeekend() {
		return false
	}
	if calendar != nil && calendar.IsHoliday(companyID, tp) {
		return false
	}
	return true
}

// =============================================================================
// TIME UTILITIES
// =============================================================================
// Note: Period type is defined in period.go to avoid duplication

func DaysBetween(from, to TimePoint) int { return int(to.normalize().Sub(from.normalize()).Hours() / 24) }
func StartOfYear(year int) TimePoint     { return NewTimePoint(year, time.January, 1) }
func EndOfYear(year int) TimePoint       { return NewTimePoint(year, time.December, 31) }
func StartOfMonth(year int, month time.Month) TimePoint { return NewTimePoint(year, month, 1) }
func EndOfMonth(year int, month time.Month) TimePoint {
	t := time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	return TimePoint{Time: t, Granularity: GranularityDay}
}
