// Package storage 提供与具体供应商无关的文件对象存储接口及实现。
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"argus/app/internal/pkg/config"
)

// ErrSizeMismatch 表示写入字节数与声明大小不匹配。
var ErrSizeMismatch = errors.New("storage size mismatch")

// FileStorage 保存一个已经完成业务校验的文件对象。
type FileStorage interface {
	Put(ctx context.Context, input PutInput) (StoredObject, error)
}

// PutInput 是存储层写入文件所需的最小输入。
type PutInput struct {
	Key         string
	Reader      io.Reader
	Size        int64
	ContentType string
}

// StoredObject 是存储层返回的对象信息。
type StoredObject struct {
	Key         string
	URL         string
	Size        int64
	ContentType string
}

// New 根据配置构造文件存储实现。
func New(cfg *config.Config) (FileStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("storage config is nil")
	}

	switch cfg.Storage.Driver {
	case config.StorageDriverLocal:
		return NewLocalStorage(cfg.Storage.Local.Root, cfg.Storage.Local.URLPrefix)
	case config.StorageDriverMinIO:
		return NewMinIOStorage(cfg.Storage.MinIO)
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", cfg.Storage.Driver)
	}
}

func validatePutInput(input PutInput) error {
	if input.Reader == nil {
		return fmt.Errorf("storage reader is nil")
	}
	if input.Size <= 0 {
		return fmt.Errorf("storage object size must be greater than zero")
	}
	if strings.TrimSpace(input.ContentType) == "" {
		return fmt.Errorf("storage content type is empty")
	}
	return validateKey(input.Key)
}

func validateKey(key string) error {
	if key == "" || filepath.IsAbs(filepath.FromSlash(key)) || strings.HasPrefix(key, "/") {
		return fmt.Errorf("invalid storage key %q", key)
	}
	if strings.Contains(key, "\\") {
		return fmt.Errorf("invalid storage key %q", key)
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("invalid storage key %q", key)
		}
	}
	return nil
}
