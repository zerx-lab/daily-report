package config

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config 应用全局配置
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	SiYuan   SiYuanConfig   `yaml:"siyuan"`
	SMTP     SMTPConfig     `yaml:"smtp"`
	Schedule ScheduleConfig `yaml:"schedule"`
	Email    EmailConfig    `yaml:"email"`
}

// ServerConfig Gin 服务器配置
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"` // debug / release / test
}

// DatabaseConfig SQLite 数据库配置
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// SiYuanConfig 思源笔记 API 配置
type SiYuanConfig struct {
	BaseURL    string `yaml:"base_url"`
	Token      string `yaml:"token"`
	NotebookID string `yaml:"notebook_id"`
	DocID      string `yaml:"doc_id"`
	AVID       string `yaml:"av_id"`
	BlockID    string `yaml:"block_id"`
	ViewID     string `yaml:"view_id"`
	KeyID      string `yaml:"key_id"`
}

// SMTPConfig 邮件发送配置
type SMTPConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
	FromName string `yaml:"from_name"`
	UseTLS   bool   `yaml:"use_tls"`
}

// ScheduleConfig 定时任务配置
type ScheduleConfig struct {
	AutoCreate   CronJob `yaml:"auto_create"`
	AutoSend     CronJob `yaml:"auto_send"`
	SkipHolidays bool    `yaml:"skip_holidays"`
}

// CronJob 单个定时任务配置
type CronJob struct {
	Enabled bool   `yaml:"enabled"`
	Cron    string `yaml:"cron"`
}

// EmailConfig 邮件收件人配置
type EmailConfig struct {
	Subject    string   `yaml:"subject"`
	Recipients []string `yaml:"recipients"`
	CC         []string `yaml:"cc"`
}

var (
	instance *Config
	once     sync.Once
)

// Load 从指定 YAML 文件加载配置（单例）
func Load(path string) (*Config, error) {
	var loadErr error
	once.Do(func() {
		instance = &Config{}
		loadErr = instance.loadFromFile(path)
	})
	if loadErr != nil {
		// 加载失败时重置单例，允许重试
		once = sync.Once{}
		instance = nil
		return nil, loadErr
	}
	return instance, nil
}

// Get 获取已加载的配置实例
func Get() *Config {
	return instance
}

// loadFromFile 从文件读取并解析 YAML 配置
func (c *Config) loadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 展开环境变量
	expanded := os.ExpandEnv(string(data))

	if err := yaml.Unmarshal([]byte(expanded), c); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	c.setDefaults()

	if err := c.validate(); err != nil {
		return fmt.Errorf("配置校验失败: %w", err)
	}

	return nil
}

// setDefaults 设置默认值
func (c *Config) setDefaults() {
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.Mode == "" {
		c.Server.Mode = "debug"
	}
	if c.Database.Path == "" {
		c.Database.Path = "data/daily_report.db"
	}
	if c.SMTP.Port == 0 {
		c.SMTP.Port = 465
	}
	if c.Email.Subject == "" {
		c.Email.Subject = "工作日报 - {{.Date}}"
	}
	if c.Schedule.AutoCreate.Cron == "" {
		c.Schedule.AutoCreate.Cron = "0 30 8 * * 1-5" // 工作日 08:30
	}
	if c.Schedule.AutoSend.Cron == "" {
		c.Schedule.AutoSend.Cron = "0 0 18 * * 1-5" // 工作日 18:00
	}
}

// validate 校验必填字段
func (c *Config) validate() error {
	if c.SiYuan.BaseURL == "" {
		return fmt.Errorf("siyuan.base_url 不能为空")
	}
	if c.SiYuan.Token == "" {
		return fmt.Errorf("siyuan.token 不能为空")
	}
	if c.SiYuan.AVID == "" {
		return fmt.Errorf("siyuan.av_id 不能为空")
	}
	if c.SiYuan.BlockID == "" {
		return fmt.Errorf("siyuan.block_id 不能为空")
	}
	return nil
}

// Addr 返回服务器监听地址
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
