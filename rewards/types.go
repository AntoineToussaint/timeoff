/*
Package rewards provides domain-specific implementations for rewards,
benefits, and point-based resource management.

PURPOSE:
  Demonstrates the versatility of the generic timed resource engine
  beyond time-off use cases. The same engine handles:
  - Wellness points (earn through activities, spend on rewards)
  - Learning credits (annual budget for professional development)
  - Recognition points (peer-to-peer kudos, redeem for gift cards)
  - Flex benefits (HSA/FSA style accounts)
  - Volunteer hours (paid time off for community service)

KEY DIFFERENCES FROM TIME-OFF:
  1. Units: Points, dollars, hours (not just days)
  2. Non-unique consumption: Can earn multiple kudos on same day
  3. Event-based accruals: Points earned from activities, not calendar
  4. Different reconciliation: Some carry over indefinitely

RESOURCE TYPES:
  wellness_points:    Earned monthly or through activities (gym, screening)
  learning_credits:   Annual professional development budget
  recognition_points: Peer recognition, redeemable for rewards
  flex_benefits:      Flexible spending account balance
  remote_days:        Work-from-home allowance per month
  volunteer_hours:    Paid volunteering time

UNITS:
  UnitPoints:  Generic points (wellness, recognition)
  UnitDollars: Currency (learning credits, flex benefits)
  UnitHours:   Time (volunteer hours)
  UnitDays:    Days (remote work allowance)

EXAMPLE FLOW:
  1. Employee earns 100 wellness points (gym visit)
  2. Employee earns 50 points (health screening)
  3. Employee redeems 75 points for fitness tracker
  4. Balance: 150 - 75 = 75 points

SEE ALSO:
  - policies.go: Pre-built policy configurations
  - accrual.go: Point-specific accrual patterns
  - timeoff/: Comparison with time-off implementation
*/
package rewards

import (
	"github.com/warp/resource-engine/generic"
)

// =============================================================================
// REWARDS RESOURCE TYPE
// =============================================================================

// Resource is the concrete resource type for rewards domain.
// Implements generic.ResourceType interface.
type Resource string

func (r Resource) ResourceID() string     { return string(r) }
func (r Resource) ResourceDomain() string { return "rewards" }

// Compile-time check that Resource implements generic.ResourceType
var _ generic.ResourceType = Resource("")

// Resource types for the rewards domain
const (
	ResourceWellnessPoints    Resource = "wellness_points"
	ResourceLearningCredits   Resource = "learning_credits"
	ResourceRecognitionPoints Resource = "recognition_points"
	ResourceFlexBenefits      Resource = "flex_benefits"
	ResourceRemoteDays        Resource = "remote_days"
	ResourceVolunteerHours    Resource = "volunteer_hours"
)

// Register all rewards resources with the generic registry
func init() {
	generic.RegisterResource(ResourceWellnessPoints)
	generic.RegisterResource(ResourceLearningCredits)
	generic.RegisterResource(ResourceRecognitionPoints)
	generic.RegisterResource(ResourceFlexBenefits)
	generic.RegisterResource(ResourceRemoteDays)
	generic.RegisterResource(ResourceVolunteerHours)
}

// Units for rewards
const (
	UnitPoints  generic.Unit = "points"
	UnitDollars generic.Unit = "dollars"
	UnitHours   generic.Unit = "hours"
	UnitDays    generic.Unit = "days"
)

// =============================================================================
// WELLNESS PROGRAM
// =============================================================================

// WellnessActivity represents an activity that earns wellness points
type WellnessActivity struct {
	ID          string
	Name        string
	Points      float64
	Category    WellnessCategory
	MaxPerMonth int // 0 = unlimited
}

type WellnessCategory string

const (
	WellnessFitness    WellnessCategory = "fitness"
	WellnessNutrition  WellnessCategory = "nutrition"
	WellnessMindful    WellnessCategory = "mindfulness"
	WellnessPreventive WellnessCategory = "preventive"
)

// Common wellness activities
var (
	ActivityGymVisit = WellnessActivity{
		ID: "gym-visit", Name: "Gym Visit", Points: 10,
		Category: WellnessFitness, MaxPerMonth: 20,
	}
	ActivityHealthScreening = WellnessActivity{
		ID: "health-screening", Name: "Annual Health Screening", Points: 200,
		Category: WellnessPreventive, MaxPerMonth: 1,
	}
	ActivityStepsGoal = WellnessActivity{
		ID: "steps-goal", Name: "Daily Steps Goal (10k)", Points: 5,
		Category: WellnessFitness, MaxPerMonth: 31,
	}
	ActivityMeditation = WellnessActivity{
		ID: "meditation", Name: "Meditation Session", Points: 5,
		Category: WellnessMindful, MaxPerMonth: 30,
	}
)

// =============================================================================
// RECOGNITION / KUDOS
// =============================================================================

// Kudos represents a peer recognition event
type Kudos struct {
	ID         string
	FromEntity generic.EntityID
	ToEntity   generic.EntityID
	Points     float64
	Message    string
	Category   KudosCategory
	CreatedAt  generic.TimePoint
}

type KudosCategory string

const (
	KudosTeamwork   KudosCategory = "teamwork"
	KudosInnovation KudosCategory = "innovation"
	KudosCustomer   KudosCategory = "customer_focus"
	KudosLeadership KudosCategory = "leadership"
	KudosGeneral    KudosCategory = "general"
)

// KudosTier defines recognition point values
type KudosTier struct {
	Name   string
	Points float64
}

var (
	KudosTierBasic  = KudosTier{Name: "Thanks", Points: 10}
	KudosTierSilver = KudosTier{Name: "Great Job", Points: 25}
	KudosTierGold   = KudosTier{Name: "Outstanding", Points: 50}
	KudosManagerBonus = KudosTier{Name: "Manager Bonus", Points: 100}
)

// =============================================================================
// LEARNING & DEVELOPMENT
// =============================================================================

// LearningExpense represents a learning/training expense
type LearningExpense struct {
	ID          string
	EntityID    generic.EntityID
	Amount      float64
	Category    LearningCategory
	Description string
	Receipt     string // URL or reference
	ApprovedBy  generic.EntityID
}

type LearningCategory string

const (
	LearningCourse       LearningCategory = "course"
	LearningConference   LearningCategory = "conference"
	LearningCertification LearningCategory = "certification"
	LearningBooks        LearningCategory = "books"
	LearningSubscription LearningCategory = "subscription"
)

// =============================================================================
// REWARDS CATALOG
// =============================================================================

// RewardItem represents something that can be redeemed with points
type RewardItem struct {
	ID           string
	Name         string
	Description  string
	PointsCost   float64
	ResourceType generic.ResourceType // Which point type can redeem this
	Category     RewardCategory
	InStock      bool
}

type RewardCategory string

const (
	RewardGiftCard    RewardCategory = "gift_card"
	RewardMerchandise RewardCategory = "merchandise"
	RewardExperience  RewardCategory = "experience"
	RewardDonation    RewardCategory = "donation"
	RewardTimeOff     RewardCategory = "time_off" // Convert points to PTO
)

// Redemption represents a reward redemption request
type Redemption struct {
	ID         string
	EntityID   generic.EntityID
	RewardItem RewardItem
	Points     float64
	Status     RedemptionStatus
	CreatedAt  generic.TimePoint
}

type RedemptionStatus string

const (
	RedemptionPending   RedemptionStatus = "pending"
	RedemptionApproved  RedemptionStatus = "approved"
	RedemptionFulfilled RedemptionStatus = "fulfilled"
	RedemptionCancelled RedemptionStatus = "cancelled"
)
