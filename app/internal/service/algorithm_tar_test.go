package service

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractAndValidateArchive_TarSlipProtection(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// 构造一个带有目录穿越的文件 ../evil.txt
	header := &tar.Header{
		Name:     "../evil.txt",
		Mode:     0o644,
		Size:     4,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("evil")); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()

	tempDir := t.TempDir()
	_, err := ExtractAndValidateArchive(&buf, tempDir, 1024*1024)
	if err == nil {
		t.Fatalf("expected error on TarSlip archive, got nil")
	}
}

func TestExtractAndValidateArchive_ValidTarGz(t *testing.T) {
	manifestJSON := `{
		"manifest_version": 1,
		"algorithm_id": "test_detector",
		"name": "Test Detector",
		"version": "1.0.0",
		"algorithm_type": "object_detection",
		"alarm_type_id": "object_detect",
		"platform_id": "macos-arm64-coreml",
		"resource_profile": {
			"fps_tiers": [{"fps": 15, "units": 100}]
		}
	}`

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// 写入 manifest.json
	h1 := &tar.Header{
		Name:     "manifest.json",
		Mode:     0o644,
		Size:     int64(len(manifestJSON)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(h1); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(manifestJSON))

	// 写入 config.schema.json
	schemaJSON := `{"type": "object"}`
	h2 := &tar.Header{
		Name:     "config.schema.json",
		Mode:     0o644,
		Size:     int64(len(schemaJSON)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(h2); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(schemaJSON))

	tw.Close()
	gw.Close()

	tempDir := t.TempDir()
	pkg, err := ExtractAndValidateArchive(&buf, tempDir, 1024*1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pkg.AlgorithmID != "test_detector" || pkg.Version != "1.0.0" {
		t.Errorf("parsed metadata mismatch: %+v", pkg)
	}
	if len(pkg.FPSTiers) != 1 || pkg.FPSTiers[0].FPS != 15 {
		t.Errorf("fps tiers mismatch: %+v", pkg.FPSTiers)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "manifest.json")); err != nil {
		t.Errorf("manifest.json should exist on disk: %v", err)
	}
}

func TestExtractAndValidateArchive_ValidZip(t *testing.T) {
	manifestJSON := `{
		"manifest_version": 1,
		"algorithm_id": "test_zip_detector",
		"name": "Test Zip Detector",
		"version": "2.0.0",
		"algorithm_type": "object_detection",
		"alarm_type_id": "object_detect",
		"platform_id": "macos-arm64-coreml",
		"resource_profile": {
			"fps_tiers": [{"fps": 30, "units": 200}]
		}
	}`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 写入 manifest.json
	w1, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w1.Write([]byte(manifestJSON)); err != nil {
		t.Fatal(err)
	}

	// 写入 testimage.jpg
	w2, err := zw.Create("testimage.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w2.Write([]byte("fake image content")); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	tempDir := t.TempDir()
	pkg, err := ExtractAndValidateArchive(&buf, tempDir, 1024*1024)
	if err != nil {
		t.Fatalf("unexpected error on zip extract: %v", err)
	}

	if pkg.AlgorithmID != "test_zip_detector" || pkg.Version != "2.0.0" {
		t.Errorf("parsed metadata mismatch: %+v", pkg)
	}
	if len(pkg.FPSTiers) != 1 || pkg.FPSTiers[0].FPS != 30 {
		t.Errorf("fps tiers mismatch: %+v", pkg.FPSTiers)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "manifest.json")); err != nil {
		t.Errorf("manifest.json should exist on disk: %v", err)
	}
}

func TestExtractAndValidateArchive_ZipSlipProtection(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	w, err := zw.Create("../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("evil payload")); err != nil {
		t.Fatal(err)
	}
	zw.Close()

	tempDir := t.TempDir()
	_, err = ExtractAndValidateArchive(&buf, tempDir, 1024*1024)
	if err == nil {
		t.Fatalf("expected error on ZipSlip archive, got nil")
	}
}

func TestExtractAndValidateArchive_ValidTar(t *testing.T) {
	manifestJSON := `{
		"manifest_version": 1,
		"algorithm_id": "test_tar_detector",
		"name": "Test Tar Detector",
		"version": "3.0.0",
		"algorithm_type": "object_detection",
		"alarm_type_id": "object_detect",
		"platform_id": "rk3576-rknn",
		"resource_profile": {
			"fps_tiers": [{"fps": 25, "units": 150}]
		}
	}`

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// 写入 manifest.json
	h1 := &tar.Header{
		Name:     "manifest.json",
		Mode:     0o644,
		Size:     int64(len(manifestJSON)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(h1); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(manifestJSON))

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	tempDir := t.TempDir()
	pkg, err := ExtractAndValidateArchive(&buf, tempDir, 1024*1024)
	if err != nil {
		t.Fatalf("unexpected error on tar extract: %v", err)
	}

	if pkg.AlgorithmID != "test_tar_detector" || pkg.Version != "3.0.0" {
		t.Errorf("parsed metadata mismatch: %+v", pkg)
	}
	if len(pkg.FPSTiers) != 1 || pkg.FPSTiers[0].FPS != 25 {
		t.Errorf("fps tiers mismatch: %+v", pkg.FPSTiers)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "manifest.json")); err != nil {
		t.Errorf("manifest.json should exist on disk: %v", err)
	}
}


