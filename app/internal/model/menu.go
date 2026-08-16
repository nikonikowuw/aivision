package model

import "sort"

// Menu 菜单/按钮（表名 menus）。
type Menu struct {
	BaseModel
	ParentID   uint64 `gorm:"column:parent_id;not null;default:0" json:"parentId"` // 0=根
	Type       string `gorm:"column:type;type:varchar(16);not null" json:"type"`   // catalog / menu / button
	Name       string `gorm:"column:name;type:varchar(64);not null" json:"name"`   // catalog/menu 为 ASCII 路由标识符；button 为中文展示名
	Title      string `gorm:"column:title;type:varchar(128)" json:"title"`         // i18n key，如 routes.system.user（决策 17）
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
)

// MenuTreeNode 树形节点视图（后续 menu/service 层消费）。
type MenuTreeNode struct {
	Menu
	Children []*MenuTreeNode `json:"children,omitempty"`
}

// BuildMenuTree 将扁平菜单列表构建为按 parent_id 组织的树（纯函数，可单测）。
// 返回排序后的根节点（parent_id=0 或父节点不在列表中的节点）。
func BuildMenuTree(menus []Menu) []*MenuTreeNode {
	byID := make(map[uint64]*MenuTreeNode, len(menus))
	for i := range menus {
		m := menus[i]
		byID[m.ID] = &MenuTreeNode{Menu: m}
	}

	var roots []*MenuTreeNode
	visited := make(map[uint64]bool, len(menus))
	for _, node := range byID {
		if visited[node.ID] {
			continue
		}
		current := node
		path := make(map[uint64]bool, 8) // 本次上溯链，用于环检测
		for {
			visited[current.ID] = true
			path[current.ID] = true
			parent, ok := byID[current.ParentID]
			if !ok {
				// 父节点不在列表中（含 parent_id=0），当前链顶端作为根
				roots = append(roots, current)
				break
			}
			if path[parent.ID] {
				// 父节点在本次上溯链中 = 环（脏数据）：不挂到父下，current 自身作为根，避免成环
				roots = append(roots, current)
				break
			}
			if visited[parent.ID] {
				// 父节点已被挂载过（它已连接到根）：挂到父下即完成，无需继续上溯
				parent.Children = append(parent.Children, current)
				break
			}
			parent.Children = append(parent.Children, current)
			current = parent
		}
	}
	sortMenuTree(roots)
	return roots
}

// sortMenuTree 按 sort 升序递归排序（同值按 ID 稳定）。
func sortMenuTree(nodes []*MenuTreeNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Sort != nodes[j].Sort {
			return nodes[i].Sort < nodes[j].Sort
		}
		return nodes[i].ID < nodes[j].ID
	})
	for _, n := range nodes {
		sortMenuTree(n.Children)
	}
}
