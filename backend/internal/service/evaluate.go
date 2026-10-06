package service

import (
	"context"
	"database/sql"
	"errors"

	"spc/internal/domain"
	"spc/internal/store"
)

// evaluateForLockedTarget 在已持有目标行锁的事务内：
// 取当前开放基准与全部子组，对生效区间（seq >= effective_from）重放判异，
// 与已存告警键比较，只插入新出现的告警，并返回新告警列表。
//
// 评估是纯函数的从头重放，所以：
//   - 同一段数据反复评估结果完全一致；
//   - 新点只会让 (rule, triggerSeq) 集合扩大，已出告警不会消失；
//   - 逐点在线追加（每次录入重放至当前尾部）与一次性从头重放完全等价。
func (s *Service) evaluateForLockedTarget(ctx context.Context, tx *sql.Tx,
	tgt domain.Target) ([]domain.Alarm, error) {
	bl, err := store.OpenBaselineInTx(ctx, tx, tgt.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // 尚未冻结基准，不判异
	}
	if err != nil {
		return nil, err
	}

	sgRows, err := store.ListSubgroupsInTx(ctx, tx, tgt.ID)
	if err != nil {
		return nil, err
	}
	sgs := make([]domain.Subgroup, 0, len(sgRows))
	for _, r := range sgRows {
		if r.Seq < bl.EffectiveFrom {
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

	existing, err := store.ExistingAlarmKeysInTx(ctx, tx, tgt.ID)
	if err != nil {
		return nil, err
	}
	var fresh []domain.Alarm
	for _, a := range candidates {
		if existing[[2]int{a.Rule, a.TriggerSeq}] {
			continue
		}
		if err := store.InsertAlarmIgnore(ctx, tx, tgt.ID,
			a.Rule, a.TriggerSeq, a.InvolvedSeq); err != nil {
			return nil, err
		}
		fresh = append(fresh, a)
	}
	return fresh, nil
}
