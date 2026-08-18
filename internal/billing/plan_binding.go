package billing

import (
	"slices"
	"strings"
)

// NormalizePlanBindings upgrades the legacy single-plan fields in memory and
// removes blank or duplicate relationships. It returns whether the key changed.
func (k *KeyState) NormalizePlanBindings() bool {
	if k == nil {
		return false
	}
	originalCount := len(k.PlanBindings)
	bindings := slices.Clone(k.PlanBindings)
	if originalCount == 0 && strings.TrimSpace(k.PlanID) != "" {
		bindings = append(bindings, PlanBinding{PlanID: k.PlanID, Cycle: k.Cycle})
	}
	normalized := normalizePlanBindings(bindings)
	changed := !slices.Equal(k.PlanBindings, normalized) || k.PlanID != "" || k.Cycle != (Cycle{})
	k.PlanBindings = normalized
	k.PlanID = ""
	k.Cycle = Cycle{}
	return changed
}

func normalizePlanBindings(bindings []PlanBinding) []PlanBinding {
	normalized := make([]PlanBinding, 0, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		binding.PlanID = strings.TrimSpace(binding.PlanID)
		if binding.PlanID == "" {
			continue
		}
		key := strings.ToLower(binding.PlanID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if !binding.Cycle.StartAt.IsZero() {
			binding.Cycle.PlanID = binding.PlanID
		}
		normalized = append(normalized, binding)
	}
	return normalized
}

func (k *KeyState) FindPlanBinding(planID string) (*PlanBinding, bool) {
	if k == nil {
		return nil, false
	}
	planID = strings.TrimSpace(planID)
	for index := range k.PlanBindings {
		if k.PlanBindings[index].PlanID == planID {
			return &k.PlanBindings[index], true
		}
	}
	return nil, false
}

func (k *KeyState) HasPlan(planID string) bool {
	if k != nil && len(k.PlanBindings) == 0 && strings.TrimSpace(k.PlanID) == planID {
		return true
	}
	_, exists := k.FindPlanBinding(planID)
	return exists
}

func (k *KeyState) BindPlan(planID string) bool {
	k.NormalizePlanBindings()
	planID = strings.TrimSpace(planID)
	if planID == "" || k.HasPlan(planID) {
		return false
	}
	k.PlanBindings = append(k.PlanBindings, PlanBinding{PlanID: planID})
	return true
}

func (k *KeyState) UnbindPlan(planID string) bool {
	k.NormalizePlanBindings()
	previous := len(k.PlanBindings)
	k.PlanBindings = slices.DeleteFunc(k.PlanBindings, func(binding PlanBinding) bool {
		return binding.PlanID == planID
	})
	return len(k.PlanBindings) != previous
}

func (k *KeyState) ResetPlanCycles() bool {
	k.NormalizePlanBindings()
	changed := false
	for index := range k.PlanBindings {
		if k.PlanBindings[index].Cycle == (Cycle{}) {
			continue
		}
		k.PlanBindings[index].Cycle = Cycle{}
		changed = true
	}
	return changed
}

func (k *KeyState) PlanIDs() []string {
	if k == nil {
		return nil
	}
	ids := make([]string, 0, len(k.PlanBindings)+1)
	for _, binding := range k.PlanBindings {
		ids = append(ids, binding.PlanID)
	}
	if len(ids) == 0 && strings.TrimSpace(k.PlanID) != "" {
		ids = append(ids, strings.TrimSpace(k.PlanID))
	}
	return ids
}
