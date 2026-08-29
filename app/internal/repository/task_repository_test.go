package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"

	"niko-vue-admin/app/internal/model"
)

// newTaskTestDB 建 sqlite 内存库并迁移任务配置模块相关表（含 JOIN 依赖的 cameras/algorithms），
// 同时初始化 desired_state_revision 单行（id=1, revision=0）。
func newTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{TranslateError: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Camera{},
		&model.Algorithm{},
		&model.AlgorithmVersion{},
		&model.AnalysisTask{},
		&model.AlgorithmInstance{},
		&model.DesiredStateRevision{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	// 对齐 000019 迁移的初始化数据
	if err := db.Create(&model.DesiredStateRevision{ID: 1, Revision: 0}).Error; err != nil {
		t.Fatalf("seed desired_state_revision: %v", err)
	}
	return db
}

func newTaskFixture(cameraID, name string) *model.AnalysisTask {
	return &model.AnalysisTask{
		CameraID:       cameraID,
		Name:           name,
		DesiredEnabled: false,
		ActualStatus:   model.TaskStatusUnspecified,
	}
}

func newInstanceFixture(instanceID, cameraID, algorithmID string, fps int32) *model.AlgorithmInstance {
	return &model.AlgorithmInstance{
		InstanceID:   instanceID,
		CameraID:     cameraID,
		AlgorithmID:  algorithmID,
		AnalysisFPS:  fps,
		ParamsJSON:   []byte(`{}`),
		RulesJSON:    []byte(`[]`),
		Enabled:      false,
		ActualStatus: model.InstanceStatusUnspecified,
	}
}

func newCameraTaskFixture(cameraID, rtspURL string) *model.Camera {
	return &model.Camera{
		CameraID:        cameraID,
		Protocol:        model.CameraProtocolRTSP,
		Name:            "cam-" + cameraID,
		RtspURL:         rtspURL,
		TransportPolicy: model.CameraTransportAuto,
		ConfigHash:      "hash-" + cameraID,
	}
}

func newAlgorithmTaskFixture(algorithmID, activeVersion string) *model.Algorithm {
	return &model.Algorithm{
		AlgorithmID:   algorithmID,
		Name:          "alg-" + algorithmID,
		AlgorithmType: "detector",
		ActiveVersion: activeVersion,
	}
}

func TestTaskRepositoryCRUD(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	task := newTaskFixture("cam-a", "大门")
	if err := repo.CreateTask(ctx, task); err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.ID == 0 {
		t.Fatal("create: id not assigned")
	}

	got, err := repo.GetTaskByCameraID(ctx, "cam-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != task.ID || got.Name != "大门" || got.DesiredEnabled {
		t.Fatalf("get = %+v, want created values", got)
	}

	got.Name = "东门"
	got.DesiredEnabled = true
	got.ActualStatus = model.TaskStatusRunning
	if err := repo.UpdateTask(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, err := repo.GetTaskByCameraID(ctx, "cam-a")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if after.Name != "东门" || !after.DesiredEnabled || after.ActualStatus != model.TaskStatusRunning {
		t.Fatalf("after update = %+v", after)
	}

	// 软删后查询返回 ErrNotFound
	deleted, err := repo.DeleteTaskCascade(ctx, "cam-a")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Fatal("delete = false, want true")
	}
	if _, err := repo.GetTaskByCameraID(ctx, "cam-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete error = %v, want ErrNotFound", err)
	}
}

func TestTaskRepositoryDuplicateCameraRejected(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	if err := repo.CreateTask(ctx, newTaskFixture("cam-dup", "第一个")); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := repo.CreateTask(ctx, newTaskFixture("cam-dup", "第二个")); !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("create duplicate error = %v, want ErrDuplicateKey", err)
	}

	// 软删后同一 camera_id 可重新建任务（复合唯一索引含 deleted_at）
	if _, err := repo.DeleteTaskCascade(ctx, "cam-dup"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := repo.CreateTask(ctx, newTaskFixture("cam-dup", "重建")); err != nil {
		t.Fatalf("recreate after delete: %v", err)
	}
}

func TestTaskRepositoryListEnabledInstanceQuotaRows(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	// 算法：alg-a 激活 1.0.0（有版本行）、alg-b 激活 2.0.0（无版本行）、alg-c 未激活
	algA := newAlgorithmTaskFixture("alg-a", "1.0.0")
	algB := newAlgorithmTaskFixture("alg-b", "2.0.0")
	algC := newAlgorithmTaskFixture("alg-c", "")
	for _, alg := range []*model.Algorithm{algA, algB, algC} {
		if err := db.Create(alg).Error; err != nil {
			t.Fatalf("create algorithm: %v", err)
		}
	}
	if err := db.Create(&model.AlgorithmVersion{
		AlgorithmID:  "alg-a",
		Version:      "1.0.0",
		PlatformID:   "macos",
		FPSTiers:     []byte(`[{"fps":5,"units":60},{"fps":25,"units":220}]`),
		ConfigSchema: []byte(`{}`), // 非空避免 sqlite 把空串扫进 json.RawMessage 报错
		ManifestRaw:  []byte(`{}`),
	}).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}

	// 实例：i-a 启用（alg-a 激活+版本行）、i-b 启用（alg-b 无版本行）、i-c 停用
	iA := newInstanceFixture("i-a", "cam-x", "alg-a", 25)
	iA.Enabled = true
	iB := newInstanceFixture("i-b", "cam-y", "alg-b", 10)
	iB.Enabled = true
	iC := newInstanceFixture("i-c", "cam-z", "alg-c", 10)
	for _, inst := range []*model.AlgorithmInstance{iA, iB, iC} {
		if err := repo.CreateInstance(ctx, inst); err != nil {
			t.Fatalf("create instance: %v", err)
		}
	}

	rows, err := repo.ListEnabledInstanceQuotaRows(ctx)
	if err != nil {
		t.Fatalf("list quota rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want 2 (i-a, i-b)", rows)
	}
	// i-a：带 active_version 与档位
	if rows[0].InstanceID != "i-a" || rows[0].ActiveVersion != "1.0.0" || string(rows[0].FPSTiers) == "" {
		t.Fatalf("rows[0] = %+v", rows[0])
	}
	// i-b：active_version 有但版本行缺失 → fps_tiers 空
	if rows[1].InstanceID != "i-b" || rows[1].ActiveVersion != "2.0.0" || len(rows[1].FPSTiers) != 0 {
		t.Fatalf("rows[1] = %+v", rows[1])
	}
}

func TestTaskRepositoryDeleteTaskCascade(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	if err := repo.CreateTask(ctx, newTaskFixture("cam-cas", "任务")); err != nil {
		t.Fatalf("create task: %v", err)
	}
	for i := 1; i <= 2; i++ {
		inst := newInstanceFixture(fmt.Sprintf("inst-%d", i), "cam-cas", "alg-a", 10)
		if err := repo.CreateInstance(ctx, inst); err != nil {
			t.Fatalf("create instance %d: %v", i, err)
		}
	}

	deleted, err := repo.DeleteTaskCascade(ctx, "cam-cas")
	if err != nil {
		t.Fatalf("cascade delete: %v", err)
	}
	if !deleted {
		t.Fatal("cascade delete = false, want true")
	}

	// 实例随任务一同软删
	insts, err := repo.ListInstancesByCameraID(ctx, "cam-cas")
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if len(insts) != 0 {
		t.Fatalf("instances after cascade = %d, want 0", len(insts))
	}
	if _, err := repo.GetInstance(ctx, "inst-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get instance after cascade error = %v, want ErrNotFound", err)
	}

	// 重复删除返回 false
	deleted, err = repo.DeleteTaskCascade(ctx, "cam-cas")
	if err != nil {
		t.Fatalf("cascade delete again: %v", err)
	}
	if deleted {
		t.Fatal("cascade delete again = true, want false")
	}
}

func TestTaskRepositoryInstanceCRUD(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	inst := newInstanceFixture("inst-crud", "cam-x", "alg-x", 10)
	inst.ParamsJSON = []byte(`{"conf":0.5}`)
	inst.RulesJSON = []byte(`[{"role":1,"lineDirection":0,"points":[{"x":0.1,"y":0.1},{"x":0.9,"y":0.1},{"x":0.5,"y":0.9}]}]`)
	if err := repo.CreateInstance(ctx, inst); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetInstance(ctx, "inst-crud")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != inst.ID || got.AnalysisFPS != 10 || string(got.ParamsJSON) != `{"conf":0.5}` {
		t.Fatalf("get = %+v, want created values", got)
	}

	got.AnalysisFPS = 15
	got.Enabled = true
	if err := repo.UpdateInstance(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, err := repo.GetInstance(ctx, "inst-crud")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if after.AnalysisFPS != 15 || !after.Enabled {
		t.Fatalf("after update = %+v", after)
	}

	// ListEnabledInstanceQuotaRows 只含启用实例（无算法/版本行时 JOIN 带出空档位）
	rows, err := repo.ListEnabledInstanceQuotaRows(ctx)
	if err != nil {
		t.Fatalf("list enabled quota rows: %v", err)
	}
	if len(rows) != 1 || rows[0].InstanceID != "inst-crud" {
		t.Fatalf("list enabled quota rows = %+v, want [inst-crud]", rows)
	}

	deleted, err := repo.DeleteInstance(ctx, "inst-crud")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Fatal("delete = false, want true")
	}
	deleted, err = repo.DeleteInstance(ctx, "inst-crud")
	if err != nil {
		t.Fatalf("delete again: %v", err)
	}
	if deleted {
		t.Fatal("delete again = true, want false")
	}
	if _, err := repo.GetInstance(ctx, "inst-crud"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete error = %v, want ErrNotFound", err)
	}
}

func TestTaskRepositoryUpdateStatuses(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	if err := repo.CreateTask(ctx, newTaskFixture("cam-st", "任务")); err != nil {
		t.Fatalf("create task: %v", err)
	}
	inst := newInstanceFixture("inst-st", "cam-st", "alg-a", 10)
	if err := repo.CreateInstance(ctx, inst); err != nil {
		t.Fatalf("create instance: %v", err)
	}

	if err := repo.UpdateTaskStatus(ctx, "cam-st", model.TaskStatusRunning, "ok"); err != nil {
		t.Fatalf("update task status: %v", err)
	}
	if err := repo.UpdateInstanceStatus(ctx, "inst-st", model.InstanceStatusError, "boom"); err != nil {
		t.Fatalf("update instance status: %v", err)
	}

	task, err := repo.GetTaskByCameraID(ctx, "cam-st")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.ActualStatus != model.TaskStatusRunning || task.StatusMessage != "ok" {
		t.Fatalf("task status = %d/%q, want running/ok", task.ActualStatus, task.StatusMessage)
	}
	got, err := repo.GetInstance(ctx, "inst-st")
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if got.ActualStatus != model.InstanceStatusError || got.StatusMessage != "boom" {
		t.Fatalf("instance status = %d/%q, want error/boom", got.ActualStatus, got.StatusMessage)
	}

	// 不存在的实体：幂等静默丢弃，不返回错误（过期回报场景）
	if err := repo.UpdateTaskStatus(ctx, "cam-ghost", model.TaskStatusRunning, ""); err != nil {
		t.Fatalf("update ghost task status error = %v, want nil", err)
	}
	if err := repo.UpdateInstanceStatus(ctx, "inst-ghost", model.InstanceStatusRunning, ""); err != nil {
		t.Fatalf("update ghost instance status error = %v, want nil", err)
	}
}

func TestTaskRepositoryBumpRevisionMonotonic(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	if rev, err := repo.CurrentRevision(ctx); err != nil || rev != 0 {
		t.Fatalf("initial revision = %d/%v, want 0/nil", rev, err)
	}

	for want := uint64(1); want <= 3; want++ {
		got, err := repo.BumpRevision(ctx)
		if err != nil {
			t.Fatalf("bump %d: %v", want, err)
		}
		if got != want {
			t.Fatalf("bump = %d, want %d", got, want)
		}
		cur, err := repo.CurrentRevision(ctx)
		if err != nil || cur != want {
			t.Fatalf("current after bump = %d/%v, want %d/nil", cur, err, want)
		}
	}
}

func TestTaskRepositoryInTxRollbackAndCommit(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	// fn 返回错误：事务整体回滚，revision 不回退也不前进
	err := repo.InTx(ctx, func(ctx context.Context, r TaskRepository) error {
		if _, err := r.BumpRevision(ctx); err != nil {
			return err
		}
		return errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("inTx error = %v, want boom", err)
	}
	rev, err := repo.CurrentRevision(ctx)
	if err != nil || rev != 0 {
		t.Fatalf("revision after rollback = %d/%v, want 0/nil", rev, err)
	}

	// 成功路径：业务写 + BumpRevision 原子提交
	err = repo.InTx(ctx, func(ctx context.Context, r TaskRepository) error {
		if err := r.CreateTask(ctx, newTaskFixture("cam-tx", "事务任务")); err != nil {
			return err
		}
		got, err := r.BumpRevision(ctx)
		if err != nil {
			return err
		}
		if got != 1 {
			return fmt.Errorf("unexpected revision %d", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inTx commit: %v", err)
	}
	rev, err = repo.CurrentRevision(ctx)
	if err != nil || rev != 1 {
		t.Fatalf("revision after commit = %d/%v, want 1/nil", rev, err)
	}
	if _, err := repo.GetTaskByCameraID(ctx, "cam-tx"); err != nil {
		t.Fatalf("task after commit: %v", err)
	}
}

func TestTaskRepositoryListTaskPageFilters(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	// cam-a：有实例；cam-b：无实例；cam-c：有实例但已软删
	if err := repo.CreateTask(ctx, newTaskFixture("cam-a", "门口")); err != nil {
		t.Fatalf("create cam-a: %v", err)
	}
	if err := repo.CreateTask(ctx, newTaskFixture("cam-b", "车库")); err != nil {
		t.Fatalf("create cam-b: %v", err)
	}
	if err := repo.CreateTask(ctx, newTaskFixture("cam-c", "走廊")); err != nil {
		t.Fatalf("create cam-c: %v", err)
	}
	if err := repo.CreateInstance(ctx, newInstanceFixture("inst-a", "cam-a", "alg-a", 10)); err != nil {
		t.Fatalf("create inst-a: %v", err)
	}
	if err := repo.CreateInstance(ctx, newInstanceFixture("inst-c", "cam-c", "alg-a", 10)); err != nil {
		t.Fatalf("create inst-c: %v", err)
	}
	if _, err := repo.DeleteInstance(ctx, "inst-c"); err != nil {
		t.Fatalf("delete inst-c: %v", err)
	}

	// 全量：按 id 倒序
	items, total, err := repo.ListTaskPage(ctx, &TaskFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("list total=%d len=%d, want 3/3", total, len(items))
	}
	if items[0].CameraID != "cam-c" || items[1].CameraID != "cam-b" || items[2].CameraID != "cam-a" {
		t.Fatalf("list order = %+v", items)
	}

	// 分页
	items, total, err = repo.ListTaskPage(ctx, &TaskFilter{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if total != 3 || len(items) != 1 || items[0].CameraID != "cam-a" {
		t.Fatalf("page2 total=%d len=%d items=%+v, want 3/1/cam-a", total, len(items), items)
	}

	// name 模糊
	items, total, err = repo.ListTaskPage(ctx, &TaskFilter{Name: "门"})
	if err != nil {
		t.Fatalf("list name: %v", err)
	}
	if total != 1 || items[0].CameraID != "cam-a" {
		t.Fatalf("name filter total=%d items=%+v, want 1/cam-a", total, items)
	}

	// camera 精确
	items, total, err = repo.ListTaskPage(ctx, &TaskFilter{CameraID: "cam-b"})
	if err != nil {
		t.Fatalf("list camera: %v", err)
	}
	if total != 1 || items[0].CameraID != "cam-b" {
		t.Fatalf("camera filter total=%d items=%+v, want 1/cam-b", total, items)
	}

	// Configured=true：存在未软删实例的任务
	configured := true
	items, total, err = repo.ListTaskPage(ctx, &TaskFilter{Configured: &configured})
	if err != nil {
		t.Fatalf("list configured: %v", err)
	}
	if total != 1 || items[0].CameraID != "cam-a" {
		t.Fatalf("configured filter total=%d items=%+v, want 1/cam-a", total, items)
	}

	// Configured=false：无未软删实例的任务
	unconfigured := false
	items, total, err = repo.ListTaskPage(ctx, &TaskFilter{Configured: &unconfigured})
	if err != nil {
		t.Fatalf("list unconfigured: %v", err)
	}
	if total != 2 {
		t.Fatalf("unconfigured filter total=%d, want 2", total)
	}
	for _, item := range items {
		if item.CameraID == "cam-a" {
			t.Fatalf("unconfigured filter contains cam-a: %+v", items)
		}
	}
}

func TestTaskRepositoryCountTasksByCameraID(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	if err := repo.CreateTask(ctx, newTaskFixture("cam-cnt", "任务")); err != nil {
		t.Fatalf("create: %v", err)
	}
	count, err := repo.CountTasksByCameraID(ctx, "cam-cnt")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	if _, err := repo.DeleteTaskCascade(ctx, "cam-cnt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	count, err = repo.CountTasksByCameraID(ctx, "cam-cnt")
	if err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("count after delete = %d, want 0", count)
	}
}

func TestTaskRepositoryLoadDesiredSnapshot(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	// 摄像头：cam-a 正常、cam-b 正常、cam-c 将被软删
	camA := newCameraTaskFixture("cam-a", "rtsp://a/live")
	camB := newCameraTaskFixture("cam-b", "rtsp://b/live")
	camC := newCameraTaskFixture("cam-c", "rtsp://c/live")
	for _, cam := range []*model.Camera{camA, camB, camC} {
		if err := db.Create(cam).Error; err != nil {
			t.Fatalf("create camera %s: %v", cam.CameraID, err)
		}
	}

	// 算法：alg-a 激活 1.0.0、alg-b 未激活、alg-c 将被软删
	algA := newAlgorithmTaskFixture("alg-a", "1.0.0")
	algB := newAlgorithmTaskFixture("alg-b", "")
	algC := newAlgorithmTaskFixture("alg-c", "2.0.0")
	for _, alg := range []*model.Algorithm{algA, algB, algC} {
		if err := db.Create(alg).Error; err != nil {
			t.Fatalf("create algorithm %s: %v", alg.AlgorithmID, err)
		}
	}

	// 任务：tA 启用、tB 停用、tC 启用但摄像头软删
	taskA := newTaskFixture("cam-a", "启用任务")
	taskA.DesiredEnabled = true
	taskB := newTaskFixture("cam-b", "停用任务")
	taskC := newTaskFixture("cam-c", "孤儿任务")
	taskC.DesiredEnabled = true
	for _, task := range []*model.AnalysisTask{taskA, taskB, taskC} {
		if err := repo.CreateTask(ctx, task); err != nil {
			t.Fatalf("create task %s: %v", task.CameraID, err)
		}
	}

	// 实例：
	//   iA1：tA/alg-a 启用 → 进快照
	//   iA2：tA/alg-a 停用 → 不进
	//   iA3：tA/alg-b 启用但算法未激活 → 跳过
	//   iB1：tB/alg-a 启用但任务停用 → 不进
	iA1 := newInstanceFixture("i-a-1", "cam-a", "alg-a", 10)
	iA1.Enabled = true
	iA1.ParamsJSON = []byte(`{"conf":0.6}`)
	iA1.RulesJSON = []byte(`[{"role":1,"lineDirection":0,"points":[{"x":0.1,"y":0.1},{"x":0.9,"y":0.1},{"x":0.5,"y":0.9}]}]`)
	iA2 := newInstanceFixture("i-a-2", "cam-a", "alg-a", 10)
	iA3 := newInstanceFixture("i-a-3", "cam-a", "alg-b", 10)
	iA3.Enabled = true
	iB1 := newInstanceFixture("i-b-1", "cam-b", "alg-a", 10)
	iB1.Enabled = true
	for _, inst := range []*model.AlgorithmInstance{iA1, iA2, iA3, iB1} {
		if err := repo.CreateInstance(ctx, inst); err != nil {
			t.Fatalf("create instance %s: %v", inst.InstanceID, err)
		}
	}

	// 软删摄像头 cam-c（任务 tC 成为孤儿，JOIN 过滤）
	if err := db.Delete(&model.Camera{}, camC.ID).Error; err != nil {
		t.Fatalf("soft delete camera cam-c: %v", err)
	}
	// 软删算法 alg-c（无实例引用，但 active versions 不应包含）
	if err := db.Delete(&model.Algorithm{}, algC.ID).Error; err != nil {
		t.Fatalf("soft delete algorithm alg-c: %v", err)
	}

	state, err := repo.LoadDesiredSnapshot(ctx)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	// tasks：仅 cam-a（cam-b 停用、cam-c 摄像头已软删）
	if len(state.Tasks) != 1 {
		t.Fatalf("tasks = %+v, want 1 (cam-a only)", state.Tasks)
	}
	if state.Tasks[0].CameraId != "cam-a" || state.Tasks[0].RtspUrl != "rtsp://a/live" || !state.Tasks[0].Enabled {
		t.Fatalf("task[0] = %+v", state.Tasks[0])
	}

	// instances：仅 i-a-1
	if len(state.Instances) != 1 {
		t.Fatalf("instances = %+v, want 1 (i-a-1 only)", state.Instances)
	}
	inst := state.Instances[0]
	if inst.InstanceId != "i-a-1" || inst.CameraId != "cam-a" || inst.AlgorithmId != "alg-a" {
		t.Fatalf("instance[0] = %+v", inst)
	}
	if inst.AlgorithmVersion != "1.0.0" {
		t.Fatalf("instance[0].algorithm_version = %q, want 1.0.0（从 algorithms.active_version 填充）", inst.AlgorithmVersion)
	}
	if inst.AnalysisFps != 10 || !inst.Enabled || inst.ParamsJson != `{"conf":0.6}` {
		t.Fatalf("instance[0] = %+v", inst)
	}
	// 规则转换：ROI 三点、方向 0
	if len(inst.Rules) != 1 {
		t.Fatalf("rules = %+v, want 1 rule", inst.Rules)
	}
	rule := inst.Rules[0]
	if rule.Role != aivisionv1.DetectionRuleRole_DETECTION_RULE_ROLE_ROI || rule.LineDirection != aivisionv1.DetectionLineDirection_DETECTION_LINE_DIRECTION_BOTH {
		t.Fatalf("rule = %+v", rule)
	}
	if len(rule.Points) != 3 || rule.Points[0].X != 0.1 || rule.Points[1].Y != 0.1 || rule.Points[2].Y != 0.9 {
		t.Fatalf("rule points = %+v", rule.Points)
	}

	// active_package_versions：仅 alg-a（alg-b 未激活、alg-c 已软删）
	if len(state.ActivePackageVersions) != 1 {
		t.Fatalf("active versions = %+v, want 1 (alg-a only)", state.ActivePackageVersions)
	}
	if state.ActivePackageVersions[0].AlgorithmId != "alg-a" || state.ActivePackageVersions[0].Version != "1.0.0" {
		t.Fatalf("active version[0] = %+v", state.ActivePackageVersions[0])
	}
}

func TestTaskRepositoryListInstancesByCameraIDs(t *testing.T) {
	db := newTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	// 空入参安全
	items, err := repo.ListInstancesByCameraIDs(ctx, nil)
	if err != nil || len(items) != 0 {
		t.Fatalf("empty cameraIDs got len=%d err=%v, want 0/nil", len(items), err)
	}

	instA1 := newInstanceFixture("inst-a-1", "cam-1", "alg-a", 10)
	instA2 := newInstanceFixture("inst-a-2", "cam-1", "alg-b", 15)
	instB1 := newInstanceFixture("inst-b-1", "cam-2", "alg-a", 20)
	instC1 := newInstanceFixture("inst-c-1", "cam-3", "alg-c", 25)

	for _, inst := range []*model.AlgorithmInstance{instA1, instA2, instB1, instC1} {
		if err := repo.CreateInstance(ctx, inst); err != nil {
			t.Fatalf("create instance: %v", err)
		}
	}

	// 查 cam-1 和 cam-2
	items, err = repo.ListInstancesByCameraIDs(ctx, []string{"cam-1", "cam-2"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got len=%d, want 3", len(items))
	}

	// 软删 inst-a-2 后不再返回
	if _, err := repo.DeleteInstance(ctx, "inst-a-2"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	items, err = repo.ListInstancesByCameraIDs(ctx, []string{"cam-1", "cam-2"})
	if err != nil || len(items) != 2 {
		t.Fatalf("after soft delete got len=%d err=%v, want 2/nil", len(items), err)
	}
}
