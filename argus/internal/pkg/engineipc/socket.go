// Package engineipc 提供 Engine 专用 gRPC over Unix Domain Socket 传输基础设施：
// 入站服务（app.sock，Engine 回调 ControlPlane/Report）、出站客户端（engine.sock，
// Go 调用 EngineService）以及安全的 socket 文件生命周期管理。
//
// 本包不引用 repository、GORM、Gin 或具体业务 service；业务方通过窄 adapter 端口接入。
package engineipc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

// socketProbeTimeout 是探测既有 socket 是否活跃的短超时。
const socketProbeTimeout = 100 * time.Millisecond

// socketFileMode 是自有 socket 文件的权限（owner/group 可读写，其他不可访问）。
// 访问控制依赖 runtime 目录 owner/group 与最小权限；UDS 上不启用额外 peer 认证。
const socketFileMode os.FileMode = 0o660

// SocketOwner 持有并管理一个自有 Unix socket 文件的生命周期。
//
// 安全不变量：
//   - 活跃 listener 的 socket 绝不被 unlink；
//   - 只清理经探测证明没有监听者的遗留 socket；
//   - 关闭时只删除经 file identity 复核仍由本进程拥有的 socket。
type SocketOwner struct {
	path string
	ln   *net.UnixListener
	info os.FileInfo // ListenUnix + chmod 后记录的 file identity
}

// BindAppSocket 安全绑定一个自有 Unix socket：
//
//  1. lstat：不存在则继续；symlink、普通文件及其他非 socket 对象一律拒绝启动。
//  2. 对已有 socket 做短超时 dial probe；连接成功表示活跃 listener，拒绝启动且绝不 unlink。
//  3. 仅当错误链是 ECONNREFUSED/ENOENT/ENOTCONN 时，重新 lstat 并确认 identity 未变化，
//     再删除遗留 socket。
//  4. ListenUnix 成功后关闭自动 unlink、设置 0660，并记录该 socket 的 file identity。
//
// 父目录不存在、不可写、路径过长、权限修改失败都属于启动失败；本函数不创建
// 或修改安全关键运行目录。调用方在失败时负责关闭已获得的 listener。
func BindAppSocket(path string) (*SocketOwner, error) {
	if err := prepareSocketPath(path); err != nil {
		return nil, err
	}

	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", path, err)
	}
	// 关闭自动 unlink：socket 文件的删除只能由本进程按 identity 复核后执行。
	ln.SetUnlinkOnClose(false)

	// macOS 对 Unix socket fd 的 fchmod 返回 EINVAL，因此以 bind 后立即 lstat
	// 记录 identity，并在 chmod 前后复核路径仍指向同一 socket。
	listenerInfo, err := os.Lstat(path)
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("lstat after listen %s: %w", path, err)
	}
	if listenerInfo.Mode()&os.ModeSocket == 0 {
		_ = ln.Close()
		return nil, fmt.Errorf("socket path %s is not a socket after listen", path)
	}
	cleanupOnError := func(cause error) (*SocketOwner, error) {
		_ = ln.Close()
		if fi, statErr := os.Lstat(path); statErr == nil && os.SameFile(fi, listenerInfo) {
			_ = os.Remove(path)
		}
		return nil, cause
	}
	if err := os.Chmod(path, socketFileMode); err != nil {
		return cleanupOnError(fmt.Errorf("chmod %s: %w", path, err))
	}

	pathInfo, err := os.Lstat(path)
	if err != nil {
		return cleanupOnError(fmt.Errorf("lstat after chmod %s: %w", path, err))
	}
	if pathInfo.Mode()&os.ModeSocket == 0 || !os.SameFile(pathInfo, listenerInfo) {
		return cleanupOnError(fmt.Errorf("socket path %s was replaced during bind", path))
	}
	return &SocketOwner{path: path, ln: ln, info: listenerInfo}, nil
}

// prepareSocketPath 检查并准备既有路径：不存在直接通过；是 socket 则探测活跃性，
// 仅清理确认的遗留 socket。返回错误时路径不被修改。
func prepareSocketPath(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("lstat %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to bind %s: existing object is not a socket (mode %v)", path, fi.Mode())
	}

	conn, err := net.DialTimeout("unix", path, socketProbeTimeout)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("refusing to bind %s: active listener detected", path)
	}
	if !isStaleSocketErr(err) {
		return fmt.Errorf("refusing to bind %s: probe failed with %w", path, err)
	}

	// 仅探测确认无监听者时清理遗留 socket；重新 lstat 确认 identity 未变化，
	// 避免删除探测期间被替换的对象。
	fi2, err2 := os.Lstat(path)
	if err2 != nil {
		if os.IsNotExist(err2) {
			return nil // 探测期间已被移除，无需清理
		}
		return fmt.Errorf("re-lstat %s: %w", path, err2)
	}
	if !os.SameFile(fi, fi2) {
		return fmt.Errorf("refusing to bind %s: socket identity changed during probe", path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket %s: %w", path, err)
	}
	return nil
}

// isStaleSocketErr 判断 dial 错误链是否属于「遗留 socket」三类 errno：
// ECONNREFUSED（无监听者）、ENOENT（已被移除）、ENOTCONN。
func isStaleSocketErr(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ECONNREFUSED, syscall.ENOENT, syscall.ENOTCONN:
			return true
		}
	}
	return false
}

// Listener 返回绑定好的 Unix listener（供 gRPC server 使用）。
func (o *SocketOwner) Listener() *net.UnixListener {
	return o.ln
}

// Path 返回 socket 文件路径。
func (o *SocketOwner) Path() string {
	return o.path
}

// Close 关闭 listener 但不删除 socket 文件；socket 文件由 Cleanup 按 identity 清理。
func (o *SocketOwner) Close() error {
	return o.ln.Close()
}

// Cleanup 在关闭时删除自有 socket 文件：重新 lstat，只有路径仍是同一个 socket
// （identity 未变化）才删除，避免误删关闭期间由其他进程创建的替代对象。
func (o *SocketOwner) Cleanup() error {
	fi, err := os.Lstat(o.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("lstat %s during cleanup: %w", o.path, err)
	}
	if fi.Mode()&os.ModeSocket == 0 || !os.SameFile(fi, o.info) {
		// 路径被替换为其他对象或另一个 socket，不属于本进程，绝不删除。
		return nil
	}
	if err := os.Remove(o.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s during cleanup: %w", o.path, err)
	}
	return nil
}
