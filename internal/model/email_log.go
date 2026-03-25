package model

import "time"

// 邮件日志类型常量
const (
	LogTypeReport = 0 // 日报邮件
	LogTypeOuting = 1 // 外出申请邮件
)

// EmailLog 邮件发送记录模型
type EmailLog struct {
	ID         uint           `json:"id" gorm:"primaryKey;autoIncrement"`
	LogType    int            `json:"log_type" gorm:"type:integer;default:0;index;comment:日志类型 0=日报 1=外出申请"`
	ReportID   *uint          `json:"report_id" gorm:"index;comment:关联日报ID"`
	Report     *Report        `json:"report,omitempty" gorm:"foreignKey:ReportID"`
	OutingID   *uint          `json:"outing_id" gorm:"index;comment:关联外出申请ID"`
	Outing     *OutingRequest `json:"outing,omitempty" gorm:"foreignKey:OutingID"`
	Subject    string         `json:"subject" gorm:"size:500;not null;comment:邮件主题"`
	Recipients string         `json:"recipients" gorm:"type:text;not null;comment:收件人列表(逗号分隔)"`
	CcList     string         `json:"cc_list" gorm:"type:text;comment:抄送列表(逗号分隔)"`
	Content    string         `json:"content" gorm:"type:text;comment:邮件正文(渲染后的HTML)"`
	Status     int            `json:"status" gorm:"default:0;index;comment:发送状态 0=待发送 1=发送中 2=成功 3=失败"`
	ErrorMsg   string         `json:"error_msg" gorm:"type:text;comment:错误信息"`
	SendType   int            `json:"send_type" gorm:"default:0;comment:发送方式 0=手动 1=定时自动"`
	RetryCount int            `json:"retry_count" gorm:"default:0;comment:重试次数"`
	SentAt     *time.Time     `json:"sent_at" gorm:"comment:实际发送时间"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// TableName 表名
func (EmailLog) TableName() string {
	return "email_logs"
}

// 邮件发送状态常量
const (
	EmailStatusPending = 0 // 待发送
	EmailStatusSending = 1 // 发送中
	EmailStatusSuccess = 2 // 发送成功
	EmailStatusFailed  = 3 // 发送失败
)

// 邮件发送方式常量
const (
	EmailSendTypeManual = 0 // 手动发送
	EmailSendTypeAuto   = 1 // 定时自动发送
)

// LogTypeText 返回日志类型的中文描述
func (e *EmailLog) LogTypeText() string {
	switch e.LogType {
	case LogTypeReport:
		return "日报"
	case LogTypeOuting:
		return "外出申请"
	default:
		return "未知"
	}
}

// LogTypeBadgeClass 返回日志类型对应的 CSS 徽章样式
func (e *EmailLog) LogTypeBadgeClass() string {
	switch e.LogType {
	case LogTypeReport:
		return "primary"
	case LogTypeOuting:
		return "warning"
	default:
		return "secondary"
	}
}

// StatusText 返回状态的中文描述
func (e *EmailLog) StatusText() string {
	switch e.Status {
	case EmailStatusPending:
		return "待发送"
	case EmailStatusSending:
		return "发送中"
	case EmailStatusSuccess:
		return "发送成功"
	case EmailStatusFailed:
		return "发送失败"
	default:
		return "未知"
	}
}

// SendTypeText 返回发送方式的中文描述
func (e *EmailLog) SendTypeText() string {
	switch e.SendType {
	case EmailSendTypeManual:
		return "手动发送"
	case EmailSendTypeAuto:
		return "定时自动"
	default:
		return "未知"
	}
}

// StatusBadgeClass 返回状态对应的 CSS 徽章样式类
func (e *EmailLog) StatusBadgeClass() string {
	switch e.Status {
	case EmailStatusPending:
		return "badge-warning"
	case EmailStatusSending:
		return "badge-info"
	case EmailStatusSuccess:
		return "badge-success"
	case EmailStatusFailed:
		return "badge-danger"
	default:
		return "badge-secondary"
	}
}
