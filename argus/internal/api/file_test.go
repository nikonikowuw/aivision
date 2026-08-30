package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/storage"
	"argus/app/internal/service"
)

func TestFileUploadHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fileContent := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	fileService := &fileUploadTestService{result: &service.UploadedFile{
		Key:         "2026/08/21/object.png",
		Name:        "photo.png",
		Size:        int64(len(fileContent)),
		ContentType: "image/png",
		URL:         "/uploads/2026/08/21/object.png",
	}}
	handler := api.NewFileHandler(fileService, &config.Config{Storage: config.Storage{MaxSize: 1024}})
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	engine.POST("/upload", handler.Upload)

	req := newMultipartUploadRequest(t, service.UploadFileFieldName, "photo.png", fileContent)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body struct {
		Code int                  `json:"code"`
		Data service.UploadedFile `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != errno.CodeOK || body.Data.URL != fileService.result.URL {
		t.Fatalf("response = %+v, want success with URL %q", body, fileService.result.URL)
	}
	if !fileService.called || fileService.name != "photo.png" || !bytes.Equal(fileService.content, fileContent) {
		t.Fatalf("service input = called:%v name:%q content:%v", fileService.called, fileService.name, fileService.content)
	}
}

func TestFileUploadHandlerWithLocalStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	store, err := storage.NewLocalStorage(root, "/uploads")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}
	svc := service.NewFileService(store, &config.Config{Storage: config.Storage{MaxSize: 1024}})
	handler := api.NewFileHandler(svc, &config.Config{Storage: config.Storage{MaxSize: 1024}})
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	engine.POST("/upload", handler.Upload)

	content := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	req := newMultipartUploadRequest(t, service.UploadFileFieldName, "avatar.png", content)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body struct {
		Code int                  `json:"code"`
		Data service.UploadedFile `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != errno.CodeOK || body.Data.URL == "" {
		t.Fatalf("response = %+v, want successful URL", body)
	}
	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(body.Data.Key)))
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("stored content = %v, want %v", stored, content)
	}
}

func TestFileUploadHandlerRejectsMissingFile(t *testing.T) {
	handler := api.NewFileHandler(&fileUploadTestService{}, &config.Config{Storage: config.Storage{MaxSize: 1024}})
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	engine.POST("/upload", handler.Upload)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/upload", bytes.NewBufferString("not multipart")))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != errno.CodeInvalidParam {
		t.Fatalf("code = %d, want %d", body.Code, errno.CodeInvalidParam)
	}
}

func TestFileUploadHandlerMapsServiceError(t *testing.T) {
	handler := api.NewFileHandler(&fileUploadTestService{err: errno.NewError(errno.CodeFileTypeNotAllowed)}, &config.Config{Storage: config.Storage{MaxSize: 1024}})
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	engine.POST("/upload", handler.Upload)

	req := newMultipartUploadRequest(t, service.UploadFileFieldName, "photo.png", []byte("content"))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != errno.CodeFileTypeNotAllowed {
		t.Fatalf("code = %d, want %d", body.Code, errno.CodeFileTypeNotAllowed)
	}
}

func TestFileUploadHandlerMapsStorageErrorToInternal(t *testing.T) {
	handler := api.NewFileHandler(&fileUploadTestService{err: errors.New("storage credentials leaked")}, &config.Config{Storage: config.Storage{MaxSize: 1024}})
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	engine.POST("/upload", handler.Upload)

	req := newMultipartUploadRequest(t, service.UploadFileFieldName, "photo.png", []byte("content"))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != errno.CodeInternal || body.Message == "storage credentials leaked" {
		t.Fatalf("response = %+v, internal error should be sanitized", body)
	}
}

func newMultipartUploadRequest(t *testing.T, field, name string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

type fileUploadTestService struct {
	result  *service.UploadedFile
	err     error
	called  bool
	name    string
	content []byte
}

func (s *fileUploadTestService) Upload(_ context.Context, input *service.UploadInput) (*service.UploadedFile, error) {
	s.called = true
	s.name = input.Name
	s.content, _ = io.ReadAll(input.Reader)
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}
