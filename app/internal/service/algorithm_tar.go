package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"niko-vue-admin/app/internal/model"
)

// DefaultMaxPackageBytes 算法包解压最大容量限制（500MB）。
const DefaultMaxPackageBytes = 500 * 1024 * 1024

// ExtractedPackage 算法包在安全暂存区解压并解析出的元数据。
type ExtractedPackage struct {
	StagingDir        string          `json:"stagingDir"`
	ManifestRaw       json.RawMessage `json:"manifestRaw"`
	ConfigSchemaRaw   json.RawMessage `json:"configSchemaRaw"`
	AlgorithmID       string          `json:"algorithmId"`
	Name              string          `json:"name"`
	Version           string          `json:"version"`
	AlgorithmType     string          `json:"algorithmType"`
	AlarmTypeID       string          `json:"alarmTypeId"`
	PlatformID        string          `json:"platformId"`
	MinAdapterVersion string          `json:"minAdapterVersion"`
	Description       string          `json:"description"`
	FPSTiers          []model.FPSTier `json:"fpsTiers"`
	TotalSizeBytes    int64           `json:"totalSizeBytes"`
}

// ManifestSchema 对齐 manifest.schema.json 的解析结构。
type ManifestSchema struct {
	ManifestVersion   int    `json:"manifest_version"`
	AlgorithmID       string `json:"algorithm_id"`
	Name              string `json:"name"`
	Version           string `json:"version"`
	AlgorithmType     string `json:"algorithm_type"`
	AlarmTypeID       string `json:"alarm_type_id"`
	PlatformID        string `json:"platform_id"`
	MinAdapterVersion string `json:"min_adapter_version"`
	Description       string `json:"description"`
	ResourceProfile   struct {
		FPSTiers []struct {
			FPS   int32  `json:"fps"`
			Units uint32 `json:"units"`
		} `json:"fps_tiers"`
		MinFreeMemoryMB uint32 `json:"min_free_memory_mb"`
	} `json:"resource_profile"`
	RuntimeConstraints struct {
		MinOSVersion string `json:"min_os_version"`
	} `json:"runtime_constraints"`
	SelfTest struct {
		InputMode string `json:"input_mode"`
		TimeoutMS uint32 `json:"timeout_ms"`
	} `json:"self_test"`
}

// ExtractAndValidateArchive 安全解压算法包（.tar.gz / .tgz）至 targetDir，
// 严密防御 TarSlip 路径穿越漏洞与解压炸弹。
func ExtractAndValidateArchive(r io.Reader, targetDir string, maxSizeBytes int64) (*ExtractedPackage, error) {
	if maxSizeBytes <= 0 {
		maxSizeBytes = DefaultMaxPackageBytes
	}

	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve target path: %w", err)
	}

	// 嗅探魔数判断格式：gzip(\x1f\x8b)
	peekBuf := make([]byte, 512)
	n, err := io.ReadFull(r, peekBuf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to read archive header: %w", err)
	}
	peeked := peekBuf[:n]
	combinedReader := io.MultiReader(bytes.NewReader(peeked), r)

	var writtenBytes int64

	if bytes.HasPrefix(peeked, []byte{0x1f, 0x8b}) {
		writtenBytes, err = extractTarGz(combinedReader, absTargetDir, maxSizeBytes)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("unsupported package format: must be a valid .tar.gz archive")
	}

	// 3. 查找并解析 manifest.json 与 config.schema.json
	packageRoot := absTargetDir
	manifestPath := filepath.Join(packageRoot, "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		entries, err := os.ReadDir(absTargetDir)
		if err == nil && len(entries) == 1 && entries[0].IsDir() {
			candidate := filepath.Join(absTargetDir, entries[0].Name())
			if _, err := os.Stat(filepath.Join(candidate, "manifest.json")); err == nil {
				packageRoot = candidate
				manifestPath = filepath.Join(packageRoot, "manifest.json")
			}
		}
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("manifest.json not found in package: %w", err)
	}

	var manifest ManifestSchema
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("malformed manifest.json: %w", err)
	}

	if strings.TrimSpace(manifest.AlgorithmID) == "" || strings.TrimSpace(manifest.Version) == "" || strings.TrimSpace(manifest.PlatformID) == "" {
		return nil, errors.New("manifest.json must specify algorithm_id, version, and platform_id")
	}

	configSchemaBytes := []byte("{}")
	schemaPath := filepath.Join(packageRoot, "config.schema.json")
	if raw, err := os.ReadFile(schemaPath); err == nil {
		var dummy map[string]any
		if err := json.Unmarshal(raw, &dummy); err != nil {
			return nil, fmt.Errorf("invalid config.schema.json: %w", err)
		}
		configSchemaBytes = raw
	}

	var fpsTiers []model.FPSTier
	for _, tier := range manifest.ResourceProfile.FPSTiers {
		fpsTiers = append(fpsTiers, model.FPSTier{
			FPS:   tier.FPS,
			Units: tier.Units,
		})
	}

	return &ExtractedPackage{
		StagingDir:        packageRoot,
		ManifestRaw:       manifestBytes,
		ConfigSchemaRaw:   configSchemaBytes,
		AlgorithmID:       manifest.AlgorithmID,
		Name:              manifest.Name,
		Version:           manifest.Version,
		AlgorithmType:     manifest.AlgorithmType,
		AlarmTypeID:       manifest.AlarmTypeID,
		PlatformID:        manifest.PlatformID,
		MinAdapterVersion: manifest.MinAdapterVersion,
		Description:       manifest.Description,
		FPSTiers:          fpsTiers,
		TotalSizeBytes:    writtenBytes,
	}, nil
}

func extractTarGz(r io.Reader, absTargetDir string, maxSizeBytes int64) (int64, error) {
	gzReader, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("invalid gzip stream: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	var writtenBytes int64

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("tar read error: %w", err)
		}

		cleanName := filepath.Clean(header.Name)
		if strings.HasPrefix(cleanName, "/") || strings.HasPrefix(cleanName, "\\") || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
			return 0, fmt.Errorf("illegal relative file path in archive: %s", header.Name)
		}

		destPath := filepath.Join(absTargetDir, cleanName)
		if !strings.HasPrefix(destPath, absTargetDir+string(filepath.Separator)) && destPath != absTargetDir {
			return 0, fmt.Errorf("path traversal attempt detected: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return 0, fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return 0, fmt.Errorf("failed to create parent directory for %s: %w", destPath, err)
			}

			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode)&0o777)
			if err != nil {
				return 0, fmt.Errorf("failed to create file %s: %w", destPath, err)
			}

			remain := maxSizeBytes - writtenBytes
			if remain <= 0 {
				outFile.Close()
				return 0, fmt.Errorf("archive exceeds maximum uncompressed size limit of %d bytes", maxSizeBytes)
			}

			copied, err := io.Copy(outFile, io.LimitReader(tarReader, remain+1))
			outFile.Close()
			if err != nil {
				return 0, fmt.Errorf("failed to write %s: %w", destPath, err)
			}
			if copied > remain {
				return 0, fmt.Errorf("archive exceeds maximum uncompressed size limit of %d bytes", maxSizeBytes)
			}
			writtenBytes += copied

		case tar.TypeSymlink:
			return 0, fmt.Errorf("symlinks are not permitted in algorithm package: %s", header.Name)
		}
	}
	return writtenBytes, nil
}
