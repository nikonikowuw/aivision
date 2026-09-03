package api

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/response"
	"argus/app/internal/service"
)

// AlarmRecordHandler 告警记录 API 控制器。
type AlarmRecordHandler struct {
	svc service.AlarmRecordService
}

// NewAlarmRecordHandler 创建 AlarmRecordHandler 实例。
func NewAlarmRecordHandler(svc service.AlarmRecordService) *AlarmRecordHandler {
	return &AlarmRecordHandler{svc: svc}
}

// ListPage 分页查询告警记录 (GET /api/record/alarms)。
// @Summary 分页查询告警记录
// @Description 支持按发生时间区间、摄像头、算法、告警类型、目标类型、置信度区间组合分页查询
// @Tags 告警记录
// @Security BearerAuth
// @Produce json
// @Param page query int false "页码" default(1)
// @Param pageSize query int false "每页条数" default(20)
// @Param startTime query string false "开始时间"
// @Param endTime query string false "结束时间"
// @Param cameraId query string false "摄像头ID"
// @Param algorithmId query string false "算法ID"
// @Param alarmTypeId query string false "告警类型ID"
// @Param targetLabel query string false "目标类型标签"
// @Param minConfidence query number false "最低置信度"
// @Param maxConfidence query number false "最高置信度"
// @Success 200 {object} response.Result{data=service.AlarmRecordPageResult} "告警分页数据"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Router /api/record/alarms [get]
func (h *AlarmRecordHandler) ListPage(c *gin.Context) {
	var query service.AlarmRecordQuery
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

// GetDetail 查询告警详情 (GET /api/record/alarms/:id)。
// @Summary 查询告警记录详情
// @Description 获取单条告警详细信息，含检测规则与目标框
// @Tags 告警记录
// @Security BearerAuth
// @Produce json
// @Param id path int true "告警记录ID"
// @Success 200 {object} response.Result{data=service.AlarmRecordDetail} "告警详情数据"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 404 {object} response.Result "记录不存在"
// @Router /api/record/alarms/{id} [get]
func (h *AlarmRecordHandler) GetDetail(c *gin.Context) {
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

// ReadImageStream 安全读取受控告警全景图 (GET /api/record/images/:id)。
// @Summary 受控读取告警图片
// @Description 校验 Bearer Token 并在后端代理读取 Engine var/images 下的安全图片流，支持 type=thumb 缩略图
// @Tags 告警记录
// @Security BearerAuth
// @Produce image/jpeg
// @Param id path string true "图片唯一标识 image_id"
// @Param type query string false "图片类型: thumb 缩略图, 默认原图"
// @Success 200 {file} binary "图片文件流"
// @Failure 400 {object} response.Result "参数错误"
// @Failure 401 {object} response.Result "未授权"
// @Failure 404 {object} response.Result "图片不存在"
// @Router /api/record/images/{id} [get]
func (h *AlarmRecordHandler) ReadImageStream(c *gin.Context) {
	imageID := c.Param("id")
	if imageID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}

	isThumbnail := c.Query("type") == "thumb" || c.Query("type") == "thumbnail"
	reader, size, contentType, err := h.svc.ReadImageStream(c.Request.Context(), imageID, isThumbnail)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	defer func() { _ = reader.Close() }()

	// 增加 HTTP 静态资源不可变缓存（7 天），提升表格滚动与反复查看时的加载速度
	c.Header("Cache-Control", "public, max-age=604800, immutable")
	c.DataFromReader(200, size, contentType, reader, nil)
}
