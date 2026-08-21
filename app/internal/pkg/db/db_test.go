package db

import (
	"testing"

	"github.com/jackc/pgx/v5"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"niko-vue-admin/app/internal/pkg/config"
)

func TestPostgresDSNInitializesGORM(t *testing.T) {
	d := config.DB{
		Host: "127.0.0.1", Port: 5432,
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
		Host: "10.0.0.1", Port: 5432,
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
