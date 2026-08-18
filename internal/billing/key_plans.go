package billing

import (
	"slices"
	"sort"
	"strings"
	"time"
)

type PlanBindingView struct {
	PlanID      string    `json:"plan_id"`
	PlanName    string    `json:"plan_name"`
	LimitUSD    float64   `json:"limit_usd"`
	SpentUSD    float64   `json:"spent_usd"`
	UsedPercent float64   `json:"used_percent"`
	Blocked     bool      `json:"blocked"`
	CycleEndAt  time.Time `json:"cycle_end_at,omitzero"`
}

type KeyView struct {
	Scope         string            `json:"scope"`
	Preview       string            `json:"preview,omitempty"`
	Label         string            `json:"label,omitempty"`
	InConfig      bool              `json:"in_config"`
	DeletedAt     time.Time         `json:"deleted_at,omitzero"`
	PlanBindings  []PlanBindingView `json:"plan_bindings"`
	PlanID        string            `json:"plan_id,omitempty"`
	PlanName      string            `json:"plan_name,omitempty"`
	ModelGroupIDs []string          `json:"model_groups"`
	Models        []string          `json:"models"`
	AllModels     bool              `json:"all_models"`
	Unlimited     bool              `json:"unlimited"`
	Blocked       bool              `json:"blocked"`
	LimitUSD      float64           `json:"limit_usd"`
	SpentUSD      float64           `json:"spent_usd"`
	UsedPercent   float64           `json:"used_percent"`
	CycleEndAt    time.Time         `json:"cycle_end_at,omitzero"`
	Lifetime      Totals            `json:"lifetime"`
	ByModel       []ModelTotals     `json:"by_model,omitempty"`
}

type KeyDirectory struct {
	Keys []KeyView `json:"keys"`
}

func (s *Store) KeyDirectory() KeyDirectory {
	now := s.Now()
	directory := KeyDirectory{Keys: []KeyView{}}
	directory.Keys = updateResult(s, func(state *State) ([]KeyView, Changes) {
		changed := []string{}
		views := make([]KeyView, 0, len(state.Keys))
		for scope, key := range state.Keys {
			if key == nil {
				continue
			}
			if settleKeyBindings(state, key, now) {
				changed = append(changed, scope)
			}
			views = append(views, keyView(scope, key, state.Plans))
		}
		sortKeyViews(views)
		return views, Changes{Keys: changed}
	})
	return directory
}

func settleKeyBindings(state *State, key *KeyState, now time.Time) bool {
	changed := key.NormalizePlanBindings()
	for _, planID := range key.PlanIDs() {
		plan, exists := state.FindPlan(planID)
		if !exists {
			changed = key.UnbindPlan(planID) || changed
			continue
		}
		binding, _ := key.FindPlanBinding(planID)
		changed = settleExpiredCycle(&binding.Cycle, plan, now) || changed
	}
	return changed
}

func keyView(scope string, key *KeyState, plans []Plan) KeyView {
	view := KeyView{
		Scope: scope, Preview: key.Preview, Label: key.Label, InConfig: key.InConfig,
		DeletedAt: key.DeletedAt, ModelGroupIDs: slices.Clone(key.ModelGroupIDs),
		Models: slices.Clone(key.Models), AllModels: len(key.ModelGroupIDs) == 0 && len(key.Models) == 0,
		Unlimited: true, Lifetime: key.Lifetime, PlanBindings: []PlanBindingView{},
	}
	for _, plan := range plans {
		binding, exists := key.FindPlanBinding(plan.ID)
		if !exists {
			continue
		}
		entry := bindingView(plan, binding.Cycle)
		view.PlanBindings = append(view.PlanBindings, entry)
		view.Blocked = view.Blocked || entry.Blocked
	}
	view.Unlimited = len(view.PlanBindings) == 0
	applyLegacyPlanSummary(&view)
	for model, totals := range key.ByModel {
		if totals != nil {
			view.ByModel = append(view.ByModel, ModelTotals{BillingModel: model, Totals: *totals})
		}
	}
	sortModelTotals(view.ByModel)
	return view
}

func bindingView(plan Plan, cycle Cycle) PlanBindingView {
	view := PlanBindingView{
		PlanID: plan.ID, PlanName: plan.Name, LimitUSD: plan.AmountUSD,
		SpentUSD: cycle.SpentUSD, CycleEndAt: cycle.EndAt,
	}
	if plan.AmountUSD > 0 {
		view.UsedPercent = cycle.SpentUSD / plan.AmountUSD * 100
		view.Blocked = cycle.SpentUSD >= plan.AmountUSD
	} else {
		view.UsedPercent = 100
		view.Blocked = true
	}
	return view
}

func applyLegacyPlanSummary(view *KeyView) {
	if view == nil || len(view.PlanBindings) == 0 {
		return
	}
	first := view.PlanBindings[0]
	view.PlanID, view.PlanName = first.PlanID, first.PlanName
	view.LimitUSD, view.SpentUSD = first.LimitUSD, first.SpentUSD
	view.UsedPercent, view.CycleEndAt = first.UsedPercent, first.CycleEndAt
}

func sortKeyViews(views []KeyView) {
	sort.Slice(views, func(i, j int) bool {
		if views[i].Blocked != views[j].Blocked {
			return views[i].Blocked
		}
		if views[i].Lifetime.CostUSD != views[j].Lifetime.CostUSD {
			return views[i].Lifetime.CostUSD > views[j].Lifetime.CostUSD
		}
		return views[i].Scope < views[j].Scope
	})
}

func sortModelTotals(entries []ModelTotals) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CostUSD != entries[j].CostUSD {
			return entries[i].CostUSD > entries[j].CostUSD
		}
		return entries[i].BillingModel < entries[j].BillingModel
	})
}

func (s *Store) BindKey(scope, planID string) error {
	scope, planID = normalizeScope(scope), strings.TrimSpace(planID)
	if scope == "" || planID == "" {
		return invalidf("API Key 标识和订阅计划 ID 都不能为空")
	}
	var errApply error
	updateResult(s, func(state *State) (struct{}, Changes) {
		if _, exists := state.FindPlan(planID); !exists {
			errApply = notFoundf("订阅计划 %q 不存在", planID)
			return struct{}{}, Changes{}
		}
		key := state.liveKey(scope)
		if key == nil {
			errApply = notFoundf("API Key %q 不存在，请先同步 Key 列表", scope)
			return struct{}{}, Changes{}
		}
		if !key.BindPlan(planID) {
			return struct{}{}, Changes{}
		}
		return struct{}{}, Changes{Keys: []string{scope}}
	})
	return errApply
}

func (s *Store) UnbindKey(scope, planID string) error {
	scope, planID = normalizeScope(scope), strings.TrimSpace(planID)
	if scope == "" {
		return invalidf("API Key 标识不能为空")
	}
	updateResult(s, func(state *State) (struct{}, Changes) {
		key := state.liveKey(scope)
		if key == nil {
			return struct{}{}, Changes{}
		}
		changed := key.NormalizePlanBindings()
		if planID == "" {
			changed = len(key.PlanBindings) > 0 || changed
			key.PlanBindings = nil
		} else {
			changed = key.UnbindPlan(planID) || changed
		}
		if !changed {
			return struct{}{}, Changes{}
		}
		return struct{}{}, Changes{Keys: []string{scope}}
	})
	return nil
}

func (s *Store) ResetCycle(scope, planID string) error {
	scope, planID = normalizeScope(scope), strings.TrimSpace(planID)
	if scope == "" {
		return invalidf("API Key 标识不能为空")
	}
	updateResult(s, func(state *State) (struct{}, Changes) {
		key := state.liveKey(scope)
		if key == nil {
			return struct{}{}, Changes{}
		}
		changed := key.NormalizePlanBindings()
		if planID == "" {
			if !key.ResetPlanCycles() && !changed {
				return struct{}{}, Changes{}
			}
		} else if binding, exists := key.FindPlanBinding(planID); exists {
			if binding.Cycle == (Cycle{}) {
				if !changed {
					return struct{}{}, Changes{}
				}
			} else {
				binding.Cycle = Cycle{}
			}
		} else if !changed {
			return struct{}{}, Changes{}
		}
		return struct{}{}, Changes{Keys: []string{scope}}
	})
	return nil
}

func (s *Store) ResetAllCycles() int {
	return updateResult(s, func(state *State) (int, Changes) {
		reset := []string{}
		for scope, key := range state.Keys {
			if key == nil || !key.DeletedAt.IsZero() {
				continue
			}
			changed := key.NormalizePlanBindings()
			for index := range key.PlanBindings {
				binding := &key.PlanBindings[index]
				plan, exists := state.FindPlan(binding.PlanID)
				if exists && plan.Period.Kind != PeriodNever && binding.Cycle != (Cycle{}) {
					binding.Cycle = Cycle{}
					changed = true
				}
			}
			if changed {
				reset = append(reset, scope)
			}
		}
		return len(reset), Changes{Keys: reset}
	})
}
