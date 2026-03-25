package model

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 全局数据库实例
var DB *gorm.DB

// InitDB 初始化 SQLite 数据库连接并执行自动迁移
func InitDB(dbPath string) error {
	// 确保数据库文件所在目录存在
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建数据库目录失败: %w", err)
		}
	}

	// 配置 GORM 日志
	gormLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)

	// 打开 SQLite 数据库（启用 WAL 模式和忙等待超时）
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON"
	var err error
	DB, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	// 配置连接池（SQLite 单写限制）
	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("获取底层数据库连接失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 执行自动迁移
	if err := autoMigrate(); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	// 初始化默认设置
	if err := InitDefaultSettings(DB); err != nil {
		return fmt.Errorf("初始化默认设置失败: %w", err)
	}

	log.Printf("[数据库] 初始化完成: %s\n", dbPath)
	return nil
}

// autoMigrate 自动迁移所有模型的表结构
func autoMigrate() error {
	return DB.AutoMigrate(
		&Report{},
		&SendLog{},
		&ScheduleTask{},
		&EmailLog{},
		&Setting{},
	)
}

// CloseDB 关闭数据库连接
func CloseDB() error {
	if DB == nil {
		return nil
	}
	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("获取底层数据库连接失败: %w", err)
	}
	log.Println("[数据库] 连接已关闭")
	return sqlDB.Close()
}

// GetDB 获取全局数据库实例（供 service 层使用）
func GetDB() *gorm.DB {
	return DB
}
