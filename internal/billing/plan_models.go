package billing

import (
	"slices"
	"sort"
	"strings"
)

func (s *State) modelsForSelection(groups, selected []string) ([]string, bool) {
	if len(groups) == 0 && len(selected) == 0 {
		return nil, false
	}
	models := slices.Clone(selected)
	for _, id := range groups {
		if group, exists := s.FindModelGroup(id); exists {
			models = append(models, group.Models...)
		}
	}
	models = dedupe(models)
	if len(models) == 0 {
		return nil, false
	}
	sort.Strings(models)
	return models, true
}

func (s *State) planMatchesModel(plan Plan, billingModel string) bool {
	models, restricted := s.modelsForSelection(plan.ModelGroupIDs, plan.Models)
	if !restricted {
		return true
	}
	return containsModel(models, billingModel)
}

func (s *State) matchingPlanBinding(key *KeyState, billingModel string) (*PlanBinding, Plan, bool) {
	for _, plan := range s.Plans {
		binding, bound := key.FindPlanBinding(plan.ID)
		if bound && s.planMatchesModel(plan, billingModel) {
			return binding, plan, true
		}
	}
	return nil, Plan{}, false
}

func (s *State) AllowedPlanModels(key *KeyState) ([]string, bool) {
	if key == nil || len(key.PlanIDs()) == 0 {
		return nil, false
	}
	models := []string{}
	valid := 0
	for _, planID := range key.PlanIDs() {
		plan, exists := s.FindPlan(planID)
		if !exists {
			continue
		}
		valid++
		selected, restricted := s.modelsForSelection(plan.ModelGroupIDs, plan.Models)
		if !restricted {
			return nil, false
		}
		models = append(models, selected...)
	}
	if valid == 0 {
		return nil, false
	}
	models = dedupe(models)
	sort.Strings(models)
	return models, true
}

func intersectModelGrants(left []string, leftRestricted bool, right []string, rightRestricted bool) ([]string, bool) {
	if !leftRestricted {
		return slices.Clone(right), rightRestricted
	}
	if !rightRestricted {
		return slices.Clone(left), true
	}
	intersection := make([]string, 0, min(len(left), len(right)))
	for _, model := range left {
		if containsModel(right, model) {
			intersection = append(intersection, model)
		}
	}
	sort.Strings(intersection)
	return intersection, true
}

func containsModel(models []string, wanted string) bool {
	for _, model := range models {
		if strings.EqualFold(model, wanted) {
			return true
		}
	}
	return false
}
