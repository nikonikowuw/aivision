package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"argus/app/internal/pkg/config"
)

type minioClient interface {
	PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error)
}

type minioStorage struct {
	client        minioClient
	bucket        string
	publicBaseURL string
}

// NewMinIOStorage 创建 MinIO 对象存储。
func NewMinIOStorage(cfg config.MinIO) (FileStorage, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	accessKey := strings.TrimSpace(cfg.AccessKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	bucket := strings.TrimSpace(cfg.Bucket)
	publicBaseURL := strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/")
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" || publicBaseURL == "" {
		return nil, fmt.Errorf("minio storage configuration is incomplete")
	}
	if err := validateMinIOPublicBaseURL(publicBaseURL); err != nil {
		return nil, err
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	return newMinIOStorage(client, bucket, publicBaseURL), nil
}

func validateMinIOPublicBaseURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid minio public base URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid minio public base URL")
	}
	return nil
}

func newMinIOStorage(client minioClient, bucket, publicBaseURL string) FileStorage {
	return &minioStorage{
		client:        client,
		bucket:        bucket,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

func (s *minioStorage) Put(ctx context.Context, input PutInput) (StoredObject, error) {
	if err := ctx.Err(); err != nil {
		return StoredObject{}, fmt.Errorf("minio storage context: %w", err)
	}
	if err := validatePutInput(input); err != nil {
		return StoredObject{}, err
	}

	if _, err := s.client.PutObject(ctx, s.bucket, input.Key, input.Reader, input.Size, minio.PutObjectOptions{
		ContentType: input.ContentType,
	}); err != nil {
		return StoredObject{}, fmt.Errorf("put minio object: %w", err)
	}

	return StoredObject{
		Key:         input.Key,
		URL:         s.publicBaseURL + "/" + input.Key,
		Size:        input.Size,
		ContentType: input.ContentType,
	}, nil
}
