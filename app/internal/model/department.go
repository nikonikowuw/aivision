package model

// Department 部门（表名 departments）。
type Department struct {
	BaseModel
	ParentID uint64 `gorm:"column:parent_id;not null;default:0;index" json:"parentId"` // 0=根
	Name     string `gorm:"column:name;type:varchar(64);not null" json:"name"`
	Sort     int    `gorm:"column:sort;default:0" json:"sort"`
	Leader   string `gorm:"column:leader;type:varchar(64)" json:"leader"`
	Phone    string `gorm:"column:phone;type:varchar(32)" json:"phone"`
	Status   int8   `gorm:"column:status" json:"status"` // 1 启用 / 0 禁用；新建默认值由 service 明确填充
}

// TableName 显式声明表名。
func (Department) TableName() string { return "departments" }

// DepartmentTreeNode 树形节点视图。
type DepartmentTreeNode struct {
	Department
	Children []*DepartmentTreeNode `json:"children,omitempty"`
}

// BuildDeptTree 将扁平部门列表构建为按 parent_id 组织的树（纯函数，可单测）。
// 返回排序后的根节点（parent_id=0 或父节点不在列表中的节点）。
func BuildDeptTree(depts []Department) []*DepartmentTreeNode {
	genericRoots := BuildTree(depts, func(d Department) uint64 { return d.ID }, func(d Department) uint64 { return d.ParentID }, func(d Department) int { return d.Sort })
	var result []*DepartmentTreeNode
	var mapNode func(node *TreeNode[Department]) *DepartmentTreeNode
	mapNode = func(node *TreeNode[Department]) *DepartmentTreeNode {
		dto := &DepartmentTreeNode{Department: node.Item}
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

// IsDeptDescendant 判断 candidateID 是否位于 ancestorID 的子树中。
// 父链中出现脏数据环时停止遍历，避免校验本身陷入死循环。
func IsDeptDescendant(depts []Department, ancestorID, candidateID uint64) bool {
	parents := make(map[uint64]uint64, len(depts))
	for _, d := range depts {
		parents[d.ID] = d.ParentID
	}
	return IsDescendant(func(id uint64) (uint64, bool) {
		p, ok := parents[id]
		return p, ok && p != 0
	}, ancestorID, candidateID)
}
