package main

import "fmt"

// requireRoot 检查有效用户 ID 是否为 root (0)。在测试模式或单元测试中可通过参数验证。
func requireRoot(euid int, fakePlatform bool) error {
	if fakePlatform {
		return nil
	}
	if euid != 0 {
		return fmt.Errorf("network service requires root privileges (EUID=0), current EUID=%d. Please run as root", euid)
	}
	return nil
}
