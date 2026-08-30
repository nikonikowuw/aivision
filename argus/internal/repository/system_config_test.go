package repository_test

import (
	"context"
	"testing"

	"argus/app/internal/model"
	"argus/app/internal/repository"
	"argus/app/internal/testutil"
)

func TestSystemConfigRepository(t *testing.T) {
	db := testutil.NewSmokeDB(t)
	repo := repository.NewSystemConfigRepository(db)
	ctx := context.Background()

	// 1. 获取不存在的配置
	cfg, err := repo.GetByKey(ctx, "non_existent_key")
	if err != nil {
		t.Fatalf("GetByKey(non_existent): %v", err)
	}
	if cfg != nil {
		t.Fatalf("GetByKey(non_existent) = %+v, want nil", cfg)
	}

	// 2. 创建配置
	if err := repo.SetByKey(ctx, model.ConfigKeyTime, `{"mode":"ntp","servers":["pool.ntp.org"]}`, "NTP Config"); err != nil {
		t.Fatalf("SetByKey(create): %v", err)
	}

	// 3. 获取刚刚创建的配置
	cfg, err = repo.GetByKey(ctx, model.ConfigKeyTime)
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if cfg == nil {
		t.Fatal("GetByKey = nil, want non-nil")
	}
	if cfg.Key != model.ConfigKeyTime {
		t.Errorf("key = %q, want %q", cfg.Key, model.ConfigKeyTime)
	}
	if cfg.Value != `{"mode":"ntp","servers":["pool.ntp.org"]}` {
		t.Errorf("value = %q, want the json string written", cfg.Value)
	}
	if cfg.Remark != "NTP Config" {
		t.Errorf("remark = %q, want NTP Config", cfg.Remark)
	}

	// 4. 更新已有配置（upsert）
	if err := repo.SetByKey(ctx, model.ConfigKeyTime, `{"mode":"manual"}`, "Manual Time Config"); err != nil {
		t.Fatalf("SetByKey(update): %v", err)
	}
	cfg, err = repo.GetByKey(ctx, model.ConfigKeyTime)
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if cfg == nil {
		t.Fatal("GetByKey = nil, want non-nil")
	}
	if cfg.Value != `{"mode":"manual"}` {
		t.Errorf("value = %q, want the updated json string", cfg.Value)
	}
	if cfg.Remark != "Manual Time Config" {
		t.Errorf("remark = %q, want Manual Time Config", cfg.Remark)
	}
}
