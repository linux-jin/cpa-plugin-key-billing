package billing

import (
	"slices"
	"strings"
)

type PlanPatch struct {
	ID            string    `json:"id"`
	Name          *string   `json:"name,omitempty"`
	AmountUSD     *float64  `json:"amount_usd,omitempty"`
	Period        *Period   `json:"period,omitempty"`
	ModelGroupIDs *[]string `json:"model_groups,omitempty"`
	Models        *[]string `json:"models,omitempty"`
}

func (s *Store) Plans() []Plan {
	plans := []Plan{}
	s.Read(func(state *State) {
		for _, plan := range state.Plans {
			plans = append(plans, plan.clone())
		}
	})
	return plans
}

func (p Plan) clone() Plan {
	p.ModelGroupIDs = slices.Clone(p.ModelGroupIDs)
	p.Models = slices.Clone(p.Models)
	return p
}

func normalizePlanSelection(plan *Plan) {
	plan.ModelGroupIDs = dedupe(plan.ModelGroupIDs)
	plan.Models = dedupe(plan.Models)
	if slices.Contains(plan.ModelGroupIDs, AllModelsGroupID) {
		plan.ModelGroupIDs, plan.Models = nil, nil
	}
}

func validatePlanSelection(state *State, plan Plan) error {
	for _, id := range plan.ModelGroupIDs {
		if _, exists := state.FindModelGroup(id); !exists {
			return notFoundf("模型分组 %q 不存在", id)
		}
	}
	return nil
}

func (s *Store) CreatePlanWithBindings(plan Plan, scopes []string) (Plan, error) {
	plan.ID, plan.Name = strings.TrimSpace(plan.ID), strings.TrimSpace(plan.Name)
	normalizePlanSelection(&plan)
	scopes = normalizeScopes(scopes)
	if plan.Period.Kind == "" {
		plan.Period.Kind = PeriodDaily
	}
	var errApply error
	stored := updateResult(s, func(state *State) (Plan, Changes) {
		if plan.ID == "" {
			plan.ID = state.freePlanID(plan.Name)
		}
		if errApply = validateNewPlan(state, plan, scopes); errApply != nil {
			return Plan{}, Changes{}
		}
		if plan.Name == "" {
			plan.Name = plan.ID
		}
		state.Plans = append(state.Plans, plan)
		for _, scope := range scopes {
			state.Keys[scope].BindPlan(plan.ID)
		}
		return plan.clone(), Changes{Plans: true, Keys: scopes}
	})
	return stored, errApply
}

func validateNewPlan(state *State, plan Plan, scopes []string) error {
	if errValidate := plan.Validate(); errValidate != nil {
		return errValidate
	}
	if _, exists := state.FindPlan(plan.ID); exists {
		return conflictf("订阅计划 %q 已存在", plan.ID)
	}
	if errSelection := validatePlanSelection(state, plan); errSelection != nil {
		return errSelection
	}
	for _, scope := range scopes {
		if state.liveKey(scope) == nil {
			return notFoundf("API Key %q 不存在，请先同步 Key 列表", scope)
		}
	}
	return nil
}

func (s *Store) UpdatePlanWithBindings(patch PlanPatch, scopes *[]string) (Plan, error) {
	patch.ID = strings.TrimSpace(patch.ID)
	if patch.ID == "" {
		return Plan{}, invalidf("订阅计划 ID 不能为空")
	}
	var errApply error
	stored := updateResult(s, func(state *State) (Plan, Changes) {
		index := slices.IndexFunc(state.Plans, func(plan Plan) bool { return plan.ID == patch.ID })
		if index < 0 {
			errApply = notFoundf("订阅计划 %q 不存在", patch.ID)
			return Plan{}, Changes{}
		}
		updated := applyPlanPatch(state.Plans[index], patch)
		if errApply = validateUpdatedPlan(state, updated, scopes); errApply != nil {
			return Plan{}, Changes{}
		}
		periodChanged := updated.Period != state.Plans[index].Period
		applyPlanScopes(state, patch.ID, scopes, periodChanged)
		state.Plans[index] = updated
		return updated.clone(), Changes{Plans: true, AllKeys: scopes != nil || periodChanged}
	})
	return stored, errApply
}

func applyPlanPatch(plan Plan, patch PlanPatch) Plan {
	if patch.Name != nil {
		plan.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.AmountUSD != nil {
		plan.AmountUSD = *patch.AmountUSD
	}
	if patch.Period != nil {
		plan.Period = *patch.Period
	}
	if patch.ModelGroupIDs != nil {
		plan.ModelGroupIDs = slices.Clone(*patch.ModelGroupIDs)
	}
	if patch.Models != nil {
		plan.Models = slices.Clone(*patch.Models)
	}
	normalizePlanSelection(&plan)
	return plan
}

func validateUpdatedPlan(state *State, plan Plan, scopes *[]string) error {
	if errValidate := plan.Validate(); errValidate != nil {
		return errValidate
	}
	if errSelection := validatePlanSelection(state, plan); errSelection != nil {
		return errSelection
	}
	if scopes == nil {
		return nil
	}
	for _, scope := range normalizeScopes(*scopes) {
		if state.liveKey(scope) == nil {
			return notFoundf("API Key %q 不存在，请先同步 Key 列表", scope)
		}
	}
	return nil
}

func applyPlanScopes(state *State, planID string, scopes *[]string, reset bool) {
	selected := make(map[string]struct{})
	if scopes != nil {
		for _, scope := range normalizeScopes(*scopes) {
			selected[scope] = struct{}{}
		}
	}
	for scope, key := range state.Keys {
		if key == nil || !key.DeletedAt.IsZero() {
			continue
		}
		key.NormalizePlanBindings()
		binding, bound := key.FindPlanBinding(planID)
		_, keep := selected[scope]
		if bound && reset {
			binding.Cycle = Cycle{}
		}
		if scopes != nil && bound && !keep {
			key.UnbindPlan(planID)
		}
	}
	if scopes != nil {
		for scope := range selected {
			state.Keys[scope].BindPlan(planID)
		}
	}
}

func (s *Store) DeletePlan(id string) (int, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0, invalidf("订阅计划 ID 不能为空")
	}
	var errApply error
	unbound := updateResult(s, func(state *State) (int, Changes) {
		index := slices.IndexFunc(state.Plans, func(plan Plan) bool { return plan.ID == id })
		if index < 0 {
			errApply = notFoundf("订阅计划 %q 不存在", id)
			return 0, Changes{}
		}
		state.Plans = slices.Delete(state.Plans, index, index+1)
		released := 0
		for _, key := range state.Keys {
			if key != nil && key.UnbindPlan(id) {
				released++
			}
		}
		return released, Changes{Plans: true, AllKeys: true}
	})
	return unbound, errApply
}

func (s *State) freePlanID(name string) string {
	return freeID(name, "plan", func(id string) bool {
		_, exists := s.FindPlan(id)
		return exists
	})
}
