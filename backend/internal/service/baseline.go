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
// 置为新基准生效起点-1；新基准对 endSeq 之后录入的子组生效。
//
// 首次基准：参考子组之外此前没有生效限，因此对生效区间做一次判异重放，
// 使基准建立那一刻的状态与「当时在线」一致。
// 重新基准：旧限期间的告警已按旧限落库且只增不删，新限不回溯判定任何旧点，
// 历史点仍按当时生效的限显示。
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
		effectiveFrom := endSeq + 1
		if len(oldBaselines) > 0 {
			// 关闭旧的开放限：生效至新限起点前一点。
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

		// 仅首次基准对生效区间重放告警；重新基准不回溯。
		if len(oldBaselines) == 0 {
			if _, err := s.evaluateForLockedTarget(ctx, tx, tgt); err != nil {
				return err
			}
		}

		row, err := store.OpenBaselineInTx(ctx, tx, targetID)
		if err != nil {
			return err
		}
		out = toDomainBaseline(row)
		_ = id
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
