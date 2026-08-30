package api

import (
	"github.com/gin-gonic/gin"

	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/response"
	"argus/app/internal/service"
)

// TaskHandler 任务配置 HTTP Handler（任务 CRUD + 实例 CRUD + 未建任务摄像头下拉）。
// 错误统一交给 ErrorHandler 中间件输出，handler 只交 errno 错误或返回数据。
type TaskHandler struct {
	svc service.TaskService
}

// NewTaskHandler 创建 TaskHandler 实例。
func NewTaskHandler(svc service.TaskService) *TaskHandler {
	return &TaskHandler{svc: svc}
}

// ListTasks 分页查询任务列表 (GET /api/task/list)。
// 支持 cameraId/name/configured 筛选；实时字段（lastFrameAt/reportedAt）由状态合并提供。
func (h *TaskHandler) ListTasks(c *gin.Context) {
	var query service.TaskListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck // 交给统一错误处理中间件
		return
	}
	res, err := h.svc.ListTasks(c.Request.Context(), &query)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, res)
}

// CreateTask 创建分析任务 (POST /api/task)。
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var input service.CreateTaskInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	task, err := h.svc.CreateTask(c.Request.Context(), &input)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, task)
}

// UpdateTask 更新任务名称 (PUT /api/task/:cameraId)。
func (h *TaskHandler) UpdateTask(c *gin.Context) {
	cameraID := c.Param("cameraId")
	if cameraID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	var input service.UpdateTaskInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if err := h.svc.UpdateTask(c.Request.Context(), cameraID, &input); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}

// SetTaskEnabled 启停任务 (PUT /api/task/:cameraId/enabled)。
func (h *TaskHandler) SetTaskEnabled(c *gin.Context) {
	cameraID := c.Param("cameraId")
	if cameraID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	var input service.SetEnabledInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if err := h.svc.SetTaskEnabled(c.Request.Context(), cameraID, *input.Enabled); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}

// DeleteTask 删除任务并级联删除其全部实例 (DELETE /api/task/:cameraId)。
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	cameraID := c.Param("cameraId")
	if cameraID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if err := h.svc.DeleteTask(c.Request.Context(), cameraID); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}

// BatchDeleteTasks 批量删除任务并级联删除实例 (DELETE /api/task/batch)。
func (h *TaskHandler) BatchDeleteTasks(c *gin.Context) {
	var input service.BatchDeleteTaskInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if err := h.svc.BatchDeleteTasks(c.Request.Context(), &input); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}

// ListAvailableCameras 查询未建任务摄像头轻量列表 (GET /api/task/available-cameras)。
// 供任务新建表单下拉（D8），无分页，value 使用 camera_id 业务键。
func (h *TaskHandler) ListAvailableCameras(c *gin.Context) {
	items, err := h.svc.ListAvailableCameras(c.Request.Context())
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, items)
}

// GetTaskStats 任务管理概览统计 (GET /api/task/stats)。
// 返回任务/实例计数与计算单元负载（used/total/reserved/available），供页面顶部统计条展示。
func (h *TaskHandler) GetTaskStats(c *gin.Context) {
	stats, err := h.svc.TaskStats(c.Request.Context())
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, stats)
}

// ListInstances 查询任务的实例列表 (GET /api/task/instance/list?cameraId=...)。
// 实时字段（currentFps/reportedAt）由状态合并提供。
func (h *TaskHandler) ListInstances(c *gin.Context) {
	cameraID := c.Query("cameraId")
	if cameraID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	items, err := h.svc.ListInstances(c.Request.Context(), cameraID)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, items)
}

// CreateInstance 创建算法实例 (POST /api/task/instance)。
func (h *TaskHandler) CreateInstance(c *gin.Context) {
	var input service.CreateInstanceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	inst, err := h.svc.CreateInstance(c.Request.Context(), &input)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, inst)
}

// UpdateInstance 整份提交实例配置 (PUT /api/task/instance/:instanceId)。
// body 必须完整携带 analysisFps + paramsJson + rules（design §4.2 原子热更新）。
func (h *TaskHandler) UpdateInstance(c *gin.Context) {
	instanceID := c.Param("instanceId")
	if instanceID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	var input service.UpdateInstanceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if err := h.svc.UpdateInstance(c.Request.Context(), instanceID, &input); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}

// SetInstanceEnabled 启停实例 (PUT /api/task/instance/:instanceId/enabled)。
// 启用前服务端完整复校 schema → 几何 → 配额，拒绝时不产生任何副作用。
func (h *TaskHandler) SetInstanceEnabled(c *gin.Context) {
	instanceID := c.Param("instanceId")
	if instanceID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	var input service.SetEnabledInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if err := h.svc.SetInstanceEnabled(c.Request.Context(), instanceID, *input.Enabled); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}

// DeleteInstance 删除实例 (DELETE /api/task/instance/:instanceId)。
func (h *TaskHandler) DeleteInstance(c *gin.Context) {
	instanceID := c.Param("instanceId")
	if instanceID == "" {
		c.Error(errno.NewError(errno.CodeInvalidParam)) //nolint:errcheck
		return
	}
	if err := h.svc.DeleteInstance(c.Request.Context(), instanceID); err != nil {
		c.Error(err) //nolint:errcheck
		return
	}
	response.Success(c, nil)
}
