package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"spc/internal/domain"
	"spc/internal/store"
)

// IngestRequest 一次录入。
// Grouped=true 表示「整组录入」：值数必须恰为子组容量 n（否则报容量不符），
//
//	且在本次入序内优先切成整组（若此前有残留散点，会先与残留点合并）。
//
// Grouped=false 表示「流式粘贴」：任意数量，按服务器点序连续入序，凑满即切。
type IngestRequest struct {
	TargetID int64
	Values   []float64
	Grouped  bool
}

// IngestResult 返回本次提交的结果。
type IngestResult struct {
	PendingBefore int               `json:"pendingBefore"`
	NewSubgroups  []domain.Subgroup `json:"newSubgroups"`
	PendingAfter  int               `json:"pendingAfter"`
	NewAlarms     []domain.Alarm    `json:"newAlarms"`
	BaselineReady bool              `json:"baselineReady"`
}

// Ingest 录入测量值并切组。整个过程在一个事务中对目标行加 FOR UPDATE 锁，
// 两个检验员并发提交同一档时被数据库行锁串行化：一次粘贴的整列数连续入序，
// 子组不丢不重、顺序就是服务器切出的顺序。
func (s *Service) Ingest(ctx context.Context, req IngestRequest) (IngestResult, error) {
	if err := domain.ValidateValues(req.Values); err != nil {
		return IngestResult{}, err
	}

	var res IngestResult
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		t, err := store.LockTarget(ctx, tx, req.TargetID)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		tgt := toDomainTarget(t)

		if req.Grouped {
			if err := domain.CheckGroupedSize(len(req.Values), tgt.SubgroupN); err != nil {
				return err
			}
		}

		pendingBefore, err := store.PendingCount(ctx, tx, req.TargetID)
		if err != nil {
			return err
		}
		res.PendingBefore = pendingBefore

		if req.Grouped {
			// 整组模式：本次 n 个值必须单独成组。若流式录入留有未凑满的散点，
			// 按服务器点序它们必须排在队头，无法让本次值单独成组，显式报错，
			// 避免检验员误以为自己提交的是一个完整子组。
			if pendingBefore > 0 {
				return fmt.Errorf("%w: 该档尚有 %d 个未凑满的散点，请先在流式模式补齐或改用自动分组",
					domain.ErrSizeMismatch, pendingBefore)
			}
		}
		// 1) 连续入序（行锁保证与其他提交不交错）。
		if _, err := store.InsertMeasurements(ctx, tx, req.TargetID, req.Values); err != nil {
			return err
		}

		// 2) 从队头开始，每凑满 n 个散点切一个子组。
		res.NewSubgroups = []domain.Subgroup{}
		lastSeq, err := store.LastSubgroupSeq(ctx, tx, req.TargetID)
		if err != nil {
			return err
		}
		pending, err := store.PendingMeasurements(ctx, tx, req.TargetID,
			((pendingBefore+len(req.Values))/tgt.SubgroupN)*tgt.SubgroupN)
		if err != nil {
			return err
		}
		for start := 0; start+tgt.SubgroupN <= len(pending); start += tgt.SubgroupN {
			chunk := pending[start : start+tgt.SubgroupN]
			vals := make([]float64, len(chunk))
			seqs := make([]int64, len(chunk))
			for i, m := range chunk {
				vals[i] = m.Value
				seqs[i] = m.Seq
			}
			lastSeq++
			sg := domain.MakeSubgroup(lastSeq, vals)
			if err := store.CreateSubgroup(ctx, tx, req.TargetID,
				sg.Seq, sg.Mean, sg.Range, seqs); err != nil {
				return err
			}
			res.NewSubgroups = append(res.NewSubgroups, sg)
		}
		res.PendingAfter = pendingBefore + len(req.Values) - len(res.NewSubgroups)*tgt.SubgroupN

		// 3) 冻结限存在时，对生效区间重放判异，只插入新告警。
		res.NewAlarms = []domain.Alarm{}
		blRows, err := store.ListBaselinesInTx(ctx, tx, req.TargetID)
		if err != nil {
			return err
		}
		res.BaselineReady = len(blRows) > 0
		if len(res.NewSubgroups) > 0 && res.BaselineReady {
			alarms, err := s.evaluateForLockedTarget(ctx, tx, tgt)
			if err != nil {
				return err
			}
			res.NewAlarms = alarms
		}
		return nil
	})
	if err != nil {
		return IngestResult{}, err
	}
	return res, nil
}

// PendingCount 返回未成组散点个数（列表页提示「还差几个」）。
func (s *Service) PendingCount(ctx context.Context, targetID int64) (int, error) {
	var n int
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := store.LockTarget(ctx, tx, targetID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrNotFound
			}
			return err
		}
		var err error
		n, err = store.PendingCount(ctx, tx, targetID)
		return err
	})
	return n, err
}
