package model

import (
	"time"

	"gorm.io/gorm"
)

// Setting 系统设置模型（键值对存储）
type Setting struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Category  string         `gorm:"size:64;not null;index:idx_category_key" json:"category"` // 分类：smtp / siyuan / schedule / general
	Key       string         `gorm:"size:128;not null;index:idx_category_key" json:"key"`     // 设置键名
	Value     string         `gorm:"type:text;not null;default:''" json:"value"`              // 设置值
	Encrypted bool           `gorm:"default:false" json:"encrypted"`                          // 是否加密存储（如密码）
	Remark    string         `gorm:"size:256;default:''" json:"remark"`                       // 备注说明
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 自定义表名
func (Setting) TableName() string {
	return "settings"
}

// ==================== 设置分类常量 ====================

const (
	CategorySMTP     = "smtp"     // SMTP 邮件配置
	CategorySiyuan   = "siyuan"   // 思源笔记配置
	CategorySchedule = "schedule" // 定时任务配置
	CategoryGeneral  = "general"  // 通用设置
	CategoryEmail    = "email"    // 邮件内容配置
	CategoryOuting   = "outing"   // 外出申请配置
	CategoryAI       = "ai"       // AI 大模型配置
	CategoryBot      = "bot"      // QQ 机器人配置
)

// ==================== 设置键名常量 ====================

// SMTP 相关
const (
	KeySMTPHost     = "host"
	KeySMTPPort     = "port"
	KeySMTPUsername = "username"
	KeySMTPPassword = "password"
	KeySMTPFrom     = "from"      // 发件人显示名称
	KeySMTPFromAddr = "from_addr" // 发件人地址
	KeySMTPUseTLS   = "use_tls"
)

// 思源笔记相关
const (
	KeySiyuanBaseURL   = "base_url"
	KeySiyuanAPIToken  = "api_token"
	KeySiyuanAvID      = "av_id"          // 属性视图 ID
	KeySiyuanBlockID   = "block_id"       // 数据库块 ID
	KeySiyuanKeyID     = "key_id"         // 日期列 Key ID（主键）
	KeySiyuanContentID = "key_content_id" // 工作内容列 Key ID
	KeySiyuanNotebook  = "notebook_id"    // 笔记本 ID
)

// 定时任务相关
const (
	KeyScheduleCreateEnabled = "create_enabled" // 是否启用自动创建
	KeyScheduleCreateCron    = "create_cron"    // 创建 Cron 表达式
	KeyScheduleSendEnabled   = "send_enabled"   // 是否启用自动发送
	KeyScheduleSendCron      = "send_cron"      // 发送 Cron 表达式
	KeyScheduleSyncEnabled   = "sync_enabled"   // 是否启用自动同步思源
	KeyScheduleSyncCron      = "sync_cron"      // 同步思源 Cron 表达式
	KeyScheduleSkipHoliday   = "skip_holiday"   // 是否跳过节假日
)

// 邮件内容相关
const (
	KeyEmailRecipients = "recipients" // 收件人列表（逗号分隔）
	KeyEmailCc         = "cc"         // 抄送列表（逗号分隔）
	KeyEmailSubject    = "subject"    // 邮件主题模板
)

// 通用设置
const (
	KeyGeneralAppName  = "app_name" // 应用名称
	KeyGeneralTimezone = "timezone" // 时区
)

// 外出申请相关
const (
	KeyOutingRecipients = "recipients" // 外出申请收件人列表（逗号分隔）
	KeyOutingCc         = "cc"         // 外出申请抄送列表（逗号分隔）
	KeyOutingSubject    = "subject"    // 外出申请邮件主题模板
	KeyOutingApplicant  = "applicant"  // 固定申请人姓名
	KeyOutingDepartment = "department" // 固定部门名称

	// 外出申请思源笔记 AV 配置
	KeyOutingAvID           = "av_id"           // 外出申请属性视图 ID
	KeyOutingBlockID        = "block_id"        // 外出申请数据库块 ID
	KeyOutingKeyOutTime     = "key_out_time"    // 外出时间列 Key ID
	KeyOutingKeyReturnTime  = "key_return_time" // 返回时间列 Key ID
	KeyOutingKeyDestination = "key_destination" // 外出地点列 Key ID
	KeyOutingKeyReason      = "key_reason"      // 外出事由列 Key ID
	KeyOutingKeyRemarks     = "key_remarks"     // 备注说明列 Key ID
)

// AI 大模型相关
const (
	KeyAIBaseURL      = "base_url"      // API 基础地址（兼容 OpenAI 接口）
	KeyAIApiKey       = "api_key"       // API Key
	KeyAIModel        = "model"         // 模型名称（如 deepseek-chat）
	KeyAIMaxTokens    = "max_tokens"    // 最大生成 token 数
	KeyAITemperature  = "temperature"   // 采样温度
	KeyAISystemPrompt = "system_prompt" // 自定义系统提示词（可选，留空使用内置）
)

// QQ 机器人相关
const (
	KeyBotEnabled      = "enabled"       // 是否启用机器人
	KeyBotAPIURL       = "api_url"       // NapCat OneBot HTTP API 地址
	KeyBotAccessToken  = "access_token"  // OneBot access_token 鉴权
	KeyBotAllowedUsers = "allowed_users" // 允许的 QQ 号白名单（逗号分隔）
	KeyBotWsEnabled    = "ws_enabled"    // 是否启用反向 WebSocket（NapCat 连到日报系统）
	KeyBotWsHost       = "ws_host"       // 反向 WebSocket 监听地址
	KeyBotWsPort       = "ws_port"       // 反向 WebSocket 监听端口
	KeyBotFwsEnabled   = "fws_enabled"   // 是否启用正向 WebSocket（日报系统连到 NapCat）
	KeyBotFwsURL       = "fws_url"       // NapCat WebSocket 服务器地址，如 ws://20.40.96.52:3001
	KeyBotFwsToken     = "fws_token"     // 正向 WebSocket 的 access_token（可与 HTTP API 不同）
)

// ==================== 数据库操作方法 ====================

// GetSetting 获取单个设置项
func GetSetting(db *gorm.DB, category, key string) (*Setting, error) {
	var setting Setting
	err := db.Where("category = ? AND `key` = ?", category, key).First(&setting).Error
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

// GetSettingValue 获取设置值，不存在则返回默认值
func GetSettingValue(db *gorm.DB, category, key, defaultValue string) string {
	setting, err := GetSetting(db, category, key)
	if err != nil || setting.Value == "" {
		return defaultValue
	}
	return setting.Value
}

// GetSettingsByCategory 获取某个分类的所有设置
func GetSettingsByCategory(db *gorm.DB, category string) ([]Setting, error) {
	var settings []Setting
	err := db.Where("category = ?", category).Find(&settings).Error
	return settings, err
}

// GetSettingsMapByCategory 获取某个分类的所有设置（返回 map）
func GetSettingsMapByCategory(db *gorm.DB, category string) (map[string]string, error) {
	settings, err := GetSettingsByCategory(db, category)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(settings))
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

// UpsertSetting 创建或更新设置项
func UpsertSetting(db *gorm.DB, category, key, value string) error {
	var setting Setting
	result := db.Where("category = ? AND `key` = ?", category, key).First(&setting)
	if result.Error != nil {
		// 不存在则创建
		setting = Setting{
			Category: category,
			Key:      key,
			Value:    value,
		}
		return db.Create(&setting).Error
	}
	// 存在则更新
	return db.Model(&setting).Update("value", value).Error
}

// UpsertSettingWithRemark 创建或更新设置项（带备注）
func UpsertSettingWithRemark(db *gorm.DB, category, key, value, remark string) error {
	var setting Setting
	result := db.Where("category = ? AND `key` = ?", category, key).First(&setting)
	if result.Error != nil {
		setting = Setting{
			Category: category,
			Key:      key,
			Value:    value,
			Remark:   remark,
		}
		return db.Create(&setting).Error
	}
	return db.Model(&setting).Updates(map[string]interface{}{
		"value":  value,
		"remark": remark,
	}).Error
}

// BatchUpsertSettings 批量创建或更新设置项
func BatchUpsertSettings(db *gorm.DB, category string, kvPairs map[string]string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for k, v := range kvPairs {
			if err := UpsertSetting(tx, category, k, v); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteSetting 删除设置项
func DeleteSetting(db *gorm.DB, category, key string) error {
	return db.Where("category = ? AND `key` = ?", category, key).Delete(&Setting{}).Error
}

// DeleteSettingsByCategory 删除某个分类的所有设置
func DeleteSettingsByCategory(db *gorm.DB, category string) error {
	return db.Where("category = ?", category).Delete(&Setting{}).Error
}

// GetAllSettings 获取所有设置（按分类分组）
func GetAllSettings(db *gorm.DB) (map[string]map[string]string, error) {
	var settings []Setting
	err := db.Find(&settings).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]string)
	for _, s := range settings {
		if _, ok := result[s.Category]; !ok {
			result[s.Category] = make(map[string]string)
		}
		result[s.Category][s.Key] = s.Value
	}
	return result, nil
}

// ==================== 便捷方法 ====================

// SMTPSettings SMTP 设置结构
type SMTPSettings struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	FromAddr string
	UseTLS   string
}

// GetSMTPSettings 获取 SMTP 配置
func GetSMTPSettings(db *gorm.DB) (*SMTPSettings, error) {
	m, err := GetSettingsMapByCategory(db, CategorySMTP)
	if err != nil {
		return nil, err
	}
	return &SMTPSettings{
		Host:     m[KeySMTPHost],
		Port:     m[KeySMTPPort],
		Username: m[KeySMTPUsername],
		Password: m[KeySMTPPassword],
		From:     m[KeySMTPFrom],
		FromAddr: m[KeySMTPFromAddr],
		UseTLS:   m[KeySMTPUseTLS],
	}, nil
}

// SiyuanSettings 思源笔记设置结构
type SiyuanSettings struct {
	BaseURL      string
	APIToken     string
	AvID         string
	BlockID      string
	KeyID        string // 日期列 Key ID（主键）
	ContentKeyID string // 工作内容列 Key ID
	NotebookID   string
}

// GetSiyuanSettings 获取思源笔记配置
func GetSiyuanSettings(db *gorm.DB) (*SiyuanSettings, error) {
	m, err := GetSettingsMapByCategory(db, CategorySiyuan)
	if err != nil {
		return nil, err
	}
	return &SiyuanSettings{
		BaseURL:      m[KeySiyuanBaseURL],
		APIToken:     m[KeySiyuanAPIToken],
		AvID:         m[KeySiyuanAvID],
		BlockID:      m[KeySiyuanBlockID],
		KeyID:        m[KeySiyuanKeyID],
		ContentKeyID: m[KeySiyuanContentID],
		NotebookID:   m[KeySiyuanNotebook],
	}, nil
}

// InitDefaultSettings 初始化默认设置值（仅在设置不存在时写入）
func InitDefaultSettings(db *gorm.DB) error {
	defaults := []Setting{
		// SMTP 默认值
		{Category: CategorySMTP, Key: KeySMTPHost, Value: "", Remark: "SMTP 服务器地址"},
		{Category: CategorySMTP, Key: KeySMTPPort, Value: "587", Remark: "SMTP 端口"},
		{Category: CategorySMTP, Key: KeySMTPUsername, Value: "", Remark: "SMTP 用户名"},
		{Category: CategorySMTP, Key: KeySMTPPassword, Value: "", Remark: "SMTP 密码", Encrypted: true},
		{Category: CategorySMTP, Key: KeySMTPFrom, Value: "日报系统", Remark: "发件人显示名称"},
		{Category: CategorySMTP, Key: KeySMTPFromAddr, Value: "", Remark: "发件人邮箱地址"},
		{Category: CategorySMTP, Key: KeySMTPUseTLS, Value: "true", Remark: "是否使用 TLS"},

		// 思源笔记默认值
		{Category: CategorySiyuan, Key: KeySiyuanBaseURL, Value: "https://note.zerx.dev", Remark: "思源笔记地址"},
		{Category: CategorySiyuan, Key: KeySiyuanAPIToken, Value: "", Remark: "API Token"},
		{Category: CategorySiyuan, Key: KeySiyuanAvID, Value: "20260324161653-vrznito", Remark: "属性视图 ID"},
		{Category: CategorySiyuan, Key: KeySiyuanBlockID, Value: "20260324161646-e2zu02m", Remark: "数据库块 ID"},
		{Category: CategorySiyuan, Key: KeySiyuanKeyID, Value: "20260324161653-iu9gqhb", Remark: "日期列 Key ID（主键）"},
		{Category: CategorySiyuan, Key: KeySiyuanContentID, Value: "20260324161653-rkbx4tv", Remark: "工作内容列 Key ID"},
		{Category: CategorySiyuan, Key: KeySiyuanNotebook, Value: "20260320105739-1y06ufo", Remark: "笔记本 ID"},

		// 定时任务默认值
		{Category: CategorySchedule, Key: KeyScheduleCreateEnabled, Value: "false", Remark: "启用自动创建日报"},
		{Category: CategorySchedule, Key: KeyScheduleCreateCron, Value: "0 30 8 * * 1-5", Remark: "自动创建 Cron（默认工作日 8:30）"},
		{Category: CategorySchedule, Key: KeyScheduleSendEnabled, Value: "false", Remark: "启用自动发送日报"},
		{Category: CategorySchedule, Key: KeyScheduleSendCron, Value: "0 0 18 * * 1-5", Remark: "自动发送 Cron（默认工作日 18:00）"},
		{Category: CategorySchedule, Key: KeyScheduleSyncEnabled, Value: "false", Remark: "启用自动同步思源笔记到本地"},
		{Category: CategorySchedule, Key: KeyScheduleSyncCron, Value: "0 50 21 * * *", Remark: "自动同步思源 Cron（默认每天 21:50）"},
		{Category: CategorySchedule, Key: KeyScheduleSkipHoliday, Value: "true", Remark: "跳过法定节假日"},

		// 邮件内容默认值
		{Category: CategoryEmail, Key: KeyEmailRecipients, Value: "", Remark: "收件人（逗号分隔）"},
		{Category: CategoryEmail, Key: KeyEmailCc, Value: "", Remark: "抄送（逗号分隔）"},
		{Category: CategoryEmail, Key: KeyEmailSubject, Value: "{{.Date}} 工作日报 - {{.Author}}", Remark: "邮件主题模板"},

		// 通用设置默认值
		{Category: CategoryGeneral, Key: KeyGeneralAppName, Value: "日报助手", Remark: "应用名称"},
		{Category: CategoryGeneral, Key: KeyGeneralTimezone, Value: "Asia/Shanghai", Remark: "时区"},

		// AI 大模型默认值
		{Category: CategoryAI, Key: KeyAIBaseURL, Value: "https://api.deepseek.com/v1", Remark: "OpenAI 兼容 API 地址"},
		{Category: CategoryAI, Key: KeyAIApiKey, Value: "", Remark: "API Key", Encrypted: true},
		{Category: CategoryAI, Key: KeyAIModel, Value: "deepseek-chat", Remark: "模型名称"},
		{Category: CategoryAI, Key: KeyAIMaxTokens, Value: "2048", Remark: "最大生成 token 数"},
		{Category: CategoryAI, Key: KeyAITemperature, Value: "0.7", Remark: "采样温度（0-2）"},
		{Category: CategoryAI, Key: KeyAISystemPrompt, Value: "", Remark: "自定义系统提示词（留空使用内置）"},

		// QQ 机器人默认值
		{Category: CategoryBot, Key: KeyBotEnabled, Value: "false", Remark: "启用 QQ 机器人"},
		{Category: CategoryBot, Key: KeyBotAPIURL, Value: "http://20.40.96.52:6099", Remark: "NapCat OneBot HTTP API 地址"},
		{Category: CategoryBot, Key: KeyBotAccessToken, Value: "", Remark: "OneBot access_token", Encrypted: true},
		{Category: CategoryBot, Key: KeyBotAllowedUsers, Value: "", Remark: "允许的 QQ 号（逗号分隔）"},
		{Category: CategoryBot, Key: KeyBotWsEnabled, Value: "false", Remark: "启用反向 WebSocket 接收消息"},
		{Category: CategoryBot, Key: KeyBotWsHost, Value: "0.0.0.0", Remark: "反向 WebSocket 监听地址"},
		{Category: CategoryBot, Key: KeyBotWsPort, Value: "8788", Remark: "反向 WebSocket 监听端口"},
		{Category: CategoryBot, Key: KeyBotFwsEnabled, Value: "false", Remark: "启用正向 WebSocket（主动连接 NapCat）"},
		{Category: CategoryBot, Key: KeyBotFwsURL, Value: "", Remark: "NapCat WebSocket 服务器地址"},
		{Category: CategoryBot, Key: KeyBotFwsToken, Value: "", Remark: "正向 WebSocket access_token", Encrypted: true},

		// 外出申请默认值
		{Category: CategoryOuting, Key: KeyOutingRecipients, Value: "", Remark: "外出申请收件人（逗号分隔）"},
		{Category: CategoryOuting, Key: KeyOutingCc, Value: "", Remark: "外出申请抄送（逗号分隔）"},
		{Category: CategoryOuting, Key: KeyOutingSubject, Value: "外出申请 - {{.Applicant}} {{.OutDate}}", Remark: "外出申请邮件主题模板"},
		{Category: CategoryOuting, Key: KeyOutingApplicant, Value: "", Remark: "固定申请人姓名"},
		{Category: CategoryOuting, Key: KeyOutingDepartment, Value: "", Remark: "固定部门名称"},
		{Category: CategoryOuting, Key: KeyOutingAvID, Value: "", Remark: "外出申请属性视图 ID"},
		{Category: CategoryOuting, Key: KeyOutingBlockID, Value: "", Remark: "外出申请数据库块 ID"},
		{Category: CategoryOuting, Key: KeyOutingKeyOutTime, Value: "", Remark: "外出时间列 Key ID"},
		{Category: CategoryOuting, Key: KeyOutingKeyReturnTime, Value: "", Remark: "返回时间列 Key ID"},
		{Category: CategoryOuting, Key: KeyOutingKeyDestination, Value: "", Remark: "外出地点列 Key ID"},
		{Category: CategoryOuting, Key: KeyOutingKeyReason, Value: "", Remark: "外出事由列 Key ID"},
		{Category: CategoryOuting, Key: KeyOutingKeyRemarks, Value: "", Remark: "备注说明列 Key ID"},
	}

	for _, d := range defaults {
		var count int64
		db.Model(&Setting{}).Where("category = ? AND `key` = ?", d.Category, d.Key).Count(&count)
		if count == 0 {
			if err := db.Create(&d).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
