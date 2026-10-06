package service

import (
	"context"
	"database/sql"

	"spc/internal/domain"
	"spc/internal/store"
)

// evaluateForLockedTarget 在已持有目标行锁、且当前开放基准为 bl 的事务内，
// 只对该版本生效区间内的子组从头重放判异。
//
// 这是版本内重放，不是全局重放：跨版本的规则 2/3/4 滑动窗口在边界处重置，
// 旧限下的点不会喂给新限。告警幂等键包含 baseline_id，因此同一规则/触发点
// 可以分别记录其在不同限版本下的判定结果；版本内重放只插入新增告警。
func (s *Service) evaluateForLockedTarget(ctx context.Context, tx *sql.Tx,
	tgt domain.Target, bl store.BaselineRow) ([]domain.Alarm, error) {
	sgRows, err := store.ListSubgroupsInTx(ctx, tx, tgt.ID)
	if err != nil {
		return nil, err
	}
	sgs := make([]domain.Subgroup, 0, len(sgRows))
	for _, r := range sgRows {
		if !r.BaselineID.Valid || r.BaselineID.Int64 != bl.ID {
			continue
		}
		sgs = append(sgs, domain.Subgroup{Seq: r.Seq, Mean: r.Mean, Range: r.Range})
	}

	enabled := map[int]bool{}
	for _, r := range tgt.EnabledRules {
		enabled[r] = true
	}
	lim := domain.ControlLimits{
		XBarBar: bl.XBarBar, RBar: bl.RBar,
		UCLX: bl.UCLX, LCLX: bl.LCLX,
		UCLR: bl.UCLR, LCLR: bl.LCLR, SigmaWithin: bl.SigmaWithin,
	}
	candidates := domain.EvaluateMeans(sgs, lim, enabled)

	existing, err := store.ExistingAlarmKeysInTx(ctx, tx, tgt.ID, bl.ID)
	if err != nil {
		return nil, err
	}
	var fresh []domain.Alarm
	for _, a := range candidates {
		if existing[[2]int{a.Rule, a.TriggerSeq}] {
			continue
		}
		if err := store.InsertAlarmIgnore(ctx, tx, tgt.ID, bl.ID,
			a.Rule, a.TriggerSeq, a.InvolvedSeq); err != nil {
			return nil, err
		}
		fresh = append(fresh, a)
	}
	return fresh, nil
}
