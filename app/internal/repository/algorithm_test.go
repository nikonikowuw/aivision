package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

// newAlgorithmRepoTestDB 建 sqlite 内存库并迁移算法模块相关表 + revision 单行。
func newAlgorithmRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{TranslateError: true},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Algorithm{},
		&model.AlgorithmVersion{},
		&model.AlgorithmInstance{},
		&model.AnalysisTask{},
		&model.DesiredStateRevision{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	if err := db.Create(&model.DesiredStateRevision{ID: 1, Revision: 0}).Error; err != nil {
		t.Fatalf("seed desired_state_revision: %v", err)
	}
	return db
}

// TestAlgorithmRepositoryCountActiveInstances 只计入当前 DesiredState 仍引用算法的实例；
// 已软删、停用或脱离任务的记录不应阻塞算法包卸载。
func TestAlgorithmRepositoryCountActiveInstances(t *testing.T) {
	db := newAlgorithmRepoTestDB(t)
	repo := NewAlgorithmRepository(db)
	ctx := context.Background()

	if err := db.Create(&model.AlgorithmInstance{
		InstanceID:  "i1",
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		Enabled:     true,
		ParamsJSON:  []byte(`{}`),
		RulesJSON:   []byte(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	softDeleted := &model.AlgorithmInstance{
		InstanceID:  "i2",
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		Enabled:     true,
		ParamsJSON:  []byte(`{}`),
		RulesJSON:   []byte(`[]`),
	}
	if err := db.Create(softDeleted).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	if err := db.Delete(softDeleted).Error; err != nil {
		t.Fatalf("soft delete instance: %v", err)
	}
	if err := db.Create(&model.AnalysisTask{
		CameraID:       "cam-a",
		Name:           "active-task",
		DesiredEnabled: true,
	}).Error; err != nil {
		t.Fatalf("seed active task: %v", err)
	}
	if err := db.Create(&model.AnalysisTask{
		CameraID:       "cam-disabled-task",
		Name:           "disabled-task",
		DesiredEnabled: false,
	}).Error; err != nil {
		t.Fatalf("seed disabled task: %v", err)
	}
	deletedTask := &model.AnalysisTask{
		CameraID:       "cam-deleted-task",
		Name:           "deleted-task",
		DesiredEnabled: true,
	}
	if err := db.Create(deletedTask).Error; err != nil {
		t.Fatalf("seed deleted task: %v", err)
	}
	if err := db.Delete(deletedTask).Error; err != nil {
		t.Fatalf("soft delete task: %v", err)
	}
	if err := db.Create(&model.AlgorithmInstance{
		InstanceID:  "i6",
		CameraID:    "cam-deleted-task",
		AlgorithmID: "yolov8n",
		Enabled:     true,
		ParamsJSON:  []byte(`{}`),
		RulesJSON:   []byte(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed instance for deleted task: %v", err)
	}

	if err := db.Create(&model.AlgorithmInstance{
		InstanceID:  "i4",
		CameraID:    "cam-disabled-task",
		AlgorithmID: "yolov8n",
		Enabled:     false,
		ParamsJSON:  []byte(`{}`),
		RulesJSON:   []byte(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed disabled instance: %v", err)
	}
	if err := db.Create(&model.AlgorithmInstance{
		InstanceID:  "i5",
		CameraID:    "cam-orphan",
		AlgorithmID: "yolov8n",
		Enabled:     true,
		ParamsJSON:  []byte(`{}`),
		RulesJSON:   []byte(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed orphan instance: %v", err)
	}

	// version 参数任意值结果一致（D11：实例不固定版本）。
	if err := db.Create(&model.AlgorithmInstance{
		InstanceID:  "i3",
		CameraID:    "cam-a",
		AlgorithmID: "yolov5s",
		Enabled:     true,
		ParamsJSON:  []byte(`{}`),
		RulesJSON:   []byte(`[]`),
	}).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	// 仅 i1 挂在启用中的任务上；停用、孤儿、软删任务和软删实例不应计入占用。
	for _, ver := range []string{"1.0.0", "9.9.9"} {
		count, err := repo.CountActiveInstances(ctx, "yolov8n", ver)
		if err != nil {
			t.Fatalf("count(%s): %v", ver, err)
		}
		if count != 1 {
			t.Fatalf("count(%s) = %d, want 1", ver, count)
		}
	}
}

// TestAlgorithmRepositoryInTxRollbackOnBumpFailure 验证 InTx 原子性：
// revision bump 失败时业务写全部回滚。
func TestAlgorithmRepositoryInTxRollbackOnBumpFailure(t *testing.T) {
	db := newAlgorithmRepoTestDB(t)
	repo := NewAlgorithmRepository(db)
	ctx := context.Background()

	mustCreate := func(v any) {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("create fixture: %v", err)
		}
	}
	mustCreate(&model.Algorithm{AlgorithmID: "yolov8n", ActiveVersion: "1.0.0"})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "yolov8n",
		Version:      "1.0.0",
		IsActive:     true,
		FPSTiers:     []byte(`[]`),
		ConfigSchema: []byte(`{}`),
		ManifestRaw:  []byte(`{}`),
	})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "yolov8n",
		Version:      "1.1.0",
		IsActive:     false,
		FPSTiers:     []byte(`[]`),
		ConfigSchema: []byte(`{}`),
		ManifestRaw:  []byte(`{}`),
	})

	// 删除 revision 单行使 bump 失败（触发 ErrRevisionMissing）
	if err := db.Delete(&model.DesiredStateRevision{}, "id = ?", 1).Error; err != nil {
		t.Fatalf("delete revision row: %v", err)
	}

	err := repo.InTx(ctx, func(ctx context.Context, r AlgorithmRepository) error {
		if err := r.ActivateVersion(ctx, "yolov8n", "1.1.0"); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	})
	if !errors.Is(err, ErrRevisionMissing) {
		t.Fatalf("err = %v, want ErrRevisionMissing", err)
	}

	// 业务写已随事务回滚
	algo, err := repo.GetAlgorithmByID(ctx, "yolov8n")
	if err != nil {
		t.Fatalf("get algorithm: %v", err)
	}
	if algo.ActiveVersion != "1.0.0" {
		t.Fatalf("active_version = %q, want 1.0.0 (rolled back)", algo.ActiveVersion)
	}
	ver, err := repo.GetVersion(ctx, "yolov8n", "1.1.0")
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	if ver.IsActive {
		t.Fatal("v1.1.0 is_active = true, want false (rolled back)")
	}
}

// TestAlgorithmRepositoryInTxCommit 正常提交：版本激活与 revision bump 原子提交。
func TestAlgorithmRepositoryInTxCommit(t *testing.T) {
	db := newAlgorithmRepoTestDB(t)
	repo := NewAlgorithmRepository(db)
	ctx := context.Background()

	mustCreate := func(v any) {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("create fixture: %v", err)
		}
	}
	mustCreate(&model.Algorithm{AlgorithmID: "yolov8n", ActiveVersion: "1.0.0"})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "yolov8n",
		Version:      "1.0.0",
		IsActive:     true,
		FPSTiers:     []byte(`[]`),
		ConfigSchema: []byte(`{}`),
		ManifestRaw:  []byte(`{}`),
	})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "yolov8n",
		Version:      "1.1.0",
		IsActive:     false,
		FPSTiers:     []byte(`[]`),
		ConfigSchema: []byte(`{}`),
		ManifestRaw:  []byte(`{}`),
	})

	var rev uint64
	err := repo.InTx(ctx, func(ctx context.Context, r AlgorithmRepository) error {
		if err := r.ActivateVersion(ctx, "yolov8n", "1.1.0"); err != nil {
			return err
		}
		var bumpErr error
		rev, bumpErr = r.BumpRevision(ctx)
		return bumpErr
	})
	if err != nil {
		t.Fatalf("InTx commit: %v", err)
	}
	if rev != 1 {
		t.Fatalf("revision = %d, want 1", rev)
	}

	algo, err := repo.GetAlgorithmByID(ctx, "yolov8n")
	if err != nil {
		t.Fatalf("get algorithm: %v", err)
	}
	if algo.ActiveVersion != "1.1.0" {
		t.Fatalf("active_version = %q, want 1.1.0", algo.ActiveVersion)
	}
	ver, err := repo.GetVersion(ctx, "yolov8n", "1.1.0")
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	if !ver.IsActive {
		t.Fatal("v1.1.0 is_active = false, want true")
	}
}

// TestAlgorithmRepositoryRestoreLastVersion 验证卸载最后一个版本后，补偿操作能按
// 原主键复活算法和版本，并恢复卸载前的激活关系。
func TestAlgorithmRepositoryRestoreLastVersion(t *testing.T) {
	db := newAlgorithmRepoTestDB(t)
	repo := NewAlgorithmRepository(db)
	ctx := context.Background()

	algo := &model.Algorithm{
		AlgorithmID:   "yolov8n",
		Name:          "YOLOv8n",
		AlgorithmType: "object_detection",
		ActiveVersion: "1.0.0",
	}
	version := &model.AlgorithmVersion{
		AlgorithmID:  "yolov8n",
		Version:      "1.0.0",
		IsActive:     true,
		FPSTiers:     []byte(`[{"fps":25,"units":220}]`),
		ConfigSchema: []byte(`{"type":"object"}`),
		ManifestRaw:  []byte(`{"algorithm_id":"yolov8n"}`),
	}
	if err := db.Create(algo).Error; err != nil {
		t.Fatalf("create algorithm: %v", err)
	}
	if err := db.Create(version).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}
	algoSnapshot := *algo
	versionSnapshot := *version

	if err := repo.DeleteVersion(ctx, "yolov8n", "1.0.0"); err != nil {
		t.Fatalf("delete version: %v", err)
	}
	if _, err := repo.GetAlgorithmByID(ctx, "yolov8n"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("algorithm after delete error = %v, want ErrNotFound", err)
	}

	if err := repo.RestoreVersionState(ctx, &algoSnapshot, &versionSnapshot); err != nil {
		t.Fatalf("restore version state: %v", err)
	}
	restoredAlgo, err := repo.GetAlgorithmByID(ctx, "yolov8n")
	if err != nil {
		t.Fatalf("get restored algorithm: %v", err)
	}
	if restoredAlgo.ID != algoSnapshot.ID || restoredAlgo.ActiveVersion != "1.0.0" {
		t.Fatalf("restored algorithm = %+v, want id %d active 1.0.0", restoredAlgo, algoSnapshot.ID)
	}
	restoredVersion, err := repo.GetVersion(ctx, "yolov8n", "1.0.0")
	if err != nil {
		t.Fatalf("get restored version: %v", err)
	}
	if restoredVersion.ID != versionSnapshot.ID || !restoredVersion.IsActive {
		t.Fatalf("restored version = %+v, want id %d active", restoredVersion, versionSnapshot.ID)
	}
}
