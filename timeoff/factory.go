/*
Package timeoff provides time-off domain-specific policy factory functions.

These factory functions create JSON policy definitions for time-off resources
(PTO, sick leave, parental leave, etc.). They construct JSON strings directly
to avoid import cycles with the factory package.

USAGE:
  import "github.com/warp/resource-engine/timeoff"
  
  jsonStr := timeoff.StandardPTOJSON("pto-standard", "Standard PTO", 24, 5)
  policy, accrual, err := factory.ParsePolicy(jsonStr)
*/
package timeoff

import (
	"encoding/json"
)

// StandardPTOJSON returns JSON for a standard PTO policy.
func StandardPTOJSON(id, name string, annualDays, maxCarryover float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "pto",
		"unit":            "days",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": annualDays,
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

// UseItOrLoseItJSON returns JSON for a use-it-or-lose-it policy.
func UseItOrLoseItJSON(id, name string, annualDays float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "pto",
		"unit":            "days",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": annualDays,
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

// SickLeaveJSON returns JSON for a sick leave policy.
func SickLeaveJSON(id, name string, annualDays float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "sick",
		"unit":            "days",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": annualDays,
			"frequency":   "monthly",
		},
		"constraints": map[string]interface{}{
			"allow_negative": true,
		},
		"reconciliation_rules": []map[string]interface{}{{
			"trigger": "period_end",
			"actions": []map[string]interface{}{{"type": "expire"}},
		}},
	}
	b, _ := json.MarshalIndent(pj, "", "  ")
	return string(b)
}

// HourlyWorkerJSON returns JSON for hourly worker policy (consume up to accrued).
func HourlyWorkerJSON(id, name string, annualDays float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "pto",
		"unit":            "days",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_up_to_accrued",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": annualDays,
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

// CarryoverBonusJSON returns JSON for carryover bonus (separate policy for carried days).
func CarryoverBonusJSON(id, name string, days float64) string {
	zero := 0.0
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "pto",
		"unit":            "days",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": days,
			"frequency":   "upfront",
		},
		"reconciliation_rules": []map[string]interface{}{{
			"trigger": "period_end",
			"actions": []map[string]interface{}{
				{"type": "carryover", "max_carryover": zero},
				{"type": "expire"},
			},
		}},
	}
	b, _ := json.MarshalIndent(pj, "", "  ")
	return string(b)
}

// MaternityLeaveJSON returns JSON for maternity/parental leave policy.
func MaternityLeaveJSON(id, name string, weeks float64) string {
	days := weeks * 5 // Convert weeks to workdays
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "parental",
		"unit":            "days",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": days,
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

// PaternityLeaveJSON returns JSON for paternity leave policy.
func PaternityLeaveJSON(id, name string, weeks float64) string {
	days := weeks * 5
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "parental",
		"unit":            "days",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": days,
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

// BereavementLeaveJSON returns JSON for bereavement leave.
func BereavementLeaveJSON(id, name string, days float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "bereavement",
		"unit":            "days",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": days,
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

// JuryDutyLeaveJSON returns JSON for jury duty leave (unlimited with approval).
func JuryDutyLeaveJSON(id, name string) string {
	pj := map[string]interface{}{
		"id":            id,
		"name":          name,
		"resource_type": "jury_duty",
		"unit":          "days",
		"period_type":   "calendar_year",
		"is_unlimited":  true,
	}
	b, _ := json.MarshalIndent(pj, "", "  ")
	return string(b)
}

// FloatingHolidayJSON returns JSON for floating holidays.
func FloatingHolidayJSON(id, name string, days float64) string {
	pj := map[string]interface{}{
		"id":              id,
		"name":            name,
		"resource_type":   "floating_holiday",
		"unit":            "days",
		"period_type":     "calendar_year",
		"consumption_mode": "consume_ahead",
		"accrual": map[string]interface{}{
			"type":        "yearly",
			"annual_days": days,
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
