package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"argus/app/internal/model"
	"argus/app/internal/repository"
)

func TestPersonFaceRepositoryCRUDAndLimit(t *testing.T) {
	db := newPersonTestDB(t)
	personRepo := repository.NewPersonRepository(db)
	faceRepo := repository.NewPersonFaceRepository(db)
	ctx := context.Background()

	// Create person first
	person := &model.Person{
		PersonID: "p001",
		Name:     "John Doe",
	}
	if err := personRepo.Create(ctx, person); err != nil {
		t.Fatalf("create person: %v", err)
	}

	// 1. Create up to 10 faces
	for i := 1; i <= 10; i++ {
		face := &model.PersonFace{
			PersonID:         "p001",
			FaceID:           fmt.Sprintf("f%03d", i),
			AlgorithmID:      "face_recognition",
			AlgorithmVersion: "1.0.0",
			Embedding:        make([]byte, 2048),
			QualityScore:     80.0 + float32(i),
			DetectionScore:   0.95,
			BoundingBox:      `{"x":0.1,"y":0.1,"width":0.5,"height":0.5}`,
			RawImageKey:      fmt.Sprintf("persons/p001/faces/raw_%d.jpg", i),
			RawImageSHA256:   fmt.Sprintf("sha256_%03d", i),
			RawImageSize:     1024,
			RawImageMime:     "image/jpeg",
			AlignedFaceKey:   fmt.Sprintf("persons/p001/faces/aligned_%d.jpg", i),
			AlignedFaceSize:  512,
			AlignedFaceMime:  "image/jpeg",
		}
		if err := faceRepo.Create(ctx, face); err != nil {
			t.Fatalf("create face %d failed: %v", i, err)
		}
	}

	// 2. 11th face should fail with ErrLimitExceeded
	face11 := &model.PersonFace{
		PersonID:         "p001",
		FaceID:           "f011",
		AlgorithmID:      "face_recognition",
		AlgorithmVersion: "1.0.0",
		Embedding:        make([]byte, 2048),
		RawImageKey:      "persons/p001/faces/raw_11.jpg",
		RawImageSHA256:   "sha256_011",
		RawImageSize:     1024,
		RawImageMime:     "image/jpeg",
		AlignedFaceKey:   "persons/p001/faces/aligned_11.jpg",
		AlignedFaceSize:  512,
		AlignedFaceMime:  "image/jpeg",
	}
	if err := faceRepo.Create(ctx, face11); !errors.Is(err, repository.ErrLimitExceeded) {
		t.Fatalf("expected ErrLimitExceeded, got %v", err)
	}

	// 3. Count
	count, err := faceRepo.CountByPersonID(ctx, "p001")
	if err != nil || count != 10 {
		t.Fatalf("CountByPersonID: got count=%d, err=%v, want 10", count, err)
	}

	// 4. Batch Count
	counts, err := faceRepo.CountByPersonIDs(ctx, []string{"p001", "nonexistent"})
	if err != nil {
		t.Fatalf("CountByPersonIDs: %v", err)
	}
	if counts["p001"] != 10 || counts["nonexistent"] != 0 {
		t.Fatalf("CountByPersonIDs mismatch: %+v", counts)
	}

	// 5. Duplicate SHA256 should fail with ErrDuplicateKey
	dupFace := &model.PersonFace{
		PersonID:         "p002",
		FaceID:           "f012",
		AlgorithmID:      "face_recognition",
		AlgorithmVersion: "1.0.0",
		Embedding:        make([]byte, 2048),
		RawImageKey:      "persons/p002/faces/raw_1.jpg",
		RawImageSHA256:   "sha256_001", // duplicate with face 1
		RawImageSize:     1024,
		RawImageMime:     "image/jpeg",
		AlignedFaceKey:   "persons/p002/faces/aligned_1.jpg",
		AlignedFaceSize:  512,
		AlignedFaceMime:  "image/jpeg",
	}
	if err := faceRepo.Create(ctx, dupFace); !errors.Is(err, repository.ErrDuplicateKey) {
		t.Fatalf("expected ErrDuplicateKey on duplicate sha256, got %v", err)
	}

	// 6. Delete a face
	deletedFace, err := faceRepo.Delete(ctx, "p001", "f001")
	if err != nil {
		t.Fatalf("delete face f001: %v", err)
	}
	if deletedFace.FaceID != "f001" {
		t.Fatalf("deletedFace ID mismatch: %s", deletedFace.FaceID)
	}

	// Count should now be 9
	count, err = faceRepo.CountByPersonID(ctx, "p001")
	if err != nil || count != 9 {
		t.Fatalf("count after delete: got %d, want 9", count)
	}

	// Now 11th face can be created
	if err := faceRepo.Create(ctx, face11); err != nil {
		t.Fatalf("create face11 after deletion failed: %v", err)
	}

	// 7. Cascading delete when person is deleted
	deleted, err := personRepo.Delete(ctx, "p001")
	if err != nil || !deleted {
		t.Fatalf("delete person p001: %v", err)
	}
	// Face count for p001 should now be 0
	count, err = faceRepo.CountByPersonID(ctx, "p001")
	if err != nil || count != 0 {
		t.Fatalf("face count after person delete: got %d, want 0", count)
	}
}
