package domain

import (
	"math"
	"testing"
)

// 所有规则测试共用：中心线 10，控制限 1..19，故 sigma = 3，2σ 区从 4/16 开始。
func ruleParams() (center, ucl, lcl float64) { return 10, 19, 1 }

func pts(means ...float64) []point {
	out := make([]point, len(means))
	for i, m := range means {
		out[i] = point{seq: i + 1, mean: m}
	}
	return out
}

func rules(nums ...int) map[int]bool {
	m := map[int]bool{}
	for _, n := range nums {
		m[n] = true
	}
	return m
}

// findAlarm 返回触发序为 trigger 的规则告警（找不到返回 nil）。
func findAlarm(alarms []Alarm, rule, trigger int) *Alarm {
	for i := range alarms {
		if alarms[i].Rule == rule && alarms[i].TriggerSeq == trigger {
			return &alarms[i]
		}
	}
	return nil
}

func TestRule1_Beyond3Sigma(t *testing.T) {
	center, ucl, lcl := ruleParams()
	en := rules(RuleBeyond3Sigma)

	// 恰好触发：一点严格超出控制限。
	a := Evaluate(pts(10, 10, 19.0001), center, ucl, lcl, en)
	if g := findAlarm(a, RuleBeyond3Sigma, 3); g == nil {
		t.Fatalf("超上限应触发，实际告警=%v", a)
	}
	a = Evaluate(pts(10, 10, 0.9999), center, ucl, lcl, en)
	if g := findAlarm(a, RuleBeyond3Sigma, 3); g == nil {
		t.Fatalf("超下限应触发，实际告警=%v", a)
	}

	// 差一点：恰在控制限上不算；在限内不算。
	a = Evaluate(pts(10, 10, ucl), center, ucl, lcl, en)
	if g := findAlarm(a, RuleBeyond3Sigma, 3); g != nil {
		t.Fatalf("恰在上限上不应触发，实际=%v", g)
	}
	a = Evaluate(pts(10, 10, lcl), center, ucl, lcl, en)
	if g := findAlarm(a, RuleBeyond3Sigma, 3); g != nil {
		t.Fatalf("恰在下限上不应触发，实际=%v", g)
	}
	a = Evaluate(pts(10, 10, 18.99), center, ucl, lcl, en)
	if len(a) != 0 {
		t.Fatalf("限内不应有告警，实际=%v", a)
	}

	// 规则关闭时不评估。
	if a := Evaluate(pts(10, 100), center, ucl, lcl, rules()); len(a) != 0 {
		t.Fatalf("规则关闭不应告警，实际=%v", a)
	}
}

func TestRule2_NineSameSide(t *testing.T) {
	center, ucl, lcl := ruleParams()
	en := rules(RuleSameSide9)

	// 恰好触发：连续 9 点同侧（第 9 点触发，涉及 1..9）。
	up := make([]float64, 9)
	for i := range up {
		up[i] = 10.5
	}
	a := Evaluate(pts(up...), center, ucl, lcl, en)
	g := findAlarm(a, RuleSameSide9, 9)
	if g == nil {
		t.Fatalf("连续9点同侧应在第9点触发，实际=%v", a)
	}
	if len(g.InvolvedSeq) != 9 || g.InvolvedSeq[0] != 1 || g.InvolvedSeq[8] != 9 {
		t.Fatalf("涉及子组应为1..9，实际=%v", g.InvolvedSeq)
	}
	// 继续延伸，第 10 点也应触发一次滑动窗口（窗口 2..10）。
	if g := findAlarm(a, RuleSameSide9, 10); g != nil {
		t.Fatalf("只有9个点时不应有第10点告警")
	}

	// 差一点：连续 8 点同侧不触发。
	eight := make([]float64, 8)
	for i := range eight {
		eight[i] = 9.1
	}
	if a := Evaluate(pts(eight...), center, ucl, lcl, en); len(a) != 0 {
		t.Fatalf("连续8点不应触发，实际=%v", a)
	}

	// 恰在中心线上的点中断计数。
	mixed := append(eight, 10.0, 10.5, 10.5) // 8 下、中心线、再 2 上
	if a := Evaluate(pts(mixed...), center, ucl, lcl, en); len(a) != 0 {
		t.Fatalf("中心线点应中断同侧计数，实际=%v", a)
	}

	// 9 点但两侧交替不触发。
	alt := []float64{10.5, 9.5, 10.5, 9.5, 10.5, 9.5, 10.5, 9.5, 10.5}
	if a := Evaluate(pts(alt...), center, ucl, lcl, en); len(a) != 0 {
		t.Fatalf("两侧交替不应触发，实际=%v", a)
	}

	// 下侧 9 点同样触发。
	down := make([]float64, 9)
	for i := range down {
		down[i] = 9.9
	}
	if a := Evaluate(pts(down...), center, ucl, lcl, en); findAlarm(a, RuleSameSide9, 9) == nil {
		t.Fatalf("连续9点在下侧也应触发")
	}
}

func TestRule3_TrendSix(t *testing.T) {
	center, ucl, lcl := ruleParams()
	en := rules(RuleTrend6)

	// 恰好触发：连续 6 点严格递增。
	a := Evaluate(pts(5, 6, 7, 8, 9, 10), center, ucl, lcl, en)
	g := findAlarm(a, RuleTrend6, 6)
	if g == nil {
		t.Fatalf("连续6点递增应在第6点触发，实际=%v", a)
	}
	if len(g.InvolvedSeq) != 6 {
		t.Fatalf("应涉及6个子组，实际=%v", g.InvolvedSeq)
	}

	// 递减也触发。
	if a := Evaluate(pts(15, 14, 13, 12, 11, 10), center, ucl, lcl, en); findAlarm(a, RuleTrend6, 6) == nil {
		t.Fatalf("连续6点递减应触发")
	}

	// 差一点：只有 5 点递增不触发。
	if a := Evaluate(pts(6, 7, 8, 9, 10), center, ucl, lcl, en); len(a) != 0 {
		t.Fatalf("连续5点递增不应触发，实际=%v", a)
	}

	// 持平中断：6 点中出现相邻相等不触发。
	if a := Evaluate(pts(5, 6, 7, 8, 8, 9), center, ucl, lcl, en); len(a) != 0 {
		t.Fatalf("相邻持平应中断趋势，实际=%v", a)
	}

	// 先增后降不触发。
	if a := Evaluate(pts(5, 6, 7, 8, 9, 8), center, ucl, lcl, en); len(a) != 0 {
		t.Fatalf("非单调不应触发，实际=%v", a)
	}
}

func TestRule4_TwoOfThreeBeyond2Sigma(t *testing.T) {
	center, ucl, lcl := ruleParams()
	en := rules(RuleTwoOfThree2s)
	// sigma=3：2σ 线为 16 / 4；2~3σ 区为 [16,19] / [1,4]。

	// 恰好触发：3 点中 2 点落入上侧 2σ 区。
	a := Evaluate(pts(10, 16.5, 17), center, ucl, lcl, en)
	if g := findAlarm(a, RuleTwoOfThree2s, 3); g == nil {
		t.Fatalf("3点中2点在上侧2σ区应触发，实际=%v", a)
	}
	// 下侧。
	if a := Evaluate(pts(10, 3.5, 3), center, ucl, lcl, en); findAlarm(a, RuleTwoOfThree2s, 3) == nil {
		t.Fatalf("3点中2点在下侧2σ区应触发")
	}
	// 3σ 外的点按同侧计入。
	if a := Evaluate(pts(10, 17, 20), center, ucl, lcl, en); findAlarm(a, RuleTwoOfThree2s, 3) == nil {
		t.Fatalf("含3σ外点时同侧两点也应触发")
	}

	// 差一点：3 点中只有 1 点在 2σ 区。
	if a := Evaluate(pts(10, 10, 17), center, ucl, lcl, en); len(a) != 0 {
		t.Fatalf("仅1点在2σ区不应触发，实际=%v", a)
	}
	// 上下侧各一点不算（必须同侧）。
	if a := Evaluate(pts(10, 17, 3), center, ucl, lcl, en); len(a) != 0 {
		t.Fatalf("2σ区点分居两侧不应触发，实际=%v", a)
	}
	// 恰在 2σ 线上按入区计（经典定义），两点压线应触发。
	if a := Evaluate(pts(10, 16, 16), center, ucl, lcl, en); findAlarm(a, RuleTwoOfThree2s, 3) == nil {
		t.Fatalf("两点恰在2σ线上应触发")
	}
	// 差一点点：15.99 在 2σ 区内缘之外。
	if a := Evaluate(pts(10, 15.99, 15.99), center, ucl, lcl, en); len(a) != 0 {
		t.Fatalf("未达2σ线不应触发，实际=%v", a)
	}

	// 只有 2 个点时不评估。
	if a := Evaluate(pts(17, 17), center, ucl, lcl, en); len(a) != 0 {
		t.Fatalf("不足3点不应触发规则4，实际=%v", a)
	}
}

func TestRuleWindowsDoNotBleed(t *testing.T) {
	// 多规则同开：一次越界只产生规则1，不凭空产生规则2。
	center, ucl, lcl := ruleParams()
	a := Evaluate(pts(10, 10, 10, 20), center, ucl, lcl,
		rules(RuleBeyond3Sigma, RuleSameSide9, RuleTrend6, RuleTwoOfThree2s))
	if len(a) != 1 || a[0].Rule != RuleBeyond3Sigma {
		t.Fatalf("单越界点应只触发规则1，实际=%v", a)
	}
}

func approx(x, y float64) bool { return math.Abs(x-y) < 1e-12 }

func TestCapabilityRelations(t *testing.T) {
	// 基准场景：均值在规格中心 10，sigma=1，规格 7..13（宽 6）。
	c := CalcCapability(10, 1, ptr(13), ptr(7))
	if c.Cp == nil || c.Cpk == nil {
		t.Fatal("双侧规格应同时给出 Cp/Cpk")
	}
	// 均值恰在中心：Cpk == Cp == 1。
	if !approx(*c.Cp, 1.0) || !approx(*c.Cpk, 1.0) {
		t.Fatalf("居中时 Cp=Cpk=1，实际 Cp=%v Cpk=%v", *c.Cp, *c.Cpk)
	}

	// 规格宽度加倍（6 -> 12），数据不变：Cp 加倍。
	wide := CalcCapability(10, 1, ptr(16), ptr(4))
	if !approx(*wide.Cp, 2.0) {
		t.Fatalf("规格宽度加倍 Cp 应为 2，实际 %v", *wide.Cp)
	}

	// 均值向一侧移（10 -> 11）：Cp 不变，Cpk 下降到 2/3。
	shift := CalcCapability(11, 1, ptr(13), ptr(7))
	if !approx(*shift.Cp, 1.0) {
		t.Fatalf("均值平移 Cp 应不变=1，实际 %v", *shift.Cp)
	}
	if !approx(*shift.Cpk, 2.0/3.0) {
		t.Fatalf("均值平移后 Cpk 应为 2/3，实际 %v", *shift.Cpk)
	}
	if *shift.Cpk >= *shift.Cp {
		t.Fatal("平移后 Cpk 应严格小于 Cp")
	}

	// 单侧规格：只有 Cpk，没有 Cp。
	one := CalcCapability(10, 1, ptr(13), nil)
	if one.Cp != nil {
		t.Fatalf("单侧上限不应有 Cp，实际 %v", *one.Cp)
	}
	if one.Cpk == nil || !approx(*one.Cpk, 1.0) {
		t.Fatalf("单侧 Cpk 应为 1，实际 %v", one.Cpk)
	}
	oneL := CalcCapability(10, 1, nil, ptr(7))
	if oneL.Cp != nil || oneL.Cpk == nil || !approx(*oneL.Cpk, 1.0) {
		t.Fatalf("单侧下限计算错误: %+v", oneL)
	}

	// sigma=0：无法计算。
	bad := CalcCapability(10, 0, ptr(13), ptr(7))
	if bad.Cp != nil || bad.Cpk != nil {
		t.Fatalf("零变异时指数应为空，实际 %+v", bad)
	}
}

func ptr(v float64) *float64 { return &v }
