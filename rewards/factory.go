/*
Package rewards provides rewards domain-specific policy factory functions.

These factory functions create JSON policy definitions for rewards resources
(wellness points, learning credits, recognition points, etc.). They construct
JSON strings directly to avoid import cycles with the factory package.

USAGE:
  import "github.com/warp/resource-engine/rewards"
  
  jsonStr := rewards.WellnessPointsJSON("wellness-program", "Wellness Points", 1200, 200)
  policy, accrual, err := factory.ParsePolicy(jsonStr)
*/
package rewards

import (
	"encoding/json"
)

// WellnessPointsJSON returns JSON for wellness/health points.
func WellnessPointsJSON(id, name string, annualPoints float64, maxCarryover float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "wellness_points",
		"unit":            "points",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_up_to_accrued",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": annualPoints,
			"frequency":   "monthly",
		},
		"constraints": map[string]interface{}{
			"allow_negative": false,
		},
		"reconciliation_rules": []map[string]interface{}{{
			"trigger": "period_end",
			"actions": []map[string]interface{}{
				{"type": "carryover", "max_carryover": maxCarryover},
				{"type": "expire"},
			},
		}},
	}
	b, _ := json.MarshalIndent(pj, "", "  ")
	return string(b)
}

// LearningCreditsJSON returns JSON for professional development/training budget.
func LearningCreditsJSON(id, name string, annualBudget float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "learning_credits",
		"unit":            "dollars",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": annualBudget,
			"frequency":   "upfront",
		},
		"constraints": map[string]interface{}{
			"allow_negative": false,
		},
		"reconciliation_rules": []map[string]interface{}{{
			"trigger": "period_end",
			"actions": []map[string]interface{}{{"type": "expire"}},
		}},
	}
	b, _ := json.MarshalIndent(pj, "", "  ")
	return string(b)
}

// RecognitionPointsJSON returns JSON for peer recognition/kudos points.
func RecognitionPointsJSON(id, name string, quarterlyAllowance float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "recognition_points",
		"unit":            "points",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_up_to_accrued",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": quarterlyAllowance * 4,
			"frequency":   "monthly",
		},
		"constraints": map[string]interface{}{
			"allow_negative": false,
		},
		"reconciliation_rules": []map[string]interface{}{{
			"trigger": "period_end",
			"actions": []map[string]interface{}{{"type": "expire"}},
		}},
	}
	b, _ := json.MarshalIndent(pj, "", "  ")
	return string(b)
}

// FlexBenefitsBudgetJSON returns JSON for flexible benefits (like FSA/HSA).
func FlexBenefitsBudgetJSON(id, name string, annualBudget float64, maxRollover float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "flex_benefits",
		"unit":            "dollars",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": annualBudget,
			"frequency":   "upfront",
		},
		"constraints": map[string]interface{}{
			"allow_negative": false,
		},
		"reconciliation_rules": []map[string]interface{}{{
			"trigger": "period_end",
			"actions": []map[string]interface{}{
				{"type": "carryover", "max_carryover": maxRollover},
				{"type": "expire"},
			},
		}},
	}
	b, _ := json.MarshalIndent(pj, "", "  ")
	return string(b)
}

// RemoteWorkDaysJSON returns JSON for work-from-home allowance.
func RemoteWorkDaysJSON(id, name string, monthlyDays float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "remote_days",
		"unit":            "days",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_up_to_accrued",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": monthlyDays * 12,
			"frequency":   "monthly",
		},
		"constraints": map[string]interface{}{
			"allow_negative": false,
		},
		"reconciliation_rules": []map[string]interface{}{{
			"trigger": "period_end",
			"actions": []map[string]interface{}{{"type": "expire"}},
		}},
	}
	b, _ := json.MarshalIndent(pj, "", "  ")
	return string(b)
}

// VolunteerHoursJSON returns JSON for paid volunteer time off.
func VolunteerHoursJSON(id, name string, annualHours float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "volunteer_hours",
		"unit":            "hours",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": annualHours,
			"frequency":   "upfront",
		},
		"reconciliation_rules": []map[string]interface{}{{
			"trigger": "period_end",
			"actions": []map[string]interface{}{{"type": "expire"}},
		}},
	}
	b, _ := json.MarshalIndent(pj, "", "  ")
	return string(b)
}
