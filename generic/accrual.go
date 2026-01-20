package generic

// =============================================================================
// ACCRUAL SCHEDULE - Interface for how resources accumulate
// =============================================================================

// AccrualSchedule generates accrual events for a time range.
// Implementations define the business logic (yearly, hourly, tenure-based, etc.)
type AccrualSchedule interface {
	// GenerateAccruals returns accrual events in [from, to].
	GenerateAccruals(from, to TimePoint) []AccrualEvent

	// IsDeterministic returns true if future accruals can be predicted.
	// - Deterministic: YearlyAccrual (20 days/year is known in advance)
	// - Non-deterministic: HoursWorkedAccrual (depends on future hours)
	//
	// This affects balance calculation:
	// - Deterministic: TotalEntitlement includes future accruals
	// - Non-deterministic: TotalEntitlement = AccruedToDate
	IsDeterministic() bool
}

// AccrualEvent represents a single accrual occurrence.
type AccrualEvent struct {
	At     TimePoint
	Amount Amount
	Reason string
}

// =============================================================================
// ACCRUAL CONFIGURATION TYPES (used by implementations)
// =============================================================================

type AccrualRate struct {
	Amount Amount
	Per    AccrualPeriod
}

type AccrualPeriod string

const (
	PerYear  AccrualPeriod = "year"
	PerMonth AccrualPeriod = "month"
	PerWeek  AccrualPeriod = "week"
	PerDay   AccrualPeriod = "day"
)

type AccrualFrequency string

const (
	FreqUpfront  AccrualFrequency = "upfront"
	FreqMonthly  AccrualFrequency = "monthly"
	FreqBiweekly AccrualFrequency = "biweekly"
	FreqDaily    AccrualFrequency = "daily"
)

type ProrateMethod string

const (
	ProrateNone   ProrateMethod = "none"
	ProrateLinear ProrateMethod = "linear"
)
