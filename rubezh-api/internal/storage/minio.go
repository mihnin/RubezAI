package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioConfig — env-параметры MinIO для документов.
type MinioConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Secure    bool
}

// MinioClient — обёртка над minio-go/v7 для документов.
// Bucket создаётся автоматически при первом обращении (Ensure).
type MinioClient struct {
	client *minio.Client
	bucket string
}

// NewMinioClient инициализирует клиент.
func NewMinioClient(cfg MinioConfig) (*MinioClient, error) {
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.Secure,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: minio init: %w", err)
	}
	return &MinioClient{client: c, bucket: cfg.Bucket}, nil
}

// EnsureBucket создаёт bucket если не существует.
func (m *MinioClient) EnsureBucket(ctx context.Context) error {
	exists, err := m.client.BucketExists(ctx, m.bucket)
	if err != nil {
		return fmt.Errorf("storage: bucket exists check: %w", err)
	}
	if !exists {
		if err := m.client.MakeBucket(ctx, m.bucket,
			minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("storage: make bucket: %w", err)
		}
	}
	return nil
}

// Upload пишет объект; возвращает size_bytes.
func (m *MinioClient) Upload(
	ctx context.Context, key string, content []byte, contentType string,
) (int64, error) {
	info, err := m.client.PutObject(ctx, m.bucket, key,
		bytes.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return 0, fmt.Errorf("storage: minio upload: %w", err)
	}
	return info.Size, nil
}

// Download читает объект целиком в память.
func (m *MinioClient) Download(
	ctx context.Context, key string,
) ([]byte, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key,
		minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: minio get: %w", err)
	}
	defer func() { _ = obj.Close() }()
	return io.ReadAll(obj)
}

// Remove удаляет объект (hard-delete). Idempotent — отсутствие
// объекта не считается ошибкой.
func (m *MinioClient) Remove(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	err := m.client.RemoveObject(ctx, m.bucket, key,
		minio.RemoveObjectOptions{})
	if err != nil {
		// MinIO возвращает 200 даже на несуществующий объект; реальная
		// ошибка — сеть/credentials.
		return fmt.Errorf("storage: minio remove: %w", err)
	}
	return nil
}
