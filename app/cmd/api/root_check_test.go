package main

import "testing"

func TestRequireRoot(t *testing.T) {
	// 非 root (euid != 0) 且未启用 fakePlatform 时必须失败
	if err := requireRoot(1000, false); err == nil {
		t.Error("requireRoot(1000, false) should return error, got nil")
	}
	if err := requireRoot(501, false); err == nil {
		t.Error("requireRoot(501, false) should return error, got nil")
	}

	// root (euid == 0) 应通过
	if err := requireRoot(0, false); err != nil {
		t.Errorf("requireRoot(0, false) should succeed, got %v", err)
	}

	// 启用 fakePlatform 时允许非 root
	if err := requireRoot(1000, true); err != nil {
		t.Errorf("requireRoot(1000, true) with fakePlatform should succeed, got %v", err)
	}
}
