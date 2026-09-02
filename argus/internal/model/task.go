package model

import (
	"time"

	"gorm.io/plugin/soft_delete"
)

// 任务配置模块模型：检测规则契约结构（Phase 1）与三张表模型
// AnalysisTask / AlgorithmInstance / DesiredStateRevision（Phase 2）。

// DetectionRuleRole 检测规则角色。
// 数值与 proto DetectionRuleRole（engine/proto/aivision/v1/engine.proto）及
// C ABI av_rule_role（sdk/include/aivision/algo.h）保持一致。
type DetectionRuleRole int32

const (
	DetectionRuleRoleUnspecified DetectionRuleRole = 0 // 未指定（非法）
	DetectionRuleRoleROI         DetectionRuleRole = 1 // 布防/检测区域：只在此区域内检测
	DetectionRuleRoleMask        DetectionRuleRole = 2 // 屏蔽/遮罩区域：此区域内目标忽略
	DetectionRuleRoleLine        DetectionRuleRole = 3 // 分界线：目标跨线触发
)

// DetectionLineDirection 分界线跨越方向，仅 LINE 规则有效。
// 数值与 proto DetectionLineDirection 及 C ABI av_line_dir 保持一致。
type DetectionLineDirection int32

const (
	DetectionLineDirectionBoth DetectionLineDirection = 0 // 双向跨越
	DetectionLineDirectionAToB DetectionLineDirection = 1 // 沿折线方向跨越
	DetectionLineDirectionBToA DetectionLineDirection = 2 // 逆折线方向跨越
)

// DetectionPoint 归一化检测点。
// 坐标原点为视频有效画面左上角，x/y 均归一化到 [0,1]（见 prd §7.6.1）。
type DetectionPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// DetectionRule 任务级检测规则（ROI / Mask / 分界线）。
// 持久化形态：algorithm_instances.rules_json 中存 []DetectionRule 的 JSON。
// 契约（design §6 / R6）：
//   - ROI/MASK 为闭合多边形，至少 3 个顶点且不自交；
//   - LINE 为折线，至少 2 个顶点；
//   - line_direction 仅 LINE 规则有效，区域规则必须为 0。
type DetectionRule struct {
	Role          DetectionRuleRole      `json:"role"`
	LineDirection DetectionLineDirection `json:"lineDirection"`
	Points        []DetectionPoint       `json:"points"`
}

// ── 运行状态码 ────────────────────────────────────────────────────────
// actual_status 列直存 proto 枚举数值，避免二次映射失配（design §2.2）。
// task 与 instance 两个枚举的数值语义不同（task 的 5=STOPPED、instance 的 5=ERROR），
// 必须分别定义常量，禁止共用。

// 分析任务状态码（analysis_tasks.actual_status，int8）。
// 数值对齐 proto TaskStatusCode（app.proto）：task 的 5=STOPPED、6=ERROR。
const (
	TaskStatusUnspecified  int8 = 0 // 未指定
	TaskStatusStarting     int8 = 1 // 启动中
	TaskStatusRunning      int8 = 2 // 运行中
	TaskStatusDegraded     int8 = 3 // 降级运行
	TaskStatusReconnecting int8 = 4 // 重连中
	TaskStatusStopped      int8 = 5 // 已停止
	TaskStatusError        int8 = 6 // 错误
)

// 算法实例状态码（algorithm_instances.actual_status，int8）。
// 数值对齐 proto InstanceStatusCode（app.proto）：instance 的 4=STOPPED、5=ERROR。
const (
	InstanceStatusUnspecified int8 = 0 // 未指定
	InstanceStatusStarting    int8 = 1 // 启动中
	InstanceStatusRunning     int8 = 2 // 运行中
	InstanceStatusDegraded    int8 = 3 // 降级运行
	InstanceStatusStopped     int8 = 4 // 已停止
	InstanceStatusError       int8 = 5 // 错误
)

// AnalysisTask 分析任务表模型（与摄像头 1:1，camera_id 即任务标识，不发明独立 task_id，见 design D2）。
// 唯一业务键 camera_id；任务软删后同一摄像头可重新建任务（D8），因此唯一索引必须
// 复合 deleted_at（database-guidelines：deleted_at=0 表示活跃，禁止原生 gorm.DeletedAt）。
type AnalysisTask struct {
	BaseModel
	// 覆写 BaseModel.DeletedAt 以加入 camera_id 复合唯一索引（对齐 users/roles 模式）。
	DeletedAt      soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_analysis_tasks_camera_id" json:"-"`
	CameraID       string                `gorm:"column:camera_id;size:36;not null;uniqueIndex:uk_analysis_tasks_camera_id" json:"cameraId"`
	Name           string                `gorm:"column:name;size:128;not null" json:"name"`
	DesiredEnabled bool                  `gorm:"column:desired_enabled;not null;default:false" json:"desiredEnabled"`
	ActualStatus   int8                  `gorm:"column:actual_status;not null" json:"actualStatus"`
	StatusMessage  string                `gorm:"column:status_message;size:255;not null;default:''" json:"statusMessage"`
	LastFrameAt    *time.Time            `gorm:"column:last_frame_at" json:"lastFrameAt"`
	ReportedAt     *time.Time            `gorm:"column:reported_at" json:"reportedAt"`
}

// TableName 显式声明表名。
func (AnalysisTask) TableName() string { return "analysis_tasks" }

// MotionGateConfig 运动门控配置（系统/引擎级）。
type MotionGateConfig struct {
	Enabled             bool    `json:"enabled"`
	Threshold           uint32  `json:"threshold,omitempty"`
	ContourArea         uint32  `json:"contourArea,omitempty"`
	FrameAlpha          float32 `json:"frameAlpha,omitempty"`
	KeepaliveIntervalMs uint64  `json:"keepaliveIntervalMs,omitempty"`
}

// AlgorithmInstance 算法实例表模型。
// 挂在 camera_id 下（不经 analysis_tasks.id，与 Engine 寻址一致，见 design D2/D9）。
// 无 algorithm_version 列（D11）：组装 DesiredState 时从 algorithms.active_version 动态填充。
type AlgorithmInstance struct {
	BaseModel
	// 覆写 BaseModel.DeletedAt 以加入 instance_id 复合唯一索引。
	DeletedAt           soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_algorithm_instances_instance_id" json:"-"`
	InstanceID          string                `gorm:"column:instance_id;size:36;not null;uniqueIndex:uk_algorithm_instances_instance_id" json:"instanceId"`
	CameraID            string                `gorm:"column:camera_id;size:36;not null;index:idx_algorithm_instances_camera_id" json:"cameraId"`
	AlgorithmID         string                `gorm:"column:algorithm_id;size:64;not null;index:idx_algorithm_instances_algorithm_id" json:"algorithmId"`
	AnalysisFPS         int32                 `gorm:"column:analysis_fps;not null;default:0" json:"analysisFps"`
	ParamsJSON          JSONRaw               `gorm:"column:params_json;type:jsonb;not null;default:'{}'" json:"paramsJson"`
	RulesJSON           JSONRaw               `gorm:"column:rules_json;type:jsonb;not null;default:'[]'" json:"rulesJson"`
	MotionGateJSON      JSONRaw               `gorm:"column:motion_gate_json;type:jsonb;not null;default:'{}'" json:"motionGateJson"`
	FaceRecognitionJSON JSONRaw               `gorm:"column:face_recognition_json;type:jsonb;not null;default:'{}'" json:"faceRecognitionJson"`
	Enabled             bool                  `gorm:"column:enabled;not null;default:false" json:"enabled"`
	ActualStatus        int8                  `gorm:"column:actual_status;not null" json:"actualStatus"`
	StatusMessage       string                `gorm:"column:status_message;size:255;not null;default:''" json:"statusMessage"`
}

// TableName 显式声明表名。
func (AlgorithmInstance) TableName() string { return "algorithm_instances" }

// FaceRecognitionConfigJSON 算法实例人脸识别比对配置（存储于 algorithm_instances.face_recognition_json）。
type FaceRecognitionConfigJSON struct {
	SimilarityThreshold float32 `json:"similarityThreshold"`
}

// FaceGalleryRevision 人脸底库版本计数器表模型（单行 id=1，只增不减）。
// 生产表由 000032 迁移创建并初始化；模型供 sqlite 单测建表与 repository 查询使用。
type FaceGalleryRevision struct {
	ID       int16 `gorm:"column:id;primaryKey;default:1" json:"id"`
	Revision int64 `gorm:"column:revision;not null;default:0" json:"revision"`
}

// TableName 显式声明表名。
func (FaceGalleryRevision) TableName() string { return "face_gallery_revision" }

// DesiredStateRevision 期望状态版本计数器表模型（单行 id=1，只增不减，见 design D4）。
// 生产表由 000019 迁移创建并初始化；模型供 sqlite 单测建表与 repository 查询使用。
type DesiredStateRevision struct {
	ID       int16 `gorm:"column:id;primaryKey;default:1" json:"id"`
	Revision int64 `gorm:"column:revision;not null;default:0" json:"revision"`
}

// TableName 显式声明表名。
func (DesiredStateRevision) TableName() string { return "desired_state_revision" }
