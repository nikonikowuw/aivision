package api

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/response"
	"argus/app/internal/service"
)

// FaceCaptureHandler 人脸抓拍记录 API 控制器。
type FaceCaptureHandler struct {
	svc service.FaceCaptureService
}

// NewFaceCaptureHandler 创建 FaceCaptureHandler 实例。
func NewFaceCaptureHandler(svc service.FaceCaptureService) *FaceCaptureHandler {
	return &FaceCaptureHandler{svc: svc}
}

// ListPage 分页查询人脸抓拍记录 (GET /api/record/captures)。
func (h *FaceCaptureHandler) ListPage(c *gin.Context) {
	var query service.FaceCaptureQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	result, err := h.svc.ListPage(c.Request.Context(), &query)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, result)
}

// GetDetail 查询单条人脸抓拍记录详情 (GET /api/record/captures/:id)。
func (h *FaceCaptureHandler) GetDetail(c *gin.Context) {
	id, err := parseFaceCaptureID(c)
	if err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	result, err := h.svc.GetDetail(c.Request.Context(), id)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, result)
}

// ReadPanoramaImage 读取最佳全景图 (GET /api/record/captures/:id/panorama)。
func (h *FaceCaptureHandler) ReadPanoramaImage(c *gin.Context) {
	h.readImage(c, "panorama", 0)
}

// ReadFaceImage 读取最佳人脸特写图 (GET /api/record/captures/:id/face)。
func (h *FaceCaptureHandler) ReadFaceImage(c *gin.Context) {
	h.readImage(c, "face", 0)
}

// ReadSnapshotPanoramaImage 读取指定序号快照的全景图 (GET /api/record/captures/:id/snapshots/:index/panorama)。
func (h *FaceCaptureHandler) ReadSnapshotPanoramaImage(c *gin.Context) {
	index, err := parseSnapshotIndex(c)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	h.readImage(c, "panorama", index)
}

// ReadSnapshotFaceImage 读取指定序号快照的人脸特写图 (GET /api/record/captures/:id/snapshots/:index/face)。
func (h *FaceCaptureHandler) ReadSnapshotFaceImage(c *gin.Context) {
	index, err := parseSnapshotIndex(c)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	h.readImage(c, "face", index)
}

func parseSnapshotIndex(c *gin.Context) (int, error) {
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index <= 0 {
		return 0, errno.NewError(errno.CodeInvalidParam)
	}
	return index, nil
}

func parseFaceCaptureID(c *gin.Context) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, err
	}
	return id, nil
}

func (h *FaceCaptureHandler) readImage(c *gin.Context, kind string, snapshotIndex int) {
	id, err := parseFaceCaptureID(c)
	if err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	reader, size, contentType, err := h.svc.ReadImageStream(c.Request.Context(), id, kind, snapshotIndex)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	defer reader.Close()

	c.Header("Content-Type", contentType)
	if size > 0 {
		c.Header("Content-Length", strconv.FormatInt(size, 10))
	}
	c.Header("Cache-Control", "private, max-age=86400")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, reader); err != nil {
		_ = c.Error(fmt.Errorf("stream face capture image: %w", err))
	}
}
