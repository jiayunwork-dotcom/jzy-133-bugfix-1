package service_test

// 外圆磨床直径档重新基准档案的复现测试。
//
// 档案：子组容量 5，规格 9.5~10.5，四条判异规则全开，40 个子组。
//   - 1~20 组围绕 10.00 波动（σ 约 0.05），用 1~20 冻结第一套限 v1；
//   - 21~40 组均值从 10.03 起每组 +0.03，期间按 v1 产生告警；
//   - 用第 11~30 组重新基准 v2（基准期跨在 v1 生效区间中间，31~40 不含在内）。
//
// 不变量（重新基准只管以后新录的子组）：
//  1. 31~40 组的点与告警永远挂 v1，不会因基准期跨过边界而改挂 v2；
//  2. 重新基准本身不重判任何旧点——录第 41 组也不会在 31~40 上
//     补出任何在 v1 下没响过的新告警；
//  3. v1 的生效区间止于真正按它判定的最后一组（30），而不是 31；
//  4. v2 生效后，规则 2/3/4 的前向窗口不混入旧限下的点（窗口在限版本处重置）：
//     连续 9 点同侧要等 v2 自己攒满 9 个点才响；
//  5. 连续两次重新基准、中间一个新子组都没录：中间版本判定 0 个子组，
//     下一个子组直接归最新版本，历史告警不动；
//  6. 并发：进行中的一次整列粘贴不会一半旧限、一半新限。

import (
	"context"
	"fmt"
	"sort"
	"testing"

	root "spc"
	"spc/internal/domain"
	"spc/internal/migrate"
	"spc/internal/service"
	"spc/internal/store"
)

// grinderValues 构造一个均值 mean、组内极差固定 0.12 的容量 5 子组。
// R̄=0.12、n=5 时 σ=R̄/d₂=0.12/2.326≈0.0516，A2·R̄=0.06924。
func grinderValues(mean float64) []float64 {
	pat := []float64{-0.06, -0.03, 0, 0.03, 0.06}
	out := make([]float64, 5)
	for i, d := range pat {
		out[i] = mean + d
	}
	return out
}

// 组 k（1..40）的均值：1..20 围绕 10.00；21..40 从 10.03 起每组 +0.03。
func grinderMean(k int) float64 {
	if k <= 20 {
		return 10.0
	}
	return 10.0 + float64(k-20)*0.03
}

// alarmKey 告警的稳定身份：(规则, 触发组)。
type alarmKey struct{ rule, trigger int }

// snapshotSeries 取整档视图，返回点归属（组号 -> 限版本 id 集合）、
// 告警身份集合与「告警 -> 其挂的限版本 id」，并附 baseline id->版本号。
func snapshotSeries(t *testing.T, svc *service.Service, ctx context.Context, tgtID int64) (
	map[int]*int64, map[alarmKey]*int64, map[int64]int,
) {
	t.Helper()
	series, err := svc.GetSeries(ctx, tgtID)
	if err != nil {
		t.Fatalf("取序列: %v", err)
	}
	ver := map[int64]int{}
	for _, b := range series.Baselines {
		ver[b.ID] = b.Version
	}
	points := map[int]*int64{}
	for _, p := range series.Points {
		points[p.Seq] = p.BaselineID
	}
	alarms := map[alarmKey]*int64{}
	for _, a := range series.Alarms {
		alarms[alarmKey{a.Rule, a.TriggerSeq}] = a.BaselineID
	}
	return points, alarms, ver
}

// TestIntegrationGrinderRebaselineDossier 复现用户描述的磨床档案。
func TestIntegrationGrinderRebaselineDossier(t *testing.T) {
	svc, ctx := openSvc(t)
	const n = 5
	usl, lsl := 10.5, 9.5
	tgt, err := svc.CreateTarget(ctx, domain.TargetSpec{
		Name: "外圆磨床直径档", Machine: "GR-01", Dimension: "直径",
		USL: &usl, LSL: &lsl, SubgroupN: n, EnabledRules: []int{1, 2, 3, 4},
	})
	if err != nil {
		t.Fatalf("建档: %v", err)
	}

	// 录入 40 个子组。
	ingestGroup := func(k int) {
		t.Helper()
		if _, err := svc.Ingest(ctx, service.IngestRequest{
			TargetID: tgt.ID, Values: grinderValues(grinderMean(k)), Grouped: true,
		}); err != nil {
			t.Fatalf("录入第 %d 组: %v", k, err)
		}
	}

	// 前 20 组还没有基准：不判异（基准期参考点）。
	for k := 1; k <= 20; k++ {
		ingestGroup(k)
	}
	v1, err := svc.CreateBaseline(ctx, tgt.ID, 1, 20)
	if err != nil {
		t.Fatalf("首次基准: %v", err)
	}

	// 21~40 组按 v1 判异，期间出一批告警。
	for k := 21; k <= 40; k++ {
		ingestGroup(k)
	}
	_, alarmsBefore, _ := snapshotSeries(t, svc, ctx, tgt.ID)
	if len(alarmsBefore) == 0 {
		t.Fatal("21~40 组漂移段在 v1 下应当产生告警")
	}
	// 31~40 组在 v1 下至少有告警（规则1 越界），这是后面要核对不动的那批。
	var driftKeys []alarmKey
	for key, bl := range alarmsBefore {
		if key.trigger >= 31 {
			if bl == nil || *bl != v1.ID {
				t.Fatalf("告警 %+v 应挂 v1(id=%d)，实际 %v", key, v1.ID, bl)
			}
			driftKeys = append(driftKeys, key)
		}
	}
	if len(driftKeys) == 0 {
		t.Fatal("31~40 组在 v1 下应至少有一条告警")
	}

	// 用 11~30 组重新基准：基准期跨在 v1 生效区间中间。
	if _, err := svc.CreateBaseline(ctx, tgt.ID, 11, 30); err != nil {
		t.Fatalf("重新基准 11~30: %v", err)
	}
	points, alarmsAfter, ver2 := snapshotSeries(t, svc, ctx, tgt.ID)
	if len(ver2) != 2 {
		t.Fatalf("应有 2 套限，实际 %d", len(ver2))
	}
	v2ID := int64(-1)
	for id, v := range ver2 {
		if v == 2 {
			v2ID = id
		}
	}

	// 期望 1：31~40 的点仍归 v1（图上不该改挂新限）。
	for k := 31; k <= 40; k++ {
		if points[k] == nil || *points[k] != v1.ID {
			got := func() int64 {
				if points[k] == nil {
					return -1
				}
				return *points[k]
			}()
			t.Fatalf("第 %d 组应仍归 v1(id=%d)，实际 %d（被基准期 %d 或新限 %d 吞掉）",
				k, v1.ID, got, 0, v2ID)
		}
	}
	// 11~20 这些「曾按 v1 判过」的点也不改归属——它们是 v1 判定的点，
	// 只是同时又被选作 v2 的参考期（参考是展示属性，不改判定归属）。
	for k := 21; k <= 30; k++ {
		if points[k] == nil || *points[k] != v1.ID {
			t.Fatalf("第 %d 组判定归属应保持 v1，实际 %v", k, points[k])
		}
	}

	// 期望 2：告警表一条不多（重新基准本身不重判旧点）、一条不少、归属不变。
	if len(alarmsAfter) != len(alarmsBefore) {
		t.Fatalf("重新基准后告警条数变化：%d -> %d（旧告警被改挂或被补判）",
			len(alarmsBefore), len(alarmsAfter))
	}
	for key, bl := range alarmsBefore {
		got, ok := alarmsAfter[key]
		if !ok {
			t.Fatalf("旧告警 %+v 在重新基准后消失", key)
		}
		if got == nil || bl == nil || *got != *bl {
			t.Fatalf("告警 %+v 归属被改：%v -> %v", key, bl, got)
		}
		if key.trigger >= 31 && *got != v1.ID {
			t.Fatalf("31~40 的告警 %+v 改挂到了 id=%d", key, *got)
		}
	}

	// 期望 3：v1 生效区间止于真正按它判定的最后一组（30），不是 31。
	series, _ := svc.GetSeries(ctx, tgt.ID)
	var old, neu *service.BaselineDTO
	for i := range series.Baselines {
		switch series.Baselines[i].Version {
		case 1:
			old = &series.Baselines[i]
		case 2:
			neu = &series.Baselines[i]
		}
	}
	if old.EffectiveTo == nil || *old.EffectiveTo != 30 {
		got := -1
		if old.EffectiveTo != nil {
			got = *old.EffectiveTo
		}
		t.Fatalf("v1 应生效至 30（实际判定末组），实际 %d", got)
	}
	if neu.EffectiveFrom != 31 {
		t.Fatalf("v2 应从 31 起生效，实际 %d", neu.EffectiveFrom)
	}
	if !neu.Active {
		t.Fatal("v2 应为当前开放限")
	}

	// 再录一组（41，均值 10.10：在 v2 下受控、略高于中心 10.0825）。
	// 期望 4：新点归 v2；它不会在 31~40 上补出任何 v1 下没响过的新告警；
	// 且因为前向窗口在限版本处重置，单独一个点也不可能触发规则 2。
	ing, err := svc.Ingest(ctx, service.IngestRequest{
		TargetID: tgt.ID, Values: grinderValues(10.10), Grouped: true,
	})
	if err != nil {
		t.Fatalf("录入第 41 组: %v", err)
	}
	if len(ing.NewAlarms) != 0 {
		t.Fatalf("v2 下第一个受控点不应产生任何告警，实际 %+v", ing.NewAlarms)
	}
	_, alarmsAt41, _ := snapshotSeries(t, svc, ctx, tgt.ID)
	if len(alarmsAt41) != len(alarmsBefore) {
		var extra []alarmKey
		for k := range alarmsAt41 {
			if _, ok := alarmsBefore[k]; !ok {
				extra = append(extra, k)
			}
		}
		t.Fatalf("录第 41 组后在旧点上补出了 %d 条新告警（应为 0）：%+v", len(extra), extra)
	}
	if points41 := mustPoint(t, svc, ctx, tgt.ID, 41); points41 != v2ID {
		t.Fatalf("第 41 组应归 v2(id=%d)，实际 %d", v2ID, points41)
	}

	// 期望 5：v2 下连续 9 点同侧必须由 v2 自己的点攒满——
	// 41 已录 1 个（10.10 > v2 中心），再录 8 个同侧点时规则 2 不响，
	// 第 9 个（第 49 组）才响；整条窗口不包含任何 v1 的点。
	for k := 42; k <= 48; k++ {
		ingestGroupAt(t, svc, tgt.ID, 10.10)
	}
	_, alarmsAt48, _ := snapshotSeries(t, svc, ctx, tgt.ID)
	if a := alarmsAt48[alarmKey{domain.RuleSameSide9, 48}]; a != nil {
		t.Fatal("v2 自己只有 8 个同侧点（41~48），规则 2 不应在 48 触发；" +
			"若触发说明窗口混进了 v1 的点")
	}
	ingestGroupAt(t, svc, tgt.ID, 10.10) // 第 49 组
	_, alarmsAt49, _ := snapshotSeries(t, svc, ctx, tgt.ID)
	a49 := alarmsAt49[alarmKey{domain.RuleSameSide9, 49}]
	if a49 == nil {
		t.Fatal("v2 攒满自己的 9 个同侧点（41~49）后规则 2 应在 49 触发")
	}
	if *a49 != v2ID {
		t.Fatalf("49 组的规则 2 应挂 v2(id=%d)，实际 %d", v2ID, *a49)
	}
	// 该告警涉及的子组必须全部属于 v2，不得跨到 v1 的点。
	var involved []int
	for _, al := range mustSeries(t, svc, ctx, tgt.ID).Alarms {
		if al.Rule == domain.RuleSameSide9 && al.TriggerSeq == 49 {
			involved = al.InvolvedSeq
		}
	}
	if fmt.Sprint(involved) != "[41 42 43 44 45 46 47 48 49]" {
		t.Fatalf("49 组规则 2 涉及窗口应为 41~49（v2 自己的点），实际 %v", involved)
	}

	// 排序只是让 driftKeys 的存在稳定（避免未使用排序告警）。
	sort.Slice(driftKeys, func(i, j int) bool {
		return driftKeys[i].trigger < driftKeys[j].trigger
	})
	if driftKeys[0].trigger != 31 {
		t.Fatalf("漂移段首个告警触发点应从 31 开始，实际 %d", driftKeys[0].trigger)
	}
}

// TestIntegrationConsecutiveRebaselineNoNewSubgroups 连续两次重新基准、
// 两次之间一个新子组都没录：中间版本判定 0 个子组，下一个子组直接归最新
// 开放限；历史点与历史告警归属不变。
func TestIntegrationConsecutiveRebaselineNoNewSubgroups(t *testing.T) {
	svc, ctx := openSvc(t)
	const n = 5
	usl, lsl := 10.5, 9.5
	tgt, err := svc.CreateTarget(ctx, domain.TargetSpec{
		Name: "连续重基档", Machine: "GR-03", Dimension: "直径",
		USL: &usl, LSL: &lsl, SubgroupN: n, EnabledRules: []int{1, 2, 3, 4},
	})
	if err != nil {
		t.Fatalf("建档: %v", err)
	}
	for k := 1; k <= 40; k++ {
		if _, err := svc.Ingest(ctx, service.IngestRequest{
			TargetID: tgt.ID, Values: grinderValues(grinderMean(k)), Grouped: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	v1, err := svc.CreateBaseline(ctx, tgt.ID, 1, 20)
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	// 21~40 已按 v1 判定（含告警）。
	series := mustSeries(t, svc, ctx, tgt.ID)
	keysUnderV1 := map[alarmKey]bool{}
	for _, a := range series.Alarms {
		if a.BaselineID == nil || *a.BaselineID != v1.ID {
			t.Fatalf("重基前告警都应挂 v1，实际 %+v", a)
		}
		keysUnderV1[alarmKey{a.Rule, a.TriggerSeq}] = true
	}

	// 第一次重基 11~30：v2 开放，但此刻 31~40 已属 v1，v2 还没有判定点。
	if _, err := svc.CreateBaseline(ctx, tgt.ID, 11, 30); err != nil {
		t.Fatalf("v2: %v", err)
	}
	// 一个新子组都不录，立刻再重基 1~20：v2 关闭，它判定 0 个子组。
	if _, err := svc.CreateBaseline(ctx, tgt.ID, 1, 20); err != nil {
		t.Fatalf("v3: %v", err)
	}
	series = mustSeries(t, svc, ctx, tgt.ID)
	idByVer := map[int]int64{}
	judged := map[int]int{}
	var toOf2 *int
	for _, b := range series.Baselines {
		idByVer[b.Version] = b.ID
		judged[b.Version] = b.JudgedCount
		if b.Version == 2 {
			toOf2 = b.EffectiveTo
		}
	}
	if len(idByVer) != 3 {
		t.Fatalf("应有 3 套限，实际 %d", len(idByVer))
	}
	if judged[1] != 20 { // 21~40
		t.Fatalf("v1 应判定 20 组（21~40），实际 %d", judged[1])
	}
	if judged[2] != 0 {
		t.Fatalf("中间版本 v2 没来得及判定任何子组，应为 0，实际 %d", judged[2])
	}
	if toOf2 == nil {
		t.Fatal("v2 应已关闭（effective_to 不为空）")
	}
	// 31~40 的点与告警仍挂 v1，一条不多一条不少。
	points, alarmsNow, _ := snapshotSeries(t, svc, ctx, tgt.ID)
	for k := 31; k <= 40; k++ {
		if points[k] == nil || *points[k] != v1.ID {
			t.Fatalf("第 %d 组应仍挂 v1，实际 %v", k, points[k])
		}
	}
	if len(alarmsNow) != len(keysUnderV1) {
		t.Fatalf("连续重基后告警数变化：%d -> %d", len(keysUnderV1), len(alarmsNow))
	}
	for k := range keysUnderV1 {
		bl, ok := alarmsNow[k]
		if !ok || bl == nil || *bl != v1.ID {
			t.Fatalf("告警 %+v 应仍挂 v1，实际 ok=%v bl=%v", k, ok, bl)
		}
	}

	// 下一个新子组（41）直接归最新开放限 v3；窗口从零开始，单点不告警。
	ing, err := svc.Ingest(ctx, service.IngestRequest{
		TargetID: tgt.ID, Values: grinderValues(10.01), Grouped: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ing.NewAlarms) != 0 {
		t.Fatalf("v3 下第一个点不应产生告警，实际 %+v", ing.NewAlarms)
	}
	if got := mustPoint(t, svc, ctx, tgt.ID, 41); got != idByVer[3] {
		t.Fatalf("第 41 组应直接归 v3(id=%d)，实际 %d", idByVer[3], got)
	}
}

// TestIntegrationRebaselineVsConcurrentIngest 重新基准与进行中的整列粘贴并发：
// 一次粘贴产生的多个子组必须同归一套限，不能一半旧限、一半新限。
func TestIntegrationRebaselineVsConcurrentIngest(t *testing.T) {
	svc, ctx := openSvc(t)
	const n = 5
	usl, lsl := 10.5, 9.5
	tgt, err := svc.CreateTarget(ctx, domain.TargetSpec{
		Name: "并发档", Machine: "GR-02", Dimension: "直径",
		USL: &usl, LSL: &lsl, SubgroupN: n, EnabledRules: []int{1, 2, 3, 4},
	})
	if err != nil {
		t.Fatalf("建档: %v", err)
	}
	// 20 组受控数据 + 冻结 v1。
	for k := 1; k <= 20; k++ {
		if _, err := svc.Ingest(ctx, service.IngestRequest{
			TargetID: tgt.ID, Values: grinderValues(10.0), Grouped: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.CreateBaseline(ctx, tgt.ID, 1, 20); err != nil {
		t.Fatalf("首次基准: %v", err)
	}

	// 流式一次粘贴 15 个整组的量（75 点），同时发起重新基准。
	// 目标行锁把两事务串行化：粘贴要么整体在重基前提交（全部归 v1），
	// 要么整体在重基后提交（全部归重基后开放的限）；不允许交错。
	batch := make([]float64, 0, 15*n)
	for g := 0; g < 15; g++ {
		batch = append(batch, grinderValues(10.0+float64(g)*0.005)...)
	}
	_ = sort.Ints

	done := make(chan error, 2)
	go func() {
		_, e := svc.Ingest(ctx, service.IngestRequest{
			TargetID: tgt.ID, Values: batch, Grouped: false,
		})
		done <- e
	}()
	go func() {
		_, e := svc.CreateBaseline(ctx, tgt.ID, 1, 20)
		done <- e
	}()
	for i := 0; i < 2; i++ {
		if e := <-done; e != nil {
			t.Fatalf("并发事务出错: %v", e)
		}
	}

	series := mustSeries(t, svc, ctx, tgt.ID)
	if len(series.Points) != 35 {
		t.Fatalf("应有 35 个子组，实际 %d", len(series.Points))
	}
	// 本次粘贴切出的 21~35 共 15 组，归属必须完全一致。
	var owner *int64
	for k := 21; k <= 35; k++ {
		p := series.Points[k-1]
		if p.BaselineID == nil {
			t.Fatalf("第 %d 组必须挂在某套限下，不能无归属", k)
		}
		if owner == nil {
			owner = p.BaselineID
		} else if *p.BaselineID != *owner {
			t.Fatalf("同一次粘贴的子组归属被重新基准切成两半：第 21 组归 %d，第 %d 组归 %d",
				*owner, k, *p.BaselineID)
		}
	}
	// 每条告警挂的限必须与其触发点归属一致。
	pointOwner := map[int]*int64{}
	for _, p := range series.Points {
		pointOwner[p.Seq] = p.BaselineID
	}
	for _, a := range series.Alarms {
		if a.BaselineID == nil || pointOwner[a.TriggerSeq] == nil ||
			*a.BaselineID != *pointOwner[a.TriggerSeq] {
			t.Fatalf("第 %d 组告警挂限 %v 与触发点归属 %v 不一致",
				a.TriggerSeq, a.BaselineID, pointOwner[a.TriggerSeq])
		}
	}
}

func ingestGroupAt(t *testing.T, svc *service.Service, tgtID int64, mean float64) {
	t.Helper()
	if _, err := svc.Ingest(ctx2(), service.IngestRequest{
		TargetID: tgtID, Values: grinderValues(mean), Grouped: true,
	}); err != nil {
		t.Fatalf("录入(mean=%v): %v", mean, err)
	}
}

// ctx2 与 openSvc 一样使用 background context（辅助函数用）。
func ctx2() context.Context { return context.Background() }

func mustPoint(t *testing.T, svc *service.Service, ctx context.Context,
	tgtID int64, seq int) int64 {
	t.Helper()
	s := mustSeries(t, svc, ctx, tgtID)
	for _, p := range s.Points {
		if p.Seq == seq {
			if p.BaselineID == nil {
				return -1
			}
			return *p.BaselineID
		}
	}
	t.Fatalf("找不到第 %d 组", seq)
	return -1
}

func mustSeries(t *testing.T, svc *service.Service, ctx context.Context, id int64) service.SeriesDTO {
	t.Helper()
	s, err := svc.GetSeries(ctx, id)
	if err != nil {
		t.Fatalf("取序列: %v", err)
	}
	return s
}

// 保持未使用导入不报错（store/root/migrate 由 openSvc 间接使用，这里仅引用以防重构遗漏）。
var _ = store.New
var _ = root.MigrationsFS
var _ = migrate.New
