package store

import (
	"context"
	"database/sql"
)

// CreateTarget 新建监控对象，返回新 id。
func (db *DB) CreateTarget(ctx context.Context, name, machine, dimension string,
	usl, lsl sql.NullFloat64, n int, rules []int) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO targets (name, machine, dimension, usl, lsl, subgroup_n, enabled_rules)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		name, machine, dimension, usl, lsl, n, IntsToInt64(rules)).Scan(&id)
	return id, err
}

// ListTargets 列出全部监控对象。
func (db *DB) ListTargets(ctx context.Context) ([]TargetRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, machine, dimension, usl, lsl, subgroup_n, enabled_rules, created_at
		FROM targets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TargetRow
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTarget 取单个监控对象。
func (db *DB) GetTarget(ctx context.Context, id int64) (TargetRow, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, machine, dimension, usl, lsl, subgroup_n, enabled_rules, created_at
		FROM targets WHERE id=$1`, id)
	return scanTarget(row)
}

// LockTarget 在事务内对监控对象行加 FOR UPDATE 行锁，串行化同一档的录入。
func LockTarget(ctx context.Context, tx *sql.Tx, id int64) (TargetRow, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, name, machine, dimension, usl, lsl, subgroup_n, enabled_rules, created_at
		FROM targets WHERE id=$1 FOR UPDATE`, id)
	return scanTarget(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTarget(s rowScanner) (TargetRow, error) {
	var t TargetRow
	err := s.Scan(&t.ID, &t.Name, &t.Machine, &t.Dimension,
		&t.USL, &t.LSL, &t.SubgroupN, &t.EnabledRules, &t.CreatedAt)
	return t, err
}
