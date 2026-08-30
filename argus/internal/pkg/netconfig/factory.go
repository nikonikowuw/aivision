package netconfig

import (
	"runtime"
)

// NewPlatform 根据当前操作系统和配置自动创建对应的 Platform 实例。
func NewPlatform(profilePath string, fakePlatform bool) (Platform, error) {
	if fakePlatform {
		var pType PlatformType
		switch runtime.GOOS {
		case "darwin":
			pType = PlatformDarwin
		case "linux":
			pType = PlatformLinux
		default:
			pType = PlatformFake
		}
		return NewFakePlatform(pType), nil
	}

	switch runtime.GOOS {
	case "darwin":
		return NewDarwinPlatform(fakePlatform)
	case "linux":
		return NewLinuxPlatform(profilePath, fakePlatform)
	default:
		return NewDefaultPlatform(fakePlatform)
	}
}
