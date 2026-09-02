package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"argus/app/internal/model"
	"argus/app/internal/pkg/engineipc"
	"argus/app/internal/pkg/errno"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
)

// fakeAlgorithmRepo 模拟仓储。
type fakeAlgorithmRepo struct {
	algos    map[string]*model.Algorithm
	versions map[string]*model.AlgorithmVersion
	bumps    uint64
}

func newFakeAlgorithmRepo() *fakeAlgorithmRepo {
	return &fakeAlgorithmRepo{
		algos:    make(map[string]*model.Algorithm),
		versions: make(map[string]*model.AlgorithmVersion),
	}
}

func (r *fakeAlgorithmRepo) UpsertAlgorithm(ctx context.Context, algo *model.Algorithm) error {
	r.algos[algo.AlgorithmID] = algo
	return nil
}

func (r *fakeAlgorithmRepo) GetAlgorithmByID(ctx context.Context, algorithmID string) (*model.Algorithm, error) {
	if a, ok := r.algos[algorithmID]; ok {
		return a, nil
	}
	return nil, repository.ErrNotFound
}

func (r *fakeAlgorithmRepo) ListAlgorithms(ctx context.Context, filter *repository.AlgorithmFilter) ([]model.Algorithm, int64, error) {
	var list []model.Algorithm
	for _, a := range r.algos {
		list = append(list, *a)
	}
	return list, int64(len(list)), nil
}

func (r *fakeAlgorithmRepo) DeleteAlgorithm(ctx context.Context, algorithmID string) error {
	delete(r.algos, algorithmID)
	return nil
}

func (r *fakeAlgorithmRepo) UpsertVersion(ctx context.Context, version *model.AlgorithmVersion) error {
	key := version.AlgorithmID + ":" + version.Version
	r.versions[key] = version
	return nil
}

func (r *fakeAlgorithmRepo) GetVersion(ctx context.Context, algorithmID, version string) (*model.AlgorithmVersion, error) {
	key := algorithmID + ":" + version
	if v, ok := r.versions[key]; ok {
		return v, nil
	}
	return nil, repository.ErrNotFound
}

func (r *fakeAlgorithmRepo) ListVersions(ctx context.Context, algorithmID string) ([]model.AlgorithmVersion, error) {
	var list []model.AlgorithmVersion
	for _, v := range r.versions {
		if v.AlgorithmID == algorithmID {
			list = append(list, *v)
		}
	}
	return list, nil
}

func (r *fakeAlgorithmRepo) ActivateVersion(ctx context.Context, algorithmID, version string) error {
	for k, v := range r.versions {
		if v.AlgorithmID == algorithmID {
			r.versions[k].IsActive = (v.Version == version)
		}
	}
	if a, ok := r.algos[algorithmID]; ok {
		a.ActiveVersion = version
	}
	return nil
}

func (r *fakeAlgorithmRepo) DeleteVersion(ctx context.Context, algorithmID, version string) error {
	key := algorithmID + ":" + version
	delete(r.versions, key)
	return nil
}

func (r *fakeAlgorithmRepo) RestoreVersionState(ctx context.Context, algo *model.Algorithm, version *model.AlgorithmVersion) error {
	r.algos[algo.AlgorithmID] = algo
	r.versions[version.AlgorithmID+":"+version.Version] = version
	for _, item := range r.versions {
		if item.AlgorithmID == algo.AlgorithmID {
			item.IsActive = item.Version == algo.ActiveVersion
		}
	}
	return nil
}

func (r *fakeAlgorithmRepo) CountActiveInstances(ctx context.Context, algorithmID, version string) (int64, error) {
	return 0, nil
}

func (r *fakeAlgorithmRepo) BumpRevision(ctx context.Context) (uint64, error) {
	r.bumps++
	return r.bumps, nil
}

func (r *fakeAlgorithmRepo) InTx(ctx context.Context, fn func(ctx context.Context, r repository.AlgorithmRepository) error) error {
	return fn(ctx, r)
}

// fakeEngineClient 模拟 EngineClient。
type fakeEngineClient struct {
	installResp      *argusv1.InstallPackageResponse
	installErr       error
	rollbackResp     *argusv1.RollbackPackageResponse
	rollbackErr      error
	rollbackVersions []string
	uninstallResp    *argusv1.UninstallPackageResponse
	uninstallErr     error
}

func (c *fakeEngineClient) InstallPackage(ctx context.Context, req *argusv1.InstallPackageRequest, opts ...grpc.CallOption) (*argusv1.InstallPackageResponse, error) {
	if c.installErr != nil {
		return nil, c.installErr
	}
	if c.installResp != nil {
		return c.installResp, nil
	}
	return &argusv1.InstallPackageResponse{Code: "", AlgorithmId: "yolov8n", Version: "1.0.0"}, nil
}

func (c *fakeEngineClient) UpgradePackage(ctx context.Context, req *argusv1.UpgradePackageRequest, opts ...grpc.CallOption) (*argusv1.UpgradePackageResponse, error) {
	return &argusv1.UpgradePackageResponse{Code: ""}, nil
}

func (c *fakeEngineClient) RollbackPackage(ctx context.Context, req *argusv1.RollbackPackageRequest, opts ...grpc.CallOption) (*argusv1.RollbackPackageResponse, error) {
	c.rollbackVersions = append(c.rollbackVersions, req.GetTargetVersion())
	if c.rollbackErr != nil {
		return nil, c.rollbackErr
	}
	if c.rollbackResp != nil {
		return c.rollbackResp, nil
	}
	return &argusv1.RollbackPackageResponse{Code: ""}, nil
}

func (c *fakeEngineClient) UninstallPackage(ctx context.Context, req *argusv1.UninstallPackageRequest, opts ...grpc.CallOption) (*argusv1.UninstallPackageResponse, error) {
	if c.uninstallErr != nil {
		return nil, c.uninstallErr
	}
	if c.uninstallResp != nil {
		return c.uninstallResp, nil
	}
	return &argusv1.UninstallPackageResponse{Code: ""}, nil
}

// TestAlgorithmServiceUploadInstallEngineErrorMapping Engine 返回的 validator 结果是业务失败，
// 不能因为 EngineClient 将响应 code 包装成 error 就误报为 CodeEngineUnavailable。
func TestAlgorithmServiceUploadInstallEngineErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		engine   error
		wantCode int
	}{
		{
			name:     "validator result invalid",
			engine:   &engineipc.RemoteError{Code: "VALIDATOR_RESULT_INVALID", ErrorMessage: "validator stdout is invalid"},
			wantCode: errno.CodeAlgoInstallFailed,
		},
		{
			name:     "package validation failed",
			engine:   &engineipc.RemoteError{Code: "PACKAGE_VALIDATION_FAILED", ErrorMessage: "self-test failed"},
			wantCode: errno.CodeAlgoInstallFailed,
		},
		{
			name:     "transport failure",
			engine:   errors.New("engine socket unavailable"),
			wantCode: errno.CodeEngineUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeAlgorithmRepo()
			svc := &algorithmService{
				repo:         repo,
				engineClient: &fakeEngineClient{installErr: tt.engine},
				logger:       zap.NewNop(),
				tmpDir:       t.TempDir(),
			}

			_, err := svc.UploadAndInstall(context.Background(), validAlgorithmArchive(t))
			if !errno.Is(err, tt.wantCode) {
				t.Fatalf("error = %v, want code %d", err, tt.wantCode)
			}
		})
	}
}

func validAlgorithmArchive(t *testing.T) *bytes.Reader {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	manifest, err := writer.Create("manifest.json")
	if err != nil {
		t.Fatalf("create manifest: %v", err)
	}
	if _, err := manifest.Write([]byte(`{"algorithm_id":"yolov8n","version":"1.0.0","platform_id":"mock"}`)); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return bytes.NewReader(archive.Bytes())
}

// TestAlgorithmServiceActivateEngineErrorMapping Engine 业务错误不能被误报为服务不可用，
// 而真正的 IPC 传输失败仍应返回 CodeEngineUnavailable。
func TestAlgorithmServiceActivateEngineErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		engine   error
		wantCode int
	}{
		{
			name:     "remote package not found",
			engine:   &engineipc.RemoteError{Code: "PACKAGE_NOT_FOUND"},
			wantCode: errno.CodeNotFound,
		},
		{
			name:     "remote restart failed",
			engine:   &engineipc.RemoteError{Code: "PACKAGE_RESTART_FAILED"},
			wantCode: errno.CodeAlgoInstallFailed,
		},
		{
			name:     "transport failure",
			engine:   errors.New("engine socket unavailable"),
			wantCode: errno.CodeEngineUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeAlgorithmRepo()
			repo.algos["yolov8n"] = &model.Algorithm{AlgorithmID: "yolov8n", ActiveVersion: "1.0.0"}
			repo.versions["yolov8n:1.0.0"] = &model.AlgorithmVersion{
				AlgorithmID: "yolov8n",
				Version:     "1.0.0",
				IsActive:    true,
			}
			repo.versions["yolov8n:1.1.0"] = &model.AlgorithmVersion{
				AlgorithmID: "yolov8n",
				Version:     "1.1.0",
			}
			engine := &fakeEngineClient{rollbackErr: tt.engine}
			svc := &algorithmService{
				repo:         repo,
				engineClient: engine,
				logger:       zap.NewNop(),
			}

			err := svc.ActivateVersion(context.Background(), "yolov8n", "1.1.0")
			if !errno.Is(err, tt.wantCode) {
				t.Fatalf("error = %v, want code %d", err, tt.wantCode)
			}
			if got := repo.algos["yolov8n"].ActiveVersion; got != "1.0.0" {
				t.Fatalf("active version after engine failure = %q, want restored 1.0.0", got)
			}
		})
	}
}

func TestAlgorithmServiceActivateRestoresEmptyPreviousVersion(t *testing.T) {
	repo := newFakeAlgorithmRepo()
	repo.algos["yolov8n"] = &model.Algorithm{AlgorithmID: "yolov8n"}
	repo.versions["yolov8n:1.0.0"] = &model.AlgorithmVersion{
		AlgorithmID: "yolov8n",
		Version:     "1.0.0",
	}
	svc := &algorithmService{
		repo:         repo,
		engineClient: &fakeEngineClient{rollbackErr: errors.New("engine socket unavailable")},
		logger:       zap.NewNop(),
	}

	err := svc.ActivateVersion(context.Background(), "yolov8n", "1.0.0")
	if !errno.Is(err, errno.CodeEngineUnavailable) {
		t.Fatalf("error = %v, want CodeEngineUnavailable", err)
	}
	if got := repo.algos["yolov8n"].ActiveVersion; got != "" {
		t.Fatalf("active version after compensation = %q, want empty", got)
	}
	if repo.versions["yolov8n:1.0.0"].IsActive {
		t.Fatal("target version remains active after compensation")
	}
}

func TestAlgorithmService_ActivateAndUninstall(t *testing.T) {
	repo := newFakeAlgorithmRepo()
	engine := &fakeEngineClient{}
	svc := &algorithmService{
		repo:         repo,
		engineClient: engine,
		logger:       zap.NewNop(),
		tmpDir:       t.TempDir(),
	}

	ctx := context.Background()

	// 初始状态
	repo.algos["yolov8n"] = &model.Algorithm{AlgorithmID: "yolov8n", ActiveVersion: "1.0.0"}
	repo.versions["yolov8n:1.0.0"] = &model.AlgorithmVersion{AlgorithmID: "yolov8n", Version: "1.0.0", IsActive: true}
	repo.versions["yolov8n:1.1.0"] = &model.AlgorithmVersion{AlgorithmID: "yolov8n", Version: "1.1.0", IsActive: false}

	// 1. 激活 1.1.0
	err := svc.ActivateVersion(ctx, "yolov8n", "1.1.0")
	if err != nil {
		t.Fatalf("activate failed: %v", err)
	}
	if repo.algos["yolov8n"].ActiveVersion != "1.1.0" {
		t.Errorf("expected active_version 1.1.0, got %s", repo.algos["yolov8n"].ActiveVersion)
	}

	// 2. 卸载 1.0.0
	err = svc.UninstallVersion(ctx, "yolov8n", "1.0.0")
	if err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	if _, err := repo.GetVersion(ctx, "yolov8n", "1.0.0"); err != repository.ErrNotFound {
		t.Errorf("version 1.0.0 should be deleted")
	}

	// 3. 卸载不存在的版本
	err = svc.UninstallVersion(ctx, "yolov8n", "9.9.9")
	if !errno.Is(err, errno.CodeNotFound) {
		t.Errorf("expected CodeNotFound on non-existent version, got %v", err)
	}
}

// TestAlgorithmServiceUninstallEngineErrorMapping Engine 业务错误不能被误报为服务不可用，
// 而真正的 IPC 传输失败仍应返回 CodeEngineUnavailable。
func TestAlgorithmServiceUninstallEngineErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		engine   error
		wantCode int
	}{
		{
			name:     "remote package in use",
			engine:   &engineipc.RemoteError{Code: "PACKAGE_IN_USE"},
			wantCode: errno.CodeAlgoInUse,
		},
		{
			name:     "remote package not found",
			engine:   &engineipc.RemoteError{Code: "PACKAGE_NOT_FOUND"},
			wantCode: errno.CodeNotFound,
		},
		{
			name:     "remote invalid argument",
			engine:   &engineipc.RemoteError{Code: "INVALID_ARG"},
			wantCode: errno.CodeInvalidParam,
		},
		{
			name:     "transport failure",
			engine:   errors.New("engine socket unavailable"),
			wantCode: errno.CodeEngineUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeAlgorithmRepo()
			repo.algos["yolov8n"] = &model.Algorithm{AlgorithmID: "yolov8n"}
			repo.versions["yolov8n:1.0.0"] = &model.AlgorithmVersion{
				AlgorithmID: "yolov8n",
				Version:     "1.0.0",
			}
			svc := &algorithmService{
				repo:         repo,
				engineClient: &fakeEngineClient{uninstallErr: tt.engine},
				logger:       zap.NewNop(),
			}

			err := svc.UninstallVersion(context.Background(), "yolov8n", "1.0.0")
			if !errno.Is(err, tt.wantCode) {
				t.Fatalf("error = %v, want code %d", err, tt.wantCode)
			}
			var remote *engineipc.RemoteError
			_, versionRestored := repo.versions["yolov8n:1.0.0"]
			if errors.As(tt.engine, &remote) && !versionRestored {
				t.Fatal("version metadata was not restored after engine business rejection")
			}
			if !errors.As(tt.engine, &remote) && versionRestored {
				t.Fatal("version metadata restored after ambiguous transport failure")
			}
		})
	}
}

// newAlgorithmServiceSQLiteEnv 创建包含算法、任务快照与 revision 依赖的 sqlite 内存环境。
func newAlgorithmServiceSQLiteEnv(t *testing.T) (*algorithmService, *fakeEngineClient, *gorm.DB, repository.TaskRepository) {
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
		&model.Camera{},
		&model.AnalysisTask{},
		&model.DesiredStateRevision{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	if err := db.Create(&model.DesiredStateRevision{ID: 1, Revision: 0}).Error; err != nil {
		t.Fatalf("seed desired_state_revision: %v", err)
	}
	engine := &fakeEngineClient{}
	svc := &algorithmService{
		repo:         repository.NewAlgorithmRepository(db),
		engineClient: engine,
		logger:       zap.NewNop(),
		tmpDir:       t.TempDir(),
	}
	return svc, engine, db, repository.NewTaskRepository(db)
}

// TestAlgorithmService_UninstallBuiltinProtected 系统内置算法版本受保护，禁止卸载。
func TestAlgorithmService_UninstallBuiltinProtected(t *testing.T) {
	svc, _, db, _ := newAlgorithmServiceSQLiteEnv(t)
	ctx := context.Background()

	mustCreate := func(v any) {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("create fixture: %v", err)
		}
	}
	mustCreate(&model.Algorithm{
		AlgorithmID:   "license_plate_recognition",
		Name:          "车牌识别",
		AlgorithmType: "license_plate_recognition",
		ActiveVersion: "1.0.0",
		IsBuiltin:     true,
	})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "license_plate_recognition",
		Version:      "1.0.0",
		IsActive:     true,
		IsBuiltin:    true,
		FPSTiers:     []byte(`[]`),
		ConfigSchema: []byte(`{}`),
		ManifestRaw:  []byte(`{}`),
	})

	err := svc.UninstallVersion(ctx, "license_plate_recognition", "1.0.0")
	if !errno.Is(err, errno.CodeBuiltinAlgoProtected) {
		t.Fatalf("uninstall builtin algorithm = %v, want CodeBuiltinAlgoProtected", err)
	}

	// 确认数据库中的版本与主记录依然存在
	repo := repository.NewAlgorithmRepository(db)
	ver, err := repo.GetVersion(ctx, "license_plate_recognition", "1.0.0")
	if err != nil || ver == nil {
		t.Fatalf("builtin version should remain intact: %v", err)
	}
}

// TestAlgorithmServiceUninstallBlockedWhenInstancesExist 存在未软删实例引用时禁止卸载（R7/§7.5.5）。
func TestAlgorithmServiceUninstallBlockedWhenInstancesExist(t *testing.T) {
	svc, _, db, _ := newAlgorithmServiceSQLiteEnv(t)
	ctx := context.Background()

	mustCreate := func(v any) {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("create fixture: %v", err)
		}
	}
	mustCreate(&model.Algorithm{AlgorithmID: "yolov8n", Name: "yolo", AlgorithmType: "detector", ActiveVersion: "1.0.0"})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "yolov8n",
		Version:      "1.0.0",
		IsActive:     true,
		FPSTiers:     []byte(`[]`),
		ConfigSchema: []byte(`{}`),
		ManifestRaw:  []byte(`{}`),
	})
	mustCreate(&model.AlgorithmInstance{
		InstanceID:  "i1",
		CameraID:    "cam-a",
		AlgorithmID: "yolov8n",
		Enabled:     true,
		ParamsJSON:  []byte(`{}`),
		RulesJSON:   []byte(`[]`),
	})

	mustCreate(&model.AnalysisTask{
		CameraID:       "cam-a",
		Name:           "active-task",
		DesiredEnabled: true,
	})

	// 1. 存在未软删实例 → 拒绝卸载并返回 CodeAlgoInUse
	err := svc.UninstallVersion(ctx, "yolov8n", "1.0.0")
	if !errno.Is(err, errno.CodeAlgoInUse) {
		t.Fatalf("uninstall with active instances = %v, want CodeAlgoInUse", err)
	}
	repo := repository.NewAlgorithmRepository(db)
	if _, err := repo.GetVersion(ctx, "yolov8n", "1.0.0"); err != nil {
		t.Fatalf("version should remain after rejected uninstall: %v", err)
	}

	// 2. 软删除实例后 → 允许卸载
	if err := db.Delete(&model.AlgorithmInstance{}, "instance_id = ?", "i1").Error; err != nil {
		t.Fatalf("soft delete instance: %v", err)
	}
	if err := svc.UninstallVersion(ctx, "yolov8n", "1.0.0"); err != nil {
		t.Fatalf("uninstall after instances removed failed: %v", err)
	}
	if _, err := repo.GetVersion(ctx, "yolov8n", "1.0.0"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("version should be deleted, got err = %v", err)
	}
}

// TestAlgorithmServiceActivateBumpsRevisionAndDesiredState
// 激活版本后：DesiredState 中实例绑定的版本同步更新，且 revision 递增（D11）。
func TestAlgorithmServiceActivateBumpsRevisionAndDesiredState(t *testing.T) {
	svc, _, db, taskRepo := newAlgorithmServiceSQLiteEnv(t)
	ctx := context.Background()

	mustCreate := func(v any) {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("create fixture: %v", err)
		}
	}
	mustCreate(&model.Camera{
		CameraID:        "cam-a",
		Protocol:        model.CameraProtocolRTSP,
		RtspURL:         "rtsp://192.168.1.10/live",
		TransportPolicy: model.CameraTransportAuto,
		ConfigHash:      "h",
	})
	mustCreate(&model.Algorithm{AlgorithmID: "alg-a", ActiveVersion: "v1"})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "alg-a",
		Version:      "v1",
		IsActive:     true,
		FPSTiers:     []byte(`[]`),
		ConfigSchema: []byte(`{}`),
		ManifestRaw:  []byte(`{}`),
	})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "alg-a",
		Version:      "v2",
		IsActive:     false,
		FPSTiers:     []byte(`[]`),
		ConfigSchema: []byte(`{}`),
		ManifestRaw:  []byte(`{}`),
	})
	mustCreate(&model.AnalysisTask{CameraID: "cam-a", Name: "task-a", DesiredEnabled: true})
	mustCreate(&model.AlgorithmInstance{
		InstanceID:  "i1",
		CameraID:    "cam-a",
		AlgorithmID: "alg-a",
		AnalysisFPS: 5,
		ParamsJSON:  []byte(`{}`),
		RulesJSON:   []byte(`[]`),
		Enabled:     true,
	})

	// 激活 v2
	if err := svc.ActivateVersion(ctx, "alg-a", "v2"); err != nil {
		t.Fatalf("activate v2 failed: %v", err)
	}

	adapter := NewDesiredStateAdapter(taskRepo, nil, zap.NewNop())
	state, err := adapter.DesiredState(ctx, 0)
	if err != nil {
		t.Fatalf("desired state error: %v", err)
	}
	if state.GetRevision() != 1 {
		t.Fatalf("revision = %d, want 1", state.GetRevision())
	}
	if len(state.GetInstances()) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(state.GetInstances()))
	}
	if state.GetInstances()[0].GetAlgorithmVersion() != "v2" {
		t.Fatalf("instance algorithm_version = %s, want v2", state.GetInstances()[0].GetAlgorithmVersion())
	}
}

// TestAlgorithmServiceActivateRollsBackOnBumpFailure
// revision bump 失败时业务写全部回滚（锁住「同事务」原子性）。
func TestAlgorithmServiceActivateRollsBackOnBumpFailure(t *testing.T) {
	svc, _, db, _ := newAlgorithmServiceSQLiteEnv(t)
	ctx := context.Background()

	mustCreate := func(v any) {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("create fixture: %v", err)
		}
	}
	mustCreate(&model.Algorithm{AlgorithmID: "alg-a", ActiveVersion: "v1"})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "alg-a",
		Version:      "v1",
		IsActive:     true,
		FPSTiers:     []byte(`[]`),
		ConfigSchema: []byte(`{}`),
		ManifestRaw:  []byte(`{}`),
	})
	mustCreate(&model.AlgorithmVersion{
		AlgorithmID:  "alg-a",
		Version:      "v2",
		IsActive:     false,
		FPSTiers:     []byte(`[]`),
		ConfigSchema: []byte(`{}`),
		ManifestRaw:  []byte(`{}`),
	})

	// 构造 revision 单行缺失：bump 必然失败，业务写必须随事务回滚
	if err := db.Delete(&model.DesiredStateRevision{}, "id = ?", 1).Error; err != nil {
		t.Fatalf("delete revision row: %v", err)
	}

	err := svc.ActivateVersion(ctx, "alg-a", "v2")
	if !errors.Is(err, repository.ErrRevisionMissing) {
		t.Fatalf("err = %v, want ErrRevisionMissing", err)
	}

	algo, err := repository.NewAlgorithmRepository(db).GetAlgorithmByID(ctx, "alg-a")
	if err != nil {
		t.Fatalf("get algorithm: %v", err)
	}
	if algo.ActiveVersion != "v1" {
		t.Fatalf("active_version = %q, want v1 (rolled back)", algo.ActiveVersion)
	}
	ver, err := repository.NewAlgorithmRepository(db).GetVersion(ctx, "alg-a", "v2")
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	if ver.IsActive {
		t.Fatal("v2 is_active = true, want false (rolled back)")
	}
}
