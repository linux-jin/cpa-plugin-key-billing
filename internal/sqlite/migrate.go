package sqlite

import (
	"database/sql"
	"fmt"
)

func (d *DB) migrate(version int) error {
	return d.transact(func(tx *sql.Tx) error {
		if version < 3 {
			if _, errCopy := tx.Exec(`
				INSERT OR IGNORE INTO key_plan_bindings (
					scope, position, plan_id, cycle_start_at, cycle_end_at, cycle_spent_usd
				)
				SELECT scope, 0, plan_id, cycle_start_at, cycle_end_at, cycle_spent_usd
				FROM api_keys WHERE plan_id <> ''`); errCopy != nil {
				return fmt.Errorf("迁移 API Key 多订阅计划绑定：%w", errCopy)
			}
		}
		_, errVersion := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
		if errVersion != nil {
			return fmt.Errorf("标记计费数据库 %s 的格式版本：%w", d.path, errVersion)
		}
		return nil
	})
}
