// Package logger 基于 zap 提供应用日志器，级别来自配置。
package logger

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"argus/app/internal/pkg/config"
)

// New 按配置日志级别构建 zap.Logger；非法级别返回错误。
func New(cfg *config.Config) (*zap.Logger, error) {
	level := zapcore.InfoLevel
	if err := level.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", cfg.Log.Level, err)
	}
	zcfg := zap.NewProductionConfig()
	zcfg.Level = zap.NewAtomicLevelAt(level)
	zcfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	return zcfg.Build()
}
