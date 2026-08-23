package netconfig

import "fmt"

func NewDefaultPlatform(fakePlatform bool) (Platform, error) {
	if fakePlatform {
		return NewFakePlatform(PlatformFake), nil
	}
	return nil, fmt.Errorf("%w: unsupported OS platform", ErrUnsupported)
}
