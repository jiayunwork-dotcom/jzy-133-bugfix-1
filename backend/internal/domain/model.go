package domain

import "errors"

// 判异规则编号，与 Nelson 规则 1..4 对应，也是前端开关和告警记录的稳定标识。
const (
	RuleBeyond3Sigma = 1 // 一点超出三倍标准差（控制限）
	RuleSameSide9    = 2 // 连续 9 点落在中心线同侧
	RuleTrend6       = 3 // 连续 6 点递增或递减
	RuleTwoOfThree2s = 4 // 连续 3 点中有 2 点落在同侧 2~3 倍标准差之间
)

// RuleName 返回规则的中文名称。
func RuleName(rule int) string {
	switch rule {
	case RuleBeyond3Sigma:
		return "一点超出三倍标准差"
	case RuleSameSide9:
		return "连续九点落在中心线同侧"
	case RuleTrend6:
		return "连续六点递增或递减"
	case RuleTwoOfThree2s:
		return "三点中有两点落在同侧两倍到三倍之间"
	default:
		return "未知规则"
	}
}

// 业务层返回、由 API 层映射为 4xx 的错误。
var (
	ErrValidation       = errors.New("参数校验失败")
	ErrNotFound         = errors.New("对象不存在")
	ErrNoBaseline       = errors.New("尚未建立基准，控制限未冻结")
	ErrBaselineTooShort = errors.New("基准期子组少于 20 个")
	ErrRangeExceedsData = errors.New("基准期结束子组超出已有数据")
	ErrBadValues        = errors.New("测量值不合法")
	ErrSizeMismatch     = errors.New("子组容量与档案不符")
)

// Target 一个监控对象：一台机床的一个尺寸。
type Target struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Machine      string   `json:"machine"`
	Dimension    string   `json:"dimension"`
	USL          *float64 `json:"usl"` // 规格上限，允许单侧为空
	LSL          *float64 `json:"lsl"` // 规格下限，允许单侧为空
	SubgroupN    int      `json:"subgroupN"`
	EnabledRules []int    `json:"enabledRules"`
	CreatedAt    string   `json:"createdAt"`
}

// Subgroup 一个切好的子组：顺序号、均值、极差及构成测量值。
type Subgroup struct {
	Seq    int       `json:"seq"`
	Mean   float64   `json:"mean"`
	Range  float64   `json:"range"`
	Values []float64 `json:"values"`
}

// Baseline 一段冻结的控制限及其生效区间（留档用）。
type Baseline struct {
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
}

// Active 为 true 表示这是当前仍在使用的控制限。
func (b Baseline) Active() bool { return b.EffectiveTo == nil }

// Alarm 一条判异告警。
type Alarm struct {
	Rule        int   `json:"rule"`
	TriggerSeq  int   `json:"triggerSeq"`
	InvolvedSeq []int `json:"involvedSeq"`
}
