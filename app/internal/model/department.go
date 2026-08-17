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
	if len(depts) == 0 {
		return nil
	}
	nodes := make([]*DepartmentTreeNode, len(depts))
	index := make(map[uint64]int, len(depts))
	for i := range depts {
		nodes[i] = &DepartmentTreeNode{Department: depts[i]}
		index[depts[i].ID] = i
	}
	roots, childrenOf := BuildTree(len(depts),
		func(i int) int {
			if j, ok := index[depts[i].ParentID]; ok {
				return j
			}
			return -1
		},
		func(i int) (int, uint64) { return depts[i].Sort, depts[i].ID },
	)
	var build func(i int) *DepartmentTreeNode
	build = func(i int) *DepartmentTreeNode {
		node := nodes[i]
		for _, c := range childrenOf(i) {
			node.Children = append(node.Children, build(c))
		}
		return node
	}
	out := make([]*DepartmentTreeNode, 0, len(roots))
	for _, r := range roots {
		out = append(out, build(r))
	}
	return out
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
		return p, ok
	}, ancestorID, candidateID)
}
