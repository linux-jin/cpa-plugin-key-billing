package billing

import (
	"testing"
	"time"
)

func TestMultiPlanBudgetsAreSelectedAndChargedByModel(t *testing.T) {
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	store := newAccountStore(t, now)
	store.ReplaceAll(func(state *State) {
		state.Prices = []PriceRule{
			{Pattern: "grok-4", InputPer1M: 1, OutputPer1M: 2},
			{Pattern: "gemini-2.5-pro", InputPer1M: 1, OutputPer1M: 2},
		}
		state.ModelGroups = []ModelGroup{
			{ID: "grok", Name: "Grok", Models: []string{"grok-4"}},
			{ID: "gemini", Name: "Gemini", Models: []string{"gemini-2.5-pro"}},
		}
		state.Plans = []Plan{
			{ID: "grok-100", AmountUSD: 0.003, Period: Period{Kind: PeriodDaily}, ModelGroupIDs: []string{"grok"}},
			{ID: "gemini-1000", AmountUSD: 1, Period: Period{Kind: PeriodDaily}, ModelGroupIDs: []string{"gemini"}},
		}
		state.Keys["scope-a"] = &KeyState{PlanBindings: []PlanBinding{
			{PlanID: "grok-100"},
			{PlanID: "gemini-1000"},
		}}
	})

	chargeModelRequest(t, store, "scope-a", "grok-4", now)
	chargeModelRequest(t, store, "scope-a", "grok-4", now)
	if decision := store.Authorize("scope-a", "grok-4", now); decision.Allowed || decision.PlanID != "grok-100" {
		t.Fatalf("grok decision = %+v, want its own exhausted plan", decision)
	}
	if decision := store.Authorize("scope-a", "gemini-2.5-pro", now); !decision.Allowed || decision.PlanID != "gemini-1000" {
		t.Fatalf("gemini decision = %+v, want its independent budget", decision)
	}

	store.Read(func(state *State) {
		key := state.Keys["scope-a"]
		grok := mustBinding(t, key, "grok-100").Cycle
		gemini := mustBinding(t, key, "gemini-1000").Cycle
		if grok.SpentUSD <= 0.003 || gemini.SpentUSD != 0 {
			t.Fatalf("grok cycle = %+v, gemini cycle = %+v", grok, gemini)
		}
	})
}

func TestMultiPlanModelGrantIsTheBoundPlanUnion(t *testing.T) {
	store := newEnforceStore(t, time.Now())
	store.ReplaceAll(func(state *State) {
		state.Plans = []Plan{
			{ID: "grok", AmountUSD: 100, Period: Period{Kind: PeriodNever}, Models: []string{"grok-4"}},
			{ID: "gemini", AmountUSD: 1000, Period: Period{Kind: PeriodNever}, Models: []string{"gemini-2.5-pro"}},
		}
		state.Prices = []PriceRule{
			{Pattern: "grok-4"}, {Pattern: "gemini-2.5-pro"}, {Pattern: "gpt-5.5"},
		}
		state.Keys["scope-a"] = &KeyState{PlanBindings: []PlanBinding{
			{PlanID: "grok"}, {PlanID: "gemini"},
		}}
	})

	for _, model := range []string{"grok-4", "gemini-2.5-pro"} {
		if decision := store.AuthorizeModel("scope-a", model, ""); !decision.Allowed {
			t.Fatalf("%s rejected: %+v", model, decision)
		}
	}
	if decision := store.AuthorizeModel("scope-a", "gpt-5.5", ""); decision.Allowed {
		t.Fatalf("outside model admitted: %+v", decision)
	}
}

func chargeModelRequest(t *testing.T, store *Store, scope, model string, at time.Time) {
	t.Helper()
	decision := store.Authorize(scope, model, at)
	if !decision.Allowed {
		t.Fatalf("%s unexpectedly blocked: %+v", model, decision)
	}
	event := subsetEvent(scope, at)
	event.Record.BillingModel = model
	event.Record.UpstreamModel = model
	event.CyclePlanID = decision.PlanID
	event.CycleStartAt = decision.CycleStartAt
	store.RecordUsage(event)
}
