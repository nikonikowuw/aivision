package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
  secure_cookie: true
log:
  level: debug
storage:
  driver: minio
  max_size: 2097152
  local:
    root: /srv/uploads
    url_prefix: /files
  minio:
    endpoint: minio.example.com:9000
    access_key: access
    secret_key: secret
    bucket: files
    use_ssl: true
    public_base_url: https://cdn.example.com/files
ipc:
  app_socket: /run/aivision/app.sock
  engine_socket: /run/aivision/engine.sock
`)
	cfg, err := load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("server.port = %d, want 9000", cfg.Server.Port)
	}
	if cfg.DB.Host != "10.0.0.1" || cfg.DB.Port != 5432 || cfg.DB.User != "app" ||
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
	if !cfg.JWT.SecureCookie {
		t.Error("jwt.secure_cookie = false, want true")
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log.level = %q, want debug", cfg.Log.Level)
	}
	if cfg.Storage.Driver != "minio" || cfg.Storage.MaxSize != 2097152 {
		t.Errorf("storage = %+v", cfg.Storage)
	}
	if cfg.Storage.Local.Root != "/srv/uploads" || cfg.Storage.Local.URLPrefix != "/files" {
		t.Errorf("storage.local = %+v", cfg.Storage.Local)
	}
	if cfg.Storage.MinIO.Endpoint != "minio.example.com:9000" ||
		cfg.Storage.MinIO.AccessKey != "access" || cfg.Storage.MinIO.SecretKey != "secret" ||
		cfg.Storage.MinIO.Bucket != "files" || !cfg.Storage.MinIO.UseSSL ||
		cfg.Storage.MinIO.PublicBaseURL != "https://cdn.example.com/files" {
		t.Errorf("storage.minio = %+v", cfg.Storage.MinIO)
	}
	if cfg.IPC.AppSocket != "/run/aivision/app.sock" || cfg.IPC.EngineSocket != "/run/aivision/engine.sock" {
		t.Errorf("ipc = %+v", cfg.IPC)
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
	if cfg.DB.Host != "127.0.0.1" || cfg.DB.Port != 5432 || cfg.DB.User != "postgres" {
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
	if cfg.JWT.SecureCookie {
		t.Error("jwt.secure_cookie default = true, want false")
	}
	if cfg.Log.Level != "info" {
		t.Errorf("log.level = %q, want default info", cfg.Log.Level)
	}
	if cfg.Storage.Driver != "local" || cfg.Storage.MaxSize != 10*1024*1024 {
		t.Errorf("storage defaults not applied: %+v", cfg.Storage)
	}
	if cfg.Storage.Local.Root != "./uploads" || cfg.Storage.Local.URLPrefix != "/uploads" {
		t.Errorf("storage.local defaults not applied: %+v", cfg.Storage.Local)
	}
	if cfg.Network.StateDir != "/var/lib/aivision/network" || cfg.Network.ProfilePath != "/etc/aivision/network-profile.json" ||
		cfg.Network.ConfirmTimeout != 120*time.Second || cfg.Network.FakePlatform {
		t.Errorf("network defaults not applied: %+v", cfg.Network)
	}
	if cfg.IPC.AppSocket != "/tmp/aivision-app.sock" || cfg.IPC.EngineSocket != "/tmp/aivision-engine.sock" {
		t.Errorf("ipc defaults not applied: %+v", cfg.IPC)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	path := writeConfig(t, `
server:
  port: 8000
db:
  host: 127.0.0.1
  port: 5432
  user: postgres
  password: "postgres"
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
	t.Setenv("APP_JWT_SECURE_COOKIE", "true")
	t.Setenv("APP_SERVER_PORT", "9001")
	t.Setenv("APP_STORAGE_MAX_SIZE", "2097152")
	t.Setenv("APP_STORAGE_LOCAL_ROOT", "/var/lib/uploads")
	t.Setenv("APP_STORAGE_LOCAL_URL_PREFIX", "/public-files")
	t.Setenv("APP_NETWORK_STATE_DIR", "/tmp/test-network")
	t.Setenv("APP_NETWORK_PROFILE_PATH", "/tmp/test-profile.json")
	t.Setenv("APP_NETWORK_CONFIRM_TIMEOUT", "60s")
	t.Setenv("APP_NETWORK_FAKE_PLATFORM", "true")
	t.Setenv("APP_IPC_APP_SOCKET", "/tmp/env-app.sock")
	t.Setenv("APP_IPC_ENGINE_SOCKET", "/tmp/env-engine.sock")

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
	if !cfg.JWT.SecureCookie {
		t.Error("jwt.secure_cookie = false, want env override true")
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
	if cfg.Storage.MaxSize != 2097152 || cfg.Storage.Local.Root != "/var/lib/uploads" ||
		cfg.Storage.Local.URLPrefix != "/public-files" {
		t.Errorf("storage env overrides not applied: %+v", cfg.Storage)
	}
	if cfg.Network.StateDir != "/tmp/test-network" || cfg.Network.ProfilePath != "/tmp/test-profile.json" ||
		cfg.Network.ConfirmTimeout != 60*time.Second || !cfg.Network.FakePlatform {
		t.Errorf("network env overrides not applied: %+v", cfg.Network)
	}
	if cfg.IPC.AppSocket != "/tmp/env-app.sock" || cfg.IPC.EngineSocket != "/tmp/env-engine.sock" {
		t.Errorf("ipc env overrides not applied: %+v", cfg.IPC)
	}
}

func TestLoadStorageEnvOverride(t *testing.T) {
	path := writeConfig(t, "storage:\n  driver: local\n")
	t.Setenv("APP_STORAGE_DRIVER", "minio")
	t.Setenv("APP_STORAGE_MAX_SIZE", "3145728")
	t.Setenv("APP_STORAGE_MINIO_ENDPOINT", "minio.internal:9000")
	t.Setenv("APP_STORAGE_MINIO_ACCESS_KEY", "env-access")
	t.Setenv("APP_STORAGE_MINIO_SECRET_KEY", "env-secret")
	t.Setenv("APP_STORAGE_MINIO_BUCKET", "env-bucket")
	t.Setenv("APP_STORAGE_MINIO_USE_SSL", "true")
	t.Setenv("APP_STORAGE_MINIO_PUBLIC_BASE_URL", "https://files.example.com/env-bucket")

	cfg, err := load(path)
	if err != nil {
		t.Fatalf("load with storage env: %v", err)
	}
	if cfg.Storage.Driver != "minio" || cfg.Storage.MaxSize != 3145728 {
		t.Errorf("storage env overrides not applied: %+v", cfg.Storage)
	}
	if cfg.Storage.MinIO.Endpoint != "minio.internal:9000" ||
		cfg.Storage.MinIO.AccessKey != "env-access" || cfg.Storage.MinIO.SecretKey != "env-secret" ||
		cfg.Storage.MinIO.Bucket != "env-bucket" || !cfg.Storage.MinIO.UseSSL ||
		cfg.Storage.MinIO.PublicBaseURL != "https://files.example.com/env-bucket" {
		t.Errorf("storage.minio env overrides not applied: %+v", cfg.Storage.MinIO)
	}
}

func TestLoadInvalidStorageDriver(t *testing.T) {
	path := writeConfig(t, "storage:\n  driver: s3\n")
	if _, err := load(path); err == nil {
		t.Fatal("load should fail for invalid storage.driver")
	}
}

func TestLoadInvalidStorageMaxSize(t *testing.T) {
	for _, content := range []string{
		"storage:\n  max_size: 0\n",
		"storage:\n  max_size: -1\n",
	} {
		if _, err := load(writeConfig(t, content)); err == nil {
			t.Errorf("load should fail for invalid storage.max_size in %q", content)
		}
	}
}

func TestLoadInvalidLocalStorageConfig(t *testing.T) {
	for _, content := range []string{
		"storage:\n  local:\n    root: \"\"\n",
		"storage:\n  local:\n    url_prefix: \"\"\n",
		"storage:\n  local:\n    url_prefix: uploads\n",
		"storage:\n  local:\n    url_prefix: /../uploads\n",
		"storage:\n  local:\n    url_prefix: /\n",
		"storage:\n  local:\n    url_prefix: //host\n",
		"storage:\n  local:\n    url_prefix: /uploads//public\n",
		"storage:\n  local:\n    url_prefix: /uploads/\n",
		"storage:\n  local:\n    url_prefix: \\uploads\n",
	} {
		if _, err := load(writeConfig(t, content)); err == nil {
			t.Errorf("load should fail for invalid local storage config in %q", content)
		}
	}
}

func TestLoadInvalidMinIOStorageConfig(t *testing.T) {
	valid := `storage:
  driver: minio
  minio:
    endpoint: minio.example.com:9000
    access_key: access
    secret_key: secret
    bucket: files
    public_base_url: https://cdn.example.com/files
`
	for _, tc := range []struct {
		name string
		from string
		to   string
	}{
		{name: "endpoint", from: "endpoint: minio.example.com:9000", to: "endpoint: \"\""},
		{name: "access key", from: "access_key: access", to: "access_key: \"\""},
		{name: "secret key", from: "secret_key: secret", to: "secret_key: \"\""},
		{name: "bucket", from: "bucket: files", to: "bucket: \"\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := strings.Replace(valid, tc.from, tc.to, 1)
			if _, err := load(writeConfig(t, content)); err == nil {
				t.Fatalf("load should fail for empty minio.%s", tc.name)
			}
		})
	}
	for _, publicBaseURL := range []string{"ftp://cdn.example.com/files", "/files", "http:///files", "https://user:pass@cdn.example.com/files", "https://cdn.example.com/files?download=1", "https://cdn.example.com/files#fragment"} {
		t.Run(publicBaseURL, func(t *testing.T) {
			content := strings.Replace(valid, "public_base_url: https://cdn.example.com/files", "public_base_url: \""+publicBaseURL+"\"", 1)
			if _, err := load(writeConfig(t, content)); err == nil {
				t.Fatalf("load should fail for invalid public_base_url %q", publicBaseURL)
			}
		})
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

func TestLoadInvalidIPC(t *testing.T) {
	cases := map[string]string{
		"empty app_socket":       "ipc:\n  app_socket: \"\"\n",
		"empty engine_socket":    "ipc:\n  engine_socket: \"\"\n",
		"relative app_socket":    "ipc:\n  app_socket: relative.sock\n",
		"relative engine_socket": "ipc:\n  engine_socket: relative.sock\n",
		"same paths":             "ipc:\n  app_socket: /tmp/x.sock\n  engine_socket: /tmp/x.sock\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := load(writeConfig(t, content)); err == nil {
				t.Fatalf("load should fail for %q", content)
			}
		})
	}
}

func TestLoadEngineProfile(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	profilePath := filepath.Join(t.TempDir(), "engine-profile.json")
	profile := fmt.Sprintf(`{
  "schema_version": 1,
  "platform_id": "test",
  "paths": {"runtime_dir": %q},
  "ipc": {"app_socket": "app.sock", "engine_socket": "engine.sock"}
}`, runtimeDir)
	if err := os.WriteFile(profilePath, []byte(profile), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	t.Setenv("AIVISION_ENGINE_PROFILE", profilePath)

	cfg, err := load(writeConfig(t, `ipc:
  app_socket: /tmp/yaml-app.sock
  engine_socket: /tmp/yaml-engine.sock
`))
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if cfg.IPC.ProfilePath != profilePath {
		t.Errorf("profile path = %q, want %q", cfg.IPC.ProfilePath, profilePath)
	}
	if cfg.IPC.AppSocket != filepath.Join(runtimeDir, "app.sock") ||
		cfg.IPC.EngineSocket != filepath.Join(runtimeDir, "engine.sock") {
		t.Errorf("profile IPC = %+v", cfg.IPC)
	}
}

func TestLoadEngineProfileRejectsPerSocketOverrides(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	profilePath := filepath.Join(t.TempDir(), "engine-profile.json")
	profile := fmt.Sprintf(`{"schema_version":1,"paths":{"runtime_dir":%q},"ipc":{"app_socket":"app.sock","engine_socket":"engine.sock"}}`, runtimeDir)
	if err := os.WriteFile(profilePath, []byte(profile), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	t.Setenv("AIVISION_ENGINE_PROFILE", profilePath)
	t.Setenv("APP_IPC_APP_SOCKET", filepath.Join(runtimeDir, "override.sock"))

	if _, err := load(writeConfig(t, "{}")); err == nil {
		t.Fatal("load should reject APP_IPC_APP_SOCKET in Profile mode")
	}
}

func TestLoadEngineProfileRejectsInvalidPaths(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	cases := map[string]string{
		"unsupported schema": fmt.Sprintf(`{"schema_version":2,"paths":{"runtime_dir":%q},"ipc":{"app_socket":"app.sock","engine_socket":"engine.sock"}}`, runtimeDir),
		"absolute socket":    fmt.Sprintf(`{"schema_version":1,"paths":{"runtime_dir":%q},"ipc":{"app_socket":"/tmp/app.sock","engine_socket":"engine.sock"}}`, runtimeDir),
		"traversal socket":   fmt.Sprintf(`{"schema_version":1,"paths":{"runtime_dir":%q},"ipc":{"app_socket":"../app.sock","engine_socket":"engine.sock"}}`, runtimeDir),
		"same socket":        fmt.Sprintf(`{"schema_version":1,"paths":{"runtime_dir":%q},"ipc":{"app_socket":"app.sock","engine_socket":"app.sock"}}`, runtimeDir),
	}
	for name, profile := range cases {
		t.Run(name, func(t *testing.T) {
			profilePath := filepath.Join(t.TempDir(), "engine-profile.json")
			if err := os.WriteFile(profilePath, []byte(profile), 0o600); err != nil {
				t.Fatalf("write profile: %v", err)
			}
			t.Setenv("AIVISION_ENGINE_PROFILE", profilePath)
			if _, err := load(writeConfig(t, "{}")); err == nil {
				t.Fatal("load should reject invalid engine profile")
			}
		})
	}
}

func TestLoadOpenPersonSyncAllowedIPs(t *testing.T) {
	valid := `open:
  person_sync_allowed_ips:
    - 192.168.1.100
    - 10.0.0.0/24
    - "2001:db8::1"
    - "2001:db8::/32"
`
	cfg, err := load(writeConfig(t, valid))
	if err != nil {
		t.Fatalf("load valid open config: %v", err)
	}
	if len(cfg.Open.PersonSyncAllowedIPs) != 4 {
		t.Fatalf("expected 4 allowed IPs, got %d", len(cfg.Open.PersonSyncAllowedIPs))
	}

	invalid := []string{
		"open:\n  person_sync_allowed_ips:\n    - \"\"\n",
		"open:\n  person_sync_allowed_ips:\n    - \"invalid_ip\"\n",
		"open:\n  person_sync_allowed_ips:\n    - \"192.168.1.300\"\n",
		"open:\n  person_sync_allowed_ips:\n    - \"10.0.0.0/33\"\n",
	}
	for _, content := range invalid {
		if _, err := load(writeConfig(t, content)); err == nil {
			t.Errorf("load should fail for invalid open config %q", content)
		}
	}
}

func TestLoadOpenPersonSyncAllowedIPsFromEnv(t *testing.T) {
	path := writeConfig(t, "open:\n  person_sync_allowed_ips: []\n")
	t.Setenv("APP_OPEN_PERSON_SYNC_ALLOWED_IPS", "192.0.2.10, 2001:db8::/32")

	cfg, err := load(path)
	if err != nil {
		t.Fatalf("load open IPs from env: %v", err)
	}
	want := []string{"192.0.2.10", "2001:db8::/32"}
	if len(cfg.Open.PersonSyncAllowedIPs) != len(want) {
		t.Fatalf("allowed IP count = %d, want %d: %v", len(cfg.Open.PersonSyncAllowedIPs), len(want), cfg.Open.PersonSyncAllowedIPs)
	}
	for i := range want {
		if cfg.Open.PersonSyncAllowedIPs[i] != want[i] {
			t.Errorf("allowed IP[%d] = %q, want %q", i, cfg.Open.PersonSyncAllowedIPs[i], want[i])
		}
	}
}

func TestLoadInvalidOpenPersonSyncAllowedIPsFromEnv(t *testing.T) {
	path := writeConfig(t, "open:\n  person_sync_allowed_ips: []\n")
	t.Setenv("APP_OPEN_PERSON_SYNC_ALLOWED_IPS", "192.0.2.10,not-an-ip")

	if _, err := load(path); err == nil {
		t.Fatal("load should fail for invalid open IP from env")
	}
}
