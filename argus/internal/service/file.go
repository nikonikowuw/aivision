package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"

	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/storage"
)

const (
	// UploadFileFieldName 是上传接口约定的 multipart 字段名。
	UploadFileFieldName = "file"
	maxMIMESniffBytes   = 512
	objectKeyRandomSize = 16
)

var allowedUploadTypes = map[string]string{
	".gif":  "image/gif",
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".pdf":  "application/pdf",
	".png":  "image/png",
	".webp": "image/webp",
}

// FileService 负责文件上传校验和存储编排。
type FileService interface {
	Upload(ctx context.Context, input *UploadInput) (*UploadedFile, error)
}

// UploadInput 是 API 层传入的文件信息，不依赖 Gin 或 multipart 类型。
type UploadInput struct {
	Name   string
	Size   int64
	Reader io.Reader
}

// UploadedFile 是上传成功后返回给调用方的文件元数据。
type UploadedFile struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
	URL         string `json:"url"`
}

type fileService struct {
	store   storage.FileStorage
	maxSize int64
}

// NewFileService 创建文件上传服务。store 不能为 nil。
func NewFileService(store storage.FileStorage, cfg *config.Config) FileService {
	if store == nil {
		panic("NewFileService: store must not be nil")
	}
	var maxSize int64
	if cfg != nil {
		maxSize = cfg.Storage.MaxSize
	}
	return &fileService{store: store, maxSize: maxSize}
}

func (s *fileService) Upload(ctx context.Context, input *UploadInput) (*UploadedFile, error) {
	if input == nil {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}
	if s.maxSize <= 0 {
		return nil, fmt.Errorf("file upload max size is invalid")
	}
	if input.Size <= 0 {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}
	if input.Size > s.maxSize {
		return nil, errno.NewError(errno.CodeFileTooLarge)
	}
	if input.Reader == nil {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}

	name, expectedType, err := normalizeUploadName(input.Name)
	if err != nil {
		return nil, err
	}

	headSize := minUploadSniffSize(input.Size)
	head := make([]byte, headSize)
	if _, err := io.ReadFull(input.Reader, head); err != nil {
		return nil, errno.NewError(errno.CodeInvalidParam)
	}
	detectedType := mimetype.Detect(head)
	if !detectedType.Is(expectedType) {
		return nil, errno.NewError(errno.CodeFileTypeNotAllowed)
	}

	key, err := newUploadKey(time.Now().UTC(), filepath.Ext(name))
	if err != nil {
		return nil, fmt.Errorf("generate upload key: %w", err)
	}
	stored, err := s.store.Put(ctx, storage.PutInput{
		Key:         key,
		Reader:      io.MultiReader(bytes.NewReader(head), input.Reader),
		Size:        input.Size,
		ContentType: expectedType,
	})
	if err != nil {
		if errors.Is(err, storage.ErrSizeMismatch) {
			return nil, errno.NewError(errno.CodeInvalidParam)
		}
		return nil, fmt.Errorf("store uploaded file: %w", err)
	}

	return &UploadedFile{
		Key:         stored.Key,
		Name:        name,
		Size:        stored.Size,
		ContentType: stored.ContentType,
		URL:         stored.URL,
	}, nil
}

func normalizeUploadName(rawName string) (string, string, error) {
	name := strings.TrimSpace(rawName)
	if name == "" || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." {
		return "", "", errno.NewError(errno.CodeInvalidParam)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", "", errno.NewError(errno.CodeInvalidParam)
		}
	}

	extension := strings.ToLower(filepath.Ext(name))
	expectedType, ok := allowedUploadTypes[extension]
	if !ok {
		return "", "", errno.NewError(errno.CodeFileTypeNotAllowed)
	}
	return name, expectedType, nil
}

func minUploadSniffSize(size int64) int {
	if size < maxMIMESniffBytes {
		return int(size)
	}
	return maxMIMESniffBytes
}

func newUploadKey(now time.Time, extension string) (string, error) {
	randomBytes := make([]byte, objectKeyRandomSize)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"%04d/%02d/%02d/%s%s",
		now.Year(), now.Month(), now.Day(), hex.EncodeToString(randomBytes), strings.ToLower(extension),
	), nil
}
