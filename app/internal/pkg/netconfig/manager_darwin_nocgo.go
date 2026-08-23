//go:build darwin && !cgo

package netconfig

import (
	"context"
	"fmt"
)

// DarwinNoCgoPlatform 在未开启 cgo 时明确返回 unsupported。
type DarwinNoCgoPlatform struct{}

func NewDarwinPlatform(fakePlatform bool) (Platform, error) {
	if fakePlatform {
		return NewFakePlatform(PlatformDarwin), nil
	}
	return nil, fmt.Errorf("%w: macOS platform requires cgo for SystemConfiguration integration", ErrUnsupported)
}
