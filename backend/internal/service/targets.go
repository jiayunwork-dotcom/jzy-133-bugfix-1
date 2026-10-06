package service

import (
	"context"
	"database/sql"
	"errors"

	"spc/internal/domain"
)

// CreateTarget 建档并返回完整对象。
func (s *Service) CreateTarget(ctx context.Context, spec domain.TargetSpec) (domain.Target, error) {
	if err := domain.ValidateTarget(spec); err != nil {
		return domain.Target{}, err
	}
	id, err := s.db.CreateTarget(ctx, spec.Name, spec.Machine, spec.Dimension,
		nullFloat(spec.USL), nullFloat(spec.LSL), spec.SubgroupN, spec.EnabledRules)
	if err != nil {
		return domain.Target{}, err
	}
	row, err := s.db.GetTarget(ctx, id)
	if err != nil {
		return domain.Target{}, err
	}
	return toDomainTarget(row), nil
}

// ListTargets 全部监控对象。
func (s *Service) ListTargets(ctx context.Context) ([]domain.Target, error) {
	rows, err := s.db.ListTargets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Target, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainTarget(r))
	}
	return out, nil
}

// GetTarget 单个监控对象。
func (s *Service) GetTarget(ctx context.Context, id int64) (domain.Target, error) {
	row, err := s.db.GetTarget(ctx, id)
	if err != nil {
		return domain.Target{}, mapNotFound(err)
	}
	return toDomainTarget(row), nil
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}
