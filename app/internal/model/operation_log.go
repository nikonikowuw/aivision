package model

import "time"

// OperationLog 操作日志（表名 operation_logs；只记写操作）。
type OperationLog struct {
	TimeFields
	// CreatedAt 覆盖 TimeFields：操作日志按时间范围查询，需要索引（设计 §2）。
	CreatedAt  time.Time `gorm:"column:created_at;index" json:"createdAt"`
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	UserID     uint64    `gorm:"column:user_id" json:"userId"` // 登录失败等场景可为空
	Username   string    `gorm:"column:username;type:varchar(64);index" json:"username"`
	Module     string    `gorm:"column:module;type:varchar(64);index" json:"module"`
	Action     string    `gorm:"column:action;type:varchar(64)" json:"action"`
	Method     string    `gorm:"column:method;type:varchar(16)" json:"method"`
	Path       string    `gorm:"column:path;type:varchar(255)" json:"path"`
	Query      string    `gorm:"column:query;type:text" json:"query"`
	Body       string    `gorm:"column:body;type:text" json:"body"` // 已脱敏 JSON
	StatusCode int       `gorm:"column:status_code;index" json:"statusCode"`
	DurationMs int64     `gorm:"column:duration_ms" json:"durationMs"`
	IP         string    `gorm:"column:ip;type:varchar(64)" json:"ip"`
	UserAgent  string    `gorm:"column:user_agent;type:varchar(255)" json:"userAgent"`
}

// TableName 显式声明表名。
func (OperationLog) TableName() string { return "operation_logs" }
