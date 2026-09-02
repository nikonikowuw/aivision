// Package storage 提供与具体供应商无关的文件对象存储接口及实现。
package storage

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// DiskUsage 表示指定路径挂载点的磁盘容量与使用率快照。
type DiskUsage struct {
	TotalBytes   uint64  `json:"totalBytes"`
	UsedBytes    uint64  `json:"usedBytes"`
	FreeBytes    uint64  `json:"freeBytes"`
	UsagePercent float64 `json:"usagePercent"`
}

// DiskUsageSampler 磁盘使用率采样器接口。
type DiskUsageSampler interface {
	GetDiskUsage(path string) (DiskUsage, error)
}

type statfsSampler struct{}

// NewDiskUsageSampler 创建基于底层系统 statfs 的磁盘采样器。
func NewDiskUsageSampler() DiskUsageSampler {
	return &statfsSampler{}
}

// GetDiskUsage 采样指定目录所在文件系统的磁盘用量。
func (s *statfsSampler) GetDiskUsage(path string) (DiskUsage, error) {
	if path == "" {
		return DiskUsage{}, fmt.Errorf("storage path is empty")
	}
	cleanPath := filepath.Clean(path)

	// 确保路径存在或检查其父级目录
	if _, err := os.Stat(cleanPath); err != nil {
		if os.IsNotExist(err) {
			// 如果目录尚不存在，尝试检查父目录
			parent := filepath.Dir(cleanPath)
			if _, pErr := os.Stat(parent); pErr != nil {
				return DiskUsage{}, fmt.Errorf("storage path does not exist: %w", err)
			}
			cleanPath = parent
		} else {
			return DiskUsage{}, fmt.Errorf("stat storage path: %w", err)
		}
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(cleanPath, &stat); err != nil {
		return DiskUsage{}, fmt.Errorf("statfs %q: %w", cleanPath, err)
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	if freeBytes > totalBytes {
		freeBytes = totalBytes
	}
	usedBytes := totalBytes - freeBytes

	var usagePercent float64
	if totalBytes > 0 {
		usagePercent = float64(usedBytes) / float64(totalBytes) * 100.0
		usagePercent = math.Round(usagePercent*100) / 100 // 保留两位小数
	}

	return DiskUsage{
		TotalBytes:   totalBytes,
		UsedBytes:    usedBytes,
		FreeBytes:    freeBytes,
		UsagePercent: usagePercent,
	}, nil
}

// MockDiskUsageSampler 用于测试的磁盘采样器替身。
type MockDiskUsageSampler struct {
	Usage DiskUsage
	Err   error
}

// NewMockDiskUsageSampler 创建指定返回结果的模拟采样器。
func NewMockDiskUsageSampler(usage DiskUsage, err error) *MockDiskUsageSampler {
	return &MockDiskUsageSampler{
		Usage: usage,
		Err:   err,
	}
}

// GetDiskUsage 返回预先设定的模拟值。
func (m *MockDiskUsageSampler) GetDiskUsage(path string) (DiskUsage, error) {
	if m.Err != nil {
		return DiskUsage{}, m.Err
	}
	return m.Usage, nil
}
