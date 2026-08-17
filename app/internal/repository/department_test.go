package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/model"
)

func TestDepartmentRepositoryDeleteRejectsParentWithChild(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Department{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	root := model.Department{Name: "root", Status: model.StatusEnabled}
	if err := db.Create(&root).Error; err != nil {
		t.Fatalf("create root: %v", err)
	}
	child := model.Department{Name: "child", ParentID: root.ID, Status: model.StatusEnabled}
	if err := db.Create(&child).Error; err != nil {
		t.Fatalf("create child: %v", err)
	}

	deleted, err := NewDepartmentRepository(db).Delete(context.Background(), root.ID)
	if !errors.Is(err, ErrDepartmentHasChildren) {
		t.Fatalf("delete parent error = %v, want ErrDepartmentHasChildren", err)
	}
	if deleted {
		t.Fatal("delete parent = true, want false")
	}
}
