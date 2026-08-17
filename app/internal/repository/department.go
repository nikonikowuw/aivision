package repository

import (
	"context"
	"errors"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"niko-vue-admin/app/internal/model"
	"niko-vue-admin/app/internal/pkg/errno"
)

// DepartmentRepository 部门数据访问接口。
type DepartmentRepository interface {
	Create(ctx context.Context, dept *model.Department) error
	Update(ctx context.Context, dept *model.Department) error
	Delete(ctx context.Context, id uint64) (bool, error)
	GetByID(ctx context.Context, id uint64) (*model.Department, error)
	ListAll(ctx context.Context) ([]model.Department, error)
}

// ErrDepartmentHasChildren 表示部门仍存在未软删的子部门。
var ErrDepartmentHasChildren = errors.New("repository: department has children")

type departmentRepository struct {
	db *gorm.DB
}

// NewDepartmentRepository 创建 DepartmentRepository 实例。
func NewDepartmentRepository(db *gorm.DB) DepartmentRepository {
	return &departmentRepository{db: db}
}

func (r *departmentRepository) Create(ctx context.Context, dept *model.Department) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDepartments(tx, dept.ParentID); err != nil {
			return err
		}
		return tx.Create(dept).Error
	})
}

func (r *departmentRepository) Update(ctx context.Context, dept *model.Department) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDepartments(tx, dept.ID, dept.ParentID); err != nil {
			return err
		}
		
		// 校验：不能将部门移动到它的子孙部门下（防止成环）。
		// 在事务内读取，确保并发移动不会产生环状结构。
		if dept.ParentID != 0 {
			var allDepts []model.Department
			if err := tx.Find(&allDepts).Error; err != nil {
				return err
			}
			
			// 校验父部门是否存在
			var parentExists bool
			for _, d := range allDepts {
				if d.ID == dept.ParentID {
					parentExists = true
					break
				}
			}
			if !parentExists {
				return ErrNotFound
			}

			if model.IsDeptDescendant(allDepts, dept.ID, dept.ParentID) {
				// 虽然这是个业务错误，但在这里通过 tx rollback，外层服务会透传出来
				return errno.NewError(errno.CodeParentIsDescendant)
			}
		}

		return tx.Save(dept).Error
	})
}

// Delete 删除部门；只有不存在未软删子部门时才会执行软删。
// 目标行锁、子部门检查和软删处于同一事务，避免并发新增子部门留下孤儿节点。
func (r *departmentRepository) Delete(ctx context.Context, id uint64) (bool, error) {
	deleted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockDepartment(tx, id); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			return err
		}

		var count int64
		if err := tx.Model(&model.Department{}).Where("parent_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrDepartmentHasChildren
		}

		result := tx.Delete(&model.Department{}, id)
		if result.Error != nil {
			return result.Error
		}
		deleted = result.RowsAffected > 0
		return nil
	})
	return deleted, err
}

// lockDepartments 按稳定顺序锁定指定的未软删部门，避免移动部门时与其他写操作互相等待。
func lockDepartments(tx *gorm.DB, ids ...uint64) error {
	ids = append([]uint64(nil), ids...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var previous uint64
	for _, id := range ids {
		if id == 0 || id == previous {
			continue
		}
		if _, err := lockDepartment(tx, id); err != nil {
			return err
		}
		previous = id
	}
	return nil
}

// lockDepartment 锁定未软删部门，供创建、编辑和删除使用。
// isSQLite 判断当前连接是否为 sqlite（用于决定如何加写锁）
func isSQLite(tx *gorm.DB) bool {
	return tx.Dialector.Name() == "sqlite"
}

// MySQL/PostgreSQL 使用行锁；SQLite 通过同值更新取得写锁，便于内存库测试保持相同的串行化语义。
func lockDepartment(tx *gorm.DB, id uint64) (*model.Department, error) {
	if isSQLite(tx) {
		if err := tx.Model(&model.Department{}).
			Where("id = ? AND deleted_at IS NULL", id).
			UpdateColumn("updated_at", gorm.Expr("updated_at")).Error; err != nil {
			return nil, err
		}
	}

	query := tx.Where("id = ? AND deleted_at IS NULL", id)
	if !isSQLite(tx) {
		query = query.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate})
	}
	var dept model.Department
	if err := query.First(&dept).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &dept, nil
}

func (r *departmentRepository) GetByID(ctx context.Context, id uint64) (*model.Department, error) {
	var dept model.Department
	if err := r.db.WithContext(ctx).First(&dept, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &dept, nil
}

func (r *departmentRepository) ListAll(ctx context.Context) ([]model.Department, error) {
	var depts []model.Department
	if err := r.db.WithContext(ctx).Find(&depts).Error; err != nil {
		return nil, err
	}
	return depts, nil
}
