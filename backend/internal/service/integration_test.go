package service_test

// 集成测试：需要真实 PostgreSQL，通过 TEST_DATABASE_URL 开启，例如：
//   TEST_DATABASE_URL=postgres://spc:spc@localhost:5432/spc_test?sslmode=disable \
//     go test ./internal/service/ -run Integration -v
//
// 未设置该环境变量时全部跳过。测试自动迁移并在独立 schema 中隔离运行。

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"

	"github.com/lib/pq"

	root "spc"
	"spc/internal/domain"
	"spc/internal/migrate"
	"spc/internal/service"
	"spc/internal/store"
)

func openSvc(t *testing.T) (*service.Service, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("跳过集成测试：未设置 TEST_DATABASE_URL")
	}
	connStr, err := pq.ParseURL(dsn)
	if err != nil {
		t.Fatalf("解析 DSN: %v", err)
	}
	// 每个测试用独立 schema 隔离。
	schema := fmt.Sprintf("it_%d", rand.Int63())
	connStr += " search_path=" + schema

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("打开数据库: %v", err)
	}

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("建 schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = db.Close()
	})

	if err := migrate.New(db, root.MigrationsFS).Up(ctx); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return service.New(store.New(db)), ctx
}

func mustTarget(t *testing.T, svc *service.Service, ctx context.Context, n int) domain.Target {
	t.Helper()
	usl, lsl := 20.0, 0.0
	tgt, err := svc.CreateTarget(ctx, domain.TargetSpec{
		Name:         fmt.Sprintf("轴 %p", t),
		Machine:      "CNC-01",
		Dimension:    "外径",
		USL:          &usl,
		LSL:          &lsl,
		SubgroupN:    n,
		EnabledRules: []int{1, 2, 3, 4},
	})
	if err != nil {
		t.Fatalf("建档: %v", err)
	}
	return tgt
}

// TestIntegrationConcurrentIngest 两个检验员同时往同一档录入：
// 子组不丢不重、顺序与服务器点序一致，且粘贴批次连续入序。
func TestIntegrationConcurrentIngest(t *testing.T) {
	svc, ctx := openSvc(t)
	const n = 5
	tgt := mustTarget(t, svc, ctx, n)

	const batches = 20    // 每人 20 个整组
	var wg sync.WaitGroup // 两个检验员
	errs := make(chan error, 2)
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(worker + 1)))
			for b := 0; b < batches; b++ {
				vals := make([]float64, n)
				for i := range vals {
					vals[i] = 10 + r.NormFloat64()*0.2 + float64(worker)
				}
				if _, err := svc.Ingest(ctx, service.IngestRequest{
					TargetID: tgt.ID, Values: vals, Grouped: true,
				}); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("并发录入出错: %v", e)
	}

	series, err := svc.GetSeries(ctx, tgt.ID)
	if err != nil {
		t.Fatalf("取序列: %v", err)
	}
	if got := len(series.Points); got != 2*batches {
		t.Fatalf("子组数应为 %d，实际 %d（丢失或重复）", 2*batches, got)
	}
	// 序号 1..40 连续无缺。
	for i, p := range series.Points {
		if p.Seq != i+1 {
			t.Fatalf("第 %d 个点序号不连续: %d", i, p.Seq)
		}
		if len(p.Values) != n {
			t.Fatalf("子组 %d 容量 %d != %d", p.Seq, len(p.Values), n)
		}
	}
	if series.Pending != 0 {
		t.Fatalf("整组录入后残留散点应为 0，实际 %d", series.Pending)
	}
}

// TestIntegrationStreamingGroupsByServerOrder 流式粘贴 + 错峰补齐：
// 先录入 n-1 个散点，再一次粘贴一整列，验证残留与新点连续拼成子组。
func TestIntegrationStreamingGroupsByServerOrder(t *testing.T) {
	svc, ctx := openSvc(t)
	const n = 4
	tgt := mustTarget(t, svc, ctx, n)

	// 3 个散点（1,2,3），再粘贴 6 个（4..9）：应切出 [1..4] 一组 + [5..8] 一组，残留 1 个。
	_, err := svc.Ingest(ctx, service.IngestRequest{
		TargetID: tgt.ID, Values: []float64{1, 2, 3}, Grouped: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Ingest(ctx, service.IngestRequest{
		TargetID: tgt.ID, Values: []float64{4, 5, 6, 7, 8, 9}, Grouped: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NewSubgroups) != 2 {
		t.Fatalf("应切出 2 组，实际 %d", len(res.NewSubgroups))
	}
	if res.PendingAfter != 1 {
		t.Fatalf("应残留 1 个散点，实际 %d", res.PendingAfter)
	}
	if res.NewSubgroups[0].Mean != 2.5 {
		t.Fatalf("第一组均值应为 2.5（1..4），实际 %v", res.NewSubgroups[0].Mean)
	}

	// 再补 3 个，残留点 9 必须排在新组队头。
	res, err = svc.Ingest(ctx, service.IngestRequest{
		TargetID: tgt.ID, Values: []float64{10, 11, 12}, Grouped: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NewSubgroups) != 1 || res.NewSubgroups[0].Mean != 10.5 {
		t.Fatalf("残留点应与新点连续成组（9..12 均值10.5），实际 %+v", res.NewSubgroups)
	}
}

// TestIntegrationBaselineFreezeAndRebaseline 冻结后再录限不变；
// 重新基准旧限留档、历史点仍挂旧限；重放告警与在线告警一致。
func TestIntegrationBaselineFreezeAndRebaseline(t *testing.T) {
	svc, ctx := openSvc(t)
	const n = 5
	tgt := mustTarget(t, svc, ctx, n)

	// 录 20 组受控数据。
	for g := 0; g < 20; g++ {
		vals := make([]float64, n)
		for i := range vals {
			vals[i] = 10 + float64(g%3)*0.01
		}
		if _, err := svc.Ingest(ctx, service.IngestRequest{
			TargetID: tgt.ID, Values: vals, Grouped: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	bl1, err := svc.CreateBaseline(ctx, tgt.ID, 1, 20)
	if err != nil {
		t.Fatalf("首次基准: %v", err)
	}

	// 基准期不足 20 必须报错。
	if _, err := svc.CreateBaseline(ctx, tgt.ID, 1, 19); err == nil {
		t.Fatal("19 组基准应被拒绝")
	}

	// 冻结后录一个越界组：产生规则1告警，限不回算。
	outVals := []float64{20, 20, 20, 20, 20}
	ing, err := svc.Ingest(ctx, service.IngestRequest{
		TargetID: tgt.ID, Values: outVals, Grouped: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ing.NewAlarms) != 1 || ing.NewAlarms[0].Rule != domain.RuleBeyond3Sigma {
		t.Fatalf("越界组应产生恰好 1 条规则1告警，实际 %+v", ing.NewAlarms)
	}
	series, err := svc.GetSeries(ctx, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Baselines) != 1 {
		t.Fatalf("冻结后录入不应产生新基准，实际 %d 条", len(series.Baselines))
	}
	if series.Baselines[0].UCLX != bl1.UCLX {
		t.Fatal("新录入后冻结限不得回算")
	}
	if series.InControl {
		t.Fatal("有告警点应标记过程不受控")
	}
	// 第 21 组挂在基准 v1 下。
	if series.Points[20].BaselineID == nil || *series.Points[20].BaselineID != bl1.ID {
		t.Fatal("新点应按冻结限显示")
	}

	// 再录 20 组「新过程」数据（中心 12、组内有变异），用 22..41 重新基准。
	// 这些点在旧限下多为越界，产生的告警属于旧限区间的历史记录（保留）。
	pat := []float64{-0.2, -0.1, 0.0, 0.1, 0.2}
	for g := 0; g < 20; g++ {
		vals := make([]float64, n)
		for i := range vals {
			vals[i] = 12 + pat[i]
		}
		if _, err := svc.Ingest(ctx, service.IngestRequest{
			TargetID: tgt.ID, Values: vals, Grouped: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.CreateBaseline(ctx, tgt.ID, 22, 41); err != nil {
		t.Fatalf("重新基准: %v", err)
	}
	series, err = svc.GetSeries(ctx, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Baselines) != 2 {
		t.Fatalf("应留档 2 条基准，实际 %d", len(series.Baselines))
	}
	// 旧限被关闭，生效区间到 41；新限从 42 生效。
	old := series.Baselines[0]
	if old.EffectiveTo == nil {
		t.Fatalf("旧限应已关闭并生效至41，但 effectiveTo 为空，baselines=%+v", series.Baselines)
	}
	if *old.EffectiveTo != 41 {
		t.Fatalf("旧限应生效至41，实际 %d", *old.EffectiveTo)
	}
	if series.Baselines[1].EffectiveFrom != 42 {
		t.Fatalf("新限应从42生效，实际 %d", series.Baselines[1].EffectiveFrom)
	}
	// 第 21 点仍按旧限显示，第 21 点的旧告警仍在且挂旧限。
	if series.Points[20].BaselineID == nil || *series.Points[20].BaselineID != old.ID {
		t.Fatal("历史点应仍按当时生效的旧限显示")
	}
	var alarm21 *service.AlarmDTO
	for i := range series.Alarms {
		if series.Alarms[i].TriggerSeq == 21 && series.Alarms[i].Rule == domain.RuleBeyond3Sigma {
			alarm21 = &series.Alarms[i]
		}
	}
	if alarm21 == nil {
		t.Fatalf("第21组的旧规则1告警应保留，实际告警=%+v", series.Alarms)
	}
	if alarm21.BaselineID == nil || *alarm21.BaselineID != old.ID {
		t.Fatal("旧告警应挂在当时生效的旧限上")
	}

	// 新限生效后再录一个受控点（中心12、正常变异）：不产生新告警。
	okVals := []float64{11.8, 11.9, 12.0, 12.1, 12.2}
	ing, err = svc.Ingest(ctx, service.IngestRequest{
		TargetID: tgt.ID, Values: okVals, Grouped: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ing.NewAlarms) != 0 {
		t.Fatalf("新限下受控点不应产生告警，实际 %+v", ing.NewAlarms)
	}
	// 新限下越界点应产生告警，且不回溯、不改变旧告警。
	series, _ = svc.GetSeries(ctx, tgt.ID)
	nBefore := len(series.Alarms)
	_, err = svc.Ingest(ctx, service.IngestRequest{
		TargetID: tgt.ID, Values: []float64{30, 30, 30, 30, 30}, Grouped: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	series, _ = svc.GetSeries(ctx, tgt.ID)
	if len(series.Alarms) != nBefore+1 {
		t.Fatalf("新限下越界点应只新增 1 条告警（旧告警不动），%d -> %d",
			nBefore, len(series.Alarms))
	}
	last := series.Alarms[len(series.Alarms)-1]
	if last.TriggerSeq != 43 || last.Rule != domain.RuleBeyond3Sigma ||
		last.BaselineID == nil || *last.BaselineID != series.Baselines[1].ID {
		t.Fatalf("新告警应挂新限、触发点为43，实际 %+v", last)
	}
}
