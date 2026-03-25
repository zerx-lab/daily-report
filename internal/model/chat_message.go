package model

import (
	"log"
	"time"

	"gorm.io/gorm"
)

// ChatMessage AI 对话消息记录（用于实现多轮对话记忆）
type ChatMessage struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	UserID    string         `gorm:"type:varchar(64);not null;index" json:"user_id"` // 用户标识（QQ号或其他）
	Role      string         `gorm:"type:varchar(16);not null" json:"role"`          // 角色：user / assistant
	Content   string         `gorm:"type:text;not null" json:"content"`              // 消息内容
	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ChatMessage) TableName() string { return "chat_messages" }

// SaveChatMessage 保存一条对话消息
func SaveChatMessage(db *gorm.DB, userID, role, content string) error {
	msg := &ChatMessage{
		UserID:  userID,
		Role:    role,
		Content: content,
	}
	return db.Create(msg).Error
}

// GetRecentMessages 获取用户最近 N 条对话消息（按时间升序，用于构建上下文）
func GetRecentMessages(db *gorm.DB, userID string, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	var messages []ChatMessage
	// 子查询：先按时间降序取最近 N 条，再反转为升序
	err := db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&messages).Error
	if err != nil {
		return nil, err
	}
	// 反转为时间升序
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// ClearChatMessages 清除指定用户的所有对话记忆（硬删除）
func ClearChatMessages(db *gorm.DB, userID string) (int64, error) {
	result := db.Unscoped().Where("user_id = ?", userID).Delete(&ChatMessage{})
	return result.RowsAffected, result.Error
}

// ClearAllChatMessages 清除所有用户的对话记忆（硬删除，定时任务使用）
func ClearAllChatMessages(db *gorm.DB) (int64, error) {
	result := db.Unscoped().Where("1 = 1").Delete(&ChatMessage{})
	if result.Error != nil {
		return 0, result.Error
	}
	log.Printf("[对话记忆] 已清除全部对话记忆，共 %d 条\n", result.RowsAffected)
	return result.RowsAffected, nil
}
