package storage

import (
	"bytes"
	"context"
	"io"
)

// NopStorage returns a FileStorage that accepts any Put and returns a zero StoredObject.
// Intended for tests that need a non-nil FileStorage but never exercise the upload path.
func NopStorage() FileStorage { return nopStorage{} }

type nopStorage struct{}

func (nopStorage) Put(_ context.Context, input PutInput) (StoredObject, error) {
	return StoredObject{
		Key:         input.Key,
		Size:        input.Size,
		ContentType: input.ContentType,
	}, nil
}

func (nopStorage) Get(_ context.Context, _ string) (io.ReadCloser, int64, error) {
	return io.NopCloser(bytes.NewReader(nil)), 0, nil
}

func (nopStorage) Delete(_ context.Context, _ string) error {
	return nil
}
