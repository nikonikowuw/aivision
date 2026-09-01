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
// @Summary 分页查询车牌过车记录
// @Description 支持按时间区间、摄像头、车牌文本、颜色、类型和置信度组合分页查询
// @Tags 车牌记录
// @Security BearerAuth
// @Produce json
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页条数" default(20)
// @Param startTime query string false "开始时间"
// @Param endTime query string false "结束时间"
// @Param cameraId query string false "摄像头ID"
// @Param plateText query string false "车牌文本"
// @Param plateColor query string false "车牌颜色"
// @Param plateType query string false "车牌类型"
// @Param minConfidence query number false "最低检测置信度"
// @Param maxConfidence query number false "最高检测置信度"
// @Param minOcrConfidence query number false "最低 OCR 置信度"
// @Success 200 {object} response.Result{data=service.PlateObservationPageResult} "车牌记录分页数据"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/record/plates [get]
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
// @Summary 查询车牌过车详情
// @Description 获取单条车牌过车记录，包含算法来源、同步状态及全景/特写图片信息
// @Tags 车牌记录
// @Security BearerAuth
// @Produce json
// @Param id path int true "车牌记录ID"
// @Success 200 {object} response.Result{data=service.PlateObservationItem} "车牌详情数据"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 404 {object} response.Result "记录不存在"
// @Router /api/record/plates/{id} [get]
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
// @Summary 读取车牌过车全景图
// @Description 读取指定车牌过车记录关联的全景 JPEG 图片
// @Tags 车牌记录
// @Security BearerAuth
// @Produce image/jpeg
// @Param id path int true "车牌记录ID"
// @Success 200 {file} binary "全景图片文件流"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 404 {object} response.Result "图片不存在"
// @Router /api/record/plates/{id}/panorama [get]
func (h *PlateObservationHandler) ReadPanoramaImage(c *gin.Context) {
	h.readImage(c, "panorama")
}

// ReadPlateImage 读取车牌特写图。
// @Summary 读取车牌特写图
// @Description 读取指定车牌过车记录关联的车牌特写 JPEG 图片
// @Tags 车牌记录
// @Security BearerAuth
// @Produce image/jpeg
// @Param id path int true "车牌记录ID"
// @Success 200 {file} binary "车牌特写图片文件流"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 404 {object} response.Result "图片不存在"
// @Router /api/record/plates/{id}/plate [get]
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
