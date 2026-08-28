package service

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
	"niko-vue-admin/app/internal/repository"
)

// fakeAlgorithmRepo 模拟仓储。
type fakeAlgorithmRepo struct {
	algos    map[string]*model.Algorithm
	versions map[string]*model.AlgorithmVersion
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

func (r *fakeAlgorithmRepo) CountActiveInstances(ctx context.Context, algorithmID, version string) (int64, error) {
	return 0, nil
}

// fakeEngineClient 模拟 EngineClient。
type fakeEngineClient struct {
	installResp   *aivisionv1.InstallPackageResponse
	rollbackResp  *aivisionv1.RollbackPackageResponse
	uninstallResp *aivisionv1.UninstallPackageResponse
}

func (c *fakeEngineClient) InstallPackage(ctx context.Context, req *aivisionv1.InstallPackageRequest, opts ...grpc.CallOption) (*aivisionv1.InstallPackageResponse, error) {
	if c.installResp != nil {
		return c.installResp, nil
	}
	return &aivisionv1.InstallPackageResponse{Code: "", AlgorithmId: "yolov8n", Version: "1.0.0"}, nil
}

func (c *fakeEngineClient) UpgradePackage(ctx context.Context, req *aivisionv1.UpgradePackageRequest, opts ...grpc.CallOption) (*aivisionv1.UpgradePackageResponse, error) {
	return &aivisionv1.UpgradePackageResponse{Code: ""}, nil
}

func (c *fakeEngineClient) RollbackPackage(ctx context.Context, req *aivisionv1.RollbackPackageRequest, opts ...grpc.CallOption) (*aivisionv1.RollbackPackageResponse, error) {
	if c.rollbackResp != nil {
		return c.rollbackResp, nil
	}
	return &aivisionv1.RollbackPackageResponse{Code: ""}, nil
}

func (c *fakeEngineClient) UninstallPackage(ctx context.Context, req *aivisionv1.UninstallPackageRequest, opts ...grpc.CallOption) (*aivisionv1.UninstallPackageResponse, error) {
	if c.uninstallResp != nil {
		return c.uninstallResp, nil
	}
	return &aivisionv1.UninstallPackageResponse{Code: ""}, nil
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
