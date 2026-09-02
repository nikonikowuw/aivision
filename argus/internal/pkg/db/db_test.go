package db

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"argus/app/internal/pkg/config"
)

func TestSqliteDSN(t *testing.T) {
	d := config.DB{
		Path:        "data/test.db",
		BusyTimeout: 5000,
	}
	dsn, err := sqliteDSN(d)
	if err != nil {
		t.Fatalf("sqliteDSN: %v", err)
	}
	if !strings.HasPrefix(dsn, "file:data/test.db?") {
		t.Errorf("dsn prefix mismatch: %s", dsn)
	}
	if !strings.Contains(dsn, "_journal_mode=WAL") {
		t.Errorf("dsn missing WAL mode: %s", dsn)
	}
	if !strings.Contains(dsn, "_busy_timeout=5000") {
		t.Errorf("dsn missing busy timeout: %s", dsn)
	}
}

func TestNewSQLiteConnectsAndCreatesDir(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "sub", "test.db")

	cfg := &config.Config{
		DB: config.DB{
			Path:        dbPath,
			BusyTimeout: 3000,
			MaxOpen:     5,
			MaxIdle:     2,
			MaxLifetime: 10 * time.Minute,
		},
	}

	logger := zap.NewNop()
	gdb, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}
