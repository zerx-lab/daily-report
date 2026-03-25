package model

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// OutingStatus 外出申请状态
type OutingStatus int

const (
	OutingStatusDraft  OutingStatus = iota // 草稿
	OutingStatusReady                      // 待发送
	OutingStatusSent                       // 已发送
	OutingStatusFailed                     // 发送失败
)

// String 返回状态的中文描述
func (s OutingStatus) String() string {
	switch s {
	case OutingStatusDraft:
		return "草稿"
	case OutingStatusReady:
		return "待发送"
	case OutingStatusSent:
		return "已发送"
	case OutingStatusFailed:
		return "发送失败"
	default:
		return "未知"
	}
}

// StatusBadgeClass 返回状态对应的 CSS badge 类名
func (s OutingStatus) StatusBadgeClass() string {
	switch s {
	case OutingStatusDraft:
		return "badge-secondary"
	case OutingStatusReady:
		return "badge-primary"
	case OutingStatusSent:
		return "badge-success"
	case OutingStatusFailed:
		return "badge-danger"
	default:
		return "badge-secondary"
	}
}

// OutingRequest 外出申请模型
type OutingRequest struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Applicant   string         `gorm:"type:varchar(64);not null" json:"applicant"`    // 申请人
	Department  string         `gorm:"type:varchar(64);not null" json:"department"`   // 部门
	OutTime     time.Time      `gorm:"not null" json:"out_time"`                      // 申请外出时间
	ReturnTime  time.Time      `gorm:"not null" json:"return_time"`                   // 预计返回时间
	Destination string         `gorm:"type:varchar(255);not null" json:"destination"` // 外出地点
	Reason      string         `gorm:"type:text;not null" json:"reason"`              // 外出事由
	Remarks     string         `gorm:"type:text" json:"remarks"`                      // 备注说明
	Status      OutingStatus   `gorm:"type:integer;default:0;index" json:"status"`    // 申请状态
	SiyuanID    string         `gorm:"type:varchar(64);index" json:"siyuan_id"`       // 思源笔记中的行 ID
	SentAt      *time.Time     `json:"sent_at"`                                       // 发送时间
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (OutingRequest) TableName() string {
	return "outing_requests"
}

// FormatOutTime 格式化外出时间，返回如 "2026年3月20日8时00分"
func (o *OutingRequest) FormatOutTime() string {
	return fmt.Sprintf("%d年%d月%d日%d时%02d分",
		o.OutTime.Year(), o.OutTime.Month(), o.OutTime.Day(),
		o.OutTime.Hour(), o.OutTime.Minute())
}

// FormatReturnTime 格式化返回时间，返回如 "2026年3月20日18时00分"
func (o *OutingRequest) FormatReturnTime() string {
	return fmt.Sprintf("%d年%d月%d日%d时%02d分",
		o.ReturnTime.Year(), o.ReturnTime.Month(), o.ReturnTime.Day(),
		o.ReturnTime.Hour(), o.ReturnTime.Minute())
}

// IsSent 是否已发送
func (o *OutingRequest) IsSent() bool {
	return o.Status == OutingStatusSent
}

// OutingListQuery 外出申请列表查询参数
type OutingListQuery struct {
	Page     int           `form:"page" binding:"-"`
	PageSize int           `form:"page_size" binding:"-"`
	Status   *OutingStatus `form:"status" binding:"-"`
	DateFrom string        `form:"date_from" binding:"-"` // 外出时间起始日期
	DateTo   string        `form:"date_to" binding:"-"`   // 外出时间截止日期
	Keyword  string        `form:"keyword" binding:"-"`   // 搜索关键词（申请人/地点/事由）
}

// Normalize 规范化查询参数
func (q *OutingListQuery) Normalize() {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 || q.PageSize > 100 {
		q.PageSize = 20
	}
}

// Offset 计算分页偏移量
func (q *OutingListQuery) Offset() int {
	return (q.Page - 1) * q.PageSize
}
