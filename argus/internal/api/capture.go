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

// CaptureHandler 通用抓拍记录 API 控制器。
type CaptureHandler struct {
	svc service.CaptureService
}

// NewCaptureHandler 创建 CaptureHandler 实例。
func NewCaptureHandler(svc service.CaptureService) *CaptureHandler {
	return &CaptureHandler{svc: svc}
}

// ListPage 分页查询通用抓拍记录。
// @Summary 分页查询通用抓拍记录
// @Description 支持按目标类型、摄像头、时间、关键词和识别状态组合查询
// @Tags 抓拍记录
// @Security BearerAuth
// @Produce json
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页条数" default(20)
// @Param targetType query string false "目标类型: all|face|person|vehicle|non_motor|generic"
// @Param cameraId query string false "摄像头ID"
// @Param keyword query string false "事件ID、摄像头名称或目标属性关键词"
// @Param isRecognized query bool false "是否已识别"
// @Param minQuality query number false "最低质量分"
// @Param maxQuality query number false "最高质量分"
// @Success 200 {object} response.Result{data=service.CapturePageResult}
// @Failure 400 {object} response.Result
// @Failure 401 {object} response.Result
// @Router /api/record/captures [get]
func (h *CaptureHandler) ListPage(c *gin.Context) {
	var query service.CaptureQuery
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

// GetDetail 查询单条通用抓拍记录详情。
// @Summary 查询通用抓拍记录详情
// @Tags 抓拍记录
// @Security BearerAuth
// @Produce json
// @Param id path int true "抓拍记录ID"
// @Success 200 {object} response.Result{data=service.CaptureItem}
// @Failure 400 {object} response.Result
// @Failure 404 {object} response.Result
// @Router /api/record/captures/{id} [get]
func (h *CaptureHandler) GetDetail(c *gin.Context) {
	id, err := parseCaptureID(c)
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

// ReadImage 读取通用抓拍图片。
// @Summary 读取通用抓拍图片
// @Description kind 支持 panorama、crop、sub_crop，type=thumb 时优先读取缩略图
// @Tags 抓拍记录
// @Security BearerAuth
// @Produce image/jpeg
// @Param id path int true "抓拍记录ID"
// @Param kind query string true "图片类型"
// @Param type query string false "图片类型: thumb 缩略图"
// @Success 200 {file} binary
// @Failure 400 {object} response.Result
// @Failure 401 {object} response.Result
// @Failure 404 {object} response.Result
// @Router /api/record/captures/{id}/image [get]
func (h *CaptureHandler) ReadImage(c *gin.Context) {
	id, err := parseCaptureID(c)
	if err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	kind := c.Query("kind")
	isThumbnail := c.Query("type") == "thumb" || c.Query("type") == "thumbnail"
	reader, size, contentType, err := h.svc.ReadImageStream(c.Request.Context(), id, kind, isThumbnail)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	defer reader.Close()

	c.Header("Content-Type", contentType)
	if size > 0 {
		c.Header("Content-Length", strconv.FormatInt(size, 10))
	}
	c.Header("Cache-Control", "private, max-age=604800, immutable")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, reader); err != nil {
		_ = c.Error(fmt.Errorf("stream capture image: %w", err))
	}
}

func parseCaptureID(c *gin.Context) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, err
	}
	return id, nil
}
