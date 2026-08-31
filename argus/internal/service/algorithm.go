package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"argus/app/internal/model"
	"argus/app/internal/pkg/engineipc"
	"argus/app/internal/pkg/errno"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
)

// AlgorithmEngineClient 定义算法包管理所需 Engine RPC 的接口子集（便于测试替身）。
type AlgorithmEngineClient interface {
	InstallPackage(ctx context.Context, req *argusv1.InstallPackageRequest, opts ...grpc.CallOption) (*argusv1.InstallPackageResponse, error)
	UpgradePackage(ctx context.Context, req *argusv1.UpgradePackageRequest, opts ...grpc.CallOption) (*argusv1.UpgradePackageResponse, error)
	RollbackPackage(ctx context.Context, req *argusv1.RollbackPackageRequest, opts ...grpc.CallOption) (*argusv1.RollbackPackageResponse, error)
	UninstallPackage(ctx context.Context, req *argusv1.UninstallPackageRequest, opts ...grpc.CallOption) (*argusv1.UninstallPackageResponse, error)
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

	resp, err := s.engineClient.InstallPackage(ctx, &argusv1.InstallPackageRequest{
		PackagePath: extracted.StagingDir,
	})
	if err != nil {
		// EngineClient 已把 validator/ABI 等业务拒绝包装为 RemoteError；
		// 只有没有响应的 gRPC/Socket 错误才表示 Engine 服务不可用。
		var remote *engineipc.RemoteError
		if errors.As(err, &remote) {
			s.logger.Warn("engine InstallPackage rejected package",
				zap.String("code", remote.Code),
				zap.String("error_message", remote.ErrorMessage))
			return nil, errno.New(errno.CodeAlgoInstallFailed)
		}
		s.logger.Error("engine InstallPackage RPC error", zap.Error(err))
		return nil, errno.New(errno.CodeEngineUnavailable)
	}
	if resp.GetCode() != "" {
		s.logger.Warn("engine InstallPackage rejected package",
			zap.String("code", resp.GetCode()),
			zap.String("error_message", resp.GetErrorMessage()))
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
		FPSTiers:          model.JSONRaw(fpsTiersJSON),
		ConfigSchema:      model.JSONRaw(extracted.ConfigSchemaRaw),
		ManifestRaw:       model.JSONRaw(extracted.ManifestRaw),
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

// mapEngineError 区分 Engine 返回的业务拒绝与 IPC 传输失败：
// 业务拒绝已由 EngineClient 包装为 *engineipc.RemoteError，交给 mapCode 按业务码映射；
// 只有没有响应的 gRPC/Socket 错误才表示 Engine 服务不可用。
func mapEngineError(err error, mapCode func(string) error) error {
	var remote *engineipc.RemoteError
	if errors.As(err, &remote) {
		return mapCode(remote.Code)
	}
	return errno.New(errno.CodeEngineUnavailable)
}

func (s *algorithmService) ActivateVersion(ctx context.Context, algorithmID, version string) error {
	if s.engineClient == nil {
		return errno.New(errno.CodeEngineUnavailable)
	}

	algo, err := s.repo.GetAlgorithmByID(ctx, algorithmID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.New(errno.CodeNotFound)
		}
		return err
	}
	target, err := s.repo.GetVersion(ctx, algorithmID, version)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.New(errno.CodeNotFound)
		}
		return err
	}
	previousAlgo := *algo
	previousTarget := *target
	previousVersion := previousAlgo.ActiveVersion
	if previousVersion == version {
		return nil
	}

	// 先提交 Go 侧权威 DesiredState。RPC 失败时再以更高 revision 恢复旧版本，
	// 即使 Engine 已收到中间状态，后续全量对账也会收敛回原状态。
	if err := s.activateVersionInTx(ctx, algorithmID, version); err != nil {
		return err
	}
	resp, rpcErr := s.engineClient.RollbackPackage(ctx, &argusv1.RollbackPackageRequest{
		AlgorithmId:   algorithmID,
		TargetVersion: version,
	})
	if rpcErr == nil && resp.GetCode() == "" {
		return nil
	}

	if restoreErr := s.restoreVersionStateInTx(ctx, &previousAlgo, &previousTarget); restoreErr != nil {
		return fmt.Errorf("activate engine failed and restore desired version %s failed: %w", previousVersion, restoreErr)
	}
	if rpcErr != nil {
		return mapEngineError(rpcErr, mapActivatePackageCode)
	}
	return mapActivatePackageCode(resp.GetCode())
}

func (s *algorithmService) activateVersionInTx(ctx context.Context, algorithmID, version string) error {
	return s.repo.InTx(ctx, func(ctx context.Context, r repository.AlgorithmRepository) error {
		if err := r.ActivateVersion(ctx, algorithmID, version); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	})
}

func (s *algorithmService) restoreVersionStateInTx(ctx context.Context, algo *model.Algorithm, version *model.AlgorithmVersion) error {
	return s.repo.InTx(ctx, func(ctx context.Context, r repository.AlgorithmRepository) error {
		if err := r.RestoreVersionState(ctx, algo, version); err != nil {
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

	algo, err := s.repo.GetAlgorithmByID(ctx, algorithmID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.New(errno.CodeNotFound)
		}
		return err
	}
	target, err := s.repo.GetVersion(ctx, algorithmID, version)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errno.New(errno.CodeNotFound)
		}
		return err
	}
	previousAlgo := *algo
	previousTarget := *target
	activeCount, err := s.repo.CountActiveInstances(ctx, algorithmID, version)
	if err != nil {
		return err
	}
	if activeCount > 0 {
		return errno.New(errno.CodeAlgoInUse)
	}

	// 先移除 DesiredState 引用，避免 Engine 物理删除成功后 Go 仍持续下发不存在的包。
	if err := s.repo.InTx(ctx, func(ctx context.Context, r repository.AlgorithmRepository) error {
		if err := r.DeleteVersion(ctx, algorithmID, version); err != nil {
			return err
		}
		_, err := r.BumpRevision(ctx)
		return err
	}); err != nil {
		return err
	}

	resp, rpcErr := s.engineClient.UninstallPackage(ctx, &argusv1.UninstallPackageRequest{
		AlgorithmId: algorithmID,
		Version:     version,
	})
	if rpcErr == nil && resp.GetCode() == "" {
		return nil
	}

	// 明确业务拒绝表示 Engine 未删除包，可以安全恢复 DB；传输失败结果不确定，
	// 保留 DB 已卸载状态，避免恢复出一个可能已不存在的 DesiredState 引用。
	var remote *engineipc.RemoteError
	businessRejected := rpcErr == nil || errors.As(rpcErr, &remote)
	if businessRejected {
		if restoreErr := s.restoreVersionStateInTx(ctx, &previousAlgo, &previousTarget); restoreErr != nil {
			return fmt.Errorf("uninstall engine rejected and restore metadata failed: %w", restoreErr)
		}
	}
	if rpcErr != nil {
		return mapEngineError(rpcErr, mapUninstallPackageCode)
	}
	return mapUninstallPackageCode(resp.GetCode())
}

// mapActivatePackageCode 将激活/回滚 RPC 的稳定业务码映射为 HTTP API 业务错误。
func mapActivatePackageCode(code string) error {
	switch code {
	case "PACKAGE_NOT_FOUND":
		return errno.New(errno.CodeNotFound)
	case "INVALID_ARG":
		return errno.New(errno.CodeInvalidParam)
	default:
		return errno.New(errno.CodeAlgoInstallFailed)
	}
}

// mapUninstallPackageCode 将卸载 RPC 的稳定业务码映射为 HTTP API 业务错误。
func mapUninstallPackageCode(code string) error {
	switch code {
	case "PACKAGE_IN_USE":
		return errno.New(errno.CodeAlgoInUse)
	case "PACKAGE_NOT_FOUND":
		return errno.New(errno.CodeNotFound)
	case "INVALID_ARG":
		return errno.New(errno.CodeInvalidParam)
	default:
		return errno.New(errno.CodeInternal)
	}
}
