package model

// Menu 菜单/按钮（表名 menus）。
type Menu struct {
	BaseModel
	ParentID   uint64 `gorm:"column:parent_id;not null;default:0;index" json:"parentId"` // 0=根
	Type       string `gorm:"column:type;type:varchar(16);not null" json:"type"`         // catalog / menu / button
	Name       string `gorm:"column:name;type:varchar(64);not null" json:"name"`         // catalog/menu 为 ASCII 路由标识符；button 为中文展示名
	Title      string `gorm:"column:title;type:varchar(128)" json:"title"`               // i18n key，如 routes.system.user（决策 17）
	Path       string `gorm:"column:path;type:varchar(128)" json:"path"`
	Component  string `gorm:"column:component;type:varchar(255)" json:"component"` // 视图相对路径；按钮级为空
	Icon       string `gorm:"column:icon;type:varchar(64)" json:"icon"`
	Sort       int    `gorm:"column:sort;default:0" json:"sort"`
	Status     int8   `gorm:"column:status;default:1" json:"status"`                 // 类型由 gorm 按驱动映射（决策 18）
	Permission string `gorm:"column:permission;type:varchar(128)" json:"permission"` // catalog/menu 存模块码，button 存动作码
	Affix      bool   `gorm:"column:affix;default:false" json:"affix"`
	KeepAlive  bool   `gorm:"column:keep_alive;default:false" json:"keepAlive"`
	HomePath   string `gorm:"column:home_path;type:varchar(128)" json:"homePath"`
}

// TableName 显式声明表名。
func (Menu) TableName() string { return "menus" }

// MenuType 菜单类型枚举（父 design.md §2）。
const (
	MenuTypeCatalog = "catalog"
	MenuTypeMenu    = "menu"
	MenuTypeButton  = "button"

	// MenuComponentBasicLayout 是顶层 catalog 使用的 vben 布局组件。
	MenuComponentBasicLayout = "BasicLayout"
)

// IsMenuDescendant 判断 candidateID 是否位于 ancestorID 的子树中。
// 父链中出现脏数据环时停止遍历，避免校验本身陷入死循环。
func IsMenuDescendant(menus []Menu, ancestorID, candidateID uint64) bool {
	parents := make(map[uint64]uint64, len(menus))
	for _, menu := range menus {
		parents[menu.ID] = menu.ParentID
	}
	return IsDescendant(func(id uint64) (uint64, bool) {
		p, ok := parents[id]
		return p, ok
	}, ancestorID, candidateID)
}

// MenuTreeNode 树形节点视图（后续 menu/service 层消费）。
type MenuTreeNode struct {
	Menu
	Children []*MenuTreeNode `json:"children,omitempty"`
}

// BuildMenuTree 将扁平菜单列表构建为按 parent_id 组织的树（纯函数，可单测）。
// 返回排序后的根节点（parent_id=0 或父节点不在列表中的节点）。
func BuildMenuTree(menus []Menu) []*MenuTreeNode {
	genericRoots := BuildTree(menus, func(m Menu) uint64 { return m.ID }, func(m Menu) uint64 { return m.ParentID }, func(m Menu) int { return m.Sort })
	var result []*MenuTreeNode
	var mapNode func(node *TreeNode[Menu]) *MenuTreeNode
	mapNode = func(node *TreeNode[Menu]) *MenuTreeNode {
		dto := &MenuTreeNode{Menu: node.Item}
		for _, child := range node.Children {
			dto.Children = append(dto.Children, mapNode(child))
		}
		return dto
	}
	for _, root := range genericRoots {
		result = append(result, mapNode(root))
	}
	return result
}
