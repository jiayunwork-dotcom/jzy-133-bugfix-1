package domain

// 均值-极差图标准常数表（ASTM/Shewhart 常用值，子组容量 2..10）。
//
//	A2: 均值图控制限系数，Xbar 限 = Xbarbar ± A2 * Rbar
//	D3,D4: 极差图控制限系数，R 限 = D3*Rbar / D4*Rbar（n<=6 时 D3=0）
//	d2: 极差换算组内标准差系数，sigma = Rbar / d2
var constants = map[int]struct {
	A2, D3, D4, D2 float64
}{
	2:  {1.880, 0.000, 3.267, 1.128},
	3:  {1.023, 0.000, 2.575, 1.693},
	4:  {0.729, 0.000, 2.282, 2.059},
	5:  {0.577, 0.000, 2.114, 2.326},
	6:  {0.483, 0.000, 2.004, 2.534},
	7:  {0.419, 0.076, 1.924, 2.704},
	8:  {0.373, 0.136, 1.864, 2.847},
	9:  {0.337, 0.184, 1.816, 2.970},
	10: {0.308, 0.223, 1.777, 3.078},
}

// ValidN 判断子组容量是否在支持范围内。
func ValidN(n int) bool {
	_, ok := constants[n]
	return ok
}

// ControlLimits 由总均值、平均极差和子组容量计算冻结的控制限。
type ControlLimits struct {
	XBarBar     float64
	RBar        float64
	UCLX, LCLX  float64
	UCLR, LCLR  float64
	SigmaWithin float64
}

// CalcLimits 计算 Xbar-R 控制限与组内标准差。
func CalcLimits(xBarBar, rBar float64, n int) ControlLimits {
	c := constants[n]
	sigma := rBar / c.D2
	return ControlLimits{
		XBarBar:     xBarBar,
		RBar:        rBar,
		UCLX:        xBarBar + c.A2*rBar,
		LCLX:        xBarBar - c.A2*rBar,
		UCLR:        c.D4 * rBar,
		LCLR:        c.D3 * rBar,
		SigmaWithin: sigma,
	}
}
