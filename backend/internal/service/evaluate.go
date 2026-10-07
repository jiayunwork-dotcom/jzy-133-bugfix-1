package service

import (
	"context"
	"database/sql"

	"spc/internal/domain"
	"spc/internal/store"
)

// evaluateBaselineInTx 在已持有目标行锁的事务内，对「某一套具体的冻结限」
// 重放判异：只加载归属该限的子组，告警键按 (rule, triggerSeq) 在该限分区内
// 幂等合并——只插入新出现的，并返回新告警列表。
//
// 关键点：喂给纯函数 Evaluate 的点全部属于同一套限，因此规则 2/3/4 这三条
// 要往前数窗口的规则，窗口天然在限版本边界处「重置」——新版本生效后的
// 头几个点不会把旧限下的点算进窗口（见 README「窗口规则的跨版本约定」）。
//
// 评估是纯函数的从头重放，所以：
//   - 同一段数据反复评估结果完全一致；
//   - 新点只会让 (rule, triggerSeq) 集合扩大，该限已出告警不会消失；
//   - 逐点在线追加（每次录入重放至当前尾部）与一次性从头重放完全等价；
//   - 重新基准不动任何旧限的数据：既不重放旧限，也不把旧点喂给新限。
func (s *Service) evaluateBaselineInTx(ctx context.Context, tx *sql.Tx,
	tgt domain.Target, bl store.BaselineRow) ([]domain.Alarm, error) {
	sgRows, err := store.ListSubgroupsByBaselineInTx(ctx, tx, tgt.ID, bl.ID)
	if err != nil {
		return nil, err
	}
	sgs := make([]domain.Subgroup, 0, len(sgRows))
	for _, r := range sgRows {
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
	fresh := []domain.Alarm{}
	for _, a := range candidates {
		if existing[[2]int{a.Rule, a.TriggerSeq}] {
			continue
		}
		if err := store.InsertAlarmIgnore(ctx, tx, tgt.ID,
			a.Rule, a.TriggerSeq, a.InvolvedSeq, bl.ID); err != nil {
			return nil, err
		}
		fresh = append(fresh, a)
	}
	return fresh, nil
}
