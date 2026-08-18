package sqlite

import (
	"database/sql"
	"fmt"

	"cpa-key-billing/internal/billing"
)

const insertKey = `
	INSERT INTO api_keys (
		scope, preview, label, in_config, deleted_at, plan_id,
		cycle_plan_id, cycle_start_at, cycle_end_at, cycle_spent_usd,
		cost_usd, requests, uncached_input_tokens, output_tokens,
		reasoning_tokens, cache_read_tokens, cache_creation_tokens
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(scope) DO UPDATE SET
		preview = excluded.preview, label = excluded.label, in_config = excluded.in_config,
		deleted_at = excluded.deleted_at, plan_id = excluded.plan_id,
		cycle_plan_id = excluded.cycle_plan_id, cycle_start_at = excluded.cycle_start_at,
		cycle_end_at = excluded.cycle_end_at, cycle_spent_usd = excluded.cycle_spent_usd,
		cost_usd = excluded.cost_usd, requests = excluded.requests,
		uncached_input_tokens = excluded.uncached_input_tokens, output_tokens = excluded.output_tokens,
		reasoning_tokens = excluded.reasoning_tokens, cache_read_tokens = excluded.cache_read_tokens,
		cache_creation_tokens = excluded.cache_creation_tokens`

func saveKey(tx *sql.Tx, scope string, key *billing.KeyState) error {
	if key == nil {
		if _, errDelete := tx.Exec("DELETE FROM api_keys WHERE scope = ?", scope); errDelete != nil {
			return fmt.Errorf("删除 API Key %s：%w", scope, errDelete)
		}
		return nil
	}
	key.NormalizePlanBindings()
	_, errKey := tx.Exec(insertKey, scope, key.Preview, key.Label, key.InConfig, nanos(key.DeletedAt),
		"", "", int64(0), int64(0), float64(0), key.Lifetime.CostUSD, key.Lifetime.Requests,
		key.Lifetime.UncachedInputTokens, key.Lifetime.OutputTokens, key.Lifetime.ReasoningTokens,
		key.Lifetime.CacheReadTokens, key.Lifetime.CacheCreationTokens)
	if errKey != nil {
		return fmt.Errorf("保存 API Key %s：%w", scope, errKey)
	}
	if errModels := saveKeyModels(tx, scope, key); errModels != nil {
		return errModels
	}
	if errAccess := saveKeyModelAccess(tx, scope, key); errAccess != nil {
		return errAccess
	}
	return saveKeyPlanBindings(tx, scope, key)
}

func saveKeyModels(tx *sql.Tx, scope string, key *billing.KeyState) error {
	if _, errClear := tx.Exec("DELETE FROM key_models WHERE scope = ?", scope); errClear != nil {
		return fmt.Errorf("保存 API Key %s 的模型用量：%w", scope, errClear)
	}
	for model, totals := range key.ByModel {
		if totals == nil {
			continue
		}
		_, errModel := tx.Exec(`
			INSERT INTO key_models (
				scope, billing_model, cost_usd, requests, uncached_input_tokens,
				output_tokens, reasoning_tokens, cache_read_tokens, cache_creation_tokens
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			scope, model, totals.CostUSD, totals.Requests, totals.UncachedInputTokens,
			totals.OutputTokens, totals.ReasoningTokens, totals.CacheReadTokens, totals.CacheCreationTokens)
		if errModel != nil {
			return fmt.Errorf("保存 API Key %s 的模型用量：%w", scope, errModel)
		}
	}
	return nil
}

func saveKeyModelAccess(tx *sql.Tx, scope string, key *billing.KeyState) error {
	for _, table := range []string{"key_model_groups", "key_allowed_models"} {
		if _, errClear := tx.Exec("DELETE FROM "+table+" WHERE scope = ?", scope); errClear != nil {
			return fmt.Errorf("保存 API Key %s 的可用模型：%w", scope, errClear)
		}
	}
	for position, group := range key.ModelGroupIDs {
		if _, errGroup := tx.Exec(
			"INSERT INTO key_model_groups (scope, position, group_id) VALUES (?, ?, ?)",
			scope, position, group); errGroup != nil {
			return fmt.Errorf("保存 API Key %s 的模型分组：%w", scope, errGroup)
		}
	}
	for position, model := range key.Models {
		if _, errModel := tx.Exec(
			"INSERT INTO key_allowed_models (scope, position, model) VALUES (?, ?, ?)",
			scope, position, model); errModel != nil {
			return fmt.Errorf("保存 API Key %s 的可用模型：%w", scope, errModel)
		}
	}
	return nil
}

func replaceKeys(tx *sql.Tx, state *billing.State) error {
	if _, errClear := tx.Exec("DELETE FROM api_keys"); errClear != nil {
		return fmt.Errorf("保存 API Key 列表：%w", errClear)
	}
	for scope, key := range state.Keys {
		if errKey := saveKey(tx, scope, key); errKey != nil {
			return errKey
		}
	}
	return nil
}

func (d *DB) loadKeys(state *billing.State) error {
	rows, errQuery := d.db.Query(`
		SELECT scope, preview, label, in_config, deleted_at, plan_id,
			cycle_plan_id, cycle_start_at, cycle_end_at, cycle_spent_usd,
			cost_usd, requests, uncached_input_tokens, output_tokens,
			reasoning_tokens, cache_read_tokens, cache_creation_tokens
		FROM api_keys`)
	if errQuery != nil {
		return fmt.Errorf("读取 API Key 列表：%w", errQuery)
	}
	defer rows.Close()
	for rows.Next() {
		var scope string
		var key billing.KeyState
		var deletedAt, cycleStart, cycleEnd int64
		if errScan := rows.Scan(&scope, &key.Preview, &key.Label, &key.InConfig, &deletedAt, &key.PlanID,
			&key.Cycle.PlanID, &cycleStart, &cycleEnd, &key.Cycle.SpentUSD,
			&key.Lifetime.CostUSD, &key.Lifetime.Requests, &key.Lifetime.UncachedInputTokens,
			&key.Lifetime.OutputTokens, &key.Lifetime.ReasoningTokens, &key.Lifetime.CacheReadTokens,
			&key.Lifetime.CacheCreationTokens); errScan != nil {
			return fmt.Errorf("读取 API Key 列表：%w", errScan)
		}
		key.DeletedAt = timeAt(deletedAt)
		key.Cycle.StartAt = timeAt(cycleStart)
		key.Cycle.EndAt = timeAt(cycleEnd)
		key.ByModel = make(map[string]*billing.Totals)
		state.Keys[scope] = &key
	}
	if errRows := rows.Err(); errRows != nil {
		return fmt.Errorf("读取 API Key 列表：%w", errRows)
	}
	if errModels := d.loadKeyModels(state); errModels != nil {
		return errModels
	}
	if errAccess := d.loadKeyModelAccess(state); errAccess != nil {
		return errAccess
	}
	return d.loadKeyPlanBindings(state)
}

func (d *DB) loadKeyModelAccess(state *billing.State) error {
	groups, errGroups := d.loadKeyStrings("key_model_groups", "group_id")
	if errGroups != nil {
		return errGroups
	}
	models, errModels := d.loadKeyStrings("key_allowed_models", "model")
	if errModels != nil {
		return errModels
	}
	for scope, key := range state.Keys {
		key.ModelGroupIDs = groups[scope]
		key.Models = models[scope]
	}
	return nil
}

func (d *DB) loadKeyStrings(table, column string) (map[string][]string, error) {
	rows, errQuery := d.db.Query("SELECT scope, " + column + " FROM " + table + " ORDER BY scope, position")
	if errQuery != nil {
		return nil, fmt.Errorf("读取 API Key 的可用模型：%w", errQuery)
	}
	defer rows.Close()
	values := make(map[string][]string)
	for rows.Next() {
		var scope, value string
		if errScan := rows.Scan(&scope, &value); errScan != nil {
			return nil, fmt.Errorf("读取 API Key 的可用模型：%w", errScan)
		}
		values[scope] = append(values[scope], value)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("读取 API Key 的可用模型：%w", errRows)
	}
	return values, nil
}

func (d *DB) loadKeyModels(state *billing.State) error {
	rows, errQuery := d.db.Query(`
		SELECT scope, billing_model, cost_usd, requests, uncached_input_tokens,
			output_tokens, reasoning_tokens, cache_read_tokens, cache_creation_tokens
		FROM key_models`)
	if errQuery != nil {
		return fmt.Errorf("读取模型用量：%w", errQuery)
	}
	defer rows.Close()
	for rows.Next() {
		var scope, model string
		var totals billing.Totals
		if errScan := rows.Scan(&scope, &model, &totals.CostUSD, &totals.Requests,
			&totals.UncachedInputTokens, &totals.OutputTokens, &totals.ReasoningTokens,
			&totals.CacheReadTokens, &totals.CacheCreationTokens); errScan != nil {
			return fmt.Errorf("读取模型用量：%w", errScan)
		}
		if key := state.Keys[scope]; key != nil {
			key.ByModel[model] = &totals
		}
	}
	if errRows := rows.Err(); errRows != nil {
		return fmt.Errorf("读取模型用量：%w", errRows)
	}
	return nil
}
