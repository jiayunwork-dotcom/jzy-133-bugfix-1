package service

import (
	"context"
	"database/sql"
	"errors"

	"spc/internal/domain"
	"spc/internal/store"
)

// Service 业务服务：所有事务边界、切组、判异、基准逻辑都在这里。
type Service struct {
	db *store.DB
}

func New(db *store.DB) *Service { return &Service{db: db} }

// ErrLockedNotFound 行锁查询无此行。
var ErrLockedNotFound = errors.New("目标不存在或已被删除")

func toDomainTarget(t store.TargetRow) domain.Target {
	gt := domain.Target{
		ID:           t.ID,
		Name:         t.Name,
		Machine:      t.Machine,
		Dimension:    t.Dimension,
		SubgroupN:    t.SubgroupN,
		EnabledRules: make([]int, len(t.EnabledRules)),
		CreatedAt:    t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	for i, r := range t.EnabledRules {
		gt.EnabledRules[i] = int(r)
	}
	if t.USL.Valid {
		v := t.USL.Float64
		gt.USL = &v
	}
	if t.LSL.Valid {
		v := t.LSL.Float64
		gt.LSL = &v
	}
	return gt
}

// nullFloat 把可空规格包成 sql.NullFloat64。
func nullFloat(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}

// withTx 在事务中执行 fn，返回错误则回滚。
func (s *Service) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
