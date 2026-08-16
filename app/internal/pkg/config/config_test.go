package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadFull(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 9000
db:
  driver: postgres
  host: 10.0.0.1
  port: 5432
  user: app
  password: secret
  name: demo_db
  time_zone: Asia/Tokyo
  max_open: 50
  max_idle: 5
  max_lifetime: 15m
jwt:
  secret: test-secret
  access_ttl: 1h
  refresh_ttl: 72h
log:
  level: debug
`)
	cfg, err := load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("server.port = %d, want 9000", cfg.Server.Port)
	}
	if cfg.DB.Driver != "postgres" || cfg.DB.Host != "10.0.0.1" || cfg.DB.Port != 5432 || cfg.DB.User != "app" ||
		cfg.DB.Password != "secret" || cfg.DB.Name != "demo_db" || cfg.DB.TimeZone != "Asia/Tokyo" {
		t.Errorf("db = %+v", cfg.DB)
	}
	if cfg.DB.MaxOpen != 50 || cfg.DB.MaxIdle != 5 || cfg.DB.MaxLifetime != 15*time.Minute {
		t.Errorf("db pool = %+v", cfg.DB)
	}
	if cfg.JWT.Secret != "test-secret" {
		t.Errorf("jwt.secret = %q", cfg.JWT.Secret)
	}
	if cfg.JWT.AccessTTL != time.Hour {
		t.Errorf("jwt.access_ttl = %v, want 1h", cfg.JWT.AccessTTL)
	}
	if cfg.JWT.RefreshTTL != 72*time.Hour {
		t.Errorf("jwt.refresh_ttl = %v, want 72h", cfg.JWT.RefreshTTL)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log.level = %q, want debug", cfg.Log.Level)
	}
}

func TestLoadDefaultsForMissingKeys(t *testing.T) {
	path := writeConfig(t, "db:\n  name: only_name\n")
	cfg, err := load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Port != 8000 {
		t.Errorf("server.port = %d, want default 8000", cfg.Server.Port)
	}
	if cfg.DB.Driver != "mysql" || cfg.DB.Host != "127.0.0.1" || cfg.DB.Port != 3306 || cfg.DB.User != "root" {
		t.Errorf("db defaults not applied: %+v", cfg.DB)
	}
	if cfg.DB.Name != "only_name" {
		t.Errorf("db.name = %q, want only_name", cfg.DB.Name)
	}
	if cfg.DB.MaxOpen != 100 || cfg.DB.MaxIdle != 10 || cfg.DB.MaxLifetime != 30*time.Minute {
		t.Errorf("db pool defaults not applied: %+v", cfg.DB)
	}
	if cfg.DB.TimeZone != "Asia/Shanghai" {
		t.Errorf("db.time_zone default = %q, want Asia/Shanghai", cfg.DB.TimeZone)
	}
	if cfg.JWT.AccessTTL != 2*time.Hour || cfg.JWT.RefreshTTL != 168*time.Hour {
		t.Errorf("jwt ttl defaults not applied: %v %v", cfg.JWT.AccessTTL, cfg.JWT.RefreshTTL)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("log.level = %q, want default info", cfg.Log.Level)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8000
db:
  host: 127.0.0.1
  port: 3306
  user: root
  password: "123456"
  name: niko_vue_admin
  time_zone: Asia/Shanghai
jwt:
  secret: dev
  access_ttl: 2h
  refresh_ttl: 168h
log:
  level: info
`)
	t.Setenv("APP_DB_HOST", "10.20.30.40")
	t.Setenv("APP_DB_TIME_ZONE", "UTC")
	t.Setenv("APP_JWT_SECRET", "env-secret")
	t.Setenv("APP_JWT_ACCESS_TTL", "30m")
	t.Setenv("APP_SERVER_PORT", "9001")

	cfg, err := load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DB.Host != "10.20.30.40" {
		t.Errorf("db.host = %q, want env override 10.20.30.40", cfg.DB.Host)
	}
	if cfg.DB.TimeZone != "UTC" {
		t.Errorf("db.time_zone = %q, want env override UTC", cfg.DB.TimeZone)
	}
	if cfg.JWT.Secret != "env-secret" {
		t.Errorf("jwt.secret = %q, want env-secret", cfg.JWT.Secret)
	}
	if cfg.JWT.AccessTTL != 30*time.Minute {
		t.Errorf("jwt.access_ttl = %v, want 30m", cfg.JWT.AccessTTL)
	}
	if cfg.Server.Port != 9001 {
		t.Errorf("server.port = %d, want 9001", cfg.Server.Port)
	}
	if cfg.DB.Name != "niko_vue_admin" {
		t.Errorf("db.name = %q, want niko_vue_admin", cfg.DB.Name)
	}
}

func TestLoadDriverDefaultsAndEnvOverride(t *testing.T) {
	path := writeConfig(t, "db:\n  name: only_name\n")
	cfg, err := load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DB.Driver != "mysql" {
		t.Errorf("db.driver = %q, want default mysql", cfg.DB.Driver)
	}

	t.Setenv("APP_DB_DRIVER", "postgres")
	cfg, err = load(path)
	if err != nil {
		t.Fatalf("load with env: %v", err)
	}
	if cfg.DB.Driver != "postgres" {
		t.Errorf("db.driver = %q, want env override postgres", cfg.DB.Driver)
	}
}

func TestLoadInvalidDriver(t *testing.T) {
	path := writeConfig(t, "db:\n  driver: oracle\n")
	if _, err := load(path); err == nil {
		t.Fatal("load should fail for invalid db.driver")
	}
}

func TestLoadInvalidTimeZone(t *testing.T) {
	path := writeConfig(t, "db:\n  time_zone: Mars/Olympus\n")
	if _, err := load(path); err == nil {
		t.Fatal("load should fail for invalid db.time_zone")
	}
}

func TestLoadInvalidTTL(t *testing.T) {
	path := writeConfig(t, "jwt:\n  access_ttl: not-a-duration\n")
	if _, err := load(path); err == nil {
		t.Fatal("load should fail for invalid access_ttl")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("load should fail when config file missing")
	}
}

func TestLoadWithEnvConfigPath(t *testing.T) {
	path := writeConfig(t, "server:\n  port: 7000\n")
	t.Setenv("APP_CONFIG_PATH", path)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with APP_CONFIG_PATH: %v", err)
	}
	if cfg.Server.Port != 7000 {
		t.Errorf("server.port = %d, want 7000", cfg.Server.Port)
	}
}

func TestLoadInvalidPort(t *testing.T) {
	for _, content := range []string{
		"server:\n  port: 0\n",
		"server:\n  port: 65536\n",
		"db:\n  port: 0\n",
		"db:\n  port: 70000\n",
	} {
		if _, err := load(writeConfig(t, content)); err == nil {
			t.Errorf("load should fail for %q", content)
		}
	}
}

func TestLoadAutoMigrateDefaultAndYaml(t *testing.T) {
	cfg, err := load(writeConfig(t, "db:\n  name: only_name\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.DB.AutoMigrate {
		t.Error("db.auto_migrate default = false, want true")
	}

	cfg, err = load(writeConfig(t, "db:\n  auto_migrate: false\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DB.AutoMigrate {
		t.Error("db.auto_migrate = true, want false from yaml")
	}
}

func TestLoadAutoMigrateEnvOverride(t *testing.T) {
	t.Setenv("APP_DB_AUTO_MIGRATE", "false")
	cfg, err := load(writeConfig(t, "db:\n  auto_migrate: true\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DB.AutoMigrate {
		t.Error("db.auto_migrate = true, want env override false")
	}
}
