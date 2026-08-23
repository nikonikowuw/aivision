//go:build linux

package netconfig

import (
	"go.uber.org/zap"
)

func newLinuxGatewayBackend(logger *zap.Logger) GatewayBackend {
	return NewLinuxGatewayBackend(logger)
}
