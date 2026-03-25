package service

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/zerx-lab/daily-report/internal/model"
	"gorm.io/gorm"
)

// OutingService 外出申请业务逻辑服务
type OutingService struct {
	db *gorm.DB
}

// NewOutingService 创建外出申请服务实例
func NewOutingService(db *gorm.DB) *OutingService {
	return &OutingService{db: db}
}

// ==================== 查询方法 ====================

// GetByID 根据 ID 获取外出申请
func (s *OutingService) GetByID(id uint) (*model.OutingRequest, error) {
	var outing model.OutingRequest
	err := s.db.First(&outing, id).Error
	if err != nil {
		return nil, fmt.Errorf("获取外出申请失败(id=%d): %w", id, err)
	}
	return &outing, nil
}

// List 分页查询外出申请列表
func (s *OutingService) List(query *model.OutingListQuery) ([]model.OutingRequest, *model.Pagination, error) {
	query.Normalize()

	tx := s.db.Model(&model.OutingRequest{})

	// 应用筛选条件
	if query.Status != nil {
		tx = tx.Where("status = ?", *query.Status)
	}
	if query.DateFrom != "" {
		tx = tx.Where("out_time >= ?", query.DateFrom)
	}
	if query.DateTo != "" {
		tx = tx.Where("out_time <= ?", query.DateTo+" 23:59:59")
	}
	if query.Keyword != "" {
		keyword := "%" + query.Keyword + "%"
		tx = tx.Where("destination LIKE ? OR reason LIKE ?", keyword, keyword)
	}

	// 查询总数
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, nil, fmt.Errorf("查询外出申请总数失败: %w", err)
	}

	// 计算分页
	totalPage := int(total) / query.PageSize
	if int(total)%query.PageSize > 0 {
		totalPage++
	}
	if totalPage == 0 {
		totalPage = 1
	}

	pagination := &model.Pagination{
		Page:      query.Page,
		PageSize:  query.PageSize,
		Total:     total,
		TotalPage: totalPage,
	}

	// 查询数据（按创建时间降序）
	var outings []model.OutingRequest
	err := tx.Order("created_at DESC").
		Offset(query.Offset()).
		Limit(query.PageSize).
		Find(&outings).Error
	if err != nil {
		return nil, nil, fmt.Errorf("查询外出申请列表失败: %w", err)
	}

	return outings, pagination, nil
}

// ListRecent 获取最近 N 条外出申请
func (s *OutingService) ListRecent(limit int) ([]model.OutingRequest, error) {
	if limit <= 0 {
		limit = 10
	}
	var outings []model.OutingRequest
	err := s.db.Order("created_at DESC").Limit(limit).Find(&outings).Error
	if err != nil {
		return nil, fmt.Errorf("查询最近外出申请失败: %w", err)
	}
	return outings, nil
}

// ==================== 创建与更新 ====================

// Create 创建外出申请
func (s *OutingService) Create(req *model.OutingRequest) (*model.OutingRequest, error) {
	// 校验必填字段
	if strings.TrimSpace(req.Applicant) == "" {
		return nil, fmt.Errorf("创建外出申请失败: 申请人不能为空")
	}
	if strings.TrimSpace(req.Department) == "" {
		return nil, fmt.Errorf("创建外出申请失败: 部门不能为空")
	}
	if req.OutTime.IsZero() {
		return nil, fmt.Errorf("创建外出申请失败: 外出时间不能为空")
	}
	if req.ReturnTime.IsZero() {
		return nil, fmt.Errorf("创建外出申请失败: 返回时间不能为空")
	}
	if strings.TrimSpace(req.Destination) == "" {
		return nil, fmt.Errorf("创建外出申请失败: 外出地点不能为空")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, fmt.Errorf("创建外出申请失败: 外出事由不能为空")
	}

	// 必填字段均不为空，默认设为待发送状态
	req.Status = model.OutingStatusReady

	if err := s.db.Create(req).Error; err != nil {
		return nil, fmt.Errorf("创建外出申请失败: %w", err)
	}

	log.Printf("[外出申请] 创建成功: id=%d, 申请人=%s, 外出地点=%s\n", req.ID, req.Applicant, req.Destination)
	return req, nil
}

// Update 更新外出申请
func (s *OutingService) Update(id uint, req *model.OutingRequest) (*model.OutingRequest, error) {
	existing, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}

	updates := map[string]interface{}{}

	if strings.TrimSpace(req.Applicant) != "" {
		updates["applicant"] = req.Applicant
	}
	if strings.TrimSpace(req.Department) != "" {
		updates["department"] = req.Department
	}
	if !req.OutTime.IsZero() {
		updates["out_time"] = req.OutTime
	}
	if !req.ReturnTime.IsZero() {
		updates["return_time"] = req.ReturnTime
	}
	if strings.TrimSpace(req.Destination) != "" {
		updates["destination"] = req.Destination
	}
	if strings.TrimSpace(req.Reason) != "" {
		updates["reason"] = req.Reason
	}
	// 备注允许清空，始终更新
	updates["remarks"] = req.Remarks

	if len(updates) == 0 {
		return existing, nil
	}

	if err := s.db.Model(existing).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("更新外出申请失败(id=%d): %w", id, err)
	}

	log.Printf("[外出申请] 更新成功: id=%d\n", id)

	// 重新加载
	return s.GetByID(id)
}

// UpdateStatus 更新外出申请状态
func (s *OutingService) UpdateStatus(id uint, status model.OutingStatus) error {
	updates := map[string]interface{}{
		"status": status,
	}
	// 如果是已发送状态，同时更新发送时间
	if status == model.OutingStatusSent {
		now := time.Now()
		updates["sent_at"] = &now
	}

	result := s.db.Model(&model.OutingRequest{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("更新外出申请状态失败(id=%d): %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("外出申请不存在(id=%d)", id)
	}

	log.Printf("[外出申请] 状态更新: id=%d, status=%s\n", id, status)
	return nil
}

// ==================== 删除方法 ====================

// Delete 删除外出申请（软删除）
func (s *OutingService) Delete(id uint) error {
	result := s.db.Delete(&model.OutingRequest{}, id)
	if result.Error != nil {
		return fmt.Errorf("删除外出申请失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("外出申请不存在(id=%d)", id)
	}
	log.Printf("[外出申请] 删除成功: id=%d\n", id)
	return nil
}
