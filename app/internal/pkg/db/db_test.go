package db

import (
	"testing"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/pkg/config"
)

// 密码含特殊字符时，DSN 必须能被驱动自身的解析器完整往返。
func TestMySQLDSNRoundTrip(t *testing.T) {
	d := config.DB{
		Driver: "mysql", Host: "10.0.0.1", Port: 3306,
		User: "app", Password: `pa@ss:/word (x)`, Name: "niko_db", TimeZone: "Asia/Shanghai",
	}
	dsn, err := mysqlDSN(d)
	if err != nil {
		t.Fatalf("mysqlDSN: %v", err)
	}
	cfg, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if cfg.User != d.User {
		t.Errorf("user = %q, want %q", cfg.User, d.User)
	}
	if cfg.Passwd != d.Password {
		t.Errorf("password = %q, want %q", cfg.Passwd, d.Password)
	}
	if cfg.DBName != d.Name {
		t.Errorf("dbname = %q, want %q", cfg.DBName, d.Name)
	}
	if cfg.Net != "tcp" || cfg.Addr != "10.0.0.1:3306" {
		t.Errorf("net/addr = %q/%q, want tcp/10.0.0.1:3306", cfg.Net, cfg.Addr)
	}
	if !cfg.ParseTime {
		t.Error("parseTime not set")
	}
	if cfg.Loc == nil || cfg.Loc.String() != "Asia/Shanghai" {
		t.Errorf("loc = %v, want Asia/Shanghai", cfg.Loc)
	}
}

func TestPostgresDSNInitializesGORM(t *testing.T) {
	d := config.DB{
		Driver: "postgres", Host: "127.0.0.1", Port: 5432,
		User: "app", Password: "secret", Name: "niko_db", TimeZone: "Asia/Shanghai",
	}
	dsn, err := postgresDSN(d)
	if err != nil {
		t.Fatalf("postgresDSN: %v", err)
	}
	if _, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{DisableAutomaticPing: true}); err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
}

func TestPostgresDSNRoundTrip(t *testing.T) {
	d := config.DB{
		Driver: "postgres", Host: "10.0.0.1", Port: 5432,
		User: "app", Password: `pa ss@/word (x)`, Name: "niko_db", TimeZone: "Asia/Shanghai",
	}
	dsn, err := postgresDSN(d)
	if err != nil {
		t.Fatalf("postgresDSN: %v", err)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.User != d.User {
		t.Errorf("user = %q, want %q", cfg.User, d.User)
	}
	if cfg.Password != d.Password {
		t.Errorf("password = %q, want %q", cfg.Password, d.Password)
	}
	if cfg.Database != d.Name {
		t.Errorf("dbname = %q, want %q", cfg.Database, d.Name)
	}
	if cfg.Host != d.Host {
		t.Errorf("host = %q, want %q", cfg.Host, d.Host)
	}
	if got := cfg.RuntimeParams["TimeZone"]; got != "Asia/Shanghai" {
		t.Errorf("TimeZone = %q, want Asia/Shanghai", got)
	}
}
