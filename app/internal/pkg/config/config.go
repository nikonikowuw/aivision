// Package config 负责加载应用配置：configs/config.yaml + APP_* 环境变量覆盖。
// 生产 IPC 端点可由唯一的 AIVISION_ENGINE_PROFILE 版本化 Profile 提供。
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 应用配置聚合。
type Config struct {
	Server  Server
	DB      DB
	JWT     JWT
	Log     Log
	Storage Storage `mapstructure:"storage"`
	Network Network `mapstructure:"network"`
	IPC     IPC     `mapstructure:"ipc"`
}

// Server HTTP 服务配置。
type Server struct {
	Port int `mapstructure:"port"`
}

// DB 数据库连接配置（PostgreSQL）。
type DB struct {
	Host        string        `mapstructure:"host"`
	Port        int           `mapstructure:"port"`
	User        string        `mapstructure:"user"`
	Password    string        `mapstructure:"password"`
	Name        string        `mapstructure:"name"`
	TimeZone    string        `mapstructure:"time_zone"`    // 数据库时区，例如 Asia/Shanghai
	MaxOpen     int           `mapstructure:"max_open"`     // 最大打开连接数（默认 100）
	MaxIdle     int           `mapstructure:"max_idle"`     // 最大空闲连接数（默认 10）
	MaxLifetime time.Duration `mapstructure:"max_lifetime"` // 连接最大生命周期（默认 30m）
}

// JWT 认证配置，TTL 已解析为 time.Duration。
type JWT struct {
	Secret       string        `mapstructure:"secret"`
	AccessTTL    time.Duration `mapstructure:"access_ttl"`
	RefreshTTL   time.Duration `mapstructure:"refresh_ttl"`
	SecureCookie bool          `mapstructure:"secure_cookie"`
}

// Log 日志配置。
type Log struct {
	Level string `mapstructure:"level"`
}

// Storage 文件存储配置。
type Storage struct {
	Driver  string `mapstructure:"driver"` // local | minio
	MaxSize int64  `mapstructure:"max_size"`
	Local   Local  `mapstructure:"local"`
	MinIO   MinIO  `mapstructure:"minio"`
}

// Local 本地文件存储配置。
type Local struct {
	Root      string `mapstructure:"root"`
	URLPrefix string `mapstructure:"url_prefix"`
}

// MinIO MinIO 文件存储配置。
type MinIO struct {
	Endpoint      string `mapstructure:"endpoint"`
	AccessKey     string `mapstructure:"access_key"`
	SecretKey     string `mapstructure:"secret_key"`
	Bucket        string `mapstructure:"bucket"`
	UseSSL        bool   `mapstructure:"use_ssl"`
	PublicBaseURL string `mapstructure:"public_base_url"`
}

// Network 网络配置服务配置。
type Network struct {
	StateDir       string        `mapstructure:"state_dir"`       // root-only 状态存储目录
	ProfilePath    string        `mapstructure:"profile_path"`    // Linux Profile 声明文件路径
	ConfirmTimeout time.Duration `mapstructure:"confirm_timeout"` // 候选确认超时时间（默认 120s）
	FakePlatform   bool          `mapstructure:"fake_platform"`   // 是否启用测试替身平台（单元/集成测试用）
}

// IPC 进程间通信（gRPC over Unix Domain Socket）配置。
type IPC struct {
	ProfilePath  string `mapstructure:"profile_path"`  // 生产 Profile 的唯一入口（AIVISION_ENGINE_PROFILE）
	AppSocket    string `mapstructure:"app_socket"`    // Go 侧 app.sock：Engine 回调 ControlPlane/Report
	EngineSocket string `mapstructure:"engine_socket"` // C++ 侧 engine.sock：Go 调用 EngineService
}

const (
	defaultConfigPath = "configs/config.yaml"

	defaultServerPort      = 8000
	defaultDBHost          = "127.0.0.1"
	defaultDBPort          = 5432
	defaultDBUser          = "postgres"
	defaultDBPassword      = "postgres" // 开发默认；生产用 APP_DB_PASSWORD 覆盖
	defaultDBName          = "niko_vue_admin_go"
	defaultDBTimeZone      = "Asia/Shanghai"
	defaultDBMaxOpen       = 100
	defaultDBMaxIdle       = 10
	defaultDBMaxLifetime   = 30 * time.Minute
	defaultJWTSecret       = "dev-secret-change-me"
	defaultAccessTTL       = 2 * time.Hour
	defaultRefreshTTL      = 168 * time.Hour
	defaultJWTSecureCookie = false
	defaultLogLevel        = "info"

	// StorageDriverLocal 使用本地文件系统存储。
	StorageDriverLocal = "local"
	// StorageDriverMinIO 使用 MinIO 对象存储。
	StorageDriverMinIO = "minio"

	defaultStorageDriver               = StorageDriverLocal
	defaultStorageMaxSize        int64 = 10 * 1024 * 1024
	defaultStorageLocalRoot            = "./uploads"
	defaultStorageLocalURLPrefix       = "/uploads"
	defaultStorageMinIOUseSSL          = false

	defaultNetworkStateDir       = "/var/lib/aivision/network"
	defaultNetworkProfilePath    = "/etc/aivision/network-profile.json"
	defaultNetworkConfirmTimeout = 120 * time.Second
	defaultNetworkFakePlatform   = false

	// 开发默认与当前 C++ Engine 的 AIVISION_{APP,ENGINE}_SOCKET 默认一致；
	// 生产部署应配置为 /var/run/aivision/{app,engine}.sock。
	defaultIPCAPPSocket    = "/tmp/aivision-app.sock"
	defaultIPCEngineSocket = "/tmp/aivision-engine.sock"
)

// Load 读取配置：默认路径 configs/config.yaml，可用环境变量 APP_CONFIG_PATH 覆盖，
// 失败返回错误。
func Load() (*Config, error) {
	path := defaultConfigPath
	if p := os.Getenv("APP_CONFIG_PATH"); p != "" {
		path = p
	}
	return load(path)
}

func load(path string) (*Config, error) {
	v := viper.New()
	// 先注册默认值（优先级最低）：yaml/env 缺失时生效，且使缺失键也能被 APP_* 覆盖。
	for _, d := range defaults() {
		v.SetDefault(d.key, d.value)
	}
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	// APP_* 环境变量覆盖：APP_DB_HOST → db.host
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	for _, key := range v.AllKeys() {
		if err := v.BindEnv(key); err != nil {
			return nil, fmt.Errorf("bind env for %s: %w", key, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := applyEngineProfile(&cfg.IPC); err != nil {
		return nil, err
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// keyValue 默认配置项（key 与 defaults() 条目一一对应，防漂移）。
type keyValue struct {
	key   string
	value any
}

func defaults() []keyValue {
	return []keyValue{
		{"server.port", defaultServerPort},
		{"db.host", defaultDBHost},
		{"db.port", defaultDBPort},
		{"db.user", defaultDBUser},
		{"db.password", defaultDBPassword},
		{"db.name", defaultDBName},
		{"db.time_zone", defaultDBTimeZone},
		{"db.max_open", defaultDBMaxOpen},
		{"db.max_idle", defaultDBMaxIdle},
		{"db.max_lifetime", defaultDBMaxLifetime},
		{"jwt.secret", defaultJWTSecret},
		{"jwt.access_ttl", defaultAccessTTL},
		{"jwt.refresh_ttl", defaultRefreshTTL},
		{"jwt.secure_cookie", defaultJWTSecureCookie},
		{"log.level", defaultLogLevel},
		{"storage.driver", defaultStorageDriver},
		{"storage.max_size", defaultStorageMaxSize},
		{"storage.local.root", defaultStorageLocalRoot},
		{"storage.local.url_prefix", defaultStorageLocalURLPrefix},
		{"storage.minio.endpoint", ""},
		{"storage.minio.access_key", ""},
		{"storage.minio.secret_key", ""},
		{"storage.minio.bucket", ""},
		{"storage.minio.use_ssl", defaultStorageMinIOUseSSL},
		{"storage.minio.public_base_url", ""},
		{"network.state_dir", defaultNetworkStateDir},
		{"network.profile_path", defaultNetworkProfilePath},
		{"network.confirm_timeout", defaultNetworkConfirmTimeout},
		{"network.fake_platform", defaultNetworkFakePlatform},
		{"ipc.app_socket", defaultIPCAPPSocket},
		{"ipc.engine_socket", defaultIPCEngineSocket},
	}
}

// UsingDefaultJWTSecret 报告当前是否仍在使用开发默认 JWT 密钥（生产需 APP_JWT_SECRET 覆盖）。
func (c *Config) UsingDefaultJWTSecret() bool {
	return c.JWT.Secret == defaultJWTSecret
}

// validate 校验显式配置的非法值。
func validate(cfg *Config) error {
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("invalid server.port: %d", cfg.Server.Port)
	}
	if cfg.DB.Port <= 0 || cfg.DB.Port > 65535 {
		return fmt.Errorf("invalid db.port: %d", cfg.DB.Port)
	}
	if strings.TrimSpace(cfg.DB.TimeZone) == "" {
		return fmt.Errorf("db.time_zone cannot be empty")
	}
	if _, err := time.LoadLocation(cfg.DB.TimeZone); err != nil {
		return fmt.Errorf("invalid db.time_zone %q: %w", cfg.DB.TimeZone, err)
	}
	if strings.TrimSpace(cfg.JWT.Secret) == "" {
		return fmt.Errorf("jwt.secret cannot be empty")
	}
	if err := validateStorage(&cfg.Storage); err != nil {
		return err
	}
	if err := validateNetwork(&cfg.Network); err != nil {
		return err
	}
	return validateIPC(&cfg.IPC)
}

func validateNetwork(network *Network) error {
	if strings.TrimSpace(network.StateDir) == "" {
		return fmt.Errorf("network.state_dir cannot be empty")
	}
	if strings.TrimSpace(network.ProfilePath) == "" {
		return fmt.Errorf("network.profile_path cannot be empty")
	}
	if network.ConfirmTimeout <= 0 {
		return fmt.Errorf("network.confirm_timeout must be greater than zero")
	}
	return nil
}

// validateIPC 校验解析后的 IPC socket 路径：非空、绝对路径、无 NUL、两个路径不能相同。
func validateIPC(ipc *IPC) error {
	if strings.TrimSpace(ipc.AppSocket) == "" {
		return fmt.Errorf("ipc.app_socket cannot be empty")
	}
	if strings.TrimSpace(ipc.EngineSocket) == "" {
		return fmt.Errorf("ipc.engine_socket cannot be empty")
	}
	if !filepath.IsAbs(ipc.AppSocket) {
		return fmt.Errorf("ipc.app_socket %q must be an absolute path", ipc.AppSocket)
	}
	if !filepath.IsAbs(ipc.EngineSocket) {
		return fmt.Errorf("ipc.engine_socket %q must be an absolute path", ipc.EngineSocket)
	}
	if strings.ContainsRune(ipc.AppSocket, '\x00') || strings.ContainsRune(ipc.EngineSocket, '\x00') {
		return fmt.Errorf("ipc socket paths must not contain NUL")
	}
	if filepath.Clean(ipc.AppSocket) == filepath.Clean(ipc.EngineSocket) {
		return fmt.Errorf("ipc.app_socket and ipc.engine_socket must be different paths")
	}
	return nil
}

const engineProfileEnv = "AIVISION_ENGINE_PROFILE"

// engineDeploymentProfile 是生产部署 Profile 中供 Go IPC 边界使用的字段。
// 其他 Profile 字段由 Engine/部署工具消费，这里保留并不复制第二份配置真相。
type engineDeploymentProfile struct {
	SchemaVersion int `json:"schema_version"`
	Paths         struct {
		RuntimeDir string `json:"runtime_dir"`
	} `json:"paths"`
	IPC struct {
		AppSocket    string `json:"app_socket"`
		EngineSocket string `json:"engine_socket"`
	} `json:"ipc"`
}

// applyEngineProfile 在设置 AIVISION_ENGINE_PROFILE 时用单一版本化 Profile 覆盖 IPC 端点。
// 未设置 Profile 时保留 APP_IPC_* 开发兼容入口；Profile 模式禁止逐项环境变量覆盖。
func applyEngineProfile(ipc *IPC) error {
	profilePath, configured := os.LookupEnv(engineProfileEnv)
	if !configured {
		return nil
	}
	if profilePath == "" || strings.TrimSpace(profilePath) != profilePath {
		return fmt.Errorf("%s cannot be empty", engineProfileEnv)
	}
	if strings.ContainsRune(profilePath, '\x00') || !filepath.IsAbs(profilePath) {
		return fmt.Errorf("%s %q must be an absolute path without NUL", engineProfileEnv, profilePath)
	}
	if _, ok := os.LookupEnv("APP_IPC_APP_SOCKET"); ok {
		return fmt.Errorf("APP_IPC_APP_SOCKET cannot be used with %s", engineProfileEnv)
	}
	if _, ok := os.LookupEnv("APP_IPC_ENGINE_SOCKET"); ok {
		return fmt.Errorf("APP_IPC_ENGINE_SOCKET cannot be used with %s", engineProfileEnv)
	}

	data, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("read engine profile %s: %w", profilePath, err)
	}
	var profile engineDeploymentProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return fmt.Errorf("decode engine profile %s: %w", profilePath, err)
	}
	if profile.SchemaVersion != 1 {
		return fmt.Errorf("unsupported engine profile schema_version %d", profile.SchemaVersion)
	}
	runtimeDir := profile.Paths.RuntimeDir
	if runtimeDir == "" || strings.TrimSpace(runtimeDir) != runtimeDir ||
		!filepath.IsAbs(runtimeDir) || strings.ContainsRune(runtimeDir, '\x00') {
		return fmt.Errorf("engine profile paths.runtime_dir must be an absolute path without NUL")
	}
	runtimeDir = filepath.Clean(runtimeDir)
	appSocket, err := resolveProfileSocket(runtimeDir, profile.IPC.AppSocket)
	if err != nil {
		return fmt.Errorf("engine profile ipc.app_socket: %w", err)
	}
	engineSocket, err := resolveProfileSocket(runtimeDir, profile.IPC.EngineSocket)
	if err != nil {
		return fmt.Errorf("engine profile ipc.engine_socket: %w", err)
	}
	if appSocket == engineSocket {
		return fmt.Errorf("engine profile app_socket and engine_socket must be different paths")
	}
	ipc.ProfilePath = profilePath
	ipc.AppSocket = appSocket
	ipc.EngineSocket = engineSocket
	return nil
}

// resolveProfileSocket resolves a relative socket name and rejects path traversal outside runtime_dir.
func resolveProfileSocket(runtimeDir, socketName string) (string, error) {
	if socketName == "" || strings.TrimSpace(socketName) != socketName {
		return "", fmt.Errorf("socket name cannot be empty or contain outer whitespace")
	}
	if strings.ContainsRune(socketName, '\x00') || filepath.IsAbs(socketName) {
		return "", fmt.Errorf("socket name must be relative and contain no NUL")
	}
	cleanName := filepath.Clean(socketName)
	if cleanName == "." || cleanName == ".." || cleanName != socketName {
		return "", fmt.Errorf("socket name %q is not a normalized relative path", socketName)
	}
	resolved := filepath.Clean(filepath.Join(runtimeDir, cleanName))
	rel, err := filepath.Rel(runtimeDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("socket name %q escapes runtime_dir", socketName)
	}
	return resolved, nil
}

func validateStorage(storage *Storage) error {
	if storage.MaxSize <= 0 {
		return fmt.Errorf("storage.max_size must be greater than zero")
	}
	if storage.Driver != StorageDriverLocal && storage.Driver != StorageDriverMinIO {
		return fmt.Errorf("invalid storage.driver %q: must be local or minio", storage.Driver)
	}

	switch storage.Driver {
	case StorageDriverLocal:
		if strings.TrimSpace(storage.Local.Root) == "" {
			return fmt.Errorf("storage.local.root cannot be empty")
		}
		if err := validateLocalURLPrefix(storage.Local.URLPrefix); err != nil {
			return err
		}
	case StorageDriverMinIO:
		if strings.TrimSpace(storage.MinIO.Endpoint) == "" {
			return fmt.Errorf("storage.minio.endpoint cannot be empty")
		}
		if strings.TrimSpace(storage.MinIO.AccessKey) == "" {
			return fmt.Errorf("storage.minio.access_key cannot be empty")
		}
		if strings.TrimSpace(storage.MinIO.SecretKey) == "" {
			return fmt.Errorf("storage.minio.secret_key cannot be empty")
		}
		if strings.TrimSpace(storage.MinIO.Bucket) == "" {
			return fmt.Errorf("storage.minio.bucket cannot be empty")
		}
		if err := validateMinIOPublicBaseURL(storage.MinIO.PublicBaseURL); err != nil {
			return err
		}
	}
	return nil
}

func validateLocalURLPrefix(prefix string) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return fmt.Errorf("storage.local.url_prefix cannot be empty")
	}
	if prefix == "/" {
		return fmt.Errorf("storage.local.url_prefix cannot be root path")
	}
	if strings.ContainsAny(prefix, "?#") || strings.Contains(prefix, `\`) || strings.HasPrefix(prefix, "//") {
		return fmt.Errorf("storage.local.url_prefix %q contains invalid path characters", prefix)
	}
	if !strings.HasPrefix(prefix, "/") {
		return fmt.Errorf("storage.local.url_prefix %q must start with /", prefix)
	}
	for _, segment := range strings.Split(strings.TrimPrefix(prefix, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("storage.local.url_prefix %q contains an invalid path segment", prefix)
		}
	}
	return nil
}

func validateMinIOPublicBaseURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid storage.minio.public_base_url %q: %w", rawURL, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("invalid storage.minio.public_base_url %q: must use http or https", rawURL)
	}
	if parsed.Host == "" {
		return fmt.Errorf("invalid storage.minio.public_base_url %q: host is required", rawURL)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid storage.minio.public_base_url %q: credentials, query, or fragment are not allowed", rawURL)
	}
	return nil
}
