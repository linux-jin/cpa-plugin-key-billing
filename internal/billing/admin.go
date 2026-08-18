package billing

import (
	"slices"
	"strings"
)

type PriceRow struct {
	PriceRule
	Source PriceSource `json:"source"`
}

// It holds one row per model the proxy currently serves, not a sparse set of
// overrides: an operator asking "what does this cost" should not have to know
// whether the answer comes from a rule they wrote or from the runtime catalog.
type PriceTable struct {
	Catalog CatalogInfo `json:"catalog"`
	Models  []PriceRow  `json:"models"`
}

func (s *Store) PriceTable() PriceTable {
	loaded := builtinCatalog()
	table := PriceTable{Catalog: loaded.info, Models: []PriceRow{}}
	s.Read(func(state *State) {
		for _, rule := range state.Prices {
			table.Models = append(table.Models, PriceRow{PriceRule: rule, Source: priceSourceOfCatalog(rule, loaded)})
		}
	})
	return table
}

type CatalogRefreshResult struct {
	Catalog       CatalogInfo `json:"catalog"`
	UpdatedModels int         `json:"updated_models"`
}

// Refreshing advances rows that followed the previous built-in value while
// preserving explicit custom prices.
func (s *Store) RefreshPriceCatalog() (CatalogRefreshResult, error) {
	previous := cachedBuiltinCatalog()
	info, errRefresh := RefreshBuiltinCatalog()
	if errRefresh != nil {
		return CatalogRefreshResult{}, errRefresh
	}
	current := builtinCatalog()
	updated := updateResult(s, func(state *State) (int, Changes) {
		changed := 0
		for i := range state.Prices {
			rule := state.Prices[i]
			previousDefault, previouslyKnown := lookupCatalog(previous, rule.Pattern, "")
			followedBuiltin := previouslyKnown && samePrice(rule, previousDefault)
			if !previouslyKnown && rule.InputPer1M == 0 && rule.OutputPer1M == 0 && rule.CacheReadPer1M == nil && rule.CacheWritePer1M == nil && rule.LongContext == nil {
				followedBuiltin = true
			}
			if !followedBuiltin {
				continue
			}
			fresh, known := lookupCatalog(current, rule.Pattern, "")
			if !known {
				fresh = PriceRule{Pattern: rule.Pattern}
			} else {
				fresh.Pattern = rule.Pattern
			}
			if samePrice(rule, fresh) {
				continue
			}
			state.Prices[i] = fresh
			changed++
		}
		return changed, Changes{Prices: changed > 0}
	})
	return CatalogRefreshResult{Catalog: info, UpdatedModels: updated}, nil
}

func priceSourceOfCatalog(rule PriceRule, loaded *catalog) PriceSource {
	def, known := lookupCatalog(loaded, rule.Pattern, "")
	if !known {
		if rule.InputPer1M == 0 && rule.OutputPer1M == 0 && rule.CacheReadPer1M == nil && rule.CacheWritePer1M == nil && rule.LongContext == nil {
			return PriceSourceNone
		}
		return PriceSourceCustom
	}
	if samePrice(rule, def) {
		return PriceSourceBuiltin
	}
	return PriceSourceCustom
}

func samePrice(a, b PriceRule) bool {
	return a.InputPer1M == b.InputPer1M &&
		a.OutputPer1M == b.OutputPer1M &&
		sameOptionalPrice(a.CacheReadPer1M, b.CacheReadPer1M) &&
		sameOptionalPrice(a.CacheWritePer1M, b.CacheWritePer1M) &&
		sameLongContextPrice(a.LongContext, b.LongContext)
}

func sameLongContextPrice(a, b *LongContextPrice) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.ThresholdInputTokens == b.ThresholdInputTokens &&
		a.InputPer1M == b.InputPer1M && a.OutputPer1M == b.OutputPer1M &&
		sameOptionalPrice(a.CacheReadPer1M, b.CacheReadPer1M) &&
		sameOptionalPrice(a.CacheWritePer1M, b.CacheWritePer1M)
}

func sameOptionalPrice(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

type ModelSyncResult struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Priced  int `json:"priced"`
}

// New models arrive priced from the runtime catalog, or at zero when it has no
// entry for them. Models that disappeared are dropped. Rows that survive keep
// whatever an administrator set, because a model list refresh must never quietly
// undo an edit.
//
// Glob rows cannot be reconciled against model names, so they are preserved.
func (s *Store) SyncModels(models []string) (ModelSyncResult, error) {
	wanted := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		lowered := strings.ToLower(model)
		if _, exists := seen[lowered]; exists {
			continue
		}
		seen[lowered] = struct{}{}
		wanted = append(wanted, model)
	}
	if len(wanted) == 0 {
		// An empty list is far more likely to be a failed read than a proxy
		// that serves nothing, and wiping the table would discard every edit.
		return ModelSyncResult{}, invalidf("模型列表为空，未执行同步")
	}

	var result ModelSyncResult
	kept := 0
	loaded := builtinCatalog()
	updateResult(s, func(state *State) (struct{}, Changes) {
		existing := make(map[string]PriceRule, len(state.Prices))
		var globs []PriceRule
		for _, rule := range state.Prices {
			if isGlob(rule.Pattern) {
				globs = append(globs, rule)
				continue
			}
			existing[strings.ToLower(strings.TrimSpace(rule.Pattern))] = rule
		}
		rows := make([]PriceRule, 0, len(wanted)+len(globs))
		for _, model := range wanted {
			if rule, exists := existing[strings.ToLower(model)]; exists {
				rows = append(rows, rule)
				kept++
				continue
			}
			seeded, known := lookupCatalog(loaded, model, "")
			if !known {
				seeded = PriceRule{Pattern: model}
			} else {
				seeded.Pattern = model
			}
			rows = append(rows, seeded)
			result.Added++
			if known {
				result.Priced++
			}
		}
		result.Removed = len(existing) - kept
		// Keep the proxy's own ordering: it is what the operator sees elsewhere.
		// Globs go last, which is also where ResolvePrice consults them.
		next := append(rows, globs...)
		// The panel synchronizes on every session start. A kept rule is the
		// stored value itself, pointer fields and all, so an unchanged list is
		// equal to the stored one and not worth rewriting the price table for.
		if slices.Equal(state.Prices, next) {
			return struct{}{}, Changes{}
		}
		state.Prices = next
		return struct{}{}, Changes{Prices: true}
	})
	return result, nil
}

func (r PriceRule) Validate() error {
	pattern := strings.TrimSpace(r.Pattern)
	if pattern == "" {
		return invalidf("模型名称或匹配规则不能为空")
	}
	if r.InputPer1M < 0 || r.OutputPer1M < 0 {
		return invalidf("模型 %q：Token 单价不能为负数", pattern)
	}
	if r.CacheReadPer1M != nil && *r.CacheReadPer1M < 0 {
		return invalidf("模型 %q：缓存读取单价不能为负数", pattern)
	}
	if r.CacheWritePer1M != nil && *r.CacheWritePer1M < 0 {
		return invalidf("模型 %q：缓存写入单价不能为负数", pattern)
	}
	if tier := r.LongContext; tier != nil {
		if tier.ThresholdInputTokens <= 0 {
			return invalidf("模型 %q：长上下文阈值必须大于 0", pattern)
		}
		if tier.InputPer1M < 0 || tier.OutputPer1M < 0 {
			return invalidf("模型 %q：长上下文 Token 单价不能为负数", pattern)
		}
		if tier.CacheReadPer1M != nil && *tier.CacheReadPer1M < 0 {
			return invalidf("模型 %q：长上下文缓存读取单价不能为负数", pattern)
		}
		if tier.CacheWritePer1M != nil && *tier.CacheWritePer1M < 0 {
			return invalidf("模型 %q：长上下文缓存写入单价不能为负数", pattern)
		}
	}
	return nil
}

func (s *Store) UpsertPrice(rule PriceRule) (PriceRule, error) {
	rule.Pattern = strings.TrimSpace(rule.Pattern)
	if errValidate := rule.Validate(); errValidate != nil {
		return PriceRule{}, errValidate
	}
	return updateResult(s, func(state *State) (PriceRule, Changes) {
		for i := range state.Prices {
			if strings.EqualFold(strings.TrimSpace(state.Prices[i].Pattern), rule.Pattern) {
				state.Prices[i] = rule
				return rule, Changes{Prices: true}
			}
		}
		state.Prices = append(state.Prices, rule)
		return rule, Changes{Prices: true}
	}), nil
}

// ResetPrices restores every row to its catalog default, dropping edits. Models
// the catalog does not know go back to zero. It reports how many rows changed.
func (s *Store) ResetPrices() int {
	loaded := builtinCatalog()
	return updateResult(s, func(state *State) (int, Changes) {
		changed := 0
		for i := range state.Prices {
			pattern := state.Prices[i].Pattern
			def, known := lookupCatalog(loaded, pattern, "")
			if !known {
				def = PriceRule{Pattern: pattern}
			} else {
				def.Pattern = pattern
			}
			if samePrice(state.Prices[i], def) {
				continue
			}
			state.Prices[i] = def
			changed++
		}
		return changed, Changes{Prices: changed > 0}
	})
}

// freeID turns a display name into an identifier nothing else answers to.
