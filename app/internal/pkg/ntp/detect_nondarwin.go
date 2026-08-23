//go:build !darwin

package ntp

func newDarwinExecutor() Executor {
	return newUnavailableExecutor("darwin executor is unavailable on this operating system")
}
