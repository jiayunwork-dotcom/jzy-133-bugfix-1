package store

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
)

// InsertAlarmIgnore 幂等写入一条告警：(target, rule, trigger) 已存在则跳过。
// 已出告警只增不删，保证重复评估结果一致。baselineID 是判定时归属的限版本，
// 在首次写入时一并冻结（重复评估必然属于同一套限）。
func InsertAlarmIgnore(ctx context.Context, tx *sql.Tx, targetID int64,
	rule int, triggerSeq int, involved []int, baselineID int64) error {
	arr := make(pq.Int64Array, len(involved))
	for i, v := range involved {
		arr[i] = int64(v)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO alarms (target_id, rule_no, trigger_seq, involved_seq, baseline_id)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (target_id, rule_no, trigger_seq) DO NOTHING`,
		targetID, rule, triggerSeq, arr, baselineID)
	return err
}

const alarmCols = `rule_no, trigger_seq, involved_seq, baseline_id, created_at`

func scanAlarm(s rowScanner) (AlarmRow, error) {
	var a AlarmRow
	err := s.Scan(&a.Rule, &a.TriggerSeq, &a.InvolvedSeq, &a.BaselineID, &a.CreatedAt)
	return a, err
}

// ListAlarms 取一个对象的全部告警，按触发序号与规则排序。
func (db *DB) ListAlarms(ctx context.Context, targetID int64) ([]AlarmRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+alarmCols+` FROM alarms WHERE target_id=$1
		 ORDER BY trigger_seq, rule_no`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlarmRows(rows)
}

// ListAlarmsInTx 事务内版本。
func ListAlarmsInTx(ctx context.Context, tx *sql.Tx, targetID int64) ([]AlarmRow, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT `+alarmCols+` FROM alarms WHERE target_id=$1
		 ORDER BY trigger_seq, rule_no`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlarmRows(rows)
}

// ExistingAlarmKeysInTx 批量取出某套限下已存在的告警键 (rule, triggerSeq)，
// 用于该限重放后只插新增。告警按归属限分区评估，不与其它版本混在一起。
func ExistingAlarmKeysInTx(ctx context.Context, tx *sql.Tx,
	targetID, baselineID int64) (map[[2]int]bool, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT rule_no, trigger_seq FROM alarms
		 WHERE target_id=$1 AND baseline_id=$2`, targetID, baselineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[[2]int]bool{}
	for rows.Next() {
		var r, t int
		if err := rows.Scan(&r, &t); err != nil {
			return nil, err
		}
		out[[2]int{r, t}] = true
	}
	return out, rows.Err()
}

// CountAlarms 返回告警条数（用于「过程不受控」标记）。
func (db *DB) CountAlarms(ctx context.Context, targetID int64) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM alarms WHERE target_id=$1`, targetID).Scan(&n)
	return n, err
}

func scanAlarmRows(rows *sql.Rows) ([]AlarmRow, error) {
	var out []AlarmRow
	for rows.Next() {
		a, err := scanAlarm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
