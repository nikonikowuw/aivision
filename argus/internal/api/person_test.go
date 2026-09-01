package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"

	"argus/app/internal/api"
	"argus/app/internal/middleware"
	"argus/app/internal/pkg/config"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/storage"
	argusv1 "argus/app/internal/proto/argus/v1"
	"argus/app/internal/repository"
	"argus/app/internal/service"
)

type apiMockFaceExtractor struct{}

func (m *apiMockFaceExtractor) ExtractFaceFeature(_ context.Context, _ *argusv1.ExtractFaceFeatureRequest, _ ...grpc.CallOption) (*argusv1.ExtractFaceFeatureResponse, error) {
	return &argusv1.ExtractFaceFeatureResponse{
		Embedding:        make([]byte, 2048),
		AlignedFaceImage: []byte("aligned-jpeg-bytes"),
		FaceBox:          &argusv1.NormalizedRect{X: 0.1, Y: 0.1, Width: 0.5, Height: 0.5},
		QualityScore:     92.0,
		DetectionScore:   0.99,
		AlgorithmId:      "face_recognition",
		AlgorithmVersion: "1.0.0",
	}, nil
}

func setupPersonAPIEngine(t *testing.T, allowedIPs []string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestAPIDB(t, "person")
	repo := repository.NewPersonRepository(db)
	faceRepo := repository.NewPersonFaceRepository(db)
	store, _ := storage.NewLocalStorage(t.TempDir(), "/uploads")
	svc := service.NewPersonServiceWithExtractor(repo, faceRepo, store, &apiMockFaceExtractor{})
	handler := api.NewPersonHandler(svc)

	cfg := &config.Config{
		Open: config.Open{
			PersonSyncAllowedIPs: allowedIPs,
		},
	}
	ipMw := middleware.NewOpenPersonIPWhitelistMiddleware(cfg)

	r := gin.New()
	r.Use(middleware.ErrorHandler())

	// 模拟已认证页面路由组
	personGrp := r.Group("/api/person")
	{
		personGrp.GET("/page", handler.GetPage)
		personGrp.POST("", handler.CreatePerson)
		personGrp.DELETE("/batch", handler.BatchDeletePerson)
		personGrp.PUT("/:personId", handler.UpdatePerson)
		personGrp.DELETE("/:personId", handler.DeletePerson)
		personGrp.POST("/:personId/faces", handler.RegisterFace)
		personGrp.GET("/:personId/faces", handler.ListFaces)
		personGrp.DELETE("/:personId/faces/:faceId", handler.DeleteFace)
		personGrp.PUT("/:personId/primary-face", handler.SetPrimaryFace)
		personGrp.GET("/:personId/faces/:faceId/image", handler.GetRawImage)
		personGrp.GET("/:personId/faces/:faceId/aligned-image", handler.GetAlignedImage)
	}

	// 开放同步路由组
	openGrp := r.Group("/api/v1/open/person")
	openGrp.Use(ipMw.Handler)
	{
		openGrp.PUT("/:personId", handler.SyncUpsertPerson)
		openGrp.DELETE("/:personId", handler.SyncDeletePerson)
	}

	return r
}

func TestPersonPageAndCRUDAPI(t *testing.T) {
	r := setupPersonAPIEngine(t, []string{"192.168.1.100"})

	// 1. Create Person (POST /api/person)
	createBody := map[string]any{
		"personId": "EMP001",
		"name":     "Bob",
	}
	body, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/api/person", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("create person got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			PersonID  string `json:"personId"`
			Name      string `json:"name"`
			CreatedAt string `json:"createdAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 || resp.Data.PersonID != "EMP001" || resp.Data.Name != "Bob" {
		t.Fatalf("unexpected create response: %+v", resp)
	}

	// 2. Query Page (GET /api/person/page)
	req = httptest.NewRequest(http.MethodGet, "/api/person/page?page=1&pageSize=10", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query page got %d: %s", w.Code, w.Body.String())
	}

	// 3. Update Person Name (PUT /api/person/:personId)
	updateBody := map[string]any{"name": "Bob Updated"}
	body, _ = json.Marshal(updateBody)
	req = httptest.NewRequest(http.MethodPut, "/api/person/EMP001", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update person got %d: %s", w.Code, w.Body.String())
	}

	// 4. Batch Delete Person (DELETE /api/person/batch)
	batchBody := map[string]any{"personIds": []string{"EMP001"}}
	body, _ = json.Marshal(batchBody)
	req = httptest.NewRequest(http.MethodDelete, "/api/person/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch delete got %d: %s", w.Code, w.Body.String())
	}

	// 5. Batch delete rejects identifiers that do not satisfy the path format.
	invalidBatchBody := map[string]any{"personIds": []string{"bad/id"}}
	body, _ = json.Marshal(invalidBatchBody)
	req = httptest.NewRequest(http.MethodDelete, "/api/person/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid batch delete got %d: %s", w.Code, w.Body.String())
	}
	var invalidBatchResp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &invalidBatchResp); err != nil {
		t.Fatalf("decode invalid batch response: %v", err)
	}
	if invalidBatchResp.Code != errno.CodeInvalidParam {
		t.Fatalf("invalid batch code = %d, want %d", invalidBatchResp.Code, errno.CodeInvalidParam)
	}
}

func TestOpenPersonSyncAPIRoutes(t *testing.T) {
	r := setupPersonAPIEngine(t, []string{"192.168.1.100"})

	// 1. Forbidden without whitelist IP
	body, _ := json.Marshal(map[string]any{"name": "Open Alice"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/open/person/OP001", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.200:1234"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", w.Code)
	}

	// 2. Success with whitelist IP (No JWT required)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/open/person/OP001", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.100:1234"
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("open upsert got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			PersonID string `json:"personId"`
			Name     string `json:"name"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != 0 || resp.Data.PersonID != "OP001" {
		t.Fatalf("unexpected open upsert resp: %+v", resp)
	}

	// 3. Delete open person idempotent
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/open/person/OP001", nil)
	req.RemoteAddr = "192.168.1.100:1234"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("open delete got %d: %s", w.Code, w.Body.String())
	}

	// Repeat delete still 200
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/open/person/OP001", nil)
	req.RemoteAddr = "192.168.1.100:1234"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("repeat open delete got %d: %s", w.Code, w.Body.String())
	}
}

func TestPersonFaceManagementAPI(t *testing.T) {
	r := setupPersonAPIEngine(t, nil)

	// 1. Create Person
	createBody, _ := json.Marshal(map[string]any{"personId": "PF001", "name": "Face User"})
	req := httptest.NewRequest(http.MethodPost, "/api/person", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create person: %d %s", w.Code, w.Body.String())
	}

	// 2. Upload face image (multipart/form-data)
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="face.jpg"`)
	h.Set("Content-Type", "image/jpeg")
	part, _ := writer.CreatePart(h)
	_, _ = part.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01})
	_ = writer.Close()

	req = httptest.NewRequest(http.MethodPost, "/api/person/PF001/faces", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("upload face got %d: %s", w.Code, w.Body.String())
	}
	var uploadResp struct {
		Code int `json:"code"`
		Data struct {
			FaceID       string  `json:"faceId"`
			QualityScore float32 `json:"qualityScore"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("unmarshal upload resp: %v", err)
	}
	if uploadResp.Code != 0 || uploadResp.Data.FaceID == "" {
		t.Fatalf("unexpected upload resp: %+v", uploadResp)
	}
	faceID := uploadResp.Data.FaceID

	// 3. List faces (GET /api/person/PF001/faces)
	req = httptest.NewRequest(http.MethodGet, "/api/person/PF001/faces", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list faces got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Code int `json:"code"`
		Data []struct {
			FaceID    string `json:"faceId"`
			IsPrimary bool   `json:"isPrimary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list resp: %v", err)
	}
	if listResp.Code != 0 || len(listResp.Data) != 1 || listResp.Data[0].FaceID != faceID || !listResp.Data[0].IsPrimary {
		t.Fatalf("unexpected list resp: %+v", listResp)
	}

	// 3.1 Test SetPrimaryFace API
	setPrimaryJSON, _ := json.Marshal(map[string]string{"faceId": faceID})
	req = httptest.NewRequest(http.MethodPut, "/api/person/PF001/primary-face", bytes.NewReader(setPrimaryJSON))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set primary face got %d: %s", w.Code, w.Body.String())
	}

	// 4. Download raw image (GET /api/person/PF001/faces/:faceId/image)
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/person/PF001/faces/%s/image", faceID), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get raw image got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("raw image content type = %s, want image/jpeg", w.Header().Get("Content-Type"))
	}

	// 5. Download aligned image (GET /api/person/PF001/faces/:faceId/aligned-image)
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/person/PF001/faces/%s/aligned-image", faceID), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get aligned image got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("aligned image content type = %s, want image/jpeg", w.Header().Get("Content-Type"))
	}

	// 6. Delete Face (DELETE /api/person/PF001/faces/:faceId)
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/person/PF001/faces/%s", faceID), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete face got %d: %s", w.Code, w.Body.String())
	}

	// 7. Verify List Faces is now empty
	req = httptest.NewRequest(http.MethodGet, "/api/person/PF001/faces", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if len(listResp.Data) != 0 {
		t.Fatalf("expected 0 faces, got %d", len(listResp.Data))
	}
}
