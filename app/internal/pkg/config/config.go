// Package config 负责加载应用配置：configs/config.yaml + APP_* 环境变量覆盖。
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 应用配置聚合。
type Config struct {
	Server Server
	DB     DB
	JWT    JWT
	Log    Log
}

// Server HTTP 服务配置。
type Server struct {
	Port int `mapstructure:"port"`
}

// DB 数据库连接配置（mysql / postgres 二选一，决策 18）。
type DB struct {
	Driver   string `mapstructure:"driver"` // mysql | postgres
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	// AutoMigrate 启动时自动建表/升级：仅 dev/test 使用；生产设为 false，
	// 表结构变更走 app/migrations 的版本化 SQL 脚本。
	AutoMigrate bool `mapstructure:"auto_migrate"`
}

// JWT 认证配置，TTL 已解析为 time.Duration。
type JWT struct {
	Secret     string        `mapstructure:"secret"`
	AccessTTL  time.Duration `mapstructure:"access_ttl"`
	RefreshTTL time.Duration `mapstructure:"refresh_ttl"`
}

// Log 日志配置。
type Log struct {
	Level string `mapstructure:"level"`
}

const (
	defaultConfigPath = "configs/config.yaml"

	defaultServerPort  = 8000
	defaultDBDriver    = "mysql"
	defaultDBHost      = "127.0.0.1"
	defaultDBPort      = 3306
	defaultDBUser      = "root"
	defaultDBPassword  = "123456" // 开发默认；生产用 APP_DB_PASSWORD 覆盖
	defaultDBName      = "niko_vue_admin"
	defaultAutoMigrate = true
	defaultJWTSecret   = "dev-secret-change-me"
	defaultAccessTTL   = 2 * time.Hour
	defaultRefreshTTL  = 168 * time.Hour
	defaultLogLevel    = "info"
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
		{"db.driver", defaultDBDriver},
		{"db.host", defaultDBHost},
		{"db.port", defaultDBPort},
		{"db.user", defaultDBUser},
		{"db.password", defaultDBPassword},
		{"db.name", defaultDBName},
		{"db.auto_migrate", defaultAutoMigrate},
		{"jwt.secret", defaultJWTSecret},
		{"jwt.access_ttl", defaultAccessTTL},
		{"jwt.refresh_ttl", defaultRefreshTTL},
		{"log.level", defaultLogLevel},
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
	if cfg.DB.Driver != "mysql" && cfg.DB.Driver != "postgres" {
		return fmt.Errorf("invalid db.driver %q: must be mysql or postgres", cfg.DB.Driver)
	}
	if cfg.DB.Port <= 0 || cfg.DB.Port > 65535 {
		return fmt.Errorf("invalid db.port: %d", cfg.DB.Port)
	}
	return nil
}
