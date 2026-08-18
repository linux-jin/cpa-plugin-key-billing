package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	"cpa-key-billing/internal/billing"
)

func (d *DB) Load(logCutoff time.Time) (billing.Snapshot, error) {
	if errPrune := pruneLog(d.db.Exec, logCutoff); errPrune != nil {
		return billing.Snapshot{}, errPrune
	}
	state := billing.NewState()
	if errKeys := d.loadKeys(state); errKeys != nil {
		return billing.Snapshot{}, errKeys
	}
	if errPlans := d.loadPlans(state); errPlans != nil {
		return billing.Snapshot{}, errPlans
	}
	if errPrices := d.loadPrices(state); errPrices != nil {
		return billing.Snapshot{}, errPrices
	}
	if errGroups := d.loadModelGroups(state); errGroups != nil {
		return billing.Snapshot{}, errGroups
	}
	if errCredentials := d.loadCredentials(state); errCredentials != nil {
		return billing.Snapshot{}, errCredentials
	}
	snapshot := billing.Snapshot{State: state}
	if errCount := d.db.QueryRow("SELECT count(*) FROM billing_log").Scan(&snapshot.LogEntries); errCount != nil {
		return billing.Snapshot{}, fmt.Errorf("读取计费日志条数：%w", errCount)
	}
	return snapshot, nil
}

// Save is the single write path. Everything one mutation touched lands in one
// transaction, so a crash cannot leave a charged request without its log entry
// or a plan without its bindings.
func (d *DB) Save(state *billing.State, changes billing.Changes) error {
	return d.transact(func(tx *sql.Tx) error {
		if changes.AllKeys {
			if errKeys := replaceKeys(tx, state); errKeys != nil {
				return errKeys
			}
		} else {
			for _, scope := range changes.Keys {
				if errKey := saveKey(tx, scope, state.Keys[scope]); errKey != nil {
					return errKey
				}
			}
		}
		if changes.Plans {
			if errPlans := replacePlans(tx, state); errPlans != nil {
				return errPlans
			}
		}
		if changes.Prices {
			if errPrices := replacePrices(tx, state); errPrices != nil {
				return errPrices
			}
		}
		if changes.ModelGroups {
			if errGroups := replaceModelGroups(tx, state); errGroups != nil {
				return errGroups
			}
		}
		if changes.Credentials {
			if errCredentials := replaceCredentials(tx, state); errCredentials != nil {
				return errCredentials
			}
		}
		return appendLog(tx, changes)
	})
}

func replacePrices(tx *sql.Tx, state *billing.State) error {
	if _, errClear := tx.Exec("DELETE FROM prices"); errClear != nil {
		return fmt.Errorf("保存模型定价：%w", errClear)
	}
	for position, rule := range state.Prices {
		var (
			threshold                                  any
			tierInput, tierOutput, tierRead, tierWrite any
		)
		if tier := rule.LongContext; tier != nil {
			threshold = tier.ThresholdInputTokens
			tierInput = tier.InputPer1M
			tierOutput = tier.OutputPer1M
			tierRead = optionalPrice(tier.CacheReadPer1M)
			tierWrite = optionalPrice(tier.CacheWritePer1M)
		}
		_, errPrice := tx.Exec(`
			INSERT INTO prices (
				position, pattern, input_per_1m, output_per_1m, cache_read_per_1m, cache_write_per_1m,
				long_context_threshold, long_context_input_per_1m, long_context_output_per_1m,
				long_context_cache_read_per_1m, long_context_cache_write_per_1m
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			position, rule.Pattern, rule.InputPer1M, rule.OutputPer1M,
			optionalPrice(rule.CacheReadPer1M), optionalPrice(rule.CacheWritePer1M),
			threshold, tierInput, tierOutput, tierRead, tierWrite)
		if errPrice != nil {
			return fmt.Errorf("保存模型 %s 的定价：%w", rule.Pattern, errPrice)
		}
	}
	return nil
}

func (d *DB) loadPrices(state *billing.State) error {
	rows, errQuery := d.db.Query(`
		SELECT pattern, input_per_1m, output_per_1m, cache_read_per_1m, cache_write_per_1m,
			long_context_threshold, long_context_input_per_1m, long_context_output_per_1m,
			long_context_cache_read_per_1m, long_context_cache_write_per_1m
		FROM prices ORDER BY position`)
	if errQuery != nil {
		return fmt.Errorf("读取模型定价：%w", errQuery)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			rule                                       billing.PriceRule
			cacheRead, cacheWrite                      sql.NullFloat64
			threshold                                  sql.NullInt64
			tierInput, tierOutput, tierRead, tierWrite sql.NullFloat64
		)
		if errScan := rows.Scan(&rule.Pattern, &rule.InputPer1M, &rule.OutputPer1M, &cacheRead, &cacheWrite,
			&threshold, &tierInput, &tierOutput, &tierRead, &tierWrite); errScan != nil {
			return fmt.Errorf("读取模型定价：%w", errScan)
		}
		rule.CacheReadPer1M = priceOrNil(cacheRead)
		rule.CacheWritePer1M = priceOrNil(cacheWrite)
		if threshold.Valid {
			rule.LongContext = &billing.LongContextPrice{
				ThresholdInputTokens: threshold.Int64,
				InputPer1M:           tierInput.Float64,
				OutputPer1M:          tierOutput.Float64,
				CacheReadPer1M:       priceOrNil(tierRead),
				CacheWritePer1M:      priceOrNil(tierWrite),
			}
		}
		state.Prices = append(state.Prices, rule)
	}
	if errRows := rows.Err(); errRows != nil {
		return fmt.Errorf("读取模型定价：%w", errRows)
	}
	return nil
}

// The display name is stored beside the credential it belongs to so that a log
// query can search and show it without this package restating how a credential
// is named.
func replaceCredentials(tx *sql.Tx, state *billing.State) error {
	if _, errClear := tx.Exec("DELETE FROM credentials"); errClear != nil {
		return fmt.Errorf("保存上游凭据：%w", errClear)
	}
	for index, credential := range state.Credentials {
		_, errCredential := tx.Exec(`
			INSERT INTO credentials (auth_index, provider, account, name) VALUES (?, ?, ?, ?)`,
			index, credential.Provider, credential.Account, credential.Name())
		if errCredential != nil {
			return fmt.Errorf("保存上游凭据 %s：%w", index, errCredential)
		}
	}
	return nil
}

func (d *DB) loadCredentials(state *billing.State) error {
	rows, errQuery := d.db.Query("SELECT auth_index, provider, account FROM credentials")
	if errQuery != nil {
		return fmt.Errorf("读取上游凭据：%w", errQuery)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			index      string
			credential billing.Credential
		)
		if errScan := rows.Scan(&index, &credential.Provider, &credential.Account); errScan != nil {
			return fmt.Errorf("读取上游凭据：%w", errScan)
		}
		state.Credentials[index] = credential
	}
	if errRows := rows.Err(); errRows != nil {
		return fmt.Errorf("读取上游凭据：%w", errRows)
	}
	return nil
}
