package model

import "time"

// RefreshToken 刷新令牌（表名 refresh_tokens；不透明随机串，本身不是 JWT）。
type RefreshToken struct {
	TimeFields
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	UserID    uint64    `gorm:"column:user_id;not null;index" json:"userId"`
	Token     string    `gorm:"column:token;type:varchar(256);not null;uniqueIndex" json:"-"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null;index" json:"expiresAt"`
	Revoked   bool      `gorm:"column:revoked;default:false" json:"revoked"`
	UserAgent string    `gorm:"column:user_agent;type:varchar(255)" json:"userAgent"`
	IP        string    `gorm:"column:ip;type:varchar(64)" json:"ip"`
}

// TableName 显式声明表名。
func (RefreshToken) TableName() string { return "refresh_tokens" }
