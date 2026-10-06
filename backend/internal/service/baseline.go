package service

import (
	"context"
	"database/sql"
	"errors"

	"spc/internal/domain"
	"spc/internal/store"
)

// CreateBaseline 显式发起基准：用 [startSeq,endSeq] 这段连续子组（>=20 个）
// 计算中心线/控制限并冻结。已有的开放基准被关闭并留档，其 effective_to
// 置为新基准实际生效起点-1。
//
// 首次基准：参考子组之外此前没有生效限，因此把基准建立时已存在的后续子组按
// 这一版限补判并固化归属，使冻结时刻的状态与「当时在线」一致。
// 重新基准：新限只对获得行锁后新切出的完整子组生效；已经判定的子组和告警
// 不按新限重判。即使基准期跨在前一版生效区间中间，effectiveFrom 也取当前
// 最后一个完整子组之后，而不是 refEndSeq+1。
func (s *Service) CreateBaseline(ctx context.Context, targetID int64,
	startSeq, endSeq int) (domain.Baseline, error) {
	var out domain.Baseline
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		t, err := store.LockTarget(ctx, tx, targetID)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		tgt := toDomainTarget(t)

		sgRows, err := store.ListSubgroupsInTx(ctx, tx, targetID)
		if err != nil {
			return err
		}
		if err := domain.ValidateBaselineRequest(startSeq, endSeq, len(sgRows)); err != nil {
			return err
		}

		// 组装基准期子组（含原始值），计算统计量。
		ref := make([]domain.Subgroup, 0, endSeq-startSeq+1)
		for _, r := range sgRows {
			if r.Seq < startSeq || r.Seq > endSeq {
				continue
			}
			vals, err := store.SubgroupValuesInTx(ctx, tx, targetID, r.Seq)
			if err != nil {
				return err
			}
			ref = append(ref, domain.Subgroup{
				Seq: r.Seq, Mean: r.Mean, Range: r.Range, Values: vals,
			})
		}
		if len(ref) < 20 {
			return domain.ErrBaselineTooShort
		}
		stats := domain.ComputeBaselineStats(ref, tgt.SubgroupN, tgt.USL, tgt.LSL)

		oldBaselines, err := store.ListBaselinesInTx(ctx, tx, targetID)
		if err != nil {
			return err
		}
		version, err := store.NextVersion(ctx, tx, targetID)
		if err != nil {
			return err
		}
		// 首次基准允许对建限前已存在、参考期之后的子组补判；重新基准则把新限
		// 起点推迟到当前最后完整子组之后，任何历史点都不回溯。
		isFirst := len(oldBaselines) == 0
		effectiveFrom := endSeq + 1
		if !isFirst {
			// 重新基准只接管获得目标行锁后切出的完整子组。基准期本身是参考数据，
			// 所以至少从 ref_end_seq+1 预留；若当时已有更靠后的完整子组，则从其后开始。
			if len(sgRows) > 0 {
				if next := sgRows[len(sgRows)-1].Seq + 1; next > effectiveFrom {
					effectiveFrom = next
				}
			}
			// 关闭旧的开放限：生效至新限实际接管前一点；两次重基准之间没有
			// 新子组时，允许产生一个空的留档版本。
			if err := store.CloseOpenBaseline(ctx, tx, targetID, effectiveFrom-1); err != nil {
				return err
			}
		}

		in := store.BaselineInput{
			Version:       version,
			RefStartSeq:   startSeq,
			RefEndSeq:     endSeq,
			EffectiveFrom: effectiveFrom,
			XBarBar:       stats.XBarBar,
			RBar:          stats.RBar,
			UCLX:          stats.UCLX,
			LCLX:          stats.LCLX,
			UCLR:          stats.UCLR,
			LCLR:          stats.LCLR,
			SigmaWithin:   stats.SigmaWithin,
			Cp:            nullFloat(stats.Cp),
			Cpk:           nullFloat(stats.Cpk),
		}
		id, err := store.InsertBaseline(ctx, tx, targetID, in)
		if err != nil {
			return err
		}

		row, err := store.OpenBaselineInTx(ctx, tx, targetID)
		if err != nil {
			return err
		}

		// 首次基准需要给建限前已存在、且位于参考期之后的点补判；先固化点归属，
		// 再只在这一版限的区间内重放。重新基准不执行，任何旧点都不回溯。
		if isFirst {
			if _, err := store.AssignBaselineToExistingSubgroups(
				ctx, tx, targetID, id, effectiveFrom); err != nil {
				return err
			}
			if _, err := s.evaluateForLockedTarget(ctx, tx, tgt, row); err != nil {
				return err
			}
		}

		out = toDomainBaseline(row)
		return nil
	})
	if err != nil {
		return domain.Baseline{}, err
	}
	return out, nil
}

func toDomainBaseline(b store.BaselineRow) domain.Baseline {
	out := domain.Baseline{
		ID:            b.ID,
		Version:       b.Version,
		RefStartSeq:   b.RefStartSeq,
		RefEndSeq:     b.RefEndSeq,
		EffectiveFrom: b.EffectiveFrom,
		XBarBar:       b.XBarBar,
		RBar:          b.RBar,
		UCLX:          b.UCLX,
		LCLX:          b.LCLX,
		UCLR:          b.UCLR,
		LCLR:          b.LCLR,
		SigmaWithin:   b.SigmaWithin,
		CreatedAt:     b.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if b.EffectiveTo.Valid {
		v := int(b.EffectiveTo.Int64)
		out.EffectiveTo = &v
	}
	if b.Cp.Valid {
		v := b.Cp.Float64
		out.Cp = &v
	}
	if b.Cpk.Valid {
		v := b.Cpk.Float64
		out.Cpk = &v
	}
	return out
}
