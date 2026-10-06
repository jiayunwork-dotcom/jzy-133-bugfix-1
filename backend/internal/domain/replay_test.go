package domain

import (
	"reflect"
	"testing"
)

// TestOnlineAppendEqualsFullReplay 验证：
// 同一段数据，逐点「在线追加」（每加一点评估到当前尾部并累积告警键）
// 与一次性对整条序列「从头重放」得到的告警集合完全相同。
func TestOnlineAppendEqualsFullReplay(t *testing.T) {
	center, ucl, lcl := 40.0, 46.0, 34.0 // sigma=2
	en := rules(RuleBeyond3Sigma, RuleSameSide9, RuleTrend6, RuleTwoOfThree2s)

	// 构造一段会同时触发多条规则的序列。
	series := []float64{
		40, 40.2, 40.4, 40.6, 40.8, 41.0, 41.2, 41.4, 41.6, // 规则2（9点同侧）+规则3（6连增）
		44.2, 44.5, // 规则4：与 41.6 组成 3 点中 2 点在 2σ(=44) 外
		47.2,               // 规则1：超 UCL
		39, 38, 37, 36, 35, // 回归后下行
		33, // 下侧规则1
	}

	type key struct{ rule, trigger int }

	// 在线：长度 1..N 逐次评估，累积全部告警键。
	online := map[key]bool{}
	for k := 1; k <= len(series); k++ {
		got := Evaluate(pts(series[:k]...), center, ucl, lcl, en)
		for _, a := range got {
			online[key{a.Rule, a.TriggerSeq}] = true
		}
	}

	// 重放：一次性评估整条。
	full := Evaluate(pts(series...), center, ucl, lcl, en)
	replay := map[key]bool{}
	for _, a := range full {
		replay[key{a.Rule, a.TriggerSeq}] = true
	}

	if len(online) != len(replay) {
		t.Fatalf("在线告警 %d 条与重放 %d 条不一致", len(online), len(replay))
	}
	for k := range online {
		if !replay[k] {
			t.Errorf("在线有而重放没有: %+v", k)
		}
	}
	for k := range replay {
		if !online[k] {
			t.Errorf("重放有而在线没有: %+v", k)
		}
	}

	// 已出告警不随后来点消失：早期触发的规则1（第12点 47.2 越界）仍在。
	if !replay[key{RuleBeyond3Sigma, 12}] {
		t.Error("第12点的规则1告警应在最终评估中保留")
	}
	if !replay[key{RuleBeyond3Sigma, 18}] {
		t.Error("第18点(33)的规则1告警也应存在")
	}
}

// TestRepeatedEvaluationDeterministic 同一段数据反复评估必须逐条一致。
func TestRepeatedEvaluationDeterministic(t *testing.T) {
	center, ucl, lcl := 100.0, 106.0, 94.0
	en := rules(RuleBeyond3Sigma, RuleSameSide9, RuleTrend6, RuleTwoOfThree2s)
	series := []float64{100, 101, 102, 103, 104, 105, 105.1, 105.2, 105.3, 107, 99, 98}

	var first []Alarm
	for i := 0; i < 5; i++ {
		got := Evaluate(pts(series...), center, ucl, lcl, en)
		if i == 0 {
			first = got
			continue
		}
		if len(got) != len(first) {
			t.Fatalf("第%d次评估告警数变化: %d != %d", i, len(got), len(first))
		}
		for j := range got {
			if !reflect.DeepEqual(got[j], first[j]) {
				t.Fatalf("第%d次评估告警不一致: %+v != %+v", i, got[j], first[j])
			}
		}
	}
}

func TestSubgroupBaselineAndLimits(t *testing.T) {
	// 25 个容量为 5 的子组，均值恒为 10、极差恒为 2。
	ref := make([]Subgroup, 25)
	for i := range ref {
		ref[i] = MakeSubgroup(i+1, []float64{9, 9.5, 10, 10.5, 11}) // 均值10、极差2
	}
	stats := ComputeBaselineStats(ref, 5, ptr(16), ptr(4))
	if !approx(stats.XBarBar, 10) {
		t.Fatalf("Xbarbar 应为 10，实际 %v", stats.XBarBar)
	}
	if !approx(stats.RBar, 2) {
		t.Fatalf("Rbar 应为 2，实际 %v", stats.RBar)
	}
	// n=5: A2=0.577 -> UCL=11.154
	if !approx(stats.UCLX, 10+0.577*2) {
		t.Fatalf("UCLx 错误: %v", stats.UCLX)
	}
	if !approx(stats.LCLX, 10-0.577*2) {
		t.Fatalf("LCLx 错误: %v", stats.LCLX)
	}
	// D4=2.114, D3=0
	if !approx(stats.UCLR, 4.228) || stats.LCLR != 0 {
		t.Fatalf("R 图限错误: ucl=%v lcl=%v", stats.UCLR, stats.LCLR)
	}
	// d2=2.326 -> sigma=0.8598...; Cp = 12/(6*sigma)
	wantSigma := 2.0 / 2.326
	if !approx(stats.SigmaWithin, wantSigma) {
		t.Fatalf("sigma 错误: %v want %v", stats.SigmaWithin, wantSigma)
	}
	wantCp := 12.0 / (6 * wantSigma)
	if !approx(*stats.Cp, wantCp) {
		t.Fatalf("Cp 错误: %v want %v", *stats.Cp, wantCp)
	}
	if !approx(*stats.Cpk, wantCp) {
		t.Fatalf("居中时 Cpk 应等于 Cp: %v vs %v", *stats.Cpk, wantCp)
	}
}

func TestValidation(t *testing.T) {
	// 上限不大于下限。
	err := ValidateTarget(TargetSpec{
		Name: "x", Machine: "m", Dimension: "d",
		USL: ptr(10), LSL: ptr(10), SubgroupN: 5, EnabledRules: []int{1},
	})
	if err == nil {
		t.Fatal("上限等于下限应报错")
	}
	err = ValidateTarget(TargetSpec{
		Name: "x", Machine: "m", Dimension: "d",
		USL: ptr(9), LSL: ptr(10), SubgroupN: 5,
	})
	if err == nil {
		t.Fatal("上限小于下限应报错")
	}

	// 单侧规格合法。
	if err := ValidateTarget(TargetSpec{
		Name: "x", Machine: "m", Dimension: "d",
		USL: ptr(10), SubgroupN: 2,
	}); err != nil {
		t.Fatalf("单侧上限应合法: %v", err)
	}

	// 容量越界。
	for _, n := range []int{0, 1, 11, -3} {
		if err := ValidateTarget(TargetSpec{
			Name: "x", Machine: "m", Dimension: "d",
			USL: ptr(10), SubgroupN: n,
		}); err == nil {
			t.Fatalf("容量 %d 应报错", n)
		}
	}

	// 基准期子组少于 20。
	if err := ValidateBaselineRequest(1, 19, 19); err == nil {
		t.Fatal("19 个基准子组应报错")
	}
	if err := ValidateBaselineRequest(1, 20, 20); err != nil {
		t.Fatalf("20 个基准子组应合法: %v", err)
	}
	if err := ValidateBaselineRequest(1, 20, 15); err == nil {
		t.Fatal("结束点超出已有数据应报错")
	}
	if err := ValidateBaselineRequest(3, 22, 15); err == nil {
		t.Fatal("基准期不足20应报错")
	}

	// 测量值容量不符。
	if err := CheckGroupedSize(4, 5); err == nil {
		t.Fatal("4 个值对容量 5 应报错")
	}
	if err := CheckGroupedSize(5, 5); err != nil {
		t.Fatalf("5 个值对容量 5 应合法: %v", err)
	}

	// 非数字（NaN）。
	if err := ValidateValues([]float64{1, nan(), 3}); err == nil {
		t.Fatal("含 NaN 应报错")
	}
	if err := ValidateValues(nil); err == nil {
		t.Fatal("空值应报错")
	}
}

func nan() float64 {
	var zero float64
	return zero / zero
}
