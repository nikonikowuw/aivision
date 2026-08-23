package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"go.uber.org/zap"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/ntp"
	"niko-vue-admin/app/internal/repository"
)

// NTPConfigDTO 对时配置响应 DTO
type NTPConfigDTO struct {
	ID        uint64    `json:"id"`
	Mode      string    `json:"mode"`
	Servers   []string  `json:"servers"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// UpdateNTPConfigInput 更新对时配置入参
type UpdateNTPConfigInput struct {
	Mode    string   `json:"mode" binding:"required"`
	Servers []string `json:"servers"`
}

// SetTimeInput 手动设时入参
type SetTimeInput struct {
	Time time.Time `json:"time" binding:"required"`
}

// NTPService 对时服务接口
type NTPService interface {
	GetConfig(ctx context.Context) (*NTPConfigDTO, error)
	UpdateConfig(ctx context.Context, input *UpdateNTPConfigInput) error
	GetStatus(ctx context.Context) (*ntp.SyncStatus, error)
	SyncNow(ctx context.Context) error
	SetTime(ctx context.Context, input *SetTimeInput) error
	IsSynced(ctx context.Context) (bool, error)
	ReplayOnBoot(ctx context.Context) error
}

type ntpService struct {
	repo     repository.SystemConfigRepository
	executor ntp.Executor
	logger   *zap.Logger
}

// NewNTPService 创建对时服务实例
func NewNTPService(
	repo repository.SystemConfigRepository,
	executor ntp.Executor,
	logger *zap.Logger,
) NTPService {
	return &ntpService{
		repo:     repo,
		executor: executor,
		logger:   logger,
	}
}

func (s *ntpService) GetConfig(ctx context.Context) (*NTPConfigDTO, error) {
	cfg, err := s.repo.GetByKey(ctx, model.ConfigKeyTime)
	if err != nil {
		s.logger.Error("failed to get ntp config", zap.Error(err))
		return nil, errno.NewError(errno.CodeInternal)
	}

	if cfg == nil {
		// 默认配置
		now := time.Now().UTC()
		return &NTPConfigDTO{
			ID:        0,
			Mode:      model.TimeModeNTP,
			Servers:   []string{"pool.ntp.org", "ntp.aliyun.com"},
			CreatedAt: now,
			UpdatedAt: now,
		}, nil
	}

	var val model.TimeConfigValue
	if err := json.Unmarshal([]byte(cfg.Value), &val); err != nil {
		s.logger.Error("failed to unmarshal ntp config json", zap.Error(err))
		return nil, errno.NewError(errno.CodeInternal)
	}

	if val.Mode != model.TimeModeNTP && val.Mode != model.TimeModeManual {
		s.logger.Error("ntp config contains invalid mode", zap.String("mode", val.Mode))
		return nil, errno.NewError(errno.CodeNTPInvalidMode)
	}
	if val.Servers == nil {
		val.Servers = []string{}
	}

	return &NTPConfigDTO{
		ID:        cfg.ID,
		Mode:      val.Mode,
		Servers:   val.Servers,
		CreatedAt: cfg.CreatedAt,
		UpdatedAt: cfg.UpdatedAt,
	}, nil
}

func normalizeNTPServers(servers []string) ([]string, error) {
	cleanServers := make([]string, 0, len(servers))
	for _, server := range servers {
		// 控制字符会破坏 chrony/timesyncd 配置行，不能进入 root 权限执行器。
		if strings.IndexFunc(server, unicode.IsControl) >= 0 {
			return nil, errno.NewError(errno.CodeInvalidParam)
		}
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		if strings.IndexFunc(server, unicode.IsSpace) >= 0 {
			return nil, errno.NewError(errno.CodeInvalidParam)
		}
		cleanServers = append(cleanServers, server)
	}
	return cleanServers, nil
}

func (s *ntpService) UpdateConfig(ctx context.Context, input *UpdateNTPConfigInput) error {
	if input == nil {
		return errno.NewError(errno.CodeInvalidParam)
	}

	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode != model.TimeModeNTP && mode != model.TimeModeManual {
		return errno.NewError(errno.CodeNTPInvalidMode)
	}

	cleanServers, err := normalizeNTPServers(input.Servers)
	if err != nil {
		return err
	}

	if mode == model.TimeModeNTP && len(cleanServers) == 0 {
		return errno.NewError(errno.CodeNTPServersEmpty)
	}

	val := model.TimeConfigValue{
		Mode:    mode,
		Servers: cleanServers,
	}

	valBytes, err := json.Marshal(val)
	if err != nil {
		return errno.NewError(errno.CodeInternal)
	}

	// 1. 持久化期望配置；执行器失败时保留它，启动时 ReplayOnBoot 可重试应用。
	if err := s.repo.SetByKey(ctx, model.ConfigKeyTime, string(valBytes), "系统对时配置"); err != nil {
		s.logger.Error("failed to persist ntp config", zap.Error(err))
		return errno.NewError(errno.CodeInternal)
	}

	// 2. 底层系统生效
	if mode == model.TimeModeNTP {
		if err := s.executor.ApplyNTP(ctx, cleanServers); err != nil {
			s.logger.Error("failed to apply ntp servers", zap.Error(err))
			return errno.NewError(errno.CodeNTPSyncFailed)
		}
	} else {
		if err := s.executor.DisableNTP(ctx); err != nil {
			s.logger.Error("failed to disable ntp", zap.Error(err))
			return errno.NewError(errno.CodeNTPExecutorUnavailable)
		}
	}

	return nil
}

func (s *ntpService) GetStatus(ctx context.Context) (*ntp.SyncStatus, error) {
	status, err := s.executor.GetStatus(ctx)
	if err != nil {
		s.logger.Error("failed to get ntp sync status", zap.Error(err))
		return nil, errno.NewError(errno.CodeNTPExecutorUnavailable)
	}
	return status, nil
}

func (s *ntpService) SyncNow(ctx context.Context) error {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}
	if cfg.Mode == model.TimeModeManual {
		return errno.NewError(errno.CodeNTPSyncNotAllowedInManualMode)
	}

	if err := s.executor.SyncNow(ctx); err != nil {
		s.logger.Error("failed to trigger sync now", zap.Error(err))
		return errno.NewError(errno.CodeNTPSyncFailed)
	}
	return nil
}

func (s *ntpService) SetTime(ctx context.Context, input *SetTimeInput) error {
	if input == nil || input.Time.IsZero() {
		return errno.NewError(errno.CodeInvalidParam)
	}

	// 先读取当前配置，确保系统设时成功后能保留原有服务器列表。
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}

	// 系统时间修改失败时不写入 manual 配置，避免重启时回放错误状态。
	if err := s.executor.SetSystemTime(ctx, input.Time); err != nil {
		s.logger.Error("failed to set system time", zap.Error(err))
		return fmt.Errorf("%w: %v", errno.NewError(errno.CodeNTPSetTimeFailed), err)
	}

	valBytes, err := json.Marshal(model.TimeConfigValue{
		Mode:    model.TimeModeManual,
		Servers: cfg.Servers,
	})
	if err != nil {
		s.logger.Error("failed to marshal manual time config", zap.Error(err))
		return errno.NewError(errno.CodeInternal)
	}
	if err := s.repo.SetByKey(ctx, model.ConfigKeyTime, string(valBytes), "系统对时配置"); err != nil {
		// 此时系统时钟已经修改，但必须向调用方报告持久化失败，避免错误返回成功。
		s.logger.Error("failed to persist manual mode after set time", zap.Error(err))
		return errno.NewError(errno.CodeInternal)
	}

	return nil
}

func (s *ntpService) IsSynced(ctx context.Context) (bool, error) {
	status, err := s.executor.GetStatus(ctx)
	if err != nil {
		s.logger.Error("failed to get ntp sync status", zap.Error(err))
		return false, errno.NewError(errno.CodeNTPExecutorUnavailable)
	}
	return status.Synced, nil
}

func (s *ntpService) ReplayOnBoot(ctx context.Context) error {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		s.logger.Warn("ntp replay on boot: failed to get config", zap.Error(err))
		return err
	}

	s.logger.Info("replaying ntp config on boot", zap.String("mode", cfg.Mode), zap.Strings("servers", cfg.Servers))
	if cfg.Mode == model.TimeModeNTP {
		if err := s.executor.ApplyNTP(ctx, cfg.Servers); err != nil {
			s.logger.Warn("ntp replay on boot: failed to apply ntp", zap.Error(err))
			return err
		}
	} else {
		if err := s.executor.DisableNTP(ctx); err != nil {
			s.logger.Warn("ntp replay on boot: failed to disable ntp", zap.Error(err))
			return err
		}
	}
	return nil
}
