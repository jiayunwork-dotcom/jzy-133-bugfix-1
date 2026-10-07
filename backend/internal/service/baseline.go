package service

import (
	"context"
	"database/sql"
	"errors"

	"spc/internal/domain"
	"spc/internal/store"
)

// CreateBaseline 显式发起基准：用 [startSeq,endSeq] 这段连续子组（>=20 个）
// 计算中心线/控制限并冻结。
//
// 这里有两个相互独立的概念，不要混为一谈：
//
//  1. 生效区间（effective_from/to，元数据）：
//     effective_from = endSeq+1——「从参考期之后的第一个组号起，这套限是
//     当前开放限」。重新基准时旧开放限的 effective_to 置为 effective_from-1。
//     基准期跨在旧限生效区间中间（如旧限 21.. 开放、新基准期 11~30）时，
//     旧限区间记为 ..30，新限从 31 起开放；但 31~40 这些在重基前就已按旧限
//     判定的点，其「判定归属」仍是旧限（见下），于是新限段 [31..] 与其首批
//     实际判定点（41 起）之间存在一段「归属先存、限段后接」的空隙——这是
//     刻意的，区间只描述限的开放时段，不负责反推点的归属。
//  2. 判定归属（subgroups/alarms.baseline_id，事实）：
//     点与告警在判定那一刻盖到当时的开放限上并落库，之后永不改变。因此
//     31~40 永远挂旧限；重新基准不重判任何旧点；新限只判定它生效后
//     「新切出」的子组。归属绝不由生效区间现算。
//
// 首次基准（此前没有任何限）的特例：生效起点同样是 endSeq+1；此前录下、
// 参考期之外的子组当时没有限可判，冻结后统一「追认」为该限的判定点
// （盖 subgroups.baseline_id），并对该限做一次重放，使冻结时刻状态与
// 「当时在线」一致。这是唯一一次给历史点盖归属。
//
// 连续两次重新基准、中间没有新子组时：新版本照常开放（effective_from 正常
// 取值），但它判定 0 个子组（JudgedCount=0）；下一个新子组直接归最新开放
// 限。留档版本与统计量仍完整保留。
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
		isFirst := len(oldBaselines) == 0

		version, err := store.NextVersion(ctx, tx, targetID)
		if err != nil {
			return err
		}

		// 从参考期之后的第一个组号起开放；旧开放限区间在其前一组关闭。
		effectiveFrom := endSeq + 1
		if !isFirst {
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

		if isFirst {
			// 唯一一次历史追认：把此前参考期之外、尚无归属的子组盖到新限上。
			// 参考期内（1..endSeq 连续区间内）的点保持 NULL（参考点不判异）。
			unjudged := make([]int, 0, len(sgRows)-len(ref))
			for _, r := range sgRows {
				if r.Seq <= endSeq || r.BaselineID.Valid {
					continue
				}
				unjudged = append(unjudged, r.Seq)
			}
			if err := store.AssignSubgroupsBaseline(ctx, tx, targetID, id, unjudged); err != nil {
				return err
			}
			row := store.BaselineRow{
				ID: id, Version: version,
				RefStartSeq: startSeq, RefEndSeq: endSeq, EffectiveFrom: effectiveFrom,
				XBarBar: stats.XBarBar, RBar: stats.RBar,
				UCLX: stats.UCLX, LCLX: stats.LCLX,
				UCLR: stats.UCLR, LCLR: stats.LCLR, SigmaWithin: stats.SigmaWithin,
			}
			if _, err := s.evaluateBaselineInTx(ctx, tx, tgt, row); err != nil {
				return err
			}
		}
		// 重新基准：不重放旧限、不回判旧点；新限区间此刻没有子组，
		// 等下一个子组切出时在录入事务中按新限判定。

		row, err := store.OpenBaselineInTx(ctx, tx, targetID)
		if err != nil {
			return err
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
