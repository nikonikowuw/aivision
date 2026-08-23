//go:build !linux

package netconfig

import "fmt"

func NewLinuxPlatform(profilePath string, fakePlatform bool) (Platform, error) {
	if fakePlatform {
		return NewFakePlatform(PlatformLinux), nil
	}
	return nil, fmt.Errorf("%w: not supported on non-linux OS", ErrUnsupported)
}
