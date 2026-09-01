package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/storage"
)

func TestFileServiceUploadAcceptsAllowedTypes(t *testing.T) {
	tests := []struct {
		name        string
		fileName    string
		content     []byte
		contentType string
	}{
		{name: "jpeg", fileName: "photo.jpeg", content: []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}, contentType: "image/jpeg"},
		{name: "png", fileName: "photo.png", content: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, contentType: "image/png"},
		{name: "gif", fileName: "photo.gif", content: []byte("GIF89a"), contentType: "image/gif"},
		{name: "webp", fileName: "photo.webp", content: []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '}, contentType: "image/webp"},
		{name: "pdf", fileName: "document.pdf", content: []byte("%PDF-1.7\n"), contentType: "application/pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &captureFileStorage{}
			svc := newTestFileService(store, 1024)
			got, err := svc.Upload(context.Background(), &UploadInput{
				Name:   tt.fileName,
				Size:   int64(len(tt.content)),
				Reader: bytes.NewReader(tt.content),
			})
			if err != nil {
				t.Fatalf("Upload: %v", err)
			}
			if got.Name != tt.fileName || got.ContentType != tt.contentType || got.Size != int64(len(tt.content)) {
				t.Fatalf("unexpected result: %+v", got)
			}
			if !bytes.Equal(store.content, tt.content) {
				t.Fatalf("stored content = %v, want %v", store.content, tt.content)
			}
			if strings.Contains(got.Key, tt.fileName) || !strings.HasSuffix(got.Key, "."+strings.TrimPrefix(strings.ToLower(tt.fileName[strings.LastIndex(tt.fileName, "."):]), ".")) {
				t.Fatalf("key exposes filename or has wrong extension: %q", got.Key)
			}
		})
	}
}

func TestFileServiceUploadRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		in   *UploadInput
		code int
	}{
		{name: "nil input", in: nil, code: errno.CodeInvalidParam},
		{name: "empty file", in: &UploadInput{Name: "empty.png", Size: 0, Reader: bytes.NewReader(nil)}, code: errno.CodeInvalidParam},
		{name: "too large", in: &UploadInput{Name: "large.png", Size: 9, Reader: bytes.NewReader([]byte("123456789"))}, code: errno.CodeFileTooLarge},
		{name: "path traversal", in: &UploadInput{Name: "../image.png", Size: 8, Reader: bytes.NewReader([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})}, code: errno.CodeInvalidParam},
		{name: "backslash path", in: &UploadInput{Name: `..\image.png`, Size: 8, Reader: bytes.NewReader([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})}, code: errno.CodeInvalidParam},
		{name: "unsupported extension", in: &UploadInput{Name: "script.js", Size: 4, Reader: strings.NewReader("text")}, code: errno.CodeFileTypeNotAllowed},
		{name: "content mismatch", in: &UploadInput{Name: "image.png", Size: 8, Reader: strings.NewReader("%PDF-1.7")}, code: errno.CodeFileTypeNotAllowed},
		{name: "short reader", in: &UploadInput{Name: "image.png", Size: 8, Reader: bytes.NewReader([]byte{0x89, 'P', 'N', 'G'})}, code: errno.CodeInvalidParam},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestFileService(&captureFileStorage{}, 8)
			_, err := svc.Upload(context.Background(), tt.in)
			if err == nil {
				t.Fatal("Upload should fail")
			}
			var businessErr *errno.Error
			if !errors.As(err, &businessErr) {
				t.Fatalf("Upload error = %T %v, want errno.Error", err, err)
			}
			if businessErr.Code != tt.code {
				t.Fatalf("error code = %d, want %d", businessErr.Code, tt.code)
			}
		})
	}
}

func TestFileServiceUploadPropagatesStorageError(t *testing.T) {
	store := &captureFileStorage{err: errors.New("storage unavailable")}
	svc := newTestFileService(store, 1024)

	_, err := svc.Upload(context.Background(), &UploadInput{
		Name:   "image.png",
		Size:   8,
		Reader: bytes.NewReader([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}),
	})
	if err == nil || !strings.Contains(err.Error(), "storage unavailable") {
		t.Fatalf("Upload error = %v, want storage error", err)
	}
}

func TestFileServiceUploadSizeMismatchReturnsInvalidParam(t *testing.T) {
	store := &captureFileStorage{err: fmt.Errorf("wrote 4, want 8: %w", storage.ErrSizeMismatch)}
	svc := newTestFileService(store, 1024)

	_, err := svc.Upload(context.Background(), &UploadInput{
		Name:   "image.png",
		Size:   8,
		Reader: bytes.NewReader([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}),
	})
	if err == nil {
		t.Fatal("Upload should fail")
	}
	var businessErr *errno.Error
	if !errors.As(err, &businessErr) {
		t.Fatalf("Upload error = %T %v, want errno.Error", err, err)
	}
	if businessErr.Code != errno.CodeInvalidParam {
		t.Fatalf("error code = %d, want %d", businessErr.Code, errno.CodeInvalidParam)
	}
}

func TestNewUploadKey(t *testing.T) {
	got, err := newUploadKey(time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC), ".PNG")
	if err != nil {
		t.Fatalf("newUploadKey: %v", err)
	}
	if !strings.HasPrefix(got, "2026/08/21/") || !strings.HasSuffix(got, ".png") {
		t.Fatalf("key = %q", got)
	}
	if strings.Contains(got, " ") {
		t.Fatalf("key contains whitespace: %q", got)
	}
}

func newTestFileService(store storage.FileStorage, maxSize int64) FileService {
	return NewFileService(store, &config.Config{Storage: config.Storage{MaxSize: maxSize}})
}

type captureFileStorage struct {
	content []byte
	err     error
}

func (s *captureFileStorage) Put(_ context.Context, input storage.PutInput) (storage.StoredObject, error) {
	if s.err != nil {
		return storage.StoredObject{}, s.err
	}
	var err error
	s.content, err = io.ReadAll(input.Reader)
	if err != nil {
		return storage.StoredObject{}, err
	}
	return storage.StoredObject{
		Key:         input.Key,
		URL:         "/uploads/" + input.Key,
		Size:        input.Size,
		ContentType: input.ContentType,
	}, nil
}

func (s *captureFileStorage) Get(_ context.Context, _ string) (io.ReadCloser, int64, error) {
	if s.err != nil {
		return nil, 0, s.err
	}
	return io.NopCloser(bytes.NewReader(s.content)), int64(len(s.content)), nil
}

func (s *captureFileStorage) Delete(_ context.Context, _ string) error {
	return s.err
}
