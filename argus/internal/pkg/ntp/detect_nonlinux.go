//go:build !linux

package ntp

func newLinuxExecutor() Executor {
	return newUnavailableExecutor("Linux executor is unavailable on this operating system")
}
