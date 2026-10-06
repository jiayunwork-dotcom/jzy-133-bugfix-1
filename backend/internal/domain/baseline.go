package domain

// 由基准期子组计算冻结统计量：中心线、控制限、组内标准差与能力指数。

// BaselineStats 基准期统计结果。
type BaselineStats struct {
	ControlLimits
	Capability
}

// ComputeBaselineStats 用基准期参考子组计算统计量。
// 调用方需保证 len(ref) >= 20 且每个子组容量为 n。
func ComputeBaselineStats(ref []Subgroup, n int, usl, lsl *float64) BaselineStats {
	var sumX, sumR float64
	for _, s := range ref {
		sumX += s.Mean
		sumR += s.Range
	}
	k := float64(len(ref))
	xBarBar := sumX / k
	rBar := sumR / k
	lim := CalcLimits(xBarBar, rBar, n)
	return BaselineStats{
		ControlLimits: lim,
		Capability:    CalcCapability(xBarBar, lim.SigmaWithin, usl, lsl),
	}
}
