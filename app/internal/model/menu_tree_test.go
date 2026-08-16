package model

import (
	"reflect"
	"testing"
)

func TestBuildMenuTreeStructure(t *testing.T) {
	menus := []Menu{
		{BaseModel: BaseModel{ID: 1}, ParentID: 0, Type: MenuTypeCatalog, Name: "System", Sort: 2},
		{BaseModel: BaseModel{ID: 2}, ParentID: 0, Type: MenuTypeCatalog, Name: "Dashboard", Sort: 1},
		{BaseModel: BaseModel{ID: 11}, ParentID: 1, Type: MenuTypeMenu, Name: "User", Sort: 1},
		{BaseModel: BaseModel{ID: 12}, ParentID: 1, Type: MenuTypeMenu, Name: "Role", Sort: 2},
		{BaseModel: BaseModel{ID: 111}, ParentID: 11, Type: MenuTypeButton, Name: "新增用户", Sort: 1},
		{BaseModel: BaseModel{ID: 112}, ParentID: 11, Type: MenuTypeButton, Name: "编辑用户", Sort: 2},
	}
	roots := BuildMenuTree(menus)
	if len(roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(roots))
	}
	if roots[0].Name != "Dashboard" || roots[1].Name != "System" {
		t.Errorf("root order = [%s, %s], want [Dashboard, System]", roots[0].Name, roots[1].Name)
	}
	sys := roots[1]
	if len(sys.Children) != 2 || sys.Children[0].Name != "User" || sys.Children[1].Name != "Role" {
		t.Fatalf("System children = %+v", sys.Children)
	}
	userNode := sys.Children[0]
	if len(userNode.Children) != 2 {
		t.Fatalf("User children = %d, want 2", len(userNode.Children))
	}
	if userNode.Children[0].Name != "新增用户" || userNode.Children[1].Name != "编辑用户" {
		t.Errorf("button order = [%s, %s]", userNode.Children[0].Name, userNode.Children[1].Name)
	}
}

func TestBuildMenuTreeEmptyAndFlat(t *testing.T) {
	if got := BuildMenuTree(nil); got != nil {
		t.Errorf("BuildMenuTree(nil) = %v, want nil", got)
	}
	flat := []Menu{
		{BaseModel: BaseModel{ID: 5}, ParentID: 0, Name: "Solo", Sort: 3},
		{BaseModel: BaseModel{ID: 9}, ParentID: 0, Name: "B", Sort: 2},
		{BaseModel: BaseModel{ID: 3}, ParentID: 0, Name: "A", Sort: 1},
	}
	roots := BuildMenuTree(flat)
	if len(roots) != 3 {
		t.Fatalf("flat roots = %d, want 3", len(roots))
	}
	got := []string{roots[0].Name, roots[1].Name, roots[2].Name}
	want := []string{"A", "B", "Solo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("flat order = %v, want %v", got, want)
	}
}

func TestBuildMenuTreeOrphanAttachedToRoot(t *testing.T) {
	// 父节点不在列表中：孤立节点自身成为根，不丢失。
	menus := []Menu{
		{BaseModel: BaseModel{ID: 7}, ParentID: 99, Name: "Orphan"},
	}
	roots := BuildMenuTree(menus)
	if len(roots) != 1 || roots[0].Name != "Orphan" {
		t.Fatalf("orphan roots = %+v", roots)
	}
}

func TestBuildMenuTreeUnorderedInput(t *testing.T) {
	// 输入乱序时结构仍正确。
	menus := []Menu{
		{BaseModel: BaseModel{ID: 3}, ParentID: 1, Name: "child3", Sort: 3},
		{BaseModel: BaseModel{ID: 1}, ParentID: 0, Name: "root"},
		{BaseModel: BaseModel{ID: 2}, ParentID: 1, Name: "child1", Sort: 1},
	}
	roots := BuildMenuTree(menus)
	if len(roots) != 1 || roots[0].ID != 1 {
		t.Fatalf("roots = %+v, want [root]", roots)
	}
	children := roots[0].Children
	if len(children) != 2 || children[0].ID != 2 || children[1].ID != 3 {
		t.Fatalf("children = %+v, want [child1, child3]", children)
	}
}

func TestBuildMenuTreeCycleBroken(t *testing.T) {
	// 脏数据 A↔B 互指（A.parent=B, B.parent=A）：算法应断开环，
	// 任意迭代起点下都有且仅有一个根、一个子节点，不产出互为父子的环。
	menus := []Menu{
		{BaseModel: BaseModel{ID: 1}, ParentID: 2, Name: "A"},
		{BaseModel: BaseModel{ID: 2}, ParentID: 1, Name: "B"},
	}
	roots := BuildMenuTree(menus)
	if len(roots) != 1 {
		t.Fatalf("cycle roots = %d, want 1", len(roots))
	}
	root := roots[0]
	if len(root.Children) != 1 {
		t.Fatalf("cycle root children = %d, want 1", len(root.Children))
	}
	// 子节点下不再挂回根（否则 A→B→A 成环）
	if len(root.Children[0].Children) != 0 {
		t.Error("cycle not fully broken: grandchild present")
	}
}

func TestIsMenuDescendant(t *testing.T) {
	menus := []Menu{
		{BaseModel: BaseModel{ID: 1}, ParentID: 0},
		{BaseModel: BaseModel{ID: 2}, ParentID: 1},
		{BaseModel: BaseModel{ID: 3}, ParentID: 2},
	}
	if !IsMenuDescendant(menus, 1, 3) {
		t.Error("expected node 3 to be a descendant of node 1")
	}
	if IsMenuDescendant(menus, 2, 1) {
		t.Error("did not expect node 1 to be a descendant of node 2")
	}
	if IsMenuDescendant(menus, 1, 99) {
		t.Error("did not expect missing node to be a descendant")
	}
}
