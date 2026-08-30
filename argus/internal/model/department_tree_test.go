package model

import (
	"reflect"
	"testing"
)

func TestBuildDeptTreeStructure(t *testing.T) {
	depts := []Department{
		{BaseModel: BaseModel{ID: 1}, ParentID: 0, Name: "总公司", Sort: 2},
		{BaseModel: BaseModel{ID: 2}, ParentID: 0, Name: "集团办", Sort: 1},
		{BaseModel: BaseModel{ID: 11}, ParentID: 1, Name: "研发部", Sort: 1},
		{BaseModel: BaseModel{ID: 12}, ParentID: 1, Name: "市场部", Sort: 2},
		{BaseModel: BaseModel{ID: 111}, ParentID: 11, Name: "前端组", Sort: 1},
		{BaseModel: BaseModel{ID: 112}, ParentID: 11, Name: "后端组", Sort: 2},
	}
	roots := BuildDeptTree(depts)
	if len(roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(roots))
	}
	if roots[0].Name != "集团办" || roots[1].Name != "总公司" {
		t.Errorf("root order = [%s, %s], want [集团办, 总公司]", roots[0].Name, roots[1].Name)
	}
	head := roots[1]
	if len(head.Children) != 2 || head.Children[0].Name != "研发部" || head.Children[1].Name != "市场部" {
		t.Fatalf("总公司 children = %+v", head.Children)
	}
	rd := head.Children[0]
	if len(rd.Children) != 2 {
		t.Fatalf("研发部 children = %d, want 2", len(rd.Children))
	}
	if rd.Children[0].Name != "前端组" || rd.Children[1].Name != "后端组" {
		t.Errorf("三级节点顺序 = [%s, %s]", rd.Children[0].Name, rd.Children[1].Name)
	}
}

func TestBuildDeptTreeEmptyAndFlat(t *testing.T) {
	if got := BuildDeptTree(nil); got != nil {
		t.Errorf("BuildDeptTree(nil) = %v, want nil", got)
	}
	flat := []Department{
		{BaseModel: BaseModel{ID: 5}, ParentID: 0, Name: "Solo", Sort: 3},
		{BaseModel: BaseModel{ID: 9}, ParentID: 0, Name: "B", Sort: 2},
		{BaseModel: BaseModel{ID: 3}, ParentID: 0, Name: "A", Sort: 1},
	}
	roots := BuildDeptTree(flat)
	if len(roots) != 3 {
		t.Fatalf("flat roots = %d, want 3", len(roots))
	}
	got := []string{roots[0].Name, roots[1].Name, roots[2].Name}
	want := []string{"A", "B", "Solo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("flat order = %v, want %v", got, want)
	}
}

func TestBuildDeptTreeOrphanAttachedToRoot(t *testing.T) {
	// 父节点不在列表中：孤立节点自身成为根，不丢失。
	depts := []Department{
		{BaseModel: BaseModel{ID: 7}, ParentID: 99, Name: "Orphan"},
	}
	roots := BuildDeptTree(depts)
	if len(roots) != 1 || roots[0].Name != "Orphan" {
		t.Fatalf("orphan roots = %+v", roots)
	}
}

func TestBuildDeptTreeUnorderedInput(t *testing.T) {
	// 输入乱序时结构仍正确。
	depts := []Department{
		{BaseModel: BaseModel{ID: 3}, ParentID: 1, Name: "child3", Sort: 3},
		{BaseModel: BaseModel{ID: 1}, ParentID: 0, Name: "root"},
		{BaseModel: BaseModel{ID: 2}, ParentID: 1, Name: "child1", Sort: 1},
	}
	roots := BuildDeptTree(depts)
	if len(roots) != 1 || roots[0].ID != 1 {
		t.Fatalf("roots = %+v, want [root]", roots)
	}
	children := roots[0].Children
	if len(children) != 2 || children[0].ID != 2 || children[1].ID != 3 {
		t.Fatalf("children = %+v, want [child1, child3]", children)
	}
}

func TestBuildDeptTreeCycleBroken(t *testing.T) {
	// 脏数据 A↔B 互指：算法应断开环，不产出互为父子的环。
	depts := []Department{
		{BaseModel: BaseModel{ID: 1}, ParentID: 2, Name: "A"},
		{BaseModel: BaseModel{ID: 2}, ParentID: 1, Name: "B"},
	}
	roots := BuildDeptTree(depts)
	if len(roots) != 1 {
		t.Fatalf("cycle roots = %d, want 1", len(roots))
	}
	root := roots[0]
	if len(root.Children) != 1 {
		t.Fatalf("cycle root children = %d, want 1", len(root.Children))
	}
	if len(root.Children[0].Children) != 0 {
		t.Error("cycle not fully broken: grandchild present")
	}
}

func TestBuildDeptTreeWide(t *testing.T) {
	const nodeCount = 100_000
	depts := make([]Department, nodeCount)
	for i := range depts {
		depts[i].ID = uint64(i + 1)
	}

	roots := BuildDeptTree(depts)
	if len(roots) != nodeCount {
		t.Fatalf("wide tree roots = %d, want %d", len(roots), nodeCount)
	}
}
