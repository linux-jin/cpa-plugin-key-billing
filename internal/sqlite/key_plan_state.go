package sqlite

import (
	"database/sql"
	"fmt"

	"cpa-key-billing/internal/billing"
)

func saveKeyPlanBindings(tx *sql.Tx, scope string, key *billing.KeyState) error {
	if _, errClear := tx.Exec("DELETE FROM key_plan_bindings WHERE scope = ?", scope); errClear != nil {
		return fmt.Errorf("保存 API Key %s 的订阅计划：%w", scope, errClear)
	}
	for position, binding := range key.PlanBindings {
		_, errBinding := tx.Exec(`
			INSERT INTO key_plan_bindings (
				scope, position, plan_id, cycle_start_at, cycle_end_at, cycle_spent_usd
			) VALUES (?, ?, ?, ?, ?, ?)`,
			scope, position, binding.PlanID, nanos(binding.Cycle.StartAt),
			nanos(binding.Cycle.EndAt), binding.Cycle.SpentUSD)
		if errBinding != nil {
			return fmt.Errorf("保存 API Key %s 的订阅计划 %s：%w", scope, binding.PlanID, errBinding)
		}
	}
	return nil
}

func (d *DB) loadKeyPlanBindings(state *billing.State) error {
	rows, errQuery := d.db.Query(`
		SELECT scope, plan_id, cycle_start_at, cycle_end_at, cycle_spent_usd
		FROM key_plan_bindings ORDER BY scope, position`)
	if errQuery != nil {
		return fmt.Errorf("读取 API Key 的订阅计划：%w", errQuery)
	}
	defer rows.Close()
	for rows.Next() {
		var scope string
		var binding billing.PlanBinding
		var startAt, endAt int64
		var spentUSD float64
		if errScan := rows.Scan(&scope, &binding.PlanID, &startAt, &endAt, &spentUSD); errScan != nil {
			return fmt.Errorf("读取 API Key 的订阅计划：%w", errScan)
		}
		binding.Cycle = billing.Cycle{
			StartAt: timeAt(startAt), EndAt: timeAt(endAt), SpentUSD: spentUSD,
		}
		if key := state.Keys[scope]; key != nil {
			key.PlanBindings = append(key.PlanBindings, binding)
		}
	}
	if errRows := rows.Err(); errRows != nil {
		return fmt.Errorf("读取 API Key 的订阅计划：%w", errRows)
	}
	for _, key := range state.Keys {
		key.NormalizePlanBindings()
	}
	return nil
}
