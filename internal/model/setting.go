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
	KeySiyuanBaseURL  = "base_url"
	KeySiyuanAPIToken = "api_token"
	KeySiyuanAvID     = "av_id"       // 属性视图 ID
	KeySiyuanBlockID  = "block_id"    // 数据库块 ID
	KeySiyuanKeyID    = "key_id"      // 主键列 ID
	KeySiyuanNotebook = "notebook_id" // 笔记本 ID
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
	BaseURL    string
	APIToken   string
	AvID       string
	BlockID    string
	KeyID      string
	NotebookID string
}

// GetSiyuanSettings 获取思源笔记配置
func GetSiyuanSettings(db *gorm.DB) (*SiyuanSettings, error) {
	m, err := GetSettingsMapByCategory(db, CategorySiyuan)
	if err != nil {
		return nil, err
	}
	return &SiyuanSettings{
		BaseURL:    m[KeySiyuanBaseURL],
		APIToken:   m[KeySiyuanAPIToken],
		AvID:       m[KeySiyuanAvID],
		BlockID:    m[KeySiyuanBlockID],
		KeyID:      m[KeySiyuanKeyID],
		NotebookID: m[KeySiyuanNotebook],
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
		{Category: CategorySiyuan, Key: KeySiyuanKeyID, Value: "20260324161653-rkbx4tv", Remark: "主键列 Key ID"},
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
