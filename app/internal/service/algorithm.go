package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/engineipc"
	"niko-vue-admin/app/internal/pkg/errno"
	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
	"niko-vue-admin/app/internal/repository"
)

// AlgorithmEngineClient 定义算法包管理所需 Engine RPC 的接口子集（便于测试替身）。
type AlgorithmEngineClient interface {
	InstallPackage(ctx context.Context, req *aivisionv1.InstallPackageRequest, opts ...grpc.CallOption) (*aivisionv1.InstallPackageResponse, error)
	UpgradePackage(ctx context.Context, req *aivisionv1.UpgradePackageRequest, opts ...grpc.CallOption) (*aivisionv1.UpgradePackageResponse, error)
	RollbackPackage(ctx context.Context, req *aivisionv1.RollbackPackageRequest, opts ...grpc.CallOption) (*aivisionv1.RollbackPackageResponse, error)
	UninstallPackage(ctx context.Context, req *aivisionv1.UninstallPackageRequest, opts ...grpc.CallOption) (*aivisionv1.UninstallPackageResponse, error)
}

// AlgorithmService 算法包业务接口。
type AlgorithmService interface {
	ListAlgorithms(ctx context.Context, filter *repository.AlgorithmFilter) ([]model.Algorithm, int64, error)
	GetAlgorithm(ctx context.Context, algorithmID string) (*model.Algorithm, error)
	ListVersions(ctx context.Context, algorithmID string) ([]model.AlgorithmVersion, error)
	UploadAndInstall(ctx context.Context, reader io.Reader) (*model.AlgorithmVersion, error)
	ActivateVersion(ctx context.Context, algorithmID, version string) error
	UninstallVersion(ctx context.Context, algorithmID, version string) error
}

type algorithmService struct {
	repo         repository.AlgorithmRepository
	engineClient AlgorithmEngineClient
	logger       *zap.Logger
	tmpDir       string
}

// NewAlgorithmService 创建 AlgorithmService。
func NewAlgorithmService(
	repo repository.AlgorithmRepository,
	engineClient *engineipc.EngineClient,
	logger *zap.Logger,
) AlgorithmService {
	return &algorithmService{
		repo:         repo,
		engineClient: engineClient,
		logger:       logger,
		tmpDir:       "data/tmp/packages",
	}
}

func (s *algorithmService) ListAlgorithms(ctx context.Context, filter *repository.AlgorithmFilter) ([]model.Algorithm, int64, error) {
	return s.repo.ListAlgorithms(ctx, filter)
}

func (s *algorithmService) GetAlgorithm(ctx context.Context, algorithmID string) (*model.Algorithm, error) {
	algo, err := s.repo.GetAlgorithmByID(ctx, algorithmID)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, errno.New(errno.CodeNotFound)
		}
		return nil, err
	}
	return algo, nil
}

func (s *algorithmService) ListVersions(ctx context.Context, algorithmID string) ([]model.AlgorithmVersion, error) {
	return s.repo.ListVersions(ctx, algorithmID)
}

func (s *algorithmService) UploadAndInstall(ctx context.Context, reader io.Reader) (*model.AlgorithmVersion, error) {
	if s.engineClient == nil {
		return nil, errno.New(errno.CodeEngineUnavailable)
	}

	// 1. 创建安全临时暂存目录（使用绝对路径，避免跨进程/跨工作目录通信时相对路径解析错位）
	stagingID := uuid.New().String()
	stagingPath, err := filepath.Abs(filepath.Join(s.tmpDir, stagingID))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve staging path: %w", err)
	}
	if err := os.MkdirAll(stagingPath, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingPath) // 无论成功与否，函数退出后清理暂存

	// 2. 解压并做初步 manifest 校验
	extracted, err := ExtractAndValidateArchive(reader, stagingPath, DefaultMaxPackageBytes)
	if err != nil {
		s.logger.Warn("algorithm package extract failed", zap.Error(err))
		return nil, errno.New(errno.CodeAlgoPackageInvalid)
	}

	// 3. 调用 Engine 的 InstallPackage RPC 执行沙箱自测与安装
	s.logger.Info("triggering engine InstallPackage",
		zap.String("algorithm_id", extracted.AlgorithmID),
		zap.String("version", extracted.Version),
		zap.String("staging_dir", extracted.StagingDir))

	resp, err := s.engineClient.InstallPackage(ctx, &aivisionv1.InstallPackageRequest{
		PackagePath: extracted.StagingDir,
	})
	if err != nil {
		s.logger.Error("engine InstallPackage RPC error", zap.Error(err))
		return nil, errno.New(errno.CodeEngineUnavailable)
	}
	if resp.Code != "" {
		s.logger.Warn("engine InstallPackage rejected package",
			zap.String("code", resp.Code),
			zap.String("error_message", resp.ErrorMessage))
		return nil, errno.New(errno.CodeAlgoInstallFailed)
	}

	// 4. Engine 校验安装成功后，持久化元数据：算法/版本/激活状态与 revision bump
	// 在同一事务内提交（design §3.2 / D11）——active_version 变更若不同事务 bump，
	// 存在「配置已变但 revision 未增 → Engine 永不感知」的崩溃窗口。
	algoModel := &model.Algorithm{
		AlgorithmID:   extracted.AlgorithmID,
		Name:          extracted.Name,
		AlgorithmType: extracted.AlgorithmType,
		AlarmTypeID:   extracted.AlarmTypeID,
		ActiveVersion: extracted.Version, // 默认首次安装即为激活版本
		Description:   extracted.Description,
	}

	fpsTiersJSON, err := json.Marshal(extracted.FPSTiers)
	if err != nil {
		fpsTiersJSON = []byte("[]")
	}

	versionModel := &model.AlgorithmVersion{
		AlgorithmID:       extracted.AlgorithmID,
		Version:           extracted.Version,
		PlatformID:        extracted.PlatformID,
		MinAdapterVersion: extracted.MinAdapterVersion,
		PackageRoot:       fmt.Sprintf("var/packages/%s/%s", extracted.AlgorithmID, extracted.Version),
		FPSTiers:          fpsTiersJSON,
		ConfigSchema:      extracted.ConfigSchemaRaw,
		ManifestRaw:       extracted.ManifestRaw,
		PackageSizeBytes:  extracted.TotalSizeBytes,
		IsActive:          true,
	}

	err = s.repo.InTx(ctx, func(ctx context.Context, r repository.AlgorithmRepository) error {
		if err := r.UpsertAlgorithm(ctx, algoModel); err != nil {
			return err
		}
		if err := r.UpsertVersion(ctx, versionModel); err != nil {
			return err
		}
		if err := r.ActivateVersion(ctx, extracted.AlgorithmID, extracted.Version); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	})
	if err != nil {
		s.logger.Error("failed to persist algorithm package to db", zap.Error(err))
		return nil, err
	}

	s.logger.Info("algorithm package installed successfully",
		zap.String("algorithm_id", extracted.AlgorithmID),
		zap.String("version", extracted.Version))

	return versionModel, nil
}

func (s *algorithmService) ActivateVersion(ctx context.Context, algorithmID, version string) error {
	if s.engineClient == nil {
		return errno.New(errno.CodeEngineUnavailable)
	}

	// 1. 查询目标版本是否存在
	_, err := s.repo.GetVersion(ctx, algorithmID, version)
	if err != nil {
		if err == repository.ErrNotFound {
			return errno.New(errno.CodeNotFound)
		}
		return err
	}

	// 2. 调用 Engine 的 RollbackPackage 将激活版本指向 target_version
	resp, err := s.engineClient.RollbackPackage(ctx, &aivisionv1.RollbackPackageRequest{
		AlgorithmId:   algorithmID,
		TargetVersion: version,
	})
	if err != nil {
		return errno.New(errno.CodeEngineUnavailable)
	}
	if resp.Code != "" {
		return errno.New(errno.CodeAlgoInstallFailed)
	}

	// 3. 更新数据库激活状态并在同一事务内递增 revision：
	// active_version 变更会改变 DesiredState 内容，bump 与业务写必须原子提交
	// （design §3.2 / D11），否则 Engine 永不感知版本切换。
	return s.repo.InTx(ctx, func(ctx context.Context, r repository.AlgorithmRepository) error {
		if err := r.ActivateVersion(ctx, algorithmID, version); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	})
}

func (s *algorithmService) UninstallVersion(ctx context.Context, algorithmID, version string) error {
	if s.engineClient == nil {
		return errno.New(errno.CodeEngineUnavailable)
	}

	// 1. 检查是否存在
	_, err := s.repo.GetVersion(ctx, algorithmID, version)
	if err != nil {
		if err == repository.ErrNotFound {
			return errno.New(errno.CodeNotFound)
		}
		return err
	}

	// 2. 检查业务层是否有实例引用该版本
	activeCount, err := s.repo.CountActiveInstances(ctx, algorithmID, version)
	if err != nil {
		return err
	}
	if activeCount > 0 {
		return errno.New(errno.CodeAlgoInUse)
	}

	// 3. 调用 Engine 执行卸载（Engine 自身也包含并发引用防护）
	resp, err := s.engineClient.UninstallPackage(ctx, &aivisionv1.UninstallPackageRequest{
		AlgorithmId: algorithmID,
		Version:     version,
	})
	if err != nil {
		return errno.New(errno.CodeEngineUnavailable)
	}
	if resp.Code != "" {
		if resp.Code == "PACKAGE_IN_USE" {
			return errno.New(errno.CodeAlgoInUse)
		}
		return errno.New(errno.CodeInternal)
	}

	// 4. 清理数据库记录并在同一事务内递增 revision：
	// 卸载活跃版本会改变 DesiredState（active_version 回退/算法删除），
	// 与 revision bump 必须原子提交（design §3.2 / D11）。
	return s.repo.InTx(ctx, func(ctx context.Context, r repository.AlgorithmRepository) error {
		if err := r.DeleteVersion(ctx, algorithmID, version); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	})
}
