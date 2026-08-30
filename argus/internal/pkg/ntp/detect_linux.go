//go:build linux

package ntp

import "os/exec"

func newLinuxExecutor() Executor {
	// 优先探测 chronyc
	if _, err := exec.LookPath("chronyc"); err == nil {
		return NewChronyExecutor()
	}
	// 其次探测 timedatectl
	if _, err := exec.LookPath("timedatectl"); err == nil {
		return NewTimesyncdExecutor()
	}
	return newUnavailableExecutor("no supported Linux NTP tool found")
}
