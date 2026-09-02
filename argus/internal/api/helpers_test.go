package api_test

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"argus/app/internal/model"
)

// newTestAPIDB 打开带唯一文件名的内存 sqlite（cache=shared 供多连接访问）并迁移表结构，
// 供各 api 测试的 engine 装配函数复用。
func newTestAPIDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:test_%s_api_%d?mode=memory&cache=shared&_busy_timeout=5000", name, time.Now().UnixNano())), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	_ = db.Create(&model.FaceGalleryRevision{ID: 1, Revision: 0}).Error
	return db
}
