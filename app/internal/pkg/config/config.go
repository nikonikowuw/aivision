// Package config 负责加载应用配置：configs/config.yaml + APP_* 环境变量覆盖。
package config

import (
	"fmt"
	"net/url"
	"os"
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
	StateDir       string        `mapstructure:"state_dir"`        // root-only 状态存储目录
	ProfilePath    string        `mapstructure:"profile_path"`     // Linux Profile 声明文件路径
	ConfirmTimeout time.Duration `mapstructure:"confirm_timeout"`  // 候选确认超时时间（默认 120s）
	FakePlatform   bool          `mapstructure:"fake_platform"`    // 是否启用测试替身平台（单元/集成测试用）
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
	return validateNetwork(&cfg.Network)
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
