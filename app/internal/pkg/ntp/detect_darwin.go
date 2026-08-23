//go:build darwin

package ntp

func newDarwinExecutor() Executor {
	return NewDarwinExecutor()
}
