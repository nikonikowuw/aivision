package service

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
)

func newDeptTestService(db *gorm.DB) DeptService {
	return NewDeptService(repository.NewDepartmentRepository(db))
}

func TestDeptServiceCRUDAndTree(t *testing.T) {
	db := setupTestDB(t)
	srv := newDeptTestService(db)
	ctx := context.Background()

	statusEnabled := model.StatusEnabled
	// 1. 建根（name/leader 首尾空白应被去除）
	head, err := srv.CreateDept(ctx, &SaveDeptInput{Name: " 总公司 ", Leader: " 王总 ", Sort: 2, Status: &statusEnabled})
	if err != nil {
		t.Fatalf("CreateDept head failed: %v", err)
	}
	if head.Name != "总公司" {
		t.Errorf("head name = %q, want trimmed 总公司", head.Name)
	}
	if head.Leader != "王总" {
		t.Errorf("head leader = %q, want trimmed 王总", head.Leader)
	}
	if head.Status != model.StatusEnabled {
		t.Errorf("head status = %d, want enabled", head.Status)
	}

	// 2. 建二级部门
	rd, err := srv.CreateDept(ctx, &SaveDeptInput{ParentID: head.ID, Name: "研发部", Sort: 1, Status: &statusEnabled})
	if err != nil {
		t.Fatalf("CreateDept rd failed: %v", err)
	}
	if _, err := srv.CreateDept(ctx, &SaveDeptInput{ParentID: head.ID, Name: "市场部", Sort: 2, Status: &statusEnabled}); err != nil {
		t.Fatalf("CreateDept mk failed: %v", err)
	}

	// 3. 建三级部门
	fe, err := srv.CreateDept(ctx, &SaveDeptInput{ParentID: rd.ID, Name: "前端组", Sort: 1, Status: &statusEnabled})
	if err != nil {
		t.Fatalf("CreateDept fe failed: %v", err)
	}
	be, err := srv.CreateDept(ctx, &SaveDeptInput{ParentID: rd.ID, Name: "后端组", Sort: 2, Status: &statusEnabled})
	if err != nil {
		t.Fatalf("CreateDept be failed: %v", err)
	}

	// 4. 树形输出：三层嵌套 + 每层排序
	tree, err := srv.GetDeptTree(ctx)
	if err != nil {
		t.Fatalf("GetDeptTree failed: %v", err)
	}
	if len(tree) != 1 || tree[0].Name != "总公司" {
		t.Fatalf("tree roots = %+v, want [总公司]", tree)
	}
	if len(tree[0].Children) != 2 ||
		tree[0].Children[0].Name != "研发部" || tree[0].Children[1].Name != "市场部" {
		t.Fatalf("children = %+v, want [研发部, 市场部]", tree[0].Children)
	}
	rdNode := tree[0].Children[0]
	if len(rdNode.Children) != 2 ||
		rdNode.Children[0].Name != "前端组" || rdNode.Children[1].Name != "后端组" {
		t.Fatalf("rd children = %+v, want [前端组, 后端组]", rdNode.Children)
	}

	// 5. 编辑生效
	edited, err := srv.UpdateDept(ctx, rd.ID, &SaveDeptInput{ParentID: head.ID, Name: "研发中心", Sort: 1, Status: &statusEnabled})
	if err != nil {
		t.Fatalf("UpdateDept failed: %v", err)
	}
	if edited.Name != "研发中心" {
		t.Errorf("edited name = %q, want 研发中心", edited.Name)
	}

	// 6. 删除叶子成功；二次删除 1011
	if err := srv.DeleteDept(ctx, fe.ID); err != nil {
		t.Fatalf("DeleteDept leaf failed: %v", err)
	}
	wantErrCode(t, srv.DeleteDept(ctx, fe.ID), errno.CodeNotFound)

	// 7. 有子部门的删除 → 1007（rd 仍有 be）
	wantErrCode(t, srv.DeleteDept(ctx, rd.ID), errno.CodeDeptHasChildren)

	// 8. 子部门软删后父可删
	if err := srv.DeleteDept(ctx, be.ID); err != nil {
		t.Fatalf("DeleteDept be failed: %v", err)
	}
	if err := srv.DeleteDept(ctx, rd.ID); err != nil {
		t.Fatalf("DeleteDept rd after child removed failed: %v", err)
	}
}

func TestDeptServiceParentValidation(t *testing.T) {
	db := setupTestDB(t)
	srv := newDeptTestService(db)
	ctx := context.Background()

	statusEnabled := model.StatusEnabled
	root, err := srv.CreateDept(ctx, &SaveDeptInput{Name: "总公司", Status: &statusEnabled})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := srv.CreateDept(ctx, &SaveDeptInput{ParentID: root.ID, Name: "研发部", Status: &statusEnabled})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	grand, err := srv.CreateDept(ctx, &SaveDeptInput{ParentID: child.ID, Name: "前端组", Status: &statusEnabled})
	if err != nil {
		t.Fatalf("create grand: %v", err)
	}

	// 创建时父不存在 → 1011
	_, err = srv.CreateDept(ctx, &SaveDeptInput{ParentID: 99999, Name: "孤儿"})
	wantErrCode(t, err, errno.CodeNotFound)
	// 编辑时父不存在 → 1011
	_, err = srv.UpdateDept(ctx, grand.ID, &SaveDeptInput{ParentID: 99999, Name: "孤儿"})
	wantErrCode(t, err, errno.CodeNotFound)
	// 编辑时父=自身 → 1010
	_, err = srv.UpdateDept(ctx, child.ID, &SaveDeptInput{ParentID: child.ID, Name: "研发部"})
	wantErrCode(t, err, errno.CodeParentIsSelf)
	// 编辑时父=自身后代 → 1013（把 grand 挂到 child 下：grand 是 child 的后代）
	_, err = srv.UpdateDept(ctx, child.ID, &SaveDeptInput{ParentID: grand.ID, Name: "研发部"})
	wantErrCode(t, err, errno.CodeParentIsDescendant)
	// 编辑不存在的部门 → 1011
	_, err = srv.UpdateDept(ctx, 99999, &SaveDeptInput{Name: "不存在"})
	wantErrCode(t, err, errno.CodeNotFound)
	// 把 grand 挂到 root 下成功（合法跨层）
	if _, err := srv.UpdateDept(ctx, grand.ID, &SaveDeptInput{ParentID: root.ID, Name: "前端组"}); err != nil {
		t.Fatalf("move grand to root failed: %v", err)
	}
}

func TestDeptServiceInputNormalization(t *testing.T) {
	db := setupTestDB(t)
	srv := newDeptTestService(db)
	ctx := context.Background()

	// 纯空白 name → 1009
	_, err := srv.CreateDept(ctx, &SaveDeptInput{Name: "   "})
	wantErrCode(t, err, errno.CodeInvalidParam)

	root, err := srv.CreateDept(ctx, &SaveDeptInput{Name: "总公司"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	// 编辑时纯空白 name → 1009
	_, err = srv.UpdateDept(ctx, root.ID, &SaveDeptInput{Name: " \t "})
	wantErrCode(t, err, errno.CodeInvalidParam)
}

func TestDeptServiceStatusSemantics(t *testing.T) {
	db := setupTestDB(t)
	srv := newDeptTestService(db)
	ctx := context.Background()

	// 新建省略 status → 默认启用
	root, err := srv.CreateDept(ctx, &SaveDeptInput{Name: "总公司"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if root.Status != model.StatusEnabled {
		t.Errorf("default status = %d, want enabled", root.Status)
	}

	// 创建显式禁用 → 必须保留 0，而不能被数据库默认值改回启用。
	statusDisabled := int8(model.StatusDisabled)
	disabledRoot, err := srv.CreateDept(ctx, &SaveDeptInput{Name: "禁用部门", Status: &statusDisabled})
	if err != nil {
		t.Fatalf("create disabled dept: %v", err)
	}
	if disabledRoot.Status != model.StatusDisabled {
		t.Errorf("created disabled status = %d, want disabled", disabledRoot.Status)
	}
	storedDisabled, err := repository.NewDepartmentRepository(db).GetByID(ctx, disabledRoot.ID)
	if err != nil {
		t.Fatalf("reload disabled dept: %v", err)
	}
	if storedDisabled.Status != model.StatusDisabled {
		t.Errorf("stored disabled status = %d, want disabled", storedDisabled.Status)
	}

	// 编辑显式禁用生效
	updated, err := srv.UpdateDept(ctx, root.ID, &SaveDeptInput{Name: "总公司", Status: &statusDisabled})
	if err != nil {
		t.Fatalf("disable dept: %v", err)
	}
	if updated.Status != model.StatusDisabled {
		t.Errorf("status = %d, want disabled", updated.Status)
	}

	// 编辑省略 status → 保持禁用不变
	kept, err := srv.UpdateDept(ctx, root.ID, &SaveDeptInput{Name: "总公司"})
	if err != nil {
		t.Fatalf("update without status: %v", err)
	}
	if kept.Status != model.StatusDisabled {
		t.Errorf("status = %d, want kept disabled", kept.Status)
	}
}

func TestDeptServiceEmptyTree(t *testing.T) {
	db := setupTestDB(t)
	srv := newDeptTestService(db)
	ctx := context.Background()

	tree, err := srv.GetDeptTree(ctx)
	if err != nil {
		t.Fatalf("GetDeptTree failed: %v", err)
	}
	if tree == nil {
		t.Fatal("GetDeptTree on empty db = nil, want empty slice")
	}
	if len(tree) != 0 {
		t.Fatalf("empty tree = %+v, want []", tree)
	}
}
