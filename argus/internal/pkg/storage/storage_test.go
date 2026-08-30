package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"argus/app/internal/pkg/config"
)

func TestLocalStoragePut(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStorage(root, "/uploads")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	content := []byte("file content")
	got, err := store.Put(context.Background(), PutInput{
		Key:         "2026/08/21/object.txt",
		Reader:      bytes.NewReader(content),
		Size:        int64(len(content)),
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got.Key != "2026/08/21/object.txt" || got.URL != "/uploads/2026/08/21/object.txt" ||
		got.Size != int64(len(content)) || got.ContentType != "text/plain" {
		t.Fatalf("unexpected object: %+v", got)
	}

	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(got.Key)))
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("stored content = %q, want %q", stored, content)
	}
}

func TestLocalStorageRejectsUnsafeKeys(t *testing.T) {
	store, err := NewLocalStorage(t.TempDir(), "/uploads")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	for _, key := range []string{"../escape.txt", "2026/../../escape.txt", "/absolute.txt", `2026\\escape.txt`} {
		t.Run(key, func(t *testing.T) {
			_, err := store.Put(context.Background(), PutInput{
				Key:         key,
				Reader:      bytes.NewReader([]byte("content")),
				Size:        7,
				ContentType: "text/plain",
			})
			if err == nil {
				t.Fatalf("Put(%q) should fail", key)
			}
		})
	}
}

func TestLocalStorageCleansTemporaryFileOnFailure(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStorage(root, "/uploads")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	_, err = store.Put(context.Background(), PutInput{
		Key:         "failed/object.bin",
		Reader:      errorReader{},
		Size:        2,
		ContentType: "application/octet-stream",
	})
	if err == nil {
		t.Fatal("Put should fail when reader fails")
	}

	var regularFiles int
	if err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			regularFiles++
		}
		return nil
	}); err != nil {
		t.Fatalf("walk storage root: %v", err)
	}
	if regularFiles != 0 {
		t.Fatalf("regular files after failed Put = %d, want 0", regularFiles)
	}
}

type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = 'x'
	return 1, errors.New("reader failed")
}

func TestMinIOStoragePut(t *testing.T) {
	client := &fakeMinIOClient{}
	store := newMinIOStorage(client, "files", "https://cdn.example.com/files")
	content := []byte("image content")

	got, err := store.Put(context.Background(), PutInput{
		Key:         "2026/08/21/object.png",
		Reader:      bytes.NewReader(content),
		Size:        int64(len(content)),
		ContentType: "image/png",
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got.URL != "https://cdn.example.com/files/2026/08/21/object.png" {
		t.Fatalf("URL = %q", got.URL)
	}
	if client.bucket != "files" || client.key != "2026/08/21/object.png" || client.size != int64(len(content)) {
		t.Fatalf("unexpected PutObject args: %+v", client)
	}
	if client.contentType != "image/png" || !bytes.Equal(client.content, content) {
		t.Fatalf("unexpected MinIO payload: %+v", client)
	}
}

func TestMinIOStorageReturnsClientError(t *testing.T) {
	client := &fakeMinIOClient{err: errors.New("minio unavailable")}
	store := newMinIOStorage(client, "files", "https://cdn.example.com/files")

	_, err := store.Put(context.Background(), PutInput{
		Key:         "object.png",
		Reader:      bytes.NewReader([]byte("content")),
		Size:        7,
		ContentType: "image/png",
	})
	if err == nil || !strings.Contains(err.Error(), "minio unavailable") {
		t.Fatalf("Put error = %v, want MinIO error", err)
	}
}

func TestNewMinIOStorageRejectsInvalidPublicURL(t *testing.T) {
	for _, publicBaseURL := range []string{"//host/files", "https://user:pass@cdn.example.com/files", "https://cdn.example.com/files?download=1"} {
		t.Run(publicBaseURL, func(t *testing.T) {
			_, err := NewMinIOStorage(config.MinIO{
				Endpoint:      "minio.example.com:9000",
				AccessKey:     "access",
				SecretKey:     "secret",
				Bucket:        "files",
				PublicBaseURL: publicBaseURL,
			})
			if err == nil {
				t.Fatalf("NewMinIOStorage should reject public URL %q", publicBaseURL)
			}
		})
	}
}

func TestNewSelectsConfiguredDriver(t *testing.T) {
	localRoot := t.TempDir()
	store, err := New(&config.Config{Storage: config.Storage{
		Driver: config.StorageDriverLocal,
		Local:  config.Local{Root: localRoot, URLPrefix: "/uploads"},
	}})
	if err != nil {
		t.Fatalf("New local storage: %v", err)
	}
	if store == nil {
		t.Fatal("New local storage returned nil")
	}

	store, err = New(&config.Config{Storage: config.Storage{
		Driver: config.StorageDriverMinIO,
		MinIO: config.MinIO{
			Endpoint:      "minio.example.com:9000",
			AccessKey:     "access",
			SecretKey:     "secret",
			Bucket:        "files",
			PublicBaseURL: "https://cdn.example.com/files",
		},
	}})
	if err != nil {
		t.Fatalf("New MinIO storage: %v", err)
	}
	if store == nil {
		t.Fatal("New MinIO storage returned nil")
	}
}

func TestNewMinIOStorage(t *testing.T) {
	store, err := NewMinIOStorage(config.MinIO{
		Endpoint:      "minio.example.com:9000",
		AccessKey:     "access",
		SecretKey:     "secret",
		Bucket:        "files",
		PublicBaseURL: "https://cdn.example.com/files",
	})
	if err != nil {
		t.Fatalf("NewMinIOStorage: %v", err)
	}
	if store == nil {
		t.Fatal("NewMinIOStorage returned nil store")
	}
}

type fakeMinIOClient struct {
	bucket      string
	key         string
	size        int64
	contentType string
	content     []byte
	err         error
}

func (f *fakeMinIOClient) PutObject(_ context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	f.bucket = bucketName
	f.key = objectName
	f.size = objectSize
	f.contentType = opts.ContentType
	f.content, _ = io.ReadAll(reader)
	if f.err != nil {
		return minio.UploadInfo{}, f.err
	}
	return minio.UploadInfo{Key: objectName, Size: objectSize}, nil
}
