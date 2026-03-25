package model

import (
	"time"

	"gorm.io/gorm"
)

// ReportStatus 日报状态
type ReportStatus int

const (
	ReportStatusDraft  ReportStatus = iota // 草稿
	ReportStatusReady                      // 待发送
	ReportStatusSent                       // 已发送
	ReportStatusFailed                     // 发送失败
)

// String 返回状态的中文描述
func (s ReportStatus) String() string {
	switch s {
	case ReportStatusDraft:
		return "草稿"
	case ReportStatusReady:
		return "待发送"
	case ReportStatusSent:
		return "已发送"
	case ReportStatusFailed:
		return "发送失败"
	default:
		return "未知"
	}
}

// Report 日报模型
type Report struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Date      string         `gorm:"type:varchar(10);not null;uniqueIndex:idx_reports_date_deleted" json:"date"` // 格式: 2026-03-24
	Weekday   string         `gorm:"type:varchar(10);not null" json:"weekday"`                                   // 星期几
	Content   string         `gorm:"type:text" json:"content"`                                                   // 日报正文内容
	Status    ReportStatus   `gorm:"type:integer;default:0;index" json:"status"`                                 // 日报状态
	SiyuanID  string         `gorm:"type:varchar(64);index" json:"siyuan_id"`                                    // 思源笔记中的行 ID
	SyncedAt  *time.Time     `json:"synced_at"`                                                                  // 最后同步思源的时间
	SentAt    *time.Time     `json:"sent_at"`                                                                    // 最后发送邮件的时间
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"uniqueIndex:idx_reports_date_deleted" json:"-"`
}

// TableName 指定表名
func (Report) TableName() string {
	return "reports"
}

// IsSent 是否已发送
func (r *Report) IsSent() bool {
	return r.Status == ReportStatusSent
}

// StatusBadgeClass 返回状态对应的 CSS badge 类名
func (r *Report) StatusBadgeClass() string {
	switch r.Status {
	case ReportStatusDraft:
		return "badge-secondary"
	case ReportStatusReady:
		return "badge-primary"
	case ReportStatusSent:
		return "badge-success"
	case ReportStatusFailed:
		return "badge-danger"
	default:
		return "badge-secondary"
	}
}

// SendLog 邮件发送记录
type SendLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ReportID   uint      `gorm:"index;not null" json:"report_id"`      // 关联日报 ID
	ReportDate string    `gorm:"type:varchar(10)" json:"report_date"`  // 日报日期（冗余字段，方便查询）
	Recipients string    `gorm:"type:text;not null" json:"recipients"` // 收件人列表，逗号分隔
	Subject    string    `gorm:"type:varchar(255)" json:"subject"`     // 邮件主题
	Body       string    `gorm:"type:text" json:"body"`                // 邮件正文（渲染后的 HTML）
	Status     int       `gorm:"type:integer;default:0" json:"status"` // 0=成功 1=失败
	Error      string    `gorm:"type:text" json:"error"`               // 失败时的错误信息
	SentAt     time.Time `gorm:"not null" json:"sent_at"`              // 发送时间
	CreatedAt  time.Time `json:"created_at"`
}

// TableName 指定表名
func (SendLog) TableName() string {
	return "send_logs"
}

// IsSuccess 是否发送成功
func (l *SendLog) IsSuccess() bool {
	return l.Status == 0
}

// ScheduleTask 定时任务记录
type ScheduleTask struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	TaskType    string     `gorm:"type:varchar(32);not null;index" json:"task_type"` // create_report / send_report
	TaskName    string     `gorm:"type:varchar(128);not null" json:"task_name"`      // 任务名称
	CronExpr    string     `gorm:"type:varchar(64);not null" json:"cron_expr"`       // cron 表达式
	Enabled     bool       `gorm:"type:boolean;default:true" json:"enabled"`         // 是否启用
	LastRunAt   *time.Time `json:"last_run_at"`                                      // 最后执行时间
	LastResult  string     `gorm:"type:text" json:"last_result"`                     // 最后执行结果
	Description string     `gorm:"type:text" json:"description"`                     // 任务描述
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TableName 指定表名
func (ScheduleTask) TableName() string {
	return "schedule_tasks"
}

// ReportListQuery 日报列表查询参数
type ReportListQuery struct {
	Page     int           `form:"page" binding:"-"`
	PageSize int           `form:"page_size" binding:"-"`
	Status   *ReportStatus `form:"status" binding:"-"`
	DateFrom string        `form:"date_from" binding:"-"`
	DateTo   string        `form:"date_to" binding:"-"`
	Keyword  string        `form:"keyword" binding:"-"`
}

// Normalize 规范化查询参数
func (q *ReportListQuery) Normalize() {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 || q.PageSize > 100 {
		q.PageSize = 20
	}
}

// Offset 计算分页偏移量
func (q *ReportListQuery) Offset() int {
	return (q.Page - 1) * q.PageSize
}

// Pagination 分页信息
type Pagination struct {
	Page      int   `json:"page"`
	PageSize  int   `json:"page_size"`
	Total     int64 `json:"total"`
	TotalPage int   `json:"total_page"`
}

// HasPrev 是否有上一页
func (p *Pagination) HasPrev() bool {
	return p.Page > 1
}

// HasNext 是否有下一页
func (p *Pagination) HasNext() bool {
	return p.Page < p.TotalPage
}

// PrevPage 上一页页码
func (p *Pagination) PrevPage() int {
	if p.Page > 1 {
		return p.Page - 1
	}
	return 1
}

// NextPage 下一页页码
func (p *Pagination) NextPage() int {
	if p.Page < p.TotalPage {
		return p.Page + 1
	}
	return p.TotalPage
}

// Pages 生成页码列表，用于模板渲染分页栏
func (p *Pagination) Pages() []int {
	pages := make([]int, 0)
	start := p.Page - 2
	if start < 1 {
		start = 1
	}
	end := start + 4
	if end > p.TotalPage {
		end = p.TotalPage
	}
	if end-start < 4 && start > 1 {
		start = end - 4
		if start < 1 {
			start = 1
		}
	}
	for i := start; i <= end; i++ {
		pages = append(pages, i)
	}
	return pages
}
