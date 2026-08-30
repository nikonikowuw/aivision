package service

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
)

const (
	// DefaultUserPassword 默认初始用户密码。
	DefaultUserPassword = "password123"
)

// UserPageQuery 描述用户分页查询参数。
type UserPageQuery struct {
	Page     int     `form:"page" binding:"omitempty,min=1"`
	PageSize int     `form:"pageSize" binding:"omitempty,min=1,max=100"`
	Username string  `form:"username"`
	Nickname string  `form:"nickname"`
	Status   *int8   `form:"status"`
	DeptID   *uint64 `form:"deptId"`
}

// UserPageResult 描述用户分页查询结果。
type UserPageResult struct {
	Items []*UserPageItem `json:"items"`
	Total int64           `json:"total"`
}

// UserPageItem 用户列表项，排除密码并包含部门名称。
type UserPageItem struct {
	model.User
	DeptName string `json:"deptName"`
}

// SaveUserInput 描述新增/修改用户的入参。
type SaveUserInput struct {
	Username string `json:"username" binding:"required,min=2,max=64"`
	Password string `json:"password" binding:"omitempty,min=6,max=32"` // 编辑时可空，新增时需处理默认密码
	Nickname string `json:"nickname" binding:"omitempty,max=64"`
	Email    string `json:"email" binding:"omitempty,email"`
	Phone    string `json:"phone" binding:"omitempty,max=32"`
	Status   *int8  `json:"status" binding:"omitempty,oneof=0 1"`
	Remark   string `json:"remark" binding:"omitempty,max=255"`
	DeptID   uint64 `json:"deptId" binding:"omitempty"`
}

// CurrentProfileDTO 描述个人中心资料。
type CurrentProfileDTO struct {
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Avatar   string `json:"avatar"`
	Remark   string `json:"remark"`
}

// UpdateCurrentProfileInput 描述修改个人资料入参。
type UpdateCurrentProfileInput struct {
	Nickname string `json:"nickname" binding:"omitempty,max=64"`
	Email    string `json:"email" binding:"omitempty,email"`
	Phone    string `json:"phone" binding:"omitempty,max=32"`
	Avatar   string `json:"avatar" binding:"omitempty,max=255"`
	Remark   string `json:"remark" binding:"omitempty,max=255"`
}

// ChangeCurrentPasswordInput 描述修改本人密码入参。
type ChangeCurrentPasswordInput struct {
	OldPassword string `json:"oldPassword" binding:"required,min=1"`
	NewPassword string `json:"newPassword" binding:"required,min=6,max=32"`
}

// UserService 封装用户管理的业务逻辑。
type UserService interface {
	GetPage(ctx context.Context, query *UserPageQuery) (*UserPageResult, error)
	CreateUser(ctx context.Context, input *SaveUserInput) (*model.User, error)
	UpdateUser(ctx context.Context, id uint64, input *SaveUserInput) (*model.User, error)
	DeleteUser(ctx context.Context, id uint64) error
	ResetPassword(ctx context.Context, id uint64, defaultPassword string) error
	GetRoleIDs(ctx context.Context, id uint64) ([]uint64, error)
	AssignRoles(ctx context.Context, id uint64, roleIDs []uint64) error
	UpdateStatus(ctx context.Context, id uint64, status int8) error
	BatchDelete(ctx context.Context, ids []uint64) error
	BatchUpdateStatus(ctx context.Context, ids []uint64, status int8) error
	GetCurrentProfile(ctx context.Context, userID uint64) (*CurrentProfileDTO, error)
	UpdateCurrentProfile(ctx context.Context, userID uint64, input *UpdateCurrentProfileInput) (*CurrentProfileDTO, error)
	ChangeCurrentPassword(ctx context.Context, userID uint64, input *ChangeCurrentPasswordInput) error
}

type userService struct {
	userRepo repository.UserRepository
	deptRepo repository.DepartmentRepository
	roleRepo repository.RoleRepository
}

// NewUserService 创建 UserService 实例。
func NewUserService(
	userRepo repository.UserRepository,
	deptRepo repository.DepartmentRepository,
	roleRepo repository.RoleRepository,
) UserService {
	return &userService{
		userRepo: userRepo,
		deptRepo: deptRepo,
		roleRepo: roleRepo,
	}
}

func hashPassword(pwd string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func normalizeSaveUserInput(input *SaveUserInput) {
	input.Username = strings.TrimSpace(input.Username)
	input.Nickname = strings.TrimSpace(input.Nickname)
	input.Email = strings.TrimSpace(input.Email)
	input.Phone = strings.TrimSpace(input.Phone)
	input.Remark = strings.TrimSpace(input.Remark)
}

func isProtectedAdminUser(user *model.User) bool {
	return user.ID == model.AdminUserID || user.Username == model.AdminUsername
}

// validateAvatarURL 校验头像地址：仅允许站内根相对路径或 http(s) 绝对 URL，
// 拒绝 javascript:/data: 等危险 scheme（该值会回显到页面 <img src>）。
func validateAvatarURL(avatar string) error {
	if avatar == "" {
		return nil
	}
	if strings.HasPrefix(avatar, "/") {
		if strings.HasPrefix(avatar, "//") || strings.Contains(avatar, "\\") {
			return errno.NewError(errno.CodeInvalidParam)
		}
		for _, r := range avatar {
			if r < 0x20 || r == 0x7f {
				return errno.NewError(errno.CodeInvalidParam)
			}
		}
		return nil
	}
	parsed, err := url.Parse(avatar)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errno.NewError(errno.CodeInvalidParam)
	}
	return nil
}

func (s *userService) GetPage(ctx context.Context, query *UserPageQuery) (*UserPageResult, error) {
	filter := &repository.UserFilter{
		Page:     query.Page,
		PageSize: query.PageSize,
		Username: strings.TrimSpace(query.Username),
		Nickname: strings.TrimSpace(query.Nickname),
		Status:   query.Status,
		DeptID:   query.DeptID,
	}

	users, total, err := s.userRepo.ListPage(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(users) == 0 {
		return &UserPageResult{
			Items: []*UserPageItem{},
			Total: total,
		}, nil
	}

	// 收集当前页关联的部门 ID 进行批量查询
	deptIDs := make([]uint64, 0, len(users))
	seenDept := make(map[uint64]struct{}, len(users))
	for _, u := range users {
		if u.DeptID > 0 {
			if _, ok := seenDept[u.DeptID]; !ok {
				seenDept[u.DeptID] = struct{}{}
				deptIDs = append(deptIDs, u.DeptID)
			}
		}
	}

	deptMap := make(map[uint64]string, len(deptIDs))
	if len(deptIDs) > 0 {
		depts, err := s.deptRepo.GetByIDs(ctx, deptIDs)
		if err != nil {
			return nil, err
		}
		for _, d := range depts {
			deptMap[d.ID] = d.Name
		}
	}

	items := make([]*UserPageItem, len(users))
	for i, u := range users {
		items[i] = &UserPageItem{
			User:     u,
			DeptName: deptMap[u.DeptID],
		}
	}

	return &UserPageResult{
		Items: items,
		Total: total,
	}, nil
}

func (s *userService) CreateUser(ctx context.Context, input *SaveUserInput) (*model.User, error) {
	normalizeSaveUserInput(input)
	if input.Username == "" {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}

	// 检查活动用户中是否已存在该用户名
	if existing, err := s.userRepo.GetByUsername(ctx, input.Username); err == nil {
		if existing.ID != 0 {
			return nil, errno.NewError(errno.CodeUsernameTaken)
		}
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, mapRepoError(err)
	}

	// 校验部门存在性
	if input.DeptID > 0 {
		if _, err := s.deptRepo.GetByID(ctx, input.DeptID); err != nil {
			return nil, mapRepoError(err)
		}
	}

	pwd := input.Password
	if pwd == "" {
		pwd = DefaultUserPassword
	}
	hashed, err := hashPassword(pwd)
	if err != nil {
		return nil, err
	}

	status := model.StatusEnabled
	if input.Status != nil {
		status = *input.Status
	}

	u := &model.User{
		Username: input.Username,
		Password: hashed,
		Nickname: input.Nickname,
		Email:    input.Email,
		Phone:    input.Phone,
		Status:   status,
		Remark:   input.Remark,
		DeptID:   input.DeptID,
	}

	if err := s.userRepo.Create(ctx, u); err != nil {
		return nil, mapRepoError(err)
	}
	return u, nil
}

func (s *userService) UpdateUser(ctx context.Context, id uint64, input *SaveUserInput) (*model.User, error) {
	normalizeSaveUserInput(input)
	if input.Username == "" {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}

	u, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, mapRepoError(err)
	}

	// admin 账号保护：不可修改用户名，不可被禁用
	if isProtectedAdminUser(u) {
		if input.Username != model.AdminUsername {
			return nil, errno.NewError(errno.CodeAdminUserProtected)
		}
		if input.Status != nil && *input.Status == model.StatusDisabled {
			return nil, errno.NewError(errno.CodeAdminUserProtected)
		}
	}

	// 检查活动用户中是否已存在该用户名
	if input.Username != u.Username {
		if existing, err := s.userRepo.GetByUsername(ctx, input.Username); err == nil {
			if existing.ID != 0 && existing.ID != id {
				return nil, errno.NewError(errno.CodeUsernameTaken)
			}
		} else if !errors.Is(err, repository.ErrNotFound) {
			return nil, mapRepoError(err)
		}
	}

	// 校验部门存在性
	if input.DeptID > 0 {
		if _, err := s.deptRepo.GetByID(ctx, input.DeptID); err != nil {
			return nil, mapRepoError(err)
		}
	}

	updates := map[string]interface{}{
		"username": input.Username,
		"nickname": input.Nickname,
		"email":    input.Email,
		"phone":    input.Phone,
		"remark":   input.Remark,
		"dept_id":  input.DeptID,
	}

	u.Username = input.Username
	u.Nickname = input.Nickname
	u.Email = input.Email
	u.Phone = input.Phone
	u.Remark = input.Remark
	u.DeptID = input.DeptID

	if input.Status != nil {
		updates["status"] = *input.Status
		u.Status = *input.Status
	}

	if input.Password != "" {
		hashed, err := hashPassword(input.Password)
		if err != nil {
			return nil, err
		}
		updates["password"] = hashed
		u.Password = hashed
	}

	if err := s.userRepo.Update(ctx, u, updates); err != nil {
		return nil, mapRepoError(err)
	}

	return u, nil
}

func (s *userService) DeleteUser(ctx context.Context, id uint64) error {
	u, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return mapRepoError(err)
	}
	if isProtectedAdminUser(u) {
		return errno.NewError(errno.CodeAdminUserProtected) // admin 自身不可删除
	}

	return s.userRepo.Delete(ctx, id)
}

func (s *userService) ResetPassword(ctx context.Context, id uint64, defaultPassword string) error {
	u, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return mapRepoError(err)
	}

	if defaultPassword == "" {
		defaultPassword = DefaultUserPassword
	}

	hashed, err := hashPassword(defaultPassword)
	if err != nil {
		return err
	}

	return s.userRepo.Update(ctx, u, map[string]interface{}{
		"password": hashed,
	})
}

func (s *userService) GetRoleIDs(ctx context.Context, id uint64) ([]uint64, error) {
	if _, err := s.userRepo.GetByID(ctx, id); err != nil {
		return nil, mapRepoError(err)
	}

	roleIDs, err := s.userRepo.GetRoleIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	if roleIDs == nil {
		roleIDs = []uint64{}
	}
	return roleIDs, nil
}

func (s *userService) AssignRoles(ctx context.Context, id uint64, roleIDs []uint64) error {
	if _, err := s.userRepo.GetByID(ctx, id); err != nil {
		return mapRepoError(err)
	}

	uniqueIDs := dedupeIDs(roleIDs)

	if len(uniqueIDs) > 0 {
		roles, err := s.roleRepo.GetByIDs(ctx, uniqueIDs)
		if err != nil {
			return err
		}
		if len(roles) != len(uniqueIDs) {
			return errno.NewError(errno.CodeNotFound)
		}
	}

	return s.userRepo.ReplaceRoles(ctx, id, uniqueIDs)
}

func (s *userService) UpdateStatus(ctx context.Context, id uint64, status int8) error {
	if status != model.StatusEnabled && status != model.StatusDisabled {
		return errno.NewError(errno.CodeInvalidParam)
	}

	u, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return mapRepoError(err)
	}

	if isProtectedAdminUser(u) && status == model.StatusDisabled {
		return errno.NewError(errno.CodeAdminUserProtected) // admin 不可禁用
	}

	return s.userRepo.Update(ctx, u, map[string]interface{}{
		"status": status,
	})
}

func (s *userService) BatchDelete(ctx context.Context, ids []uint64) error {
	uniqueIDs, err := normalizeBatchIDs(ids)
	if err != nil {
		return err
	}

	// 检查是否包含受保护的 admin 用户。
	for _, id := range uniqueIDs {
		u, err := s.userRepo.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				continue
			}
			return mapRepoError(err)
		}
		if isProtectedAdminUser(u) {
			return errno.NewError(errno.CodeAdminUserProtected)
		}
	}

	return s.userRepo.BatchDelete(ctx, uniqueIDs)
}

func (s *userService) BatchUpdateStatus(ctx context.Context, ids []uint64, status int8) error {
	if status != model.StatusEnabled && status != model.StatusDisabled {
		return errno.NewError(errno.CodeInvalidParam)
	}
	uniqueIDs, err := normalizeBatchIDs(ids)
	if err != nil {
		return err
	}

	if status == model.StatusDisabled {
		for _, id := range uniqueIDs {
			u, err := s.userRepo.GetByID(ctx, id)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					continue
				}
				return mapRepoError(err)
			}
			if isProtectedAdminUser(u) {
				return errno.NewError(errno.CodeAdminUserProtected)
			}
		}
	}

	return s.userRepo.BatchUpdateStatus(ctx, uniqueIDs, status)
}

func userToProfileDTO(u *model.User) *CurrentProfileDTO {
	return &CurrentProfileDTO{
		Username: u.Username,
		Nickname: u.Nickname,
		Email:    u.Email,
		Phone:    u.Phone,
		Avatar:   u.Avatar,
		Remark:   u.Remark,
	}
}

func (s *userService) GetCurrentProfile(ctx context.Context, userID uint64) (*CurrentProfileDTO, error) {
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	return userToProfileDTO(u), nil
}

func (s *userService) UpdateCurrentProfile(ctx context.Context, userID uint64, input *UpdateCurrentProfileInput) (*CurrentProfileDTO, error) {
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, mapRepoError(err)
	}

	nickname := strings.TrimSpace(input.Nickname)
	email := strings.TrimSpace(input.Email)
	phone := strings.TrimSpace(input.Phone)
	avatar := strings.TrimSpace(input.Avatar)
	remark := strings.TrimSpace(input.Remark)

	if err := validateAvatarURL(avatar); err != nil {
		return nil, err
	}

	updates := map[string]interface{}{
		"nickname": nickname,
		"email":    email,
		"phone":    phone,
		"avatar":   avatar,
		"remark":   remark,
	}

	u.Nickname = nickname
	u.Email = email
	u.Phone = phone
	u.Avatar = avatar
	u.Remark = remark

	if err := s.userRepo.Update(ctx, u, updates); err != nil {
		return nil, mapRepoError(err)
	}
	return userToProfileDTO(u), nil
}

func (s *userService) ChangeCurrentPassword(ctx context.Context, userID uint64, input *ChangeCurrentPasswordInput) error {
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return mapRepoError(err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(input.OldPassword)); err != nil {
		return errno.NewError(errno.CodeWrongOldPassword)
	}

	hashed, err := hashPassword(input.NewPassword)
	if err != nil {
		return err
	}

	return mapRepoError(s.userRepo.ChangePasswordAndRevokeSessions(ctx, userID, hashed))
}
