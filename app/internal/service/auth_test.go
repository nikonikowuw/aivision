package service_test

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
	"niko-vue-admin/app/internal/service"
)

func setupAuthServiceTest(t *testing.T) (service.AuthService, *gorm.DB) {
	t.Helper()
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	cfg := &config.Config{
		JWT: config.JWT{
			Secret:     "auth-test-secret-key-123456",
			AccessTTL:  time.Hour,
			RefreshTTL: 7 * 24 * time.Hour,
		},
	}

	authRepo := repository.NewAuthRepository(db)
	menuRepo := repository.NewMenuRepository(db)
	svc := service.NewAuthService(authRepo, repository.NewUserRepository(db), menuRepo, cfg)

	return svc, db
}

func TestAuthServiceLoginSuccess(t *testing.T) {
	svc, db := setupAuthServiceTest(t)
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	user := model.User{
		Username: "admin",
		Password: string(hash),
		Nickname: "超级管理员",
		Status:   model.StatusEnabled,
		Avatar:   "avatar.png",
		Remark:   "admin desc",
	}
	db.Create(&user)
	role := model.Role{
		Name:   "超级管理员",
		Code:   model.RoleSuperCode,
		Status: model.StatusEnabled,
	}
	db.Create(&role)
	db.Create(&model.UserRole{UserID: user.ID, RoleID: role.ID})

	client := service.ClientInfo{UserAgent: "test-agent", IP: "127.0.0.1"}
	res, err := svc.Login(ctx, "admin", "admin123", client)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatalf("Tokens should not be empty, got access: %s, refresh: %s", res.AccessToken, res.RefreshToken)
	}
	if res.Username != "admin" || res.RealName != "超级管理员" || len(res.Roles) != 1 || res.Roles[0] != model.RoleSuperCode {
		t.Errorf("Unexpected user info in login result: %+v", res.UserInfoDTO)
	}

	// 验证 refresh token 已落库
	var rt model.RefreshToken
	if err := db.Where("token = ?", res.RefreshToken).First(&rt).Error; err != nil {
		t.Fatalf("refresh token not found in db: %v", err)
	}
	if rt.UserID != user.ID || rt.Revoked || rt.UserAgent != "test-agent" || rt.IP != "127.0.0.1" {
		t.Errorf("Unexpected rt record: %+v", rt)
	}
}

func TestAuthServiceLoginFailures(t *testing.T) {
	svc, db := setupAuthServiceTest(t)
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	enabledUser := model.User{Username: "alice", Password: string(hash), Status: model.StatusEnabled}
	db.Create(&enabledUser)

	disabledUser := model.User{Username: "bob", Password: string(hash), Status: model.StatusDisabled}
	db.Create(&disabledUser)
	db.Model(&model.User{}).Where("id = ?", disabledUser.ID).Update("status", model.StatusDisabled)

	client := service.ClientInfo{}

	// 1. 用户不存在 -> 1001 BadCredential
	_, err := svc.Login(ctx, "nonexistent", "admin123", client)
	t.Logf("err 1 = %v (%T)", err, err)
	if e, ok := err.(*errno.Error); !ok || e.Code != errno.CodeBadCredential {
		t.Fatalf("expected CodeBadCredential, got %v", err)
	}

	// 2. 密码错误 -> 1001 BadCredential
	_, err = svc.Login(ctx, "alice", "wrongpassword", client)
	t.Logf("err 2 = %v (%T)", err, err)
	if e, ok := err.(*errno.Error); !ok || e.Code != errno.CodeBadCredential {
		t.Fatalf("expected CodeBadCredential, got %v", err)
	}

	// 3. 启用但无角色的用户不能建立会话，也不应留下 refresh token
	_, err = svc.Login(ctx, "alice", "admin123", client)
	if e, ok := err.(*errno.Error); !ok || e.Code != errno.CodeUnauthorized {
		t.Fatalf("expected CodeUnauthorized for roleless user, got %v", err)
	}
	var refreshCount int64
	db.Model(&model.RefreshToken{}).Where("user_id = ?", enabledUser.ID).Count(&refreshCount)
	if refreshCount != 0 {
		t.Fatalf("roleless login created %d refresh tokens, want 0", refreshCount)
	}

	// 4. 用户禁用 -> 1008 UserDisabled
	_, err = svc.Login(ctx, "bob", "admin123", client)
	t.Logf("err 3 = %v (%T)", err, err)
	if e, ok := err.(*errno.Error); !ok || e.Code != errno.CodeUserDisabled {
		t.Fatalf("expected CodeUserDisabled, got %v", err)
	}
}

func TestAuthServiceRefreshTokenRotation(t *testing.T) {
	svc, db := setupAuthServiceTest(t)
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	user := model.User{Username: "alice", Password: string(hash), Status: model.StatusEnabled}
	db.Create(&user)
	role := model.Role{Name: "Normal", Code: "normal", Status: model.StatusEnabled}
	db.Create(&role)
	db.Create(&model.UserRole{UserID: user.ID, RoleID: role.ID})

	client := service.ClientInfo{UserAgent: "agent-1", IP: "10.0.0.1"}
	loginRes, err := svc.Login(ctx, "alice", "admin123", client)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	oldRT := loginRes.RefreshToken

	// 1. 正常刷新
	client2 := service.ClientInfo{UserAgent: "agent-2", IP: "10.0.0.2"}
	rotateRes, err := svc.RefreshToken(ctx, oldRT, client2)
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if rotateRes.AccessToken == "" || rotateRes.RefreshToken == "" || rotateRes.RefreshToken == oldRT {
		t.Fatalf("Expected new token pair, got %+v", rotateRes)
	}

	// 验证旧 refresh token 被 revoke
	var oldRecord model.RefreshToken
	db.Where("token = ?", oldRT).First(&oldRecord)
	if !oldRecord.Revoked {
		t.Errorf("Old refresh token was not revoked")
	}

	// 2. 使用已 revoke 的旧 refresh token 再次刷新 -> 401 Unauthorized
	_, err = svc.RefreshToken(ctx, oldRT, client2)
	if e, ok := err.(*errno.Error); !ok || e.Code != errno.CodeUnauthorized {
		t.Fatalf("expected CodeUnauthorized on revoked refresh token, got %v", err)
	}

	// 3. 禁用用户后刷新 -> 1008 UserDisabled
	db.Model(&model.User{}).Where("id = ?", user.ID).Update("status", model.StatusDisabled)
	_, err = svc.RefreshToken(ctx, rotateRes.RefreshToken, client2)
	if e, ok := err.(*errno.Error); !ok || e.Code != errno.CodeUserDisabled {
		t.Fatalf("expected CodeUserDisabled on disabled user refresh, got %v", err)
	}
}

func TestAuthServiceRefreshTokenRotationIsSingleUse(t *testing.T) {
	svc, db := setupAuthServiceTest(t)
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	user := model.User{Username: "concurrent", Password: string(hash), Status: model.StatusEnabled}
	db.Create(&user)
	role := model.Role{Name: "Normal", Code: "normal", Status: model.StatusEnabled}
	db.Create(&role)
	db.Create(&model.UserRole{UserID: user.ID, RoleID: role.ID})

	loginRes, err := svc.Login(ctx, "concurrent", "admin123", service.ClientInfo{})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := svc.RefreshToken(ctx, loginRes.RefreshToken, service.ClientInfo{})
			results <- err
		}()
	}

	successes := 0
	unauthorized := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		businessErr, ok := err.(*errno.Error)
		if ok && businessErr.Code == errno.CodeUnauthorized {
			unauthorized++
			continue
		}
		t.Errorf("unexpected concurrent refresh error: %v", err)
	}

	if successes != 1 || unauthorized != 1 {
		t.Fatalf("concurrent refresh results = successes:%d unauthorized:%d, want 1/1", successes, unauthorized)
	}

	var oldToken model.RefreshToken
	if err := db.Where("token = ?", loginRes.RefreshToken).First(&oldToken).Error; err != nil {
		t.Fatalf("find old refresh token: %v", err)
	}
	if !oldToken.Revoked {
		t.Fatal("old refresh token should be revoked")
	}
}

func TestAuthServiceLogout(t *testing.T) {
	svc, db := setupAuthServiceTest(t)
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	user := model.User{Username: "alice", Password: string(hash), Status: model.StatusEnabled}
	db.Create(&user)
	role := model.Role{Name: "Normal", Code: "normal", Status: model.StatusEnabled}
	db.Create(&role)
	db.Create(&model.UserRole{UserID: user.ID, RoleID: role.ID})

	client := service.ClientInfo{}
	loginRes, err := svc.Login(ctx, "alice", "admin123", client)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if err := svc.Logout(ctx, loginRes.RefreshToken); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	var rt model.RefreshToken
	db.Where("token = ?", loginRes.RefreshToken).First(&rt)
	if !rt.Revoked {
		t.Fatalf("Refresh token should be revoked after logout")
	}
}

func TestAuthServiceGetAccessCodes(t *testing.T) {
	svc, db := setupAuthServiceTest(t)
	ctx := context.Background()

	roleNormal := model.Role{Name: "Normal", Code: "normal", Status: model.StatusEnabled}
	db.Create(&roleNormal)

	menu1 := model.Menu{Name: "UserAdd", Permission: "system:user:add", Status: model.StatusEnabled}
	menu2 := model.Menu{Name: "UserEdit", Permission: "system:user:edit", Status: model.StatusEnabled}
	db.Create(&menu1)
	db.Create(&menu2)
	db.Create(&model.RoleMenu{RoleID: roleNormal.ID, MenuID: menu1.ID})
	db.Create(&model.RoleMenu{RoleID: roleNormal.ID, MenuID: menu2.ID})

	// 1. Super 角色返回 ["*"]
	superCodes, err := svc.GetAccessCodes(ctx, []string{model.RoleSuperCode}, []uint64{})
	if err != nil {
		t.Fatalf("GetAccessCodes for super failed: %v", err)
	}
	if len(superCodes) != 1 || superCodes[0] != "*" {
		t.Errorf("Expected ['*'], got %v", superCodes)
	}

	// 2. 普通角色返回对应权限码列表
	codes, err := svc.GetAccessCodes(ctx, []string{"normal"}, []uint64{roleNormal.ID})
	if err != nil {
		t.Fatalf("GetAccessCodes for normal failed: %v", err)
	}
	if len(codes) != 2 {
		t.Errorf("Expected 2 codes, got %v", codes)
	}
}
