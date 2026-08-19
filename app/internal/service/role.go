package service

import (
	"context"
	"errors"
	"strings"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
	"niko-vue-admin/app/internal/repository"
)

// SaveRoleInput 新增或修改角色输入。
type SaveRoleInput struct {
	Name   string `json:"name" binding:"required"`
	Code   string `json:"code" binding:"required"`
	Status *int8  `json:"status"` // 省略时新建默认启用；编辑省略表示不变
	Sort   int    `json:"sort"`
	Remark string `json:"remark"`
}

// FillModel 将输入字段填充到 model.Role 结构中。
func (in *SaveRoleInput) FillModel(m *model.Role) {
	m.Name = in.Name
	m.Code = in.Code
	if in.Status != nil {
		m.Status = *in.Status
	} else if m.ID == 0 {
		m.Status = model.StatusEnabled // 新建未指定 status 时默认启用
	}
	m.Sort = in.Sort
	m.Remark = in.Remark
}

// RolePageQuery 角色分页查询参数。
type RolePageQuery struct {
	Page     int    `form:"page"`
	PageSize int    `form:"pageSize"`
	Name     string `form:"name"`
	Code     string `form:"code"`
	Status   *int8  `form:"status"`
}

// RolePageResult 角色分页结果。
type RolePageResult struct {
	Items []model.Role `json:"items"`
	Total int64        `json:"total"`
}

// RoleService 角色服务接口。
type RoleService interface {
	GetPage(ctx context.Context, query *RolePageQuery) (*RolePageResult, error)
	CreateRole(ctx context.Context, input *SaveRoleInput) (*model.Role, error)
	UpdateRole(ctx context.Context, id uint64, input *SaveRoleInput) (*model.Role, error)
	DeleteRole(ctx context.Context, id uint64) error
	BatchDelete(ctx context.Context, ids []uint64) error
	GetMenuIDs(ctx context.Context, id uint64) ([]uint64, error)
	AssignMenus(ctx context.Context, id uint64, menuIDs []uint64) error
}

type roleService struct {
	roleRepo repository.RoleRepository
	menuRepo repository.MenuRepository
}

// NewRoleService 创建 RoleService 实例。
func NewRoleService(roleRepo repository.RoleRepository, menuRepo repository.MenuRepository) RoleService {
	return &roleService{roleRepo: roleRepo, menuRepo: menuRepo}
}

func isProtectedRole(role *model.Role) bool {
	return role.ID == model.BuiltinAdminRoleID ||
		role.Code == model.RoleSuperCode ||
		role.Code == model.RoleAdminCode
}

func (s *roleService) GetPage(ctx context.Context, query *RolePageQuery) (*RolePageResult, error) {
	filter := &repository.RoleFilter{
		Page:     query.Page,
		PageSize: query.PageSize,
		Name:     strings.TrimSpace(query.Name),
		Code:     strings.TrimSpace(query.Code),
		Status:   query.Status,
	}
	items, total, err := s.roleRepo.ListPage(ctx, filter)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []model.Role{}
	}
	return &RolePageResult{Items: items, Total: total}, nil
}

func (s *roleService) CreateRole(ctx context.Context, input *SaveRoleInput) (*model.Role, error) {
	if err := normalizeSaveRoleInput(input); err != nil {
		return nil, err
	}

	var role model.Role
	input.FillModel(&role)

	// 检查活动角色中是否已存在该 code
	if existing, err := s.roleRepo.GetByCode(ctx, role.Code); err == nil {
		if existing.ID != 0 {
			return nil, errno.NewError(errno.CodeRoleCodeTaken)
		}
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, mapRepoError(err)
	}

	if err := s.roleRepo.Create(ctx, &role); err != nil {
		return nil, mapRepoError(err)
	}
	return &role, nil
}

func (s *roleService) UpdateRole(ctx context.Context, id uint64, input *SaveRoleInput) (*model.Role, error) {
	if err := normalizeSaveRoleInput(input); err != nil {
		return nil, err
	}

	role, err := s.roleRepo.GetByID(ctx, id)
	if err != nil {
		return nil, mapRepoError(err)
	}

	// super 角色不可停用、不可修改 code（其余字段允许修改）。
	if role.Code == model.RoleSuperCode {
		if input.Status != nil && *input.Status == model.StatusDisabled {
			return nil, errno.NewError(errno.CodeSuperRoleProtected)
		}
		if input.Code != model.RoleSuperCode {
			return nil, errno.NewError(errno.CodeSuperRoleProtected)
		}
	}

	// 检查活动角色中是否已存在该 code（如果 code 变了）
	if role.Code != input.Code {
		if existing, err := s.roleRepo.GetByCode(ctx, input.Code); err == nil {
			if existing.ID != 0 && existing.ID != id {
				return nil, errno.NewError(errno.CodeRoleCodeTaken)
			}
		} else if !errors.Is(err, repository.ErrNotFound) {
			return nil, mapRepoError(err)
		}
	}

	input.FillModel(role)

	if err := s.roleRepo.Update(ctx, role); err != nil {
		return nil, mapRepoError(err)
	}
	return role, nil
}

func (s *roleService) DeleteRole(ctx context.Context, id uint64) error {
	role, err := s.roleRepo.GetByID(ctx, id)
	if err != nil {
		return mapRepoError(err)
	}
	if isProtectedRole(role) {
		return errno.NewError(errno.CodeSuperRoleProtected)
	}

	return s.roleRepo.Delete(ctx, id)
}

func (s *roleService) BatchDelete(ctx context.Context, ids []uint64) error {
	uniqueIDs, err := normalizeBatchIDs(ids)
	if err != nil {
		return err
	}

	roles, err := s.roleRepo.GetByIDs(ctx, uniqueIDs)
	if err != nil {
		return err
	}
	for _, r := range roles {
		if isProtectedRole(&r) {
			return errno.NewError(errno.CodeSuperRoleProtected)
		}
	}

	return s.roleRepo.BatchDelete(ctx, uniqueIDs)
}

func (s *roleService) GetMenuIDs(ctx context.Context, id uint64) ([]uint64, error) {
	if _, err := s.roleRepo.GetByID(ctx, id); err != nil {
		return nil, mapRepoError(err)
	}

	menuIDs, err := s.roleRepo.GetMenuIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	if menuIDs == nil {
		menuIDs = []uint64{}
	}
	return menuIDs, nil
}

func (s *roleService) AssignMenus(ctx context.Context, id uint64, menuIDs []uint64) error {
	role, err := s.roleRepo.GetByID(ctx, id)
	if err != nil {
		return mapRepoError(err)
	}
	// super 角色绕过 role_menus 直接放行，分配无意义且会覆盖 seed 全量绑定。
	if role.Code == model.RoleSuperCode {
		return errno.NewError(errno.CodeSuperRoleProtected)
	}

	uniqueIDs := dedupeIDs(menuIDs)

	// 菜单存在性校验（GetByIDs 自动排除软删）。
	menus, err := s.menuRepo.GetByIDs(ctx, uniqueIDs)
	if err != nil {
		return err
	}
	if len(menus) != len(uniqueIDs) {
		return errno.NewError(errno.CodeInvalidParam)
	}

	return s.roleRepo.ReplaceMenus(ctx, id, uniqueIDs)
}

// normalizeSaveRoleInput 去除 name/code 首尾空白并校验非空：
func normalizeSaveRoleInput(input *SaveRoleInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.Code = strings.TrimSpace(input.Code)
	if input.Name == "" || input.Code == "" {
		return errno.NewError(errno.CodeInvalidParam)
	}
	return nil
}

// normalizeBatchIDs 校验批量请求 ID 均为正数，并去除重复项。
func normalizeBatchIDs(ids []uint64) ([]uint64, error) {
	if len(ids) == 0 {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}
	for _, id := range ids {
		if id == 0 {
			return nil, errno.NewError(errno.CodeInvalidParam)
		}
	}
	return dedupeIDs(ids), nil
}

// dedupeIDs 去除 uint64 切片中的重复元素，保持首次出现顺序。
func dedupeIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	res := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		res = append(res, id)
	}
	return res
}
