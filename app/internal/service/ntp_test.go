package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/pkg/ntp"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/service"
	"niko-vue-admin/app/internal/testutil"
)

// expectErrno 断言 err 链中含指定 errno 码，否则调用 t.Fatalf。
func expectErrno(t *testing.T, err error, code int) {
	t.Helper()
	var appErr *errno.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %v, want errno %d", err, code)
	}
	if appErr.Code != code {
		t.Fatalf("errno code = %d, want %d", appErr.Code, code)
	}
}

func TestNTPService(t *testing.T) {
	db := testutil.NewSmokeDB(t)
	repo := repository.NewSystemConfigRepository(db)
	mockExec := ntp.NewMockExecutor()
	logger := zap.NewNop()
	srv := service.NewNTPService(repo, mockExec, logger)
	ctx := context.Background()

	t.Run("GetConfig default", func(t *testing.T) {
		cfg, err := srv.GetConfig(ctx)
		if err != nil {
			t.Fatalf("GetConfig: %v", err)
		}
		if cfg.Mode != "ntp" {
			t.Errorf("mode = %q, want ntp", cfg.Mode)
		}
		if len(cfg.Servers) == 0 {
			t.Error("servers empty, want non-empty default")
		}
	})

	t.Run("UpdateConfig valid ntp", func(t *testing.T) {
		if err := srv.UpdateConfig(ctx, &service.UpdateNTPConfigInput{
			Mode:    "ntp",
			Servers: []string{"ntp.aliyun.com", "time.google.com"},
		}); err != nil {
			t.Fatalf("UpdateConfig: %v", err)
		}

		cfg, err := srv.GetConfig(ctx)
		if err != nil {
			t.Fatalf("GetConfig: %v", err)
		}
		if cfg.Mode != "ntp" {
			t.Errorf("mode = %q, want ntp", cfg.Mode)
		}
		if len(cfg.Servers) != 2 || cfg.Servers[0] != "ntp.aliyun.com" || cfg.Servers[1] != "time.google.com" {
			t.Errorf("servers = %v, want [ntp.aliyun.com time.google.com]", cfg.Servers)
		}
	})

	t.Run("UpdateConfig preserves desired config when executor apply fails", func(t *testing.T) {
		mockExec.ApplyErr = errors.New("chrony unavailable")
		defer func() { mockExec.ApplyErr = nil }()
		err := srv.UpdateConfig(ctx, &service.UpdateNTPConfigInput{
			Mode:    "ntp",
			Servers: []string{"retry.example.com"},
		})
		expectErrno(t, err, errno.CodeNTPSyncFailed)

		cfg, err := srv.GetConfig(ctx)
		if err != nil {
			t.Fatalf("GetConfig: %v", err)
		}
		if len(cfg.Servers) != 1 || cfg.Servers[0] != "retry.example.com" {
			t.Errorf("servers = %v, want [retry.example.com]", cfg.Servers)
		}
	})

	t.Run("UpdateConfig invalid mode", func(t *testing.T) {
		err := srv.UpdateConfig(ctx, &service.UpdateNTPConfigInput{
			Mode: "invalid_mode",
		})
		expectErrno(t, err, errno.CodeNTPInvalidMode)
	})

	t.Run("UpdateConfig rejects internal whitespace in servers", func(t *testing.T) {
		err := srv.UpdateConfig(ctx, &service.UpdateNTPConfigInput{
			Mode:    "ntp",
			Servers: []string{"ntp aliyun.com"},
		})
		expectErrno(t, err, errno.CodeInvalidParam)
	})

	t.Run("UpdateConfig ntp empty servers", func(t *testing.T) {
		err := srv.UpdateConfig(ctx, &service.UpdateNTPConfigInput{
			Mode:    "ntp",
			Servers: []string{"", "  "},
		})
		expectErrno(t, err, errno.CodeNTPServersEmpty)
	})

	t.Run("UpdateConfig rejects control characters in servers", func(t *testing.T) {
		err := srv.UpdateConfig(ctx, &service.UpdateNTPConfigInput{
			Mode:    "ntp",
			Servers: []string{"ntp.aliyun.com\nmakestep"},
		})
		expectErrno(t, err, errno.CodeInvalidParam)
	})

	t.Run("SyncNow in ntp mode", func(t *testing.T) {
		if err := srv.SyncNow(ctx); err != nil {
			t.Fatalf("SyncNow: %v", err)
		}
	})

	t.Run("Switch to manual and SetTime", func(t *testing.T) {
		// SetTime 应成功并自动切换为 manual
		targetTime := time.Date(2025, 8, 22, 12, 0, 0, 0, time.UTC)
		if err := srv.SetTime(ctx, &service.SetTimeInput{Time: targetTime}); err != nil {
			t.Fatalf("SetTime: %v", err)
		}

		// 验证模式已切换为 manual
		cfg, err := srv.GetConfig(ctx)
		if err != nil {
			t.Fatalf("GetConfig: %v", err)
		}
		if cfg.Mode != "manual" {
			t.Errorf("mode = %q, want manual", cfg.Mode)
		}

		// manual 模式下触发 NTP 同步应报 1202
		err = srv.SyncNow(ctx)
		expectErrno(t, err, errno.CodeNTPSyncNotAllowedInManualMode)
	})

	t.Run("GetStatus & IsSynced", func(t *testing.T) {
		status, err := srv.GetStatus(ctx)
		if err != nil {
			t.Fatalf("GetStatus: %v", err)
		}
		if status == nil {
			t.Fatal("GetStatus = nil, want non-nil")
		}

		synced, err := srv.IsSynced(ctx)
		if err != nil {
			t.Fatalf("IsSynced: %v", err)
		}
		if synced != status.Synced {
			t.Errorf("IsSynced = %v, want %v", synced, status.Synced)
		}
	})

	t.Run("ReplayOnBoot", func(t *testing.T) {
		if err := srv.ReplayOnBoot(ctx); err != nil {
			t.Fatalf("ReplayOnBoot: %v", err)
		}
	})

	t.Run("SetTime invalid zero time", func(t *testing.T) {
		err := srv.SetTime(ctx, &service.SetTimeInput{})
		expectErrno(t, err, errno.CodeInvalidParam)

		err = srv.SetTime(ctx, nil)
		expectErrno(t, err, errno.CodeInvalidParam)
	})

	t.Run("SetTime executor failure", func(t *testing.T) {
		mockExec.SetTimeErr = errors.New("no root permission")
		defer func() { mockExec.SetTimeErr = nil }()

		targetTime := time.Date(2025, 8, 22, 12, 0, 0, 0, time.UTC)
		err := srv.SetTime(ctx, &service.SetTimeInput{Time: targetTime})
		expectErrno(t, err, errno.CodeNTPSetTimeFailed)
	})
}

func TestNTPServiceSetTimePersistenceFailure(t *testing.T) {
	srv := service.NewNTPService(
		failingSystemConfigRepository{},
		ntp.NewMockExecutor(),
		zap.NewNop(),
	)

	err := srv.SetTime(context.Background(), &service.SetTimeInput{
		Time: time.Date(2025, 8, 22, 12, 0, 0, 0, time.UTC),
	})
	expectErrno(t, err, errno.CodeInternal)
}

type failingSystemConfigRepository struct{}

func (failingSystemConfigRepository) GetByKey(context.Context, string) (*model.SystemConfig, error) {
	return &model.SystemConfig{
		ID:    1,
		Value: `{"mode":"ntp","servers":["pool.ntp.org"]}`,
	}, nil
}

func (failingSystemConfigRepository) SetByKey(context.Context, string, string, string) error {
	return errors.New("database unavailable")
}
