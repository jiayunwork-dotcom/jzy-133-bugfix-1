package domain

import "math"

// 判异评估（Nelson 规则 1..4）。
//
// 评估策略（详见 README「判异评估策略」）：
// 每次追加子组后，在服务端事务内对「冻结控制限生效之后」的整段子组
// 做一次从头重放式的纯函数评估（Evaluate），再与库里已存告警按键
// (rule, triggerSeq) 合并：只插入新出现的、从不删除旧的。
//
// 选这种策略的理由：
//   - 评估是纯函数，只依赖 (子组序列, 冻结的中心线/控制限, 启用规则)，
//     同一段数据无论何时评估结果完全一致；
//   - 在线逐点追加 = 每次重放到当前尾部，触发键只增不减，天然满足
//     「同一段数据反复评估一致」「新点不会让旧告警消失」；
//   - 比起增量维护四套滑动窗口状态，重放无状态、易测试、不会累积状态
//     bug；代价是 O(k) 时间（k 为生效子组数）。车间每 2 小时一个子组，
//     一年约千余点，单次评估微秒级，代价可忽略。
//
// 所有规则只作用在均值图（Xbar）上；极差图只展示，不参与判异。

// point 评估用的单点视图。
type point struct {
	seq  int
	mean float64
}

// Evaluate 对从控制限生效起的均值点序列做判异。
// points 必须按 seq 升序；enabled 为启用的规则编号集合。
// 同一触发点可能命中多条规则，返回的告警按 (规则, 触发序) 排序。
func Evaluate(points []point, center, ucl, lcl float64, enabled map[int]bool) []Alarm {
	var alarms []Alarm
	sigma := (ucl - center) / 3.0 // Xbar 的 sigma；ucl/lcl 关于 center 对称
	if sigma <= 0 || math.IsNaN(sigma) || math.IsInf(sigma, 0) {
		// 控制限退化（基准期极差全为 0 等）：除规则 1 的严格越界外无法分区，
		// 其余规则无可靠意义，直接不评估。
		sigma = 0
	}

	for i, p := range points {
		if enabled[RuleBeyond3Sigma] {
			// 规则 1：一点落在控制限之外（严格大于/小于，恰在限上不算）。
			if p.mean > ucl || p.mean < lcl {
				alarms = append(alarms, Alarm{
					Rule: RuleBeyond3Sigma, TriggerSeq: p.seq, InvolvedSeq: []int{p.seq},
				})
			}
		}

		if enabled[RuleSameSide9] && i >= 8 {
			// 规则 2：连续 9 点落在中心线同一侧（恰在中心线上的点中断计数）。
			side := signStrict(p.mean, center)
			if side != 0 {
				ok := true
				for j := i - 8; j <= i; j++ {
					if signStrict(points[j].mean, center) != side {
						ok = false
						break
					}
				}
				if ok {
					alarms = append(alarms, Alarm{
						Rule: RuleSameSide9, TriggerSeq: p.seq,
						InvolvedSeq: seqRange(points[i-8].seq, p.seq),
					})
				}
			}
		}

		if enabled[RuleTrend6] && i >= 5 {
			// 规则 3：连续 6 点递增或递减（相邻严格不等；持平即中断）。
			dir := 0
			if p.mean > points[i-1].mean {
				dir = 1
			} else if p.mean < points[i-1].mean {
				dir = -1
			}
			if dir != 0 {
				ok := true
				for j := i - 4; j <= i; j++ {
					var d int
					if points[j].mean > points[j-1].mean {
						d = 1
					} else if points[j].mean < points[j-1].mean {
						d = -1
					}
					if d != dir {
						ok = false
						break
					}
				}
				if ok {
					alarms = append(alarms, Alarm{
						Rule: RuleTrend6, TriggerSeq: p.seq,
						InvolvedSeq: seqRange(points[i-5].seq, p.seq),
					})
				}
			}
		}

		if enabled[RuleTwoOfThree2s] && sigma > 0 && i >= 2 {
			// 规则 4：连续 3 点中至少 2 点落在同侧 2σ~3σ 区。
			// 判定为「|x-center| >= 2σ」，即恰在 2σ 线上算入区；
			// 3σ 外的点按同侧计入（经典 Nelson 定义）。
			up := 0
			down := 0
			for j := i - 2; j <= i; j++ {
				z := (points[j].mean - center) / sigma
				if z >= 2 {
					up++
				} else if z <= -2 {
					down++
				}
			}
			if up >= 2 || down >= 2 {
				alarms = append(alarms, Alarm{
					Rule: RuleTwoOfThree2s, TriggerSeq: p.seq,
					InvolvedSeq: seqRange(points[i-2].seq, p.seq),
				})
			}
		}
	}
	return alarms
}

// EvaluateMeans 是面向服务层的便捷入口：传入 Subgroup 切片与启用规则。
func EvaluateMeans(sgs []Subgroup, lim ControlLimits, enabled map[int]bool) []Alarm {
	pts := make([]point, len(sgs))
	for i, s := range sgs {
		pts[i] = point{seq: s.Seq, mean: s.Mean}
	}
	return Evaluate(pts, lim.XBarBar, lim.UCLX, lim.LCLX, enabled)
}

// signStrict 返回 1（上）、-1（下）、0（恰在中心线上）。
func signStrict(v, center float64) int {
	if v > center {
		return 1
	}
	if v < center {
		return -1
	}
	return 0
}

func seqRange(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for s := from; s <= to; s++ {
		out = append(out, s)
	}
	return out
}
