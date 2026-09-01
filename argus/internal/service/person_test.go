package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"argus/app/internal/model"
	"argus/app/internal/pkg/engineipc"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/storage"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

const maxBatchDeleteForTest = 100

func newPersonServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func newTestPersonService(t *testing.T, extractor service.FaceFeatureExtractor) (service.PersonService, repository.PersonRepository, repository.PersonFaceRepository, storage.FileStorage) {
	t.Helper()
	db := newPersonServiceTestDB(t)
	repo := repository.NewPersonRepository(db)
	faceRepo := repository.NewPersonFaceRepository(db)
	store := storage.NopStorage()
	if extractor == nil {
		extractor = &mockFaceExtractor{}
	}
	svc := service.NewPersonServiceWithExtractor(repo, faceRepo, store, extractor)
	return svc, repo, faceRepo, store
}

type mockFaceExtractor struct {
	resp *argusv1.ExtractFaceFeatureResponse
	err  error
}

func (m *mockFaceExtractor) ExtractFaceFeature(_ context.Context, _ *argusv1.ExtractFaceFeatureRequest, _ ...grpc.CallOption) (*argusv1.ExtractFaceFeatureResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.resp != nil {
		return m.resp, nil
	}
	return &argusv1.ExtractFaceFeatureResponse{
		Embedding:        make([]byte, 2048),
		AlignedFaceImage: []byte("aligned-jpeg-data"),
		FaceBox:          &argusv1.NormalizedRect{X: 0.1, Y: 0.1, Width: 0.5, Height: 0.5},
		QualityScore:     90.0,
		DetectionScore:   0.98,
		AlgorithmId:      "face_recognition",
		AlgorithmVersion: "1.0.0",
	}, nil
}

func TestPersonServiceCreateValidationAndAutoID(t *testing.T) {
	svc, _, _, _ := newTestPersonService(t, nil)
	ctx := context.Background()

	// 1. Auto generate UUID
	dto, err := svc.CreatePerson(ctx, &service.CreatePersonInput{
		Name: "Alice",
	})
	if err != nil {
		t.Fatalf("create without personId failed: %v", err)
	}
	if len(dto.PersonID) != 32 || strings.Contains(dto.PersonID, "-") {
		t.Fatalf("invalid auto generated personId: %s", dto.PersonID)
	}
	if dto.Name != "Alice" {
		t.Fatalf("name mismatch: %s", dto.Name)
	}

	// 2. Invalid personId (starts with symbol)
	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "_invalid",
		Name:     "Bob",
	})
	if !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for _invalid, got %v", err)
	}

	// 3. Invalid personId (contains spaces or illegal symbols)
	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "valid/part",
		Name:     "Bob",
	})
	if !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for slash in personId, got %v", err)
	}

	// 4. Invalid name (empty, too long, control chars)
	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "bob1",
		Name:     "   ",
	})
	if !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for empty name, got %v", err)
	}

	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "bob2",
		Name:     "Bob\x00Name",
	})
	if !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for control char in name, got %v", err)
	}

	longName := strings.Repeat("字", 65)
	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "bob3",
		Name:     longName,
	})
	if !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for >64 chars name, got %v", err)
	}

	// 5. Valid custom personId
	dto2, err := svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "Emp:001-A.b_c",
		Name:     "Charlie",
	})
	if err != nil {
		t.Fatalf("create custom personId failed: %v", err)
	}
	if dto2.PersonID != "Emp:001-A.b_c" {
		t.Fatalf("personId changed: %s", dto2.PersonID)
	}

	// 6. Duplicate active personId -> CodePersonIDTaken
	_, err = svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "Emp:001-A.b_c",
		Name:     "Charlie 2",
	})
	if !isErrCode(err, errno.CodePersonIDTaken) {
		t.Fatalf("expected CodePersonIDTaken, got %v", err)
	}

	// 7. Duplicate deleted personId -> restore and update name
	if err := svc.DeletePerson(ctx, "Emp:001-A.b_c"); err != nil {
		t.Fatalf("delete person failed: %v", err)
	}
	restored, err := svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "Emp:001-A.b_c",
		Name:     "Charlie Restored",
	})
	if err != nil {
		t.Fatalf("create deleted person should restore: %v", err)
	}
	if restored.Name != "Charlie Restored" {
		t.Fatalf("restored name mismatch: %s", restored.Name)
	}
}

func TestPersonServiceSyncUpsertAndDelete(t *testing.T) {
	svc, _, _, _ := newTestPersonService(t, nil)
	ctx := context.Background()

	// 1. SyncUpsert new
	dto, err := svc.SyncUpsertPerson(ctx, "sync_01", &service.UpdatePersonInput{
		Name: "Sync User 1",
	})
	if err != nil {
		t.Fatalf("sync upsert failed: %v", err)
	}
	if dto.PersonID != "sync_01" || dto.Name != "Sync User 1" {
		t.Fatalf("unexpected dto: %+v", dto)
	}

	// 2. SyncUpsert existing
	dto2, err := svc.SyncUpsertPerson(ctx, "sync_01", &service.UpdatePersonInput{
		Name: "Sync User 1 Updated",
	})
	if err != nil {
		t.Fatalf("sync update failed: %v", err)
	}
	if dto2.Name != "Sync User 1 Updated" {
		t.Fatalf("unexpected name: %s", dto2.Name)
	}

	// 3. SyncDelete idempotent
	if err := svc.SyncDeletePerson(ctx, "sync_01"); err != nil {
		t.Fatalf("sync delete failed: %v", err)
	}
	// delete again should succeed
	if err := svc.SyncDeletePerson(ctx, "sync_01"); err != nil {
		t.Fatalf("sync delete again should succeed: %v", err)
	}
	if err := svc.SyncDeletePerson(ctx, "non_existent"); err != nil {
		t.Fatalf("sync delete non_existent should succeed: %v", err)
	}

	// 4. SyncUpsert on deleted record restores it
	dto3, err := svc.SyncUpsertPerson(ctx, "sync_01", &service.UpdatePersonInput{
		Name: "Sync User 1 Restored",
	})
	if err != nil {
		t.Fatalf("sync upsert after delete failed: %v", err)
	}
	if dto3.Name != "Sync User 1 Restored" {
		t.Fatalf("restored name mismatch: %s", dto3.Name)
	}
}

func TestPersonServiceBatchDelete(t *testing.T) {
	svc, _, _, _ := newTestPersonService(t, nil)
	ctx := context.Background()

	invalidCases := []struct {
		name  string
		input *service.BatchDeletePersonInput
	}{
		{name: "nil input", input: nil},
		{name: "empty list", input: &service.BatchDeletePersonInput{PersonIDs: []string{}}},
		{name: "empty id", input: &service.BatchDeletePersonInput{PersonIDs: []string{"   "}}},
		{name: "illegal slash", input: &service.BatchDeletePersonInput{PersonIDs: []string{"bad/id"}}},
		{name: "illegal space", input: &service.BatchDeletePersonInput{PersonIDs: []string{"bad id"}}},
		{name: "invalid first character", input: &service.BatchDeletePersonInput{PersonIDs: []string{"_bad"}}},
		{name: "non ASCII", input: &service.BatchDeletePersonInput{PersonIDs: []string{"é"}}},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := svc.BatchDeletePerson(ctx, tc.input); !isErrCode(err, errno.CodeInvalidParam) {
				t.Fatalf("expected CodeInvalidParam, got %v", err)
			}
		})
	}

	// Exactly 100 valid identifiers is within the API contract.
	validIDs := make([]string, maxBatchDeleteForTest)
	for i := range validIDs {
		validIDs[i] = fmt.Sprintf("p%d", i)
	}
	if err := svc.BatchDeletePerson(ctx, &service.BatchDeletePersonInput{PersonIDs: validIDs}); err != nil {
		t.Fatalf("100 valid IDs should be accepted: %v", err)
	}

	// More than 100 identifiers is invalid.
	tooManyIDs := make([]string, maxBatchDeleteForTest+1)
	for i := range tooManyIDs {
		tooManyIDs[i] = fmt.Sprintf("p%d", i)
	}
	if err := svc.BatchDeletePerson(ctx, &service.BatchDeletePersonInput{PersonIDs: tooManyIDs}); !isErrCode(err, errno.CodeInvalidParam) {
		t.Fatalf("expected CodeInvalidParam for >100 batch, got %v", err)
	}
}

func isErrCode(err error, code int) bool {
	if err == nil {
		return false
	}
	var e *errno.Error
	if errors.As(err, &e) {
		return e.Code == code
	}
	return false
}

func createTestFileHeader(filename string, content []byte, contentType string) *multipart.FileHeader {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", contentType)
	part, _ := writer.CreatePart(h)
	_, _ = part.Write(content)
	_ = writer.Close()

	reader := multipart.NewReader(body, writer.Boundary())
	form, _ := reader.ReadForm(int64(len(content)) + 4096)
	if form != nil && len(form.File["file"]) > 0 {
		return form.File["file"][0]
	}
	return nil
}

func TestPersonServiceFaceRegistrationAndManagement(t *testing.T) {
	extractor := &mockFaceExtractor{}
	svc, _, _, _ := newTestPersonService(t, extractor)
	ctx := context.Background()

	// 1. Create person
	person, err := svc.CreatePerson(ctx, &service.CreatePersonInput{
		PersonID: "test-person-1",
		Name:     "Alice Wonderland",
	})
	if err != nil {
		t.Fatalf("create person: %v", err)
	}

	// 2. Register with invalid image (not jpeg/png/webp)
	invalidHeader := createTestFileHeader("test.txt", []byte("not an image"), "text/plain")
	_, err = svc.RegisterFace(ctx, person.PersonID, invalidHeader)
	if !isErrCode(err, errno.CodeFileTypeNotAllowed) {
		t.Fatalf("expected CodeFileTypeNotAllowed, got %v", err)
	}

	// Valid JPEG magic bytes
	jpegContent := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01}
	validHeader := createTestFileHeader("face1.jpg", jpegContent, "image/jpeg")

	// 3. Register face 1
	face1, err := svc.RegisterFace(ctx, person.PersonID, validHeader)
	if err != nil {
		t.Fatalf("register face 1: %v", err)
	}
	if face1.FaceID == "" || face1.QualityScore != 90.0 {
		t.Fatalf("unexpected face1 DTO: %+v", face1)
	}

	// 4. Duplicate image registration should fail with CodeFaceDuplicateImage
	dupHeader := createTestFileHeader("face1_dup.jpg", jpegContent, "image/jpeg")
	_, err = svc.RegisterFace(ctx, person.PersonID, dupHeader)
	if !isErrCode(err, errno.CodeFaceDuplicateImage) {
		t.Fatalf("expected CodeFaceDuplicateImage, got %v", err)
	}

	// 5. List faces
	faces, err := svc.ListFaces(ctx, person.PersonID)
	if err != nil {
		t.Fatalf("list faces: %v", err)
	}
	if len(faces) != 1 || faces[0].FaceID != face1.FaceID {
		t.Fatalf("unexpected faces: %+v", faces)
	}

	// 6. Check GetPage includes faceCount
	page, err := svc.GetPage(ctx, &service.PersonPageQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].FaceCount != 1 {
		t.Fatalf("expected FaceCount=1, got %+v", page.Items[0])
	}

	// 7. Engine RemoteError mapping (e.g. NO_FACE_DETECTED)
	extractor.err = &engineipc.RemoteError{Code: "NO_FACE_DETECTED"}
	jpeg2 := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x02}
	hdr2 := createTestFileHeader("face2.jpg", jpeg2, "image/jpeg")
	_, err = svc.RegisterFace(ctx, person.PersonID, hdr2)
	if !isErrCode(err, errno.CodeFaceNoFaceDetected) {
		t.Fatalf("expected CodeFaceNoFaceDetected, got %v", err)
	}

	// Reset extractor error
	extractor.err = nil

	// 8. Delete Face
	if err := svc.DeleteFace(ctx, person.PersonID, face1.FaceID); err != nil {
		t.Fatalf("delete face: %v", err)
	}
	// Face list should be empty
	faces, err = svc.ListFaces(ctx, person.PersonID)
	if err != nil || len(faces) != 0 {
		t.Fatalf("expected 0 faces after deletion, got %d", len(faces))
	}

	// 9. Re-register after deletion with same image should now succeed
	time.Sleep(5 * time.Millisecond)
	face1Re, err := svc.RegisterFace(ctx, person.PersonID, validHeader)
	if err != nil {
		t.Fatalf("re-register deleted face: %v", err)
	}
	if face1Re.FaceID == "" {
		t.Fatalf("expected non-empty face ID on re-registration")
	}

	// 10. SyncDeletePerson cleans up face samples and storage files
	time.Sleep(5 * time.Millisecond)
	if err := svc.SyncDeletePerson(ctx, person.PersonID); err != nil {
		t.Fatalf("sync delete person with faces: %v", err)
	}
	facesAfterSyncDel, err := svc.ListFaces(ctx, person.PersonID)
	if !isErrCode(err, errno.CodeNotFound) {
		t.Fatalf("expected CodeNotFound for deleted person, got faces=%+v, err=%v", facesAfterSyncDel, err)
	}
}
