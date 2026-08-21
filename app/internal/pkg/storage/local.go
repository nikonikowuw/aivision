package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type localStorage struct {
	root      string
	urlPrefix string
}

// NewLocalStorage 创建本地文件系统存储，并确保根目录存在。
func NewLocalStorage(root, urlPrefix string) (FileStorage, error) {
	root = strings.TrimSpace(root)
	urlPrefix = strings.TrimRight(strings.TrimSpace(urlPrefix), "/")
	if root == "" {
		return nil, fmt.Errorf("local storage root is empty")
	}
	if urlPrefix == "" || urlPrefix == "/" || !strings.HasPrefix(urlPrefix, "/") || strings.ContainsAny(urlPrefix, "?#") || strings.Contains(urlPrefix, `\`) || strings.HasPrefix(urlPrefix, "//") {
		return nil, fmt.Errorf("local storage URL prefix must be a non-root path without query or fragment")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(urlPrefix, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return nil, fmt.Errorf("local storage URL prefix contains an invalid path segment")
		}
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create local storage root: %w", err)
	}
	return &localStorage{root: filepath.Clean(root), urlPrefix: urlPrefix}, nil
}

func (s *localStorage) Put(ctx context.Context, input PutInput) (StoredObject, error) {
	if err := ctx.Err(); err != nil {
		return StoredObject{}, fmt.Errorf("local storage context: %w", err)
	}
	if err := validatePutInput(input); err != nil {
		return StoredObject{}, err
	}

	target, err := s.targetPath(input.Key)
	if err != nil {
		return StoredObject{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return StoredObject{}, fmt.Errorf("create local storage directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(target), ".upload-*")
	if err != nil {
		return StoredObject{}, fmt.Errorf("create local storage temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()

	written, err := io.Copy(temporary, io.LimitReader(input.Reader, input.Size+1))
	if err != nil {
		_ = temporary.Close()
		return StoredObject{}, fmt.Errorf("write local storage temporary file: %w", err)
	}
	if written != input.Size {
		_ = temporary.Close()
		return StoredObject{}, fmt.Errorf("local storage size mismatch: wrote %d, want %d", written, input.Size)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return StoredObject{}, fmt.Errorf("sync local storage temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return StoredObject{}, fmt.Errorf("close local storage temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return StoredObject{}, fmt.Errorf("local storage context: %w", err)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return StoredObject{}, fmt.Errorf("commit local storage file: %w", err)
	}
	committed = true

	return StoredObject{
		Key:         input.Key,
		URL:         s.urlPrefix + "/" + input.Key,
		Size:        input.Size,
		ContentType: input.ContentType,
	}, nil
}

func (s *localStorage) targetPath(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}

	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", fmt.Errorf("resolve local storage root: %w", err)
	}
	target := filepath.Join(root, filepath.FromSlash(key))
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve local storage target: %w", err)
	}
	if targetAbs != root && !strings.HasPrefix(targetAbs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("local storage key escapes root")
	}
	return targetAbs, nil
}
