// Package timeoff implements time-off specific resource management.
// It uses the generic engine with time-off specific policies and accrual schedules.
package timeoff

import "github.com/warp/resource-engine/generic"

// =============================================================================
// TIME-OFF RESOURCE TYPE
// =============================================================================

// Resource is the concrete resource type for time-off domain.
// Implements generic.ResourceType interface.
type Resource string

func (r Resource) ResourceID() string     { return string(r) }
func (r Resource) ResourceDomain() string { return "timeoff" }

// Compile-time check that Resource implements generic.ResourceType
var _ generic.ResourceType = Resource("")

// Resource types for time-off
const (
	ResourcePTO             Resource = "pto"
	ResourceSick            Resource = "sick"
	ResourcePersonal        Resource = "personal"
	ResourceParental        Resource = "parental"
	ResourceFloatingHoliday Resource = "floating_holiday"
	ResourceBereavement     Resource = "bereavement"
	ResourceJuryDuty        Resource = "jury_duty"
	ResourceVacation        Resource = "vacation"
)

// Register all time-off resources with the generic registry
func init() {
	generic.RegisterResource(ResourcePTO)
	generic.RegisterResource(ResourceSick)
	generic.RegisterResource(ResourcePersonal)
	generic.RegisterResource(ResourceParental)
	generic.RegisterResource(ResourceFloatingHoliday)
	generic.RegisterResource(ResourceBereavement)
	generic.RegisterResource(ResourceJuryDuty)
	generic.RegisterResource(ResourceVacation)
}

// TimeOffRequest represents a request for time off.
type TimeOffRequest struct {
	ID         string
	EntityID   generic.EntityID
	PolicyID   generic.PolicyID
	Resource   generic.ResourceType
	Days       []generic.TimePoint // specific days requested
	HoursPerDay float64            // hours per day (default 8)
	Status     RequestStatus
	Reason     string
}

type RequestStatus string

const (
	StatusDraft    RequestStatus = "draft"
	StatusPending  RequestStatus = "pending"
	StatusApproved RequestStatus = "approved"
	StatusRejected RequestStatus = "rejected"
	StatusCanceled RequestStatus = "canceled"
)

// ToConsumptionEvents converts request to generic consumption events.
func (r *TimeOffRequest) ToConsumptionEvents() []generic.ConsumptionEvent {
	hoursPerDay := r.HoursPerDay
	if hoursPerDay == 0 {
		hoursPerDay = 8 // default
	}

	events := make([]generic.ConsumptionEvent, len(r.Days))
	for i, day := range r.Days {
		events[i] = generic.ConsumptionEvent{
			At:     day,
			Amount: generic.NewAmount(hoursPerDay/8, generic.UnitDays), // normalize to days
		}
	}
	return events
}

// FilterWorkdays removes weekends from requested days.
func (r *TimeOffRequest) FilterWorkdays() {
	var workdays []generic.TimePoint
	for _, day := range r.Days {
		if day.IsWorkday() {
			workdays = append(workdays, day)
		}
	}
	r.Days = workdays
}
