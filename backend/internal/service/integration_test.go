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
	"time"

	"github.com/lib/pq"

	root "spc"
	"spc/internal/domain"
	"spc/internal/migrate"
	"spc/internal/service"
	"spc/internal/store"
)

func openSvc(t *testing.T) (*service.Service, *store.DB, context.Context) {
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
	return service.New(store.New(db)), store.New(db), ctx
}

func mustTarget(t *testing.T, svc *service.Service, ctx context.Context, n int) domain.Target {
	t.Helper()
	tgt, err := svc.CreateTarget(ctx, makeTargetSpec(t, n, 20.0, 0.0))
	if err != nil {
		t.Fatalf("建档: %v", err)
	}
	return tgt
}

func ingestGroup(t *testing.T, svc *service.Service, ctx context.Context,
	targetID int64, vals []float64) {
	t.Helper()
	if _, err := svc.Ingest(ctx, service.IngestRequest{
		TargetID: targetID, Values: vals, Grouped: true,
	}); err != nil {
		t.Fatalf("录入子组: %v", err)
	}
}

func makeTargetSpec(t *testing.T, n int, usl, lsl float64) domain.Target {
	t.Helper()
	return domain.TargetSpec{
		Name:         fmt.Sprintf("轴 %p", t),
		Machine:      "CNC-01",
		Dimension:    "外径",
		USL:          &usl,
		LSL:          &lsl,
		SubgroupN:    n,
		EnabledRules: []int{1, 2, 3, 4},
	}
}

// TestIntegrationRebaselineDoesNotReattributeHistory 复现外圆磨床档案：
// v1 用 1..20 冻结，21..40 已在漂移中产生告警；再以 11..30 重新基准。
// 31..40 的点和全部旧告警必须继续挂 v1；新限从 41 开始，且跨窗口规则不把旧点带入。
func TestIntegrationRebaselineDoesNotReattributeHistory(t *testing.T) {
	svc, _, ctx := openSvc(t)
	const n = 5
	usl, lsl := 10.5, 9.5
	tgt, err := svc.CreateTarget(ctx, makeTargetSpec(t, n, usl, lsl))
	if err != nil {
		t.Fatalf("建档: %v", err)
	}

	offsets := []float64{-0.06, -0.03, 0.0, 0.03, 0.06}
	groupVals := func(mean float64) []float64 {
		out := make([]float64, n)
		for i := range out {
			out[i] = mean + offsets[i]
		}
		return out
	}

	for g := 1; g <= 20; g++ {
		ingestGroup(t, svc, ctx, tgt.ID, groupVals(10.0))
	}
	bl1, err := svc.CreateBaseline(ctx, tgt.ID, 1, 20)
	if err != nil {
		t.Fatalf("冻结 v1: %v", err)
	}

	// 21..40 从 10.00 起每组上漂 0.03，在 v1 下产生漂移/越界告警。
	for g := 21; g <= 40; g++ {
		ingestGroup(t, svc, ctx, tgt.ID, groupVals(10+float64(g-20)*0.03))
	}
	before, err := svc.GetSeries(ctx, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldAlarmCount := len(before.Alarms)
	if oldAlarmCount == 0 {
		t.Fatal("漂移子组在 v1 下应产生告警")
	}
	for _, a := range before.Alarms {
		if a.BaselineID == nil || *a.BaselineID != bl1.ID {
			t.Fatalf("重新基准前告警应挂 v1：%+v", a)
		}
	}

	// 基准期故意取在中间（11..30），旧限必须保留到 40，新限只从 41 接管。
	bl2, err := svc.CreateBaseline(ctx, tgt.ID, 11, 30)
	if err != nil {
		t.Fatalf("重新基准: %v", err)
	}
	if bl2.EffectiveFrom != 41 {
		t.Fatalf("新限应从下一个新子组 41 生效，实际 %d", bl2.EffectiveFrom)
	}
	after, err := svc.GetSeries(ctx, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Alarms) != oldAlarmCount {
		t.Fatalf("重新基准本身不得新增/删除告警，旧告警 %d 条，实际 %d 条",
			oldAlarmCount, len(after.Alarms))
	}
	for seq := 31; seq <= 40; seq++ {
		p := after.Points[seq-1]
		if p.BaselineID == nil || *p.BaselineID != bl1.ID {
			t.Fatalf("第 %d 组必须继续挂 v1，实际 %+v", seq, p.BaselineID)
		}
	}
	for _, a := range after.Alarms {
		if a.BaselineID == nil || *a.BaselineID != bl1.ID {
			t.Fatalf("旧告警不得改挂新限：%+v", a)
		}
	}
	v1, v2 := after.Baselines[0], after.Baselines[1]
	if v1.EffectiveTo == nil || *v1.EffectiveTo != 40 || !v2.Active {
		t.Fatalf("生效区间错误：v1=%+v v2=%+v", v1, v2)
	}

	// 连续两次重新基准、中间没有新子组：v2 是 41..40 的空留档版本，v3 从 41 接管。
	bl3, err := svc.CreateBaseline(ctx, tgt.ID, 12, 31)
	if err != nil {
		t.Fatalf("第二次重新基准: %v", err)
	}
	if bl3.EffectiveFrom != 41 {
		t.Fatalf("空 v2 后的 v3 仍应从下一实际新点 41 生效，实际 %d", bl3.EffectiveFrom)
	}
	series, err := svc.GetSeries(ctx, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Baselines) != 3 {
		t.Fatalf("应保留 3 个基准版本，实际 %d", len(series.Baselines))
	}
	if series.Baselines[1].EffectiveTo == nil ||
		*series.Baselines[1].EffectiveTo != 40 {
		t.Fatalf("v2 应为空留档区间 41..40，实际 %+v", series.Baselines[1])
	}
	if len(series.Alarms) != oldAlarmCount {
		t.Fatalf("连续空重新基准不得补告警，旧 %d 条，实际 %d 条",
			oldAlarmCount, len(series.Alarms))
	}
	for _, a := range series.Alarms {
		if a.BaselineID == nil || *a.BaselineID != bl1.ID {
			t.Fatalf("空版本切换不得改挂历史告警：%+v", a)
		}
	}

	// 第 41 组：在 v3 限内（远离 2σ 线）。若错误带入旧点，会因旧窗口延续触发
	// 规则 2/4；版本边界重置后，单独一个新限点不应产生任何告警。
	ingestGroup(t, svc, ctx, tgt.ID, groupVals(10.10))
	series, err = svc.GetSeries(ctx, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Alarms) != oldAlarmCount {
		t.Fatalf("新限首个受控点不应把旧窗口带入而补告警，旧 %d 条，实际 %d 条",
			oldAlarmCount, len(series.Alarms))
	}
	if p := series.Points[40]; p.BaselineID == nil || *p.BaselineID != bl3.ID {
		t.Fatalf("第41组应挂重新基准后的 v3，实际 %+v", p.BaselineID)
	}

	// 基准期恰好取当前末尾（21~40）时，下一限也必须只影响后续新组。
	if _, err := svc.CreateBaseline(ctx, tgt.ID, 21, 40); err != nil {
		t.Fatalf("末尾基准期重新基准: %v", err)
	}
	series, err = svc.GetSeries(ctx, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Baselines) != 4 {
		t.Fatalf("末尾基准期版本后应保留 4 个版本，实际 %d", len(series.Baselines))
	}
	if to := series.Baselines[2].EffectiveTo; to == nil || *to != 41 {
		t.Fatalf("第41组之后立即再重基准时，v3 应只留档到41，实际 %+v", to)
	}
	if got := series.Baselines[len(series.Baselines)-1].EffectiveFrom; got != 42 {
		t.Fatalf("末尾基准期版本应从 42 生效，实际 %d", got)
	}
	if p := series.Points[40]; p.BaselineID == nil || *p.BaselineID != bl3.ID {
		t.Fatalf("已判定的第41组不得因再次重新基准改挂，实际 %+v", p.BaselineID)
	}
}

// TestIntegrationRebaselineConcurrentWithPaste 验证重新基准与整列粘贴串行化：
// 同一批切出的所有子组必须全部归属同一限版本，不能半批旧限、半批新限。
func TestIntegrationRebaselineConcurrentWithPaste(t *testing.T) {
	svc, db, ctx := openSvc(t)
	const n = 5
	usl, lsl := 10.5, 9.5
	tgt, err := svc.CreateTarget(ctx, makeTargetSpec(t, n, usl, lsl))
	if err != nil {
		t.Fatalf("建档: %v", err)
	}
	for g := 0; g < 40; g++ {
		ingestGroup(t, svc, ctx, tgt.ID,
			[]float64{9.94, 9.97, 10.0, 10.03, 10.06})
	}
	if _, err := svc.CreateBaseline(ctx, tgt.ID, 1, 20); err != nil {
		t.Fatal(err)
	}

	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ExecContext(ctx,
		`SELECT id FROM targets WHERE id=$1 FOR UPDATE`, tgt.ID); err != nil {
		t.Fatalf("外部持锁: %v", err)
	}
	t.Cleanup(func() { _ = holder.Rollback() })

	ingDone := make(chan error, 1)
	go func() {
		values := make([]float64, 5*n)
		for i := range values {
			values[i] = 10.2
		}
		_, err := svc.Ingest(ctx, service.IngestRequest{
			TargetID: tgt.ID,
			Values:   values,
			Grouped:  false,
		})
		ingDone <- err
	}()

	waitBlocked := make(chan error, 1)
	go func() {
		var blocked bool
		for i := 0; i < 100; i++ {
			var waiting int
			err := db.QueryRowContext(ctx, `
				SELECT count(*)
				FROM pg_stat_activity
				WHERE wait_event_type = 'Lock'
				  AND query LIKE '%FOR UPDATE%'`,
			).Scan(&waiting)
			if err != nil {
				waitBlocked <- err
				return
			}
			if waiting > 0 {
				blocked = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !blocked {
			waitBlocked <- fmt.Errorf("未等到录入事务等待目标行锁")
		}
		close(waitBlocked)
	}()
	if err := <-waitBlocked; err != nil {
		t.Fatal(err)
	}

	rebDone := make(chan error, 1)
	go func() {
		// 录入已先进入等待队列；发起重新基准，证明两个操作最终由行锁串行化。
		_, err := svc.CreateBaseline(ctx, tgt.ID, 11, 30)
		rebDone <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if err := holder.Commit(); err != nil {
		t.Fatalf("释放外部锁: %v", err)
	}
	if err := <-ingDone; err != nil {
		t.Fatalf("等待后的录入: %v", err)
	}
	if err := <-rebDone; err != nil {
		t.Fatalf("等待后的重新基准: %v", err)
	}

	series, err := svc.GetSeries(ctx, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := series.Points[40]
	if p.BaselineID == nil {
		t.Fatal("第41组必须固化归属，不能为 NULL")
	}
	batchID := *p.BaselineID
	for seq := 41; seq <= 45; seq++ {
		p := series.Points[seq-1]
		if p.BaselineID == nil || *p.BaselineID != batchID {
			t.Fatalf("同一次流式粘贴的第 %d 组被分到不同限：第41组=%d，本组=%+v",
				seq, batchID, p.BaselineID)
		}
	}
	if batchID == series.Baselines[0].ID {
		if to := series.Baselines[0].EffectiveTo; to == nil || *to != 45 {
			t.Fatalf("粘贴先获锁时 v1 应到45，实际 %+v", to)
		}
		if from := series.Baselines[1].EffectiveFrom; from != 46 {
			t.Fatalf("粘贴先获锁时 v2 应从46开始，实际 %d", from)
		}
	} else {
		if to := series.Baselines[0].EffectiveTo; to == nil || *to != 40 {
			t.Fatalf("重新基准先获锁时 v1 应到40，实际 %+v", to)
		}
		if from := series.Baselines[1].EffectiveFrom; from != 41 {
			t.Fatalf("重新基准先获锁时 v2 应从41开始，实际 %d", from)
		}
	}
}

// TestIntegrationConcurrentIngest 两个检验员同时往同一档录入：
// 子组不丢不重、顺序与服务器点序一致，且粘贴批次连续入序。
func TestIntegrationConcurrentIngest(t *testing.T) {
	svc, _, ctx := openSvc(t)
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
	svc, _, ctx := openSvc(t)
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
	svc, _, ctx := openSvc(t)
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
