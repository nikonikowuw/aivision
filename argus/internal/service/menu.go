package service

import (
	"context"
	"errors"
	"slices"

	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
)

// VbenRouteMeta 对齐 vben 路由 meta 结构。
type VbenRouteMeta struct {
	Title     string `json:"title"`
	Icon      string `json:"icon,omitempty"`
	Order     int    `json:"order"`
	AffixTab  bool   `json:"affixTab,omitempty"`
	KeepAlive bool   `json:"keepAlive,omitempty"`
}

// VbenRouteRecord 对齐 vben 动态路由结构 (RouteRecordStringComponent)。
type VbenRouteRecord struct {
	ID        uint64             `json:"id"`
	PID       uint64             `json:"pid"`
	Name      string             `json:"name"`
	Path      string             `json:"path"`
	Component string             `json:"component"`
	Type      string             `json:"type"`
	Meta      VbenRouteMeta      `json:"meta"`
	Children  []*VbenRouteRecord `json:"children,omitempty"`
}

// SaveMenuInput 新增或修改菜单输入。
type SaveMenuInput struct {
	ParentID   uint64 `json:"parentId"`
	Type       string `json:"type" binding:"required,oneof=catalog menu button"`
	Name       string `json:"name" binding:"required"`
	Title      string `json:"title"`
	Path       string `json:"path"`
	Component  string `json:"component"`
	Icon       string `json:"icon"`
	Sort       int    `json:"sort"`
	Status     *int8  `json:"status"`
	Permission string `json:"permission"`
	Affix      bool   `json:"affix"`
	KeepAlive  bool   `json:"keepAlive"`
	HomePath   string `json:"homePath"`
}

// FillModel 将输入字段填充到 model.Menu 结构中。
func (in *SaveMenuInput) FillModel(m *model.Menu) {
	m.ParentID = in.ParentID
	m.Type = in.Type
	m.Name = in.Name
	m.Title = in.Title
	m.Path = in.Path
	component := in.Component
	if in.Type == model.MenuTypeCatalog && in.ParentID == 0 {
		component = model.MenuComponentBasicLayout
	}
	m.Component = component
	m.Icon = in.Icon
	m.Sort = in.Sort
	if in.Status != nil {
		m.Status = *in.Status
	} else if m.ID == 0 {
		m.Status = model.StatusEnabled // 新建未指定 status 时默认启用
	}
	m.Permission = in.Permission
	m.Affix = in.Affix
	m.KeepAlive = in.KeepAlive
	m.HomePath = in.HomePath
}

// MenuService 菜单服务接口。
type MenuService interface {
	GetMenuTree(ctx context.Context) ([]*model.MenuTreeNode, error)
	GetUserMenuTree(ctx context.Context, roleCodes []string, roleIDs []uint64) ([]*VbenRouteRecord, error)
	CreateMenu(ctx context.Context, input *SaveMenuInput) (*model.Menu, error)
	UpdateMenu(ctx context.Context, id uint64, input *SaveMenuInput) (*model.Menu, error)
	DeleteMenu(ctx context.Context, id uint64) error
}

type menuService struct {
	repo repository.MenuRepository
}

// NewMenuService 创建 MenuService 实例。
func NewMenuService(repo repository.MenuRepository) MenuService {
	return &menuService{repo: repo}
}

func (s *menuService) GetMenuTree(ctx context.Context) ([]*model.MenuTreeNode, error) {
	menus, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	tree := model.BuildMenuTree(menus)
	if tree == nil {
		return []*model.MenuTreeNode{}, nil
	}
	return tree, nil
}

func (s *menuService) GetUserMenuTree(ctx context.Context, roleCodes []string, roleIDs []uint64) ([]*VbenRouteRecord, error) {
	var menus []model.Menu
	var err error

	isSuper := slices.Contains(roleCodes, model.RoleSuperCode)

	if isSuper {
		menus, err = s.repo.ListAll(ctx)
	} else {
		menuIDs, errGetIDs := s.repo.GetMenuIDsByRoleIDs(ctx, roleIDs)
		if errGetIDs != nil {
			return nil, errGetIDs
		}
		if len(menuIDs) == 0 {
			return []*VbenRouteRecord{}, nil
		}
		menus, err = s.repo.GetByIDs(ctx, menuIDs)
	}
	if err != nil {
		return nil, err
	}

	// 1. 过滤：仅保留启用状态且非 button 节点 (catalog + menu)
	filtered := make([]model.Menu, 0, len(menus))
	for _, m := range menus {
		if m.Status == model.StatusEnabled && m.Type != model.MenuTypeButton {
			filtered = append(filtered, m)
		}
	}

	// 2. 构建 model 树
	tree := model.BuildMenuTree(filtered)

	// 3. 转换为 vben 路由格式
	return ConvertToVbenRoutes(tree), nil
}

func (s *menuService) CreateMenu(ctx context.Context, input *SaveMenuInput) (*model.Menu, error) {
	if input.ParentID != 0 {
		if _, err := s.repo.GetByID(ctx, input.ParentID); err != nil {
			return nil, mapRepoError(err)
		}
	}

	var menu model.Menu
	input.FillModel(&menu)

	if err := s.repo.Create(ctx, &menu); err != nil {
		return nil, err
	}
	return &menu, nil
}

func (s *menuService) UpdateMenu(ctx context.Context, id uint64, input *SaveMenuInput) (*model.Menu, error) {
	menu, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapRepoError(err)
	}

	if input.ParentID != 0 {
		if input.ParentID == id {
			return nil, errno.NewError(errno.CodeParentIsSelf)
		}
		if _, err := s.repo.GetByID(ctx, input.ParentID); err != nil {
			return nil, mapRepoError(err)
		}
		menus, err := s.repo.ListAll(ctx)
		if err != nil {
			return nil, err
		}
		if model.IsMenuDescendant(menus, id, input.ParentID) {
			return nil, errno.NewError(errno.CodeParentIsDescendant)
		}
	}

	input.FillModel(menu)

	if err := s.repo.Update(ctx, menu); err != nil {
		return nil, err
	}
	return menu, nil
}

func (s *menuService) DeleteMenu(ctx context.Context, id uint64) error {
	count, err := s.repo.CountByParentID(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return errno.NewError(errno.CodeMenuHasChildren)
	}
	deleted, err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return errno.NewError(errno.CodeNotFound)
	}
	return nil
}

// mapRepoError 把 repository 层错误映射为业务错误码：
// repository.ErrNotFound → CodeNotFound；其他保持原样（由统一错误处理中间件兜底为内部错误）。
func mapRepoError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errno.NewError(errno.CodeNotFound)
	}
	return err
}

// ConvertToVbenRoutes 将 model.MenuTreeNode 树转换为 VbenRouteRecord 数组（纯函数，可单测）。
func ConvertToVbenRoutes(nodes []*model.MenuTreeNode) []*VbenRouteRecord {
	if len(nodes) == 0 {
		return []*VbenRouteRecord{}
	}
	res := make([]*VbenRouteRecord, 0, len(nodes))
	for _, node := range nodes {
		component := node.Component
		if node.Type == model.MenuTypeCatalog && node.ParentID == 0 {
			component = model.MenuComponentBasicLayout
		}
		rec := &VbenRouteRecord{
			ID:        node.ID,
			PID:       node.ParentID,
			Name:      node.Name,
			Path:      node.Path,
			Component: component,
			Type:      node.Type,
			Meta: VbenRouteMeta{
				Title:     node.Title,
				Icon:      node.Icon,
				Order:     node.Sort,
				AffixTab:  node.Affix,
				KeepAlive: node.KeepAlive,
			},
		}
		if len(node.Children) > 0 {
			rec.Children = ConvertToVbenRoutes(node.Children)
		}
		res = append(res, rec)
	}
	return res
}
