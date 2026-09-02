package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatfsSampler_RealDir(t *testing.T) {
	tempDir := t.TempDir()

	sampler := NewDiskUsageSampler()
	usage, err := sampler.GetDiskUsage(tempDir)
	require.NoError(t, err)

	assert.Greater(t, usage.TotalBytes, uint64(0))
	assert.GreaterOrEqual(t, usage.TotalBytes, usage.UsedBytes)
	assert.GreaterOrEqual(t, usage.TotalBytes, usage.FreeBytes)
	assert.GreaterOrEqual(t, usage.UsagePercent, 0.0)
	assert.LessOrEqual(t, usage.UsagePercent, 100.0)
}

func TestStatfsSampler_NonExistentPathWithExistingParent(t *testing.T) {
	tempDir := t.TempDir()
	nonExistentSubDir := filepath.Join(tempDir, "sub_dir_not_created_yet")

	sampler := NewDiskUsageSampler()
	usage, err := sampler.GetDiskUsage(nonExistentSubDir)
	require.NoError(t, err)
	assert.Greater(t, usage.TotalBytes, uint64(0))
}

func TestStatfsSampler_InvalidPath(t *testing.T) {
	sampler := NewDiskUsageSampler()
	_, err := sampler.GetDiskUsage("/path/that/definitely/does/not/exist/12345/67890")
	require.Error(t, err)
}

func TestMockDiskUsageSampler(t *testing.T) {
	expectedUsage := DiskUsage{
		TotalBytes:   1000,
		UsedBytes:    850,
		FreeBytes:    150,
		UsagePercent: 85.0,
	}

	mock := NewMockDiskUsageSampler(expectedUsage, nil)
	usage, err := mock.GetDiskUsage("/any/path")
	require.NoError(t, err)
	assert.Equal(t, expectedUsage, usage)

	mockErr := errors.New("simulated io error")
	mockWithError := NewMockDiskUsageSampler(DiskUsage{}, mockErr)
	_, err = mockWithError.GetDiskUsage("/any/path")
	require.ErrorIs(t, err, mockErr)
}

func TestDiskUsageCalculations(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.dat")
	err := os.WriteFile(testFile, make([]byte, 1024), 0o600)
	require.NoError(t, err)

	sampler := NewDiskUsageSampler()
	usage, err := sampler.GetDiskUsage(tempDir)
	require.NoError(t, err)
	assert.Equal(t, usage.TotalBytes-usage.FreeBytes, usage.UsedBytes)
}
