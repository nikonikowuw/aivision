//go:build !darwin

package netconfig

import "fmt"

func NewDarwinPlatform(fakePlatform bool) (Platform, error) {
	if fakePlatform {
		return NewFakePlatform(PlatformDarwin), nil
	}
	return nil, fmt.Errorf("%w: not supported on non-darwin OS", ErrUnsupported)
}
