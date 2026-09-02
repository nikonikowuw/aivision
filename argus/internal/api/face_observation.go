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

// FaceObservationHandler 人脸识别记录 API 控制器。
type FaceObservationHandler struct {
	svc service.FaceObservationService
}

// NewFaceObservationHandler 创建 FaceObservationHandler 实例。
func NewFaceObservationHandler(svc service.FaceObservationService) *FaceObservationHandler {
	return &FaceObservationHandler{svc: svc}
}

// ListPage 分页查询人脸识别记录 (GET /api/record/faces)。
// @Summary 分页查询人脸识别记录
// @Description 支持按人员、摄像头、时间区间和相似度组合分页查询
// @Tags 人脸识别记录
// @Security BearerAuth
// @Produce json
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页条数" default(20)
// @Param startTime query string false "开始时间"
// @Param endTime query string false "结束时间"
// @Param cameraId query string false "摄像头ID"
// @Param personId query string false "人员ID"
// @Param personName query string false "人员姓名"
// @Param minSimilarity query number false "最低相似度"
// @Param maxSimilarity query number false "最高相似度"
// @Success 200 {object} response.Result{data=service.FaceObservationPageResult} "人脸识别记录分页数据"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/record/faces [get]
func (h *FaceObservationHandler) ListPage(c *gin.Context) {
	var query service.FaceObservationQuery
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

// GetDetail 查询单条人脸识别记录详情 (GET /api/record/faces/:id)。
// @Summary 查询人脸识别记录详情
// @Description 获取单条人脸识别记录，包含人员、相似度、算法来源及两张抓拍图
// @Tags 人脸识别记录
// @Security BearerAuth
// @Produce json
// @Param id path int true "人脸识别记录ID"
// @Success 200 {object} response.Result{data=service.FaceObservationItem} "人脸识别详情数据"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 404 {object} response.Result "记录不存在"
// @Router /api/record/faces/{id} [get]
func (h *FaceObservationHandler) GetDetail(c *gin.Context) {
	id, err := parseFaceObservationID(c)
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

// ReadPanoramaImage 读取全景图 (GET /api/record/faces/:id/panorama)。
// @Summary 读取人脸识别全景图
// @Tags 人脸识别记录
// @Security BearerAuth
// @Produce image/jpeg
// @Param id path int true "人脸识别记录ID"
// @Success 200 {file} binary "全景图片文件流"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 404 {object} response.Result "图片不存在"
// @Router /api/record/faces/{id}/panorama [get]
func (h *FaceObservationHandler) ReadPanoramaImage(c *gin.Context) {
	h.readImage(c, "panorama")
}

// ReadFaceImage 读取人脸特写图 (GET /api/record/faces/:id/face)。
// @Summary 读取人脸特写图
// @Tags 人脸识别记录
// @Security BearerAuth
// @Produce image/jpeg
// @Param id path int true "人脸识别记录ID"
// @Success 200 {file} binary "人脸特写图片文件流"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 404 {object} response.Result "图片不存在"
// @Router /api/record/faces/{id}/face [get]
func (h *FaceObservationHandler) ReadFaceImage(c *gin.Context) {
	h.readImage(c, "face")
}

func parseFaceObservationID(c *gin.Context) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, err
	}
	return id, nil
}

func (h *FaceObservationHandler) readImage(c *gin.Context, kind string) {
	id, err := parseFaceObservationID(c)
	if err != nil {
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
		_ = c.Error(fmt.Errorf("stream face observation image: %w", err))
	}
}
