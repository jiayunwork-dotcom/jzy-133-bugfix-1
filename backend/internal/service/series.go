package service

import (
	"context"

	"spc/internal/domain"
)

// 图表与页面对外的数据结构（数字全部由后端计算）。

type BaselineDTO struct {
	ID            int64    `json:"id"`
	Version       int      `json:"version"`
	RefStartSeq   int      `json:"refStartSeq"`
	RefEndSeq     int      `json:"refEndSeq"`
	EffectiveFrom int      `json:"effectiveFrom"`
	EffectiveTo   *int     `json:"effectiveTo"`
	XBarBar       float64  `json:"xbarBar"`
	RBar          float64  `json:"rbar"`
	UCLX          float64  `json:"uclX"`
	LCLX          float64  `json:"lclX"`
	UCLR          float64  `json:"uclR"`
	LCLR          float64  `json:"lclR"`
	SigmaWithin   float64  `json:"sigmaWithin"`
	Cp            *float64 `json:"cp"`
	Cpk           *float64 `json:"cpk"`
	CreatedAt     string   `json:"createdAt"`
	Active        bool     `json:"active"`
	JudgedCount   int      `json:"judgedCount"` // 实际按这套限判定过的子组数（可能为 0）
}

type PointDTO struct {
	Seq int `json:"seq"`
	// BaselineID 是该点「判定那一刻」归属的限，落库不变；
	// 基准期参考点、首次基准前录入的点为 null。
	BaselineID *int64 `json:"baselineId"`
	// RefBaselineID 仅用于展示：该点作为哪一版限的参考期点（灰点）。
	// 参考是独立属性，不改判定归属——一个点可以既按 v1 判定过，
	// 又被后来的 v2 选作参考期。
	RefBaselineID *int64    `json:"refBaselineId"`
	Mean          float64   `json:"mean"`
	Range         float64   `json:"range"`
	Values        []float64 `json:"values"`
}

type AlarmDTO struct {
	Rule        int    `json:"rule"`
	RuleName    string `json:"ruleName"`
	TriggerSeq  int    `json:"triggerSeq"`
	InvolvedSeq []int  `json:"involvedSeq"`
	BaselineID  *int64 `json:"baselineId"`
}

// SeriesDTO 主图所需的全部数据。
type SeriesDTO struct {
	Target     domain.Target `json:"target"`
	SubgroupN  int           `json:"subgroupN"`
	Pending    int           `json:"pending"`
	Baselines  []BaselineDTO `json:"baselines"`
	Points     []PointDTO    `json:"points"`
	Alarms     []AlarmDTO    `json:"alarms"`
	InControl  bool          `json:"inControl"`
	Capability *BaselineDTO  `json:"capabilityBaseline"`
}

func baselineToDTO(b domain.Baseline) BaselineDTO {
	return BaselineDTO{
		ID:            b.ID,
		Version:       b.Version,
		RefStartSeq:   b.RefStartSeq,
		RefEndSeq:     b.RefEndSeq,
		EffectiveFrom: b.EffectiveFrom,
		EffectiveTo:   b.EffectiveTo,
		XBarBar:       b.XBarBar,
		RBar:          b.RBar,
		UCLX:          b.UCLX,
		LCLX:          b.LCLX,
		UCLR:          b.UCLR,
		LCLR:          b.LCLR,
		SigmaWithin:   b.SigmaWithin,
		Cp:            b.Cp,
		Cpk:           b.Cpk,
		CreatedAt:     b.CreatedAt,
		Active:        b.Active(),
	}
}

// GetSeries 组装一个监控对象的完整视图。
func (s *Service) GetSeries(ctx context.Context, id int64) (SeriesDTO, error) {
	tRow, err := s.db.GetTarget(ctx, id)
	if err != nil {
		return SeriesDTO{}, mapNotFound(err)
	}
	tgt := toDomainTarget(tRow)

	blRows, err := s.db.ListBaselines(ctx, id)
	if err != nil {
		return SeriesDTO{}, err
	}
	sgRows, err := s.db.ListSubgroups(ctx, id)
	if err != nil {
		return SeriesDTO{}, err
	}
	values, err := s.db.ListSubgroupValues(ctx, id)
	if err != nil {
		return SeriesDTO{}, err
	}
	alRows, err := s.db.ListAlarms(ctx, id)
	if err != nil {
		return SeriesDTO{}, err
	}
	pending, err := s.PendingCount(ctx, id)
	if err != nil {
		return SeriesDTO{}, err
	}

	dto := SeriesDTO{
		Target:    tgt,
		SubgroupN: tgt.SubgroupN,
		Pending:   pending,
		InControl: true,
		Baselines: []BaselineDTO{},
		Points:    []PointDTO{},
		Alarms:    []AlarmDTO{},
	}

	// 基线列表。限段画在「实际判定点」序号范围内，由前端按点归属计算，
	// 不使用生效区间（跨边界重基时判定点可越过登记的生效终点）。
	for _, b := range blRows {
		bd := baselineToDTO(toDomainBaseline(b))
		bd.Active = !b.EffectiveTo.Valid
		dto.Baselines = append(dto.Baselines, bd)
	}

	// 每版限实际判定的子组数（事务读取，落库归属计数）。
	judged, err := s.db.CountJudgedByBaselines(ctx, id)
	if err != nil {
		return SeriesDTO{}, err
	}
	for i := range dto.Baselines {
		dto.Baselines[i].JudgedCount = judged[dto.Baselines[i].ID]
	}

	// 点归属读 subgroups.baseline_id（判定那一刻落库，永不重算）。
	// 参考期是独立展示属性：取「最高版本、其参考期包含该点」的限。
	for i, r := range sgRows {
		vals := values[i]
		if vals == nil {
			vals = []float64{}
		}
		p := PointDTO{Seq: r.Seq, Mean: r.Mean, Range: r.Range, Values: vals}
		if r.BaselineID.Valid {
			idv := r.BaselineID.Int64
			p.BaselineID = &idv
		} else {
			// 无判定归属的点：若落在某版限的参考期内，标记为该版参考点（灰）。
			// 取最高版本，保证展示与「最新一次基准」的参考期一致。
			var refID *int64
			refVer := 0
			for _, b := range blRows {
				if r.Seq >= b.RefStartSeq && r.Seq <= b.RefEndSeq {
					if b.Version >= refVer {
						idv := b.ID
						refID = &idv
						refVer = b.Version
					}
				}
			}
			p.RefBaselineID = refID
		}
		dto.Points = append(dto.Points, p)
	}

	// 告警归属读 alarms.baseline_id（判定那一刻落库，不按区间现算）。
	for _, a := range alRows {
		var blid *int64
		if a.BaselineID.Valid {
			idv := a.BaselineID.Int64
			blid = &idv
		}
		inv := make([]int, len(a.InvolvedSeq))
		for i, v := range a.InvolvedSeq {
			inv[i] = int(v)
		}
		dto.Alarms = append(dto.Alarms, AlarmDTO{
			Rule:        a.Rule,
			RuleName:    domain.RuleName(a.Rule),
			TriggerSeq:  a.TriggerSeq,
			InvolvedSeq: inv,
			BaselineID:  blid,
		})
	}
	dto.InControl = len(alRows) == 0

	// 能力指数卡片：用当前开放（最新）基准。
	for i := range dto.Baselines {
		if dto.Baselines[i].Active {
			b := dto.Baselines[i]
			dto.Capability = &b
		}
	}
	return dto, nil
}
