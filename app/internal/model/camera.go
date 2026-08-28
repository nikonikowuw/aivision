package model

import "time"

// 摄像头协议（当前 MVP 固定 rtsp，前端不可编辑）。
const (
	CameraProtocolRTSP = "rtsp"
)

// 传输策略（当前 MVP 固定 auto：TCP 优先，失败回退 UDP，前端不提供选择）。
const (
	CameraTransportAuto = "auto"
)

// 最近一次测活状态。
const (
	CameraProbeNever   = "never"   // 从未测活
	CameraProbeSuccess = "success" // 最近一次测活成功
	CameraProbeFailed  = "failed"  // 最近一次测活失败
)

// Camera 摄像头视频源表模型。
// 内部主键自增 uint64；camera_id 为不可变 UUID（对外暴露，用于 Go/C++ IPC、任务与事件关联）。
// 完整 RTSP URL（可含百分号编码 userinfo）按产品决策明文持久化，不做额外脱敏。
type Camera struct {
	BaseModel
	CameraID   string `gorm:"column:camera_id;size:36;not null;uniqueIndex:uk_cameras_camera_id" json:"cameraId"`
	Protocol   string `gorm:"column:protocol;size:16;not null;default:'rtsp'" json:"protocol"`
	Name       string `gorm:"column:name;size:128;not null" json:"name"`
	RtspURL    string `gorm:"column:rtsp_url;size:2048;not null" json:"rtspUrl"`
	SubRtspURL string `gorm:"column:sub_rtsp_url;size:2048;not null;default:''" json:"subRtspUrl"`
	Remark     string `gorm:"column:remark;size:255;not null;default:''" json:"remark"`
	// 传输策略（当前固定 auto）。Go/C++ 通过协议/策略适配选择实现，前端不可编辑。
	TransportPolicy string `gorm:"column:transport_policy;size:16;not null;default:'auto'" json:"transportPolicy"`

	// 配置指纹（protocol+canonical_url+transport_policy 的 sha256），用于测活乐观并发控制。
	// 内部字段，不暴露给前端。
	ConfigHash string `gorm:"column:config_hash;size:64;not null;default:''" json:"-"`

	// 最近一次测活结果（成功/失败均更新）。
	LastProbeStatus       string     `gorm:"column:last_probe_status;size:16;not null;default:'never'" json:"lastProbeStatus"`
	LastProbeAt           *time.Time `gorm:"column:last_probe_at" json:"lastProbeAt"`
	LastProbeErrorCode    string     `gorm:"column:last_probe_error_code;size:64;not null;default:''" json:"lastProbeErrorCode"`
	LastProbeErrorMessage string     `gorm:"column:last_probe_error_message;size:255;not null;default:''" json:"-"`

	// 最后已知成功媒体信息：失败测活更新当前失败状态但保留该历史；配置变更后旧结果标记为不适用于当前配置。
	LastSuccessAt        *time.Time `gorm:"column:last_success_at" json:"lastSuccessAt"`
	LastSuccessTransport string     `gorm:"column:last_success_transport;size:16;not null;default:''" json:"lastSuccessTransport"`
	LastCodec            string     `gorm:"column:last_codec;size:16;not null;default:''" json:"lastCodec"`
	LastWidth            int        `gorm:"column:last_width;not null;default:0" json:"lastWidth"`
	LastHeight           int        `gorm:"column:last_height;not null;default:0" json:"lastHeight"`
	LastFPS              float64    `gorm:"column:last_fps;not null;default:0" json:"lastFps"`
}

// TableName 返回摄像头表名。
func (Camera) TableName() string {
	return "cameras"
}
