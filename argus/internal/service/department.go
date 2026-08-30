package service

import (
	"context"
	"errors"
	"strings"

	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/repository"
)

// SaveDeptInput 新增或修改部门输入。
type SaveDeptInput struct {
	ParentID uint64 `json:"parentId"` // 0=根
	Name     string `json:"name" binding:"required"`
	Sort     int    `json:"sort"`
	Leader   string `json:"leader"`
	Phone    string `json:"phone"`
	Status   *int8  `json:"status"` // 省略时新建默认启用；编辑省略表示不变
}

// FillModel 将输入字段填充到 model.Department 结构中。
func (in *SaveDeptInput) FillModel(m *model.Department) {
	m.ParentID = in.ParentID
	m.Name = in.Name
	m.Sort = in.Sort
	m.Leader = in.Leader
	m.Phone = in.Phone
	if in.Status != nil {
		m.Status = *in.Status
	}
}

// DeptService 部门服务接口。
type DeptService interface {
	GetDeptTree(ctx context.Context) ([]*model.DepartmentTreeNode, error)
	CreateDept(ctx context.Context, input *SaveDeptInput) (*model.Department, error)
	UpdateDept(ctx context.Context, id uint64, input *SaveDeptInput) (*model.Department, error)
	DeleteDept(ctx context.Context, id uint64) error
}

type deptService struct {
	repo repository.DepartmentRepository
}

// NewDeptService 创建 DeptService 实例。
func NewDeptService(repo repository.DepartmentRepository) DeptService {
	return &deptService{repo: repo}
}

func (s *deptService) GetDeptTree(ctx context.Context) ([]*model.DepartmentTreeNode, error) {
	depts, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	tree := model.BuildDeptTree(depts)
	if tree == nil {
		return []*model.DepartmentTreeNode{}, nil
	}
	return tree, nil
}

func (s *deptService) CreateDept(ctx context.Context, input *SaveDeptInput) (*model.Department, error) {
	if err := normalizeDeptInput(input); err != nil {
		return nil, err
	}

	if input.ParentID != 0 {
		if _, err := s.repo.GetByID(ctx, input.ParentID); err != nil {
			return nil, mapRepoError(err)
		}
	}

	var dept model.Department
	input.FillModel(&dept)
	if input.Status == nil {
		dept.Status = model.StatusEnabled
	}

	if err := s.repo.Create(ctx, &dept); err != nil {
		return nil, mapRepoError(err)
	}
	return &dept, nil
}

func (s *deptService) UpdateDept(ctx context.Context, id uint64, input *SaveDeptInput) (*model.Department, error) {
	dept, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapRepoError(err)
	}

	if err := normalizeDeptInput(input); err != nil {
		return nil, err
	}

	if input.ParentID != 0 && input.ParentID == id {
		return nil, errno.NewError(errno.CodeParentIsSelf)
	}

	input.FillModel(dept)

	if err := s.repo.Update(ctx, dept); err != nil {
		return nil, mapRepoError(err)
	}
	return dept, nil
}

func (s *deptService) DeleteDept(ctx context.Context, id uint64) error {
	deleted, err := s.repo.Delete(ctx, id)
	if errors.Is(err, repository.ErrDepartmentHasChildren) {
		return errno.NewError(errno.CodeDeptHasChildren)
	}
	if err != nil {
		return err
	}
	if !deleted {
		return errno.NewError(errno.CodeNotFound)
	}
	return nil
}

// normalizeDeptInput 去除 name/leader/phone 首尾空白并校验 name 非空：
// binding:required 只挡空字符串，挡不住纯空白输入，这里兜底并规范入库值。
func normalizeDeptInput(input *SaveDeptInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return errno.NewError(errno.CodeInvalidParam)
	}
	input.Leader = strings.TrimSpace(input.Leader)
	input.Phone = strings.TrimSpace(input.Phone)
	return nil
}
