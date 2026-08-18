package billing

import (
	"strings"
	"time"
)

type Decision struct {
	Allowed      bool
	PlanID       string
	PlanName     string
	LimitUSD     float64
	SpentUSD     float64
	CycleStartAt time.Time
	ResetAt      time.Time
}

// It fails open by design. Anything the plugin cannot resolve — an
// unattributable request, an unknown key, no subscription, or a deleted plan —
// is allowed through. A billing plugin that starts
// rejecting traffic because of its own missing state would be worse than one
// that briefly under-charges.
//
// The first admitted use starts a key-relative period. An elapsed period is
// closed and a fresh one starts at this use; idle time never consumes a new
// period.
func (s *Store) Authorize(scope, billingModel string, at time.Time) Decision {
	allowed := Decision{Allowed: true}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return allowed
	}
	if at.IsZero() {
		at = s.Now()
	}
	decision := updateResult(s, func(state *State) (Decision, Changes) {
		key := state.Keys[scope]
		if key == nil {
			return allowed, Changes{}
		}
		changed := key.NormalizePlanBindings()
		for _, planID := range key.PlanIDs() {
			if _, exists := state.FindPlan(planID); !exists {
				changed = key.UnbindPlan(planID) || changed
			}
		}
		binding, plan, matched := state.matchingPlanBinding(key, billingModel)
		if !matched {
			if changed {
				return allowed, Changes{Keys: []string{scope}}
			}
			return allowed, Changes{}
		}
		if activateCycle(&binding.Cycle, plan, at) {
			changed = true
		}
		current := Decision{
			Allowed:      true,
			PlanID:       plan.ID,
			PlanName:     plan.Name,
			LimitUSD:     plan.AmountUSD,
			SpentUSD:     binding.Cycle.SpentUSD,
			CycleStartAt: binding.Cycle.StartAt,
			ResetAt:      binding.Cycle.EndAt,
		}
		changes := Changes{}
		if changed {
			changes.Keys = []string{scope}
		}
		if binding.Cycle.SpentUSD < plan.AmountUSD {
			return current, changes
		}
		current.Allowed = false
		return current, changes
	})
	return decision
}
