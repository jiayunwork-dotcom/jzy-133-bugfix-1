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
	LineFrom      int      `json:"lineFrom"`
	LineTo        *int     `json:"lineTo"`
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
}

type PointDTO struct {
	Seq        int       `json:"seq"`
	Mean       float64   `json:"mean"`
	Range      float64   `json:"range"`
	Values     []float64 `json:"values"`
	BaselineID *int64    `json:"baselineId"` // 该点判定时生效的限；基准期内为 null
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

	// 基线列表，含画图线段范围。
	byID := map[int64]BaselineDTO{}
	for _, b := range blRows {
		bd := baselineToDTO(toDomainBaseline(b))
		// 线段：首版从其基准期起点开始画；后续版本从生效起点画。
		bd.LineFrom = bd.EffectiveFrom
		if bd.Version == 1 {
			bd.LineFrom = bd.RefStartSeq
		}
		bd.LineTo = bd.EffectiveTo
		bd.Active = !b.EffectiveTo.Valid
		byID[b.ID] = bd
		dto.Baselines = append(dto.Baselines, bd)
	}

	// 点归属到「判定时生效的基准」：
	// 基准期参考点无基准；之后按版本顺序匹配 [effective_from, effective_to]。
	for i, r := range sgRows {
		vals := values[i]
		if vals == nil {
			vals = []float64{}
		}
		p := PointDTO{Seq: r.Seq, Mean: r.Mean, Range: r.Range, Values: vals}
		for _, b := range blRows {
			if r.Seq >= b.RefStartSeq && r.Seq <= b.RefEndSeq {
				p.BaselineID = nil
				break
			}
			to := 1 << 30
			if b.EffectiveTo.Valid {
				to = int(b.EffectiveTo.Int64)
			}
			if r.Seq >= b.EffectiveFrom && r.Seq <= to {
				idv := b.ID
				p.BaselineID = &idv
				break
			}
		}
		dto.Points = append(dto.Points, p)
	}

	// 告警。
	for _, a := range alRows {
		var blid *int64
		// 找到触发点当时生效的限。
		for _, b := range blRows {
			to := 1 << 30
			if b.EffectiveTo.Valid {
				to = int(b.EffectiveTo.Int64)
			}
			if a.TriggerSeq >= b.EffectiveFrom && a.TriggerSeq <= to {
				idv := b.ID
				blid = &idv
				break
			}
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
