package engineipc

import (
	"net"
	"os"
	"testing"

	"argus/app/internal/testutil"
)

// testSocketPath delegates to the shared short-path helper used by app tests.
func testSocketPath(t *testing.T, name string) string {
	return testutil.SocketPath(t, name)
}

// TestBindAppSocketWhenPathAbsent 路径不存在时绑定成功，且权限为 0660。
func TestBindAppSocketWhenPathAbsent(t *testing.T) {
	path := testSocketPath(t, "app.sock")
	owner, err := BindAppSocket(path)
	if err != nil {
		t.Fatalf("BindAppSocket: %v", err)
	}
	defer owner.Close()
	defer owner.Cleanup()

	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat after bind: %v", err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("path is not a socket: %v", fi.Mode())
	}
	if perm := fi.Mode().Perm(); perm != socketFileMode {
		t.Errorf("socket mode = %v, want %v", perm, socketFileMode)
	}
}

// TestBindAppSocketStaleCleanup 遗留 socket（无监听者）会被清理并重新绑定成功。
func TestBindAppSocketStaleCleanup(t *testing.T) {
	path := testSocketPath(t, "stale.sock")

	// 创建一个 socket 文件后关闭且禁用自动 unlink，模拟遗留 socket。
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen stale: %v", err)
	}
	ln.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatalf("close stale listener: %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket should exist: %v", err)
	}

	owner, err := BindAppSocket(path)
	if err != nil {
		t.Fatalf("BindAppSocket over stale socket: %v", err)
	}
	defer owner.Close()
	defer owner.Cleanup()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("socket should exist after rebind: %v", err)
	}
}

// TestBindAppSocketActiveRefused 活跃 listener 的 socket 拒绝抢占，且不删除文件。
func TestBindAppSocketActiveRefused(t *testing.T) {
	path := testSocketPath(t, "active.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen active: %v", err)
	}
	defer ln.Close()

	if _, err := BindAppSocket(path); err == nil {
		t.Fatal("BindAppSocket should fail when active listener exists")
	}
	// 活跃 socket 绝不能被 unlink。
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("active socket was removed: %v", err)
	}
}

// TestBindAppSocketSymlinkRefused 路径是 symlink 时拒绝绑定且不删除。
func TestBindAppSocketSymlinkRefused(t *testing.T) {
	path := testSocketPath(t, "link.sock")
	target := testSocketPath(t, "target.sock")
	// 创建真实 socket 作为 symlink 目标。
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: target, Net: "unix"})
	if err != nil {
		t.Fatalf("listen target: %v", err)
	}
	defer ln.Close()
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := BindAppSocket(path); err == nil {
		t.Fatal("BindAppSocket should fail for symlink path")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("symlink was removed: %v", err)
	}
}

// TestBindAppSocketRegularFileRefused 路径是普通文件时拒绝绑定且不删除。
func TestBindAppSocketRegularFileRefused(t *testing.T) {
	path := testSocketPath(t, "not-socket")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := BindAppSocket(path); err == nil {
		t.Fatal("BindAppSocket should fail for regular file")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("regular file was removed: %v", err)
	}
}

// TestBindAppSocketMissingParent 父目录不存在时启动失败，不自动创建运行目录。
func TestBindAppSocketMissingParent(t *testing.T) {
	path := testSocketPath(t, "no/such/dir/app.sock")
	if _, err := BindAppSocket(path); err == nil {
		t.Fatal("BindAppSocket should fail when parent directory missing")
	}
}

// TestSocketOwnerCleanup 正常关闭后 socket 文件被按 identity 删除。
func TestSocketOwnerCleanup(t *testing.T) {
	path := testSocketPath(t, "cleanup.sock")
	owner, err := BindAppSocket(path)
	if err != nil {
		t.Fatalf("BindAppSocket: %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := owner.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket should be removed after cleanup, got err=%v", err)
	}
}

// TestSocketOwnerCleanupIdentityReplaced 关闭前路径被替换为另一个 socket 时，
// 不删除替代对象（identity 复核）。
func TestSocketOwnerCleanupIdentityReplaced(t *testing.T) {
	path := testSocketPath(t, "replaced.sock")
	owner, err := BindAppSocket(path)
	if err != nil {
		t.Fatalf("BindAppSocket: %v", err)
	}
	// 模拟另一个进程在关闭期间创建替代 socket。
	if err := owner.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = os.Remove(path)
	replacement, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen replacement: %v", err)
	}
	defer replacement.Close()

	if err := owner.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	// 替代 socket 必须保留。
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("replacement socket was removed by stale owner: %v", err)
	}
}

// TestSocketOwnerCleanupRegularFileReplaced 关闭前路径被替换为普通文件时，不删除。
func TestSocketOwnerCleanupRegularFileReplaced(t *testing.T) {
	path := testSocketPath(t, "replaced-file.sock")
	owner, err := BindAppSocket(path)
	if err != nil {
		t.Fatalf("BindAppSocket: %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = os.Remove(path)
	if err := os.WriteFile(path, []byte("other"), 0o600); err != nil {
		t.Fatalf("write replacement file: %v", err)
	}

	if err := owner.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("replacement regular file was removed: %v", err)
	}
}

// TestSocketOwnerCleanupAlreadyAbsent 路径已不存在时 Cleanup 为幂等成功。
func TestSocketOwnerCleanupAlreadyAbsent(t *testing.T) {
	path := testSocketPath(t, "absent.sock")
	owner, err := BindAppSocket(path)
	if err != nil {
		t.Fatalf("BindAppSocket: %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := owner.Cleanup(); err != nil {
		t.Fatalf("Cleanup should be idempotent when absent: %v", err)
	}
}

// TestBindAppSocketOwnerDoesNotTouchForeignSocket 绑定失败（活跃占用）时绝不对
// 其他进程的 socket 做任何修改。
func TestBindAppSocketOwnerDoesNotTouchForeignSocket(t *testing.T) {
	path := testSocketPath(t, "foreign.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen foreign: %v", err)
	}
	defer ln.Close()

	// 尝试绑定同一路径应失败，且原始 listener 仍可用。
	if _, err := BindAppSocket(path); err == nil {
		t.Fatal("BindAppSocket should fail")
	}
	conn, err := net.DialTimeout("unix", path, socketProbeTimeout)
	if err != nil {
		t.Fatalf("foreign listener should still accept: %v", err)
	}
	_ = conn.Close()
}
