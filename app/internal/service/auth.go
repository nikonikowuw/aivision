package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/config"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
)

// ClientInfo 包含客户端环境信息（UserAgent、IP）。
type ClientInfo struct {
	UserAgent string
	IP        string
}

// UserInfoDTO 对齐前端 vben UserInfo 契约。
type UserInfoDTO struct {
	UserID   string   `json:"userId"`
	Username string   `json:"username"`
	RealName string   `json:"realName"`
	Roles    []string `json:"roles"`
	Avatar   string   `json:"avatar"`
	Desc     string   `json:"desc"`
	HomePath string   `json:"homePath"`
}

// TokenPair 包含 AccessToken 和 RefreshToken。
type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// LoginResult 登录返回结果；refresh token 仅供服务端写入 HttpOnly Cookie，不序列化到 JSON。
type LoginResult struct {
	UserInfoDTO
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"-"`
}

// Authenticator 身份认证提供者（可扩展点设计：未来可接入 OAuth / LDAP 等）。
type Authenticator interface {
	Authenticate(ctx context.Context, username, password string) (*model.User, error)
}

// TokenIssuer 令牌签发器。
type TokenIssuer interface {
	IssueTokenPair(ctx context.Context, user *model.User, client ClientInfo) (*TokenPair, error)
	RotateRefreshToken(ctx context.Context, oldRefreshTokenStr string, client ClientInfo) (*TokenPair, error)
	RevokeRefreshToken(ctx context.Context, refreshTokenStr string) error
}

// LogoutOperator 描述登出操作人信息（用于审计日志）。
type LogoutOperator struct {
	UserID    uint64
	Username  string
	RoleIDs   []uint64
	RoleCodes []string
}

// AuthService 认证与令牌管理业务接口。
type AuthService interface {
	Login(ctx context.Context, username, password string, client ClientInfo) (*LoginResult, error)
	RefreshToken(ctx context.Context, refreshTokenStr string, client ClientInfo) (*TokenPair, error)
	Logout(ctx context.Context, refreshTokenStr string) (*LogoutOperator, error)
	GetUserInfo(ctx context.Context, userID uint64) (*UserInfoDTO, error)
	GetAccessCodes(ctx context.Context, roleCodes []string, roleIDs []uint64) ([]string, error)
}

type authService struct {
	authRepo repository.AuthRepository
	userRepo repository.UserRepository
	menuRepo repository.MenuRepository
	cfg      *config.Config
}

// NewAuthService 创建 AuthService 实例。
func NewAuthService(
	authRepo repository.AuthRepository,
	userRepo repository.UserRepository,
	menuRepo repository.MenuRepository,
	cfg *config.Config,
) AuthService {
	return &authService{
		authRepo: authRepo,
		userRepo: userRepo,
		menuRepo: menuRepo,
		cfg:      cfg,
	}
}

// Authenticate 校验用户名和密码（密码统一 bcrypt，防止时序与枚举攻击，失败统一 BadCredential）。
func (s *authService) Authenticate(ctx context.Context, username, password string) (*model.User, error) {
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeBadCredential)
		}
		return nil, err
	}

	if user.Status == model.StatusDisabled {
		return nil, errno.NewError(errno.CodeUserDisabled)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, errno.NewError(errno.CodeBadCredential)
	}

	return user, nil
}

// buildTokenPair 生成 access token 及待持久化的 refresh token 记录。
func (s *authService) buildTokenPair(user *model.User, client ClientInfo) (*TokenPair, *model.RefreshToken, error) {
	now := time.Now()
	accessClaims := jwt.RegisteredClaims{
		Subject:   strconv.FormatUint(user.ID, 10),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.JWT.AccessTTL)),
		IssuedAt:  jwt.NewNumericDate(now),
	}
	tokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessToken, err := tokenObj.SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return nil, nil, fmt.Errorf("sign access token: %w", err)
	}

	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, nil, fmt.Errorf("generate random refresh token: %w", err)
	}
	refreshTokenStr := hex.EncodeToString(randomBytes)

	return &TokenPair{
			AccessToken:  accessToken,
			RefreshToken: refreshTokenStr,
		}, &model.RefreshToken{
			UserID:    user.ID,
			Token:     refreshTokenStr,
			ExpiresAt: now.Add(s.cfg.JWT.RefreshTTL),
			Revoked:   false,
			UserAgent: client.UserAgent,
			IP:        client.IP,
		}, nil
}

// IssueTokenPair 签发 HS256 access token 与随机 hex refresh token，并持久化 refresh token。
func (s *authService) IssueTokenPair(ctx context.Context, user *model.User, client ClientInfo) (*TokenPair, error) {
	pair, refreshToken, err := s.buildTokenPair(user, client)
	if err != nil {
		return nil, err
	}
	if err := s.authRepo.CreateRefreshToken(ctx, refreshToken); err != nil {
		return nil, fmt.Errorf("create refresh token record: %w", err)
	}
	return pair, nil
}

// RotateRefreshToken 验证旧 refresh token 并以原子操作轮换新 token 对。
func (s *authService) RotateRefreshToken(ctx context.Context, oldRefreshTokenStr string, client ClientInfo) (*TokenPair, error) {
	if oldRefreshTokenStr == "" {
		return nil, errno.NewError(errno.CodeUnauthorized)
	}

	rt, err := s.authRepo.GetRefreshToken(ctx, oldRefreshTokenStr)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeUnauthorized)
		}
		return nil, err
	}

	if rt.Revoked || time.Now().After(rt.ExpiresAt) {
		return nil, errno.NewError(errno.CodeUnauthorized)
	}

	user, err := s.userRepo.GetByID(ctx, rt.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeUnauthorized)
		}
		return nil, err
	}
	if user.Status == model.StatusDisabled {
		return nil, errno.NewError(errno.CodeUserDisabled)
	}
	if _, err := s.getActiveIdentity(ctx, user.ID); err != nil {
		return nil, err
	}

	pair, newRefreshToken, err := s.buildTokenPair(user, client)
	if err != nil {
		return nil, err
	}
	consumed, err := s.authRepo.RotateRefreshToken(ctx, oldRefreshTokenStr, newRefreshToken)
	if err != nil {
		return nil, fmt.Errorf("rotate refresh token: %w", err)
	}
	if !consumed {
		return nil, errno.NewError(errno.CodeUnauthorized)
	}
	return pair, nil
}

// RevokeRefreshToken 吊销指定的 refresh token。
func (s *authService) RevokeRefreshToken(ctx context.Context, refreshTokenStr string) error {
	if refreshTokenStr == "" {
		return nil
	}
	return s.authRepo.RevokeRefreshToken(ctx, refreshTokenStr)
}

// Login 登录流程编排。
func (s *authService) Login(ctx context.Context, username, password string, client ClientInfo) (*LoginResult, error) {
	user, err := s.Authenticate(ctx, username, password)
	if err != nil {
		return nil, err
	}

	identity, err := s.getActiveIdentity(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	userInfo := buildUserInfo(user, identity)

	pair, err := s.IssueTokenPair(ctx, user, client)
	if err != nil {
		return nil, err
	}

	return &LoginResult{
		UserInfoDTO:  *userInfo,
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	}, nil
}

// RefreshToken 刷新 token 流程编排。
func (s *authService) RefreshToken(ctx context.Context, refreshTokenStr string, client ClientInfo) (*TokenPair, error) {
	return s.RotateRefreshToken(ctx, refreshTokenStr, client)
}

// Logout 退出登录流程编排：先查找关联用户身份（供审计日志记录），再吊销 refresh token。
func (s *authService) Logout(ctx context.Context, refreshTokenStr string) (*LogoutOperator, error) {
	if refreshTokenStr == "" {
		return nil, nil
	}

	var operator *LogoutOperator
	tokenRecord, err := s.authRepo.GetRefreshToken(ctx, refreshTokenStr)
	if err == nil && tokenRecord != nil && tokenRecord.UserID > 0 {
		identity, err := s.authRepo.GetActiveIdentity(ctx, tokenRecord.UserID)
		if err == nil && identity != nil {
			username, roleIDs, roleCodes := identity.ToIdentity(tokenRecord.UserID)
			operator = &LogoutOperator{
				UserID:    tokenRecord.UserID,
				Username:  username,
				RoleIDs:   roleIDs,
				RoleCodes: roleCodes,
			}
		}
	}

	if err := s.RevokeRefreshToken(ctx, refreshTokenStr); err != nil {
		return nil, err
	}
	return operator, nil
}

func (s *authService) getActiveIdentity(ctx context.Context, userID uint64) (*repository.AuthIdentity, error) {
	identity, err := s.authRepo.GetActiveIdentity(ctx, userID)
	if err != nil {
		return nil, err
	}
	if identity == nil || len(identity.Roles) == 0 {
		return nil, errno.NewError(errno.CodeUnauthorized)
	}
	return identity, nil
}

func buildUserInfo(user *model.User, identity *repository.AuthIdentity) *UserInfoDTO {
	roleCodes := make([]string, 0, len(identity.Roles))
	for _, role := range identity.Roles {
		roleCodes = append(roleCodes, role.Code)
	}

	return &UserInfoDTO{
		UserID:   strconv.FormatUint(user.ID, 10),
		Username: user.Username,
		RealName: user.Nickname,
		Roles:    roleCodes,
		Avatar:   user.Avatar,
		Desc:     user.Remark,
		HomePath: "",
	}
}

// GetUserInfo 获取用户信息（对齐 vben UserInfo）。
func (s *authService) GetUserInfo(ctx context.Context, userID uint64) (*UserInfoDTO, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errno.NewError(errno.CodeUnauthorized)
		}
		return nil, err
	}

	identity, err := s.getActiveIdentity(ctx, userID)
	if err != nil {
		return nil, err
	}

	return buildUserInfo(user, identity), nil
}

// GetAccessCodes 获取权限码列表。
func (s *authService) GetAccessCodes(ctx context.Context, roleCodes []string, roleIDs []uint64) ([]string, error) {
	if slices.Contains(roleCodes, model.RoleSuperCode) {
		return []string{"*"}, nil
	}

	if len(roleIDs) == 0 {
		return []string{}, nil
	}

	perms, err := s.menuRepo.GetPermissionsByRoleIDs(ctx, roleIDs)
	if err != nil {
		return nil, err
	}
	if perms == nil {
		return []string{}, nil
	}

	return perms, nil
}
