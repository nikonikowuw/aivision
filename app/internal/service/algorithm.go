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

	// 1. 创建安全临时暂存目录
	stagingID := uuid.New().String()
	stagingPath := filepath.Join(s.tmpDir, stagingID)
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

	// 4. Engine 校验安装成功后，持久化元数据至数据库
	algoModel := &model.Algorithm{
		AlgorithmID:   extracted.AlgorithmID,
		Name:          extracted.Name,
		AlgorithmType: extracted.AlgorithmType,
		AlarmTypeID:   extracted.AlarmTypeID,
		ActiveVersion: extracted.Version, // 默认首次安装即为激活版本
		Description:   extracted.Description,
	}
	if err := s.repo.UpsertAlgorithm(ctx, algoModel); err != nil {
		s.logger.Error("failed to upsert algorithm to db", zap.Error(err))
		return nil, err
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
	if err := s.repo.UpsertVersion(ctx, versionModel); err != nil {
		s.logger.Error("failed to upsert algorithm version to db", zap.Error(err))
		return nil, err
	}

	// 将其他版本置为非激活并激活当前
	if err := s.repo.ActivateVersion(ctx, extracted.AlgorithmID, extracted.Version); err != nil {
		s.logger.Warn("failed to set version active in db", zap.Error(err))
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

	// 3. 更新数据库激活状态
	return s.repo.ActivateVersion(ctx, algorithmID, version)
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

	// 4. 清理数据库记录
	return s.repo.DeleteVersion(ctx, algorithmID, version)
}
