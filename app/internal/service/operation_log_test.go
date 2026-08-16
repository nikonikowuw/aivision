package service_test

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/service"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.OperationLog{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func TestOperationLogService(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewOperationLogRepository(db)
	srv := service.NewOperationLogService(repo)
	ctx := context.Background()

	now := time.Now()

	// 1. 插入多条测试记录
	logs := []model.OperationLog{
		{
			CreatedAt:  now.Add(-2 * time.Hour),
			UserID:     1,
			Username:   "admin",
			Module:     "auth",
			Action:     "POST /api/auth/login",
			Method:     "POST",
			Path:       "/api/auth/login",
			StatusCode: 200,
			DurationMs: 15,
			IP:         "127.0.0.1",
			UserAgent:  "curl/7.0",
		},
		{
			CreatedAt:  now.Add(-1 * time.Hour),
			UserID:     1,
			Username:   "admin",
			Module:     "menu",
			Action:     "POST /api/menu",
			Method:     "POST",
			Path:       "/api/menu",
			StatusCode: 200,
			DurationMs: 25,
			IP:         "127.0.0.1",
			UserAgent:  "curl/7.0",
		},
		{
			CreatedAt:  now,
			UserID:     2,
			Username:   "testuser",
			Module:     "auth",
			Action:     "POST /api/auth/login",
			Method:     "POST",
			Path:       "/api/auth/login",
			StatusCode: 401,
			DurationMs: 10,
			IP:         "127.0.0.1",
			UserAgent:  "curl/7.0",
		},
	}

	for i := range logs {
		if err := srv.Record(ctx, &logs[i]); err != nil {
			t.Fatalf("failed to record log %d: %v", i, err)
		}
	}

	// 2. 分页全量查询
	pageRes, err := srv.GetPage(ctx, &service.LogPageQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("GetPage failed: %v", err)
	}
	if pageRes.Total != 3 {
		t.Errorf("expected total 3, got %d", pageRes.Total)
	}
	if len(pageRes.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(pageRes.Items))
	}

	// 3. 筛选 module = "auth"
	authRes, err := srv.GetPage(ctx, &service.LogPageQuery{Module: "auth"})
	if err != nil {
		t.Fatalf("GetPage with module failed: %v", err)
	}
	if authRes.Total != 2 {
		t.Errorf("expected total 2 for module=auth, got %d", authRes.Total)
	}

	// 4. 筛选 statusCode = 401
	statusRes, err := srv.GetPage(ctx, &service.LogPageQuery{StatusCode: 401})
	if err != nil {
		t.Fatalf("GetPage with statusCode failed: %v", err)
	}
	if statusRes.Total != 1 {
		t.Errorf("expected total 1 for statusCode=401, got %d", statusRes.Total)
	}

	// 5. 筛选 username 模糊匹配
	userRes, err := srv.GetPage(ctx, &service.LogPageQuery{Username: "adm"})
	if err != nil {
		t.Fatalf("GetPage with username failed: %v", err)
	}
	if userRes.Total != 2 {
		t.Errorf("expected total 2 for username=adm, got %d", userRes.Total)
	}

	// 6. 按 ID 查询单条
	log1, err := srv.GetByID(ctx, logs[0].ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if log1.Username != "admin" || log1.Module != "auth" {
		t.Errorf("unexpected log data: %+v", log1)
	}

	// 7. 查询不存在的 ID
	_, err = srv.GetByID(ctx, 99999)
	if err == nil {
		t.Fatal("expected error for non-existent ID, got nil")
	}
	var e *errno.Error
	if !errnoIs(err, errno.CodeNotFound) {
		t.Errorf("expected CodeNotFound, got %v", e)
	}
}

func errnoIs(err error, code int) bool {
	if e, ok := err.(*errno.Error); ok {
		return e.Code == code
	}
	return false
}
