package domain

import "math"

// 过程能力（只按基准期、组内标准差 Rbar/d2 估计）。
//
// 双侧规格：
//   Cp  = (USL-LSL) / (6σ)
//   Cpk = min((USL-x̄)/(3σ), (x̄-LSL)/(3σ))
// 单侧规格（只有限度）：只算对应的 Cpk，Cp 为 nil。
// σ<=0（基准期无变异）时无法计算，两个指数都为 nil。

// Capability 过程能力指数。
type Capability struct {
	Cp  *float64
	Cpk *float64
}

// CalcCapability 计算基准期的 Cp / Cpk。
func CalcCapability(mean, sigma float64, usl, lsl *float64) Capability {
	if sigma <= 0 || math.IsNaN(sigma) || math.IsInf(sigma, 0) {
		return Capability{}
	}
	var cap Capability
	if usl != nil && lsl != nil {
		cp := (*usl - *lsl) / (6 * sigma)
		cap.Cp = &cp
	}
	switch {
	case usl != nil && lsl != nil:
		cpu := (*usl - mean) / (3 * sigma)
		cpl := (mean - *lsl) / (3 * sigma)
		cpk := math.Min(cpu, cpl)
		cap.Cpk = &cpk
	case usl != nil:
		cpk := (*usl - mean) / (3 * sigma)
		cap.Cpk = &cpk
	case lsl != nil:
		cpk := (mean - *lsl) / (3 * sigma)
		cap.Cpk = &cpk
	}
	return cap
}
