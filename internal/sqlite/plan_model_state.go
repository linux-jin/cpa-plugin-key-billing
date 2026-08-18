package sqlite

import (
	"database/sql"
	"fmt"

	"cpa-key-billing/internal/billing"
)

func savePlanModelAccess(tx *sql.Tx, plan billing.Plan) error {
	for position, group := range plan.ModelGroupIDs {
		_, errGroup := tx.Exec(`
			INSERT INTO plan_model_groups (plan_id, position, group_id) VALUES (?, ?, ?)`,
			plan.ID, position, group)
		if errGroup != nil {
			return fmt.Errorf("保存订阅计划 %s 的模型分组：%w", plan.ID, errGroup)
		}
	}
	for position, model := range plan.Models {
		_, errModel := tx.Exec(`
			INSERT INTO plan_allowed_models (plan_id, position, model) VALUES (?, ?, ?)`,
			plan.ID, position, model)
		if errModel != nil {
			return fmt.Errorf("保存订阅计划 %s 的可用模型：%w", plan.ID, errModel)
		}
	}
	return nil
}

func (d *DB) loadPlanStrings(table, column string) (map[string][]string, error) {
	rows, errQuery := d.db.Query("SELECT plan_id, " + column + " FROM " + table + " ORDER BY plan_id, position")
	if errQuery != nil {
		return nil, fmt.Errorf("读取订阅计划的可用模型：%w", errQuery)
	}
	defer rows.Close()
	values := make(map[string][]string)
	for rows.Next() {
		var planID, value string
		if errScan := rows.Scan(&planID, &value); errScan != nil {
			return nil, fmt.Errorf("读取订阅计划的可用模型：%w", errScan)
		}
		values[planID] = append(values[planID], value)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("读取订阅计划的可用模型：%w", errRows)
	}
	return values, nil
}
