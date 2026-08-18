package billing

import (
	"math"
	"strings"
	"time"
)

const maxCustomPeriodSeconds = int64(math.MaxInt64) / int64(time.Second)

func (p Plan) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return invalidf("订阅计划 ID 不能为空")
	}
	if !(p.AmountUSD > 0) {
		return invalidf("订阅计划 %s：额度必须大于 0", p.ID)
	}
	switch p.Period.Kind {
	case PeriodDaily, PeriodWeekly, PeriodMonthly, PeriodNever:
		return nil
	case PeriodCustom:
		if p.Period.Seconds <= 0 {
			return invalidf("订阅计划 %s：自定义周期必须大于 0 秒", p.ID)
		}
		if p.Period.Seconds > maxCustomPeriodSeconds {
			return invalidf("订阅计划 %s：自定义周期不能超过 %d 秒", p.ID, maxCustomPeriodSeconds)
		}
		return nil
	default:
		return invalidf("订阅计划 %s：不支持周期类型 %q", p.ID, p.Period.Kind)
	}
}

// CycleEnd returns the end of a period started by one key at start. Calendar
// boundaries and time zones are deliberately absent: daily, weekly, and
// monthly are fixed 24-hour, 7-day, and 30-day subscription lengths.
func (p Plan) CycleEnd(start time.Time) time.Time {
	switch p.Period.Kind {
	case PeriodWeekly:
		return start.Add(7 * 24 * time.Hour)
	case PeriodMonthly:
		return start.Add(30 * 24 * time.Hour)
	case PeriodCustom:
		seconds := p.Period.Seconds
		if seconds <= 0 {
			seconds = 1
		} else if seconds > maxCustomPeriodSeconds {
			seconds = maxCustomPeriodSeconds
		}
		return start.Add(time.Duration(seconds) * time.Second)
	case PeriodNever:
		return time.Time{}
	default:
		return start.Add(24 * time.Hour)
	}
}

func (s *State) FindPlan(id string) (Plan, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Plan{}, false
	}
	for _, plan := range s.Plans {
		if plan.ID == id {
			return plan, true
		}
	}
	return Plan{}, false
}

// settleExpiredCycle returns an elapsed timed subscription to its inactive
// initial state. It never starts a new period; reads and manual resets therefore
// cannot make an idle key's clock run.
func settleExpiredCycle(cycle *Cycle, plan Plan, now time.Time) bool {
	if cycle == nil || plan.Period.Kind == PeriodNever || cycle.StartAt.IsZero() || cycle.EndAt.IsZero() || now.Before(cycle.EndAt) {
		return false
	}
	*cycle = Cycle{}
	return true
}

// activateCycle settles an expired period and lazily starts the next one at the
// instant this key is actually used. A never-reset plan records its first-use
// time for history but intentionally has no EndAt.
func activateCycle(cycle *Cycle, plan Plan, now time.Time) bool {
	changed := settleExpiredCycle(cycle, plan, now)
	if cycle == nil || !cycle.StartAt.IsZero() {
		return changed
	}
	*cycle = Cycle{PlanID: plan.ID, StartAt: now, EndAt: plan.CycleEnd(now)}
	return true
}
