package storage

import "context"

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
