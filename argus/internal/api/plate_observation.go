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

// PlateObservationHandler 车牌抓拍过车记录 API 控制器。
type PlateObservationHandler struct {
	svc service.PlateObservationService
}

// NewPlateObservationHandler 创建 PlateObservationHandler 实例。
func NewPlateObservationHandler(svc service.PlateObservationService) *PlateObservationHandler {
	return &PlateObservationHandler{svc: svc}
}

// ListPage 分页查询车牌抓拍过车记录 (GET /api/record/plates)。
func (h *PlateObservationHandler) ListPage(c *gin.Context) {
	var query service.PlateObservationQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	res, err := h.svc.ListPage(c.Request.Context(), &query)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, res)
}

// GetDetail 查询单条车牌过车详情 (GET /api/record/plates/:id)。
func (h *PlateObservationHandler) GetDetail(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	detail, err := h.svc.GetDetail(c.Request.Context(), id)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, detail)
}

// ReadPanoramaImage 读取全景大图。
func (h *PlateObservationHandler) ReadPanoramaImage(c *gin.Context) {
	h.readImage(c, "panorama")
}

// ReadPlateImage 读取车牌特写图。
func (h *PlateObservationHandler) ReadPlateImage(c *gin.Context) {
	h.readImage(c, "plate")
}

// ReadImageByKind 统一图片读取入口。
func (h *PlateObservationHandler) ReadImageByKind(c *gin.Context) {
	kind := c.Param("kind")
	if kind != "plate" && kind != "panorama" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	h.readImage(c, kind)
}

func (h *PlateObservationHandler) readImage(c *gin.Context, kind string) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	stream, size, contentType, err := h.svc.ReadImageStream(c.Request.Context(), id, kind)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	defer stream.Close()

	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Header("Cache-Control", "private, max-age=86400")

	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, stream); err != nil {
		_ = c.Error(fmt.Errorf("stream image: %w", err))
	}
}
