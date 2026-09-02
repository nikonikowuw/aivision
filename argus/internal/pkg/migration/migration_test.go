package migration

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunnerAutoMigrateAndCheckSchemaReady(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	runner, err := New(db)
	if err != nil {
		t.Fatalf("New runner: %v", err)
	}

	if err := runner.CheckSchemaReady(); err != nil {
		t.Fatalf("CheckSchemaReady: %v", err)
	}

	// 再次调用校验幂等性
	if err := runner.CheckSchemaReady(); err != nil {
		t.Fatalf("second CheckSchemaReady: %v", err)
	}
}
