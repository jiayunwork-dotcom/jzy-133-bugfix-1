package store

import (
	"context"
	"database/sql"
)

// BaselineInput 创建一条新基准所需字段。
type BaselineInput struct {
	Version       int
	RefStartSeq   int
	RefEndSeq     int
	EffectiveFrom int
	XBarBar       float64
	RBar          float64
	UCLX          float64
	LCLX          float64
	UCLR          float64
	LCLR          float64
	SigmaWithin   float64
	Cp            sql.NullFloat64
	Cpk           sql.NullFloat64
}

// InsertBaseline 插入一条冻结基准（在事务内）。
func InsertBaseline(ctx context.Context, tx *sql.Tx, targetID int64, in BaselineInput) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO baselines
		  (target_id, version, ref_start_seq, ref_end_seq, effective_from,
		   xbar_bar, rbar, ucl_x, lcl_x, ucl_r, lcl_r, sigma_within, cp, cpk)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id`,
		targetID, in.Version, in.RefStartSeq, in.RefEndSeq, in.EffectiveFrom,
		in.XBarBar, in.RBar, in.UCLX, in.LCLX, in.UCLR, in.LCLR,
		in.SigmaWithin, in.Cp, in.Cpk).Scan(&id)
	return id, err
}

// CloseOpenBaseline 在事务内把当前开放基准的生效终点置为给定值。
func CloseOpenBaseline(ctx context.Context, tx *sql.Tx, targetID int64, effectiveTo int) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE baselines SET effective_to=$2
		WHERE target_id=$1 AND effective_to IS NULL`, targetID, effectiveTo)
	return err
}

// NextVersion 返回事务内该对象的下一基准版本号。
func NextVersion(ctx context.Context, tx *sql.Tx, targetID int64) (int, error) {
	var v int
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(max(version),0)+1 FROM baselines WHERE target_id=$1`,
		targetID).Scan(&v)
	return v, err
}

const baselineCols = `id, version, ref_start_seq, ref_end_seq, effective_from, effective_to,
	xbar_bar, rbar, ucl_x, lcl_x, ucl_r, lcl_r, sigma_within, cp, cpk, created_at`

func scanBaseline(s rowScanner) (BaselineRow, error) {
	var b BaselineRow
	err := s.Scan(&b.ID, &b.Version, &b.RefStartSeq, &b.RefEndSeq,
		&b.EffectiveFrom, &b.EffectiveTo, &b.XBarBar, &b.RBar,
		&b.UCLX, &b.LCLX, &b.UCLR, &b.LCLR, &b.SigmaWithin, &b.Cp, &b.Cpk,
		&b.CreatedAt)
	return b, err
}

// ListBaselines 取一个对象的全部基准（含已失效的），按版本升序。
func (db *DB) ListBaselines(ctx context.Context, targetID int64) ([]BaselineRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+baselineCols+` FROM baselines WHERE target_id=$1 ORDER BY version`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBaselineRows(rows)
}

// ListBaselinesInTx 事务内版本（调用方持有目标行锁）。
func ListBaselinesInTx(ctx context.Context, tx *sql.Tx, targetID int64) ([]BaselineRow, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT `+baselineCols+` FROM baselines WHERE target_id=$1 ORDER BY version`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBaselineRows(rows)
}

// OpenBaselineInTx 事务内取当前仍生效（未关闭）的基准，无则返回 sql.ErrNoRows。
func OpenBaselineInTx(ctx context.Context, tx *sql.Tx, targetID int64) (BaselineRow, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT `+baselineCols+` FROM baselines
		 WHERE target_id=$1 AND effective_to IS NULL ORDER BY version DESC LIMIT 1`, targetID)
	return scanBaseline(row)
}

func scanBaselineRows(rows *sql.Rows) ([]BaselineRow, error) {
	var out []BaselineRow
	for rows.Next() {
		b, err := scanBaseline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
