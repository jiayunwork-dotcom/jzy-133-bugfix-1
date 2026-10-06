package store

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
)

// PendingCount 事务内统计尚未成组的散点个数（调用方需先持有目标行锁）。
func PendingCount(ctx context.Context, tx *sql.Tx, targetID int64) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM measurements WHERE target_id=$1 AND subgroup_seq IS NULL`,
		targetID).Scan(&count)
	return count, err
}

// LastSubgroupSeq 事务内返回当前最大子组序号，无子组返回 0。
func LastSubgroupSeq(ctx context.Context, tx *sql.Tx, targetID int64) (int, error) {
	var seq int
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(max(seq),0) FROM subgroups WHERE target_id=$1`, targetID).Scan(&seq)
	return seq, err
}

// InsertMeasurements 把一批测量值按服务器顺序连续插入，返回它们各自的全局序号。
// 必须在已对 targets 行加锁的事务中调用，保证并发录入时序号不交错。
func InsertMeasurements(ctx context.Context, tx *sql.Tx, targetID int64,
	values []float64) ([]int64, error) {
	seqs := make([]int64, len(values))
	for i, v := range values {
		err := tx.QueryRowContext(ctx, `
			INSERT INTO measurements (target_id, seq, value)
			VALUES ($1, COALESCE((SELECT max(seq) FROM measurements WHERE target_id=$1),0)+1, $2)
			RETURNING seq`, targetID, v).Scan(&seqs[i])
		if err != nil {
			return nil, err
		}
	}
	return seqs, nil
}

// PendingMeasurements 取所有未成组散点，按全局点序升序。
func PendingMeasurements(ctx context.Context, tx *sql.Tx, targetID int64,
	limit int) ([]MeasurementRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT seq, value, subgroup_seq FROM measurements
		WHERE target_id=$1 AND subgroup_seq IS NULL
		ORDER BY seq LIMIT $2`, targetID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMeasurements(rows)
}

// CreateSubgroup 落一个切好的子组，并把对应散点标记为属于该子组。
func CreateSubgroup(ctx context.Context, tx *sql.Tx, targetID int64,
	seq int, mean, rng float64, pointSeqs []int64) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO subgroups (target_id, seq, mean, range) VALUES ($1,$2,$3,$4)`,
		targetID, seq, mean, rng); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE measurements SET subgroup_seq=$2
		WHERE target_id=$1 AND seq = ANY($3)`,
		targetID, seq, pq.Int64Array(pointSeqs))
	return err
}

// ListSubgroups 取全部子组（升序）。
func (db *DB) ListSubgroups(ctx context.Context, targetID int64) ([]SubgroupRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq, mean, range, created_at
		FROM subgroups WHERE target_id=$1 ORDER BY seq`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSubgroupRows(rows)
}

// ListSubgroupsInTx 事务内取全部子组（升序）。
func ListSubgroupsInTx(ctx context.Context, tx *sql.Tx, targetID int64) ([]SubgroupRow, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT seq, mean, range, created_at FROM subgroups
		 WHERE target_id=$1 ORDER BY seq`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSubgroupRows(rows)
}

func scanSubgroupRows(rows *sql.Rows) ([]SubgroupRow, error) {
	var out []SubgroupRow
	for rows.Next() {
		var s SubgroupRow
		if err := rows.Scan(&s.Seq, &s.Mean, &s.Range, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListSubgroupValues 取每个子组的原始测量值（按子组序、点序）。
func (db *DB) ListSubgroupValues(ctx context.Context, targetID int64) ([][]float64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.seq, m.value
		FROM subgroups s
		JOIN measurements m
		  ON m.target_id = s.target_id AND m.subgroup_seq = s.seq
		WHERE s.target_id=$1
		ORDER BY s.seq, m.seq`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out [][]float64
	for rows.Next() {
		var seq int
		var v float64
		if err := rows.Scan(&seq, &v); err != nil {
			return nil, err
		}
		for len(out) < seq {
			out = append(out, nil)
		}
		out[seq-1] = append(out[seq-1], v)
	}
	return out, rows.Err()
}

// SubgroupValuesInTx 事务内取一个子组的原始测量值。
func SubgroupValuesInTx(ctx context.Context, tx *sql.Tx, targetID int64,
	seq int) ([]float64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT value FROM measurements
		WHERE target_id=$1 AND subgroup_seq=$2 ORDER BY seq`, targetID, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func scanMeasurements(rows *sql.Rows) ([]MeasurementRow, error) {
	var out []MeasurementRow
	for rows.Next() {
		var m MeasurementRow
		if err := rows.Scan(&m.Seq, &m.Value, &m.SubgroupSeq); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
