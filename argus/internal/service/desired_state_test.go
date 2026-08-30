package service

import (
	"context"
	"fmt"
	"testing"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"argus/app/internal/model"
	"argus/app/internal/repository"
)

// newDesiredStateTestDB 建 sqlite 内存库并迁移快照组装所需全部表，
// 初始化 desired_state_revision 单行（对齐 000019 迁移与 repository 单测基建）。
func newDesiredStateTestDB(t *testing.T) *gorm.DB {
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
	if err := db.Create(&model.DesiredStateRevision{ID: 1, Revision: 0}).Error; err != nil {
		t.Fatalf("seed desired_state_revision: %v", err)
	}
	return db
}

// TestDesiredStateAdapterFullSnapshot 返回完整快照：revision 由计数器填充、
// device_id 用占位常量、任务/实例按启用过滤、版本从 active_version 动态填充。
func TestDesiredStateAdapterFullSnapshot(t *testing.T) {
	db := newDesiredStateTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	ctx := context.Background()

	// 数据：cam-a 任务启用（实例 i1 启用）；cam-b 任务停用（实例 i2 即使启用也不进快照）；
	// alg-a 激活 v1；alg-b 无激活版本（其实例被跳过）。
	mustCreate := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("create fixture: %v", err)
		}
	}
	mustCreate(&model.Camera{CameraID: "cam-a", Protocol: model.CameraProtocolRTSP, RtspURL: "rtsp://a/live", TransportPolicy: model.CameraTransportAuto, ConfigHash: "h"})
	mustCreate(&model.Camera{CameraID: "cam-b", Protocol: model.CameraProtocolRTSP, RtspURL: "rtsp://b/live", TransportPolicy: model.CameraTransportAuto, ConfigHash: "h"})
	mustCreate(&model.Algorithm{AlgorithmID: "alg-a", ActiveVersion: "v1"})
	mustCreate(&model.Algorithm{AlgorithmID: "alg-b", ActiveVersion: ""})
	mustCreate(&model.AnalysisTask{CameraID: "cam-a", Name: "task-a", DesiredEnabled: true, ActualStatus: model.TaskStatusRunning})
	mustCreate(&model.AnalysisTask{CameraID: "cam-b", Name: "task-b", DesiredEnabled: false, ActualStatus: model.TaskStatusStopped})
	mustCreate(&model.AlgorithmInstance{InstanceID: "i1", CameraID: "cam-a", AlgorithmID: "alg-a", AnalysisFPS: 15, ParamsJSON: []byte(`{}`), RulesJSON: []byte(`[]`), Enabled: true})
	mustCreate(&model.AlgorithmInstance{InstanceID: "i2", CameraID: "cam-b", AlgorithmID: "alg-a", AnalysisFPS: 5, ParamsJSON: []byte(`{}`), RulesJSON: []byte(`[]`), Enabled: true})
	mustCreate(&model.AlgorithmInstance{InstanceID: "i3", CameraID: "cam-a", AlgorithmID: "alg-b", AnalysisFPS: 5, ParamsJSON: []byte(`{}`), RulesJSON: []byte(`[]`), Enabled: true})

	// 业务写入一次，revision 应为 1。
	if err := taskRepo.InTx(ctx, func(ctx context.Context, r repository.TaskRepository) error {
		if _, err := r.BumpRevision(ctx); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("bump revision: %v", err)
	}

	adapter := NewDesiredStateAdapter(taskRepo, zap.NewNop())
	// 传入当前 revision=1（未变化）也应返回完整快照（design §3.3）。
	state, err := adapter.DesiredState(ctx, 1)
	if err != nil {
		t.Fatalf("DesiredState: %v", err)
	}

	if state.GetRevision() != 1 {
		t.Fatalf("revision = %d, want 1", state.GetRevision())
	}
	if state.GetDeviceId() != deviceIDPlaceholder {
		t.Fatalf("deviceId = %q, want %q", state.GetDeviceId(), deviceIDPlaceholder)
	}
	if len(state.GetTasks()) != 1 || state.GetTasks()[0].GetCameraId() != "cam-a" {
		t.Fatalf("tasks = %+v, want only enabled cam-a", state.GetTasks())
	}
	if got := state.GetTasks()[0].GetRtspUrl(); got != "rtsp://a/live" {
		t.Fatalf("rtspUrl = %q", got)
	}
	if len(state.GetInstances()) != 1 || state.GetInstances()[0].GetInstanceId() != "i1" {
		t.Fatalf("instances = %+v, want only i1", state.GetInstances())
	}
	if got := state.GetInstances()[0].GetAlgorithmVersion(); got != "v1" {
		t.Fatalf("algorithmVersion = %q, want v1 (from active_version)", got)
	}
	versions := state.GetActivePackageVersions()
	if len(versions) != 1 || versions[0].GetAlgorithmId() != "alg-a" || versions[0].GetVersion() != "v1" {
		t.Fatalf("activePackageVersions = %+v, want only alg-a@v1", versions)
	}
}

// TestDesiredStateAdapterFailClosed revision 单行缺失时返回错误（fail closed），
// 不让 Engine 拿到 revision=0 的「配置被清空」快照。
func TestDesiredStateAdapterFailClosed(t *testing.T) {
	db := newDesiredStateTestDB(t)
	// 删除单行计数器模拟迁移未初始化/数据被破坏。
	if err := db.Delete(&model.DesiredStateRevision{}, "id = ?", 1).Error; err != nil {
		t.Fatalf("delete revision row: %v", err)
	}
	adapter := NewDesiredStateAdapter(repository.NewTaskRepository(db), zap.NewNop())
	if _, err := adapter.DesiredState(context.Background(), 0); err == nil {
		t.Fatal("DesiredState unexpectedly succeeded with missing revision row")
	}
}
