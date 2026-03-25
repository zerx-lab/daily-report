package service

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/zerx-lab/daily-report/internal/model"
	"gorm.io/gorm"
)

// ReportService 日报业务逻辑服务
type ReportService struct {
	db *gorm.DB
}

// NewReportService 创建日报服务实例
func NewReportService(db *gorm.DB) *ReportService {
	return &ReportService{db: db}
}

// ==================== 查询方法 ====================

// GetByID 根据 ID 获取日报
func (s *ReportService) GetByID(id uint) (*model.Report, error) {
	var report model.Report
	err := s.db.First(&report, id).Error
	if err != nil {
		return nil, fmt.Errorf("获取日报失败(id=%d): %w", id, err)
	}
	return &report, nil
}

// GetByDate 根据日期获取日报（日期格式: 2026-03-24）
func (s *ReportService) GetByDate(date string) (*model.Report, error) {
	var report model.Report
	err := s.db.Where("date = ?", date).First(&report).Error
	if err != nil {
		return nil, fmt.Errorf("获取日报失败(date=%s): %w", date, err)
	}
	return &report, nil
}

// GetToday 获取今日日报
func (s *ReportService) GetToday() (*model.Report, error) {
	today := s.todayStr()
	return s.GetByDate(today)
}

// List 分页查询日报列表
func (s *ReportService) List(query *model.ReportListQuery) ([]model.Report, *model.Pagination, error) {
	query.Normalize()

	tx := s.db.Model(&model.Report{})

	// 应用筛选条件
	if query.Status != nil {
		tx = tx.Where("status = ?", *query.Status)
	}
	if query.DateFrom != "" {
		tx = tx.Where("date >= ?", query.DateFrom)
	}
	if query.DateTo != "" {
		tx = tx.Where("date <= ?", query.DateTo)
	}
	if query.Keyword != "" {
		keyword := "%" + query.Keyword + "%"
		tx = tx.Where("content LIKE ?", keyword)
	}

	// 查询总数
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, nil, fmt.Errorf("查询日报总数失败: %w", err)
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

	// 查询数据（按日期降序）
	var reports []model.Report
	err := tx.Order("date DESC").
		Offset(query.Offset()).
		Limit(query.PageSize).
		Find(&reports).Error
	if err != nil {
		return nil, nil, fmt.Errorf("查询日报列表失败: %w", err)
	}

	return reports, pagination, nil
}

// ListRecent 获取最近 N 条日报
func (s *ReportService) ListRecent(limit int) ([]model.Report, error) {
	if limit <= 0 {
		limit = 10
	}
	var reports []model.Report
	err := s.db.Order("date DESC").Limit(limit).Find(&reports).Error
	if err != nil {
		return nil, fmt.Errorf("查询最近日报失败: %w", err)
	}
	return reports, nil
}

// ==================== 创建与更新 ====================

// Create 创建日报（指定日期）
func (s *ReportService) Create(date string, content string) (*model.Report, error) {
	// 解析日期获取星期
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("日期格式无效(应为 yyyy-MM-dd): %w", err)
	}

	weekday := weekdayCN(t.Weekday())

	// 检查是否已存在
	var existing model.Report
	result := s.db.Where("date = ?", date).First(&existing)
	if result.Error == nil {
		return &existing, fmt.Errorf("日期 %s 的日报已存在(id=%d)", date, existing.ID)
	}

	// 确定状态
	status := model.ReportStatusDraft
	if strings.TrimSpace(content) != "" && content != "待填写" {
		status = model.ReportStatusReady
	}

	report := &model.Report{
		Date:    date,
		Weekday: weekday,
		Content: content,
		Status:  status,
	}

	if err := s.db.Create(report).Error; err != nil {
		return nil, fmt.Errorf("创建日报失败: %w", err)
	}

	log.Printf("[日报] 创建成功: %s %s\n", date, weekday)
	return report, nil
}

// CreateToday 创建今日日报
func (s *ReportService) CreateToday(content string) (*model.Report, error) {
	today := s.todayStr()
	return s.Create(today, content)
}

// CreateTodayIfNotExist 如果今日日报不存在则创建（定时任务使用）
func (s *ReportService) CreateTodayIfNotExist(defaultContent string) (*model.Report, bool, error) {
	today := s.todayStr()

	// 检查是否已存在
	existing, err := s.GetByDate(today)
	if err == nil {
		// 已存在，返回且 created=false
		return existing, false, nil
	}

	if defaultContent == "" {
		defaultContent = "待填写"
	}

	report, err := s.Create(today, defaultContent)
	if err != nil {
		return nil, false, err
	}
	return report, true, nil
}

// Update 更新日报内容
func (s *ReportService) Update(id uint, content string) (*model.Report, error) {
	report, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}

	updates := map[string]interface{}{
		"content": content,
	}

	// 根据内容更新状态
	if strings.TrimSpace(content) != "" && content != "待填写" {
		// 如果还是草稿状态，自动设为待发送
		if report.Status == model.ReportStatusDraft {
			updates["status"] = model.ReportStatusReady
		}
	} else {
		updates["status"] = model.ReportStatusDraft
	}

	if err := s.db.Model(report).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("更新日报失败: %w", err)
	}

	// 重新加载
	return s.GetByID(id)
}

// UpdateContent 更新日报内容（简化版）
func (s *ReportService) UpdateContent(id uint, content string) error {
	_, err := s.Update(id, content)
	return err
}

// UpdateStatus 更新日报状态
func (s *ReportService) UpdateStatus(id uint, status model.ReportStatus) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if status == model.ReportStatusSent {
		now := time.Now()
		updates["sent_at"] = &now
	}
	return s.db.Model(&model.Report{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateSiyuanID 更新思源笔记关联 ID
func (s *ReportService) UpdateSiyuanID(id uint, siyuanID string) error {
	now := time.Now()
	return s.db.Model(&model.Report{}).Where("id = ?", id).Updates(map[string]interface{}{
		"siyuan_id": siyuanID,
		"synced_at": &now,
	}).Error
}

// ==================== 删除方法 ====================

// Delete 删除日报（软删除）
func (s *ReportService) Delete(id uint) error {
	result := s.db.Delete(&model.Report{}, id)
	if result.Error != nil {
		return fmt.Errorf("删除日报失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("日报不存在(id=%d)", id)
	}
	log.Printf("[日报] 删除成功: id=%d\n", id)
	return nil
}

// ==================== 统计方法 ====================

// DashboardStats 仪表盘统计数据
type DashboardStats struct {
	TotalReports   int64           // 日报总数
	DraftCount     int64           // 草稿数
	ReadyCount     int64           // 待发送数
	SentCount      int64           // 已发送数
	TodayReport    *model.Report   // 今日日报
	TodayExists    bool            // 今日日报是否存在
	RecentReports  []model.Report  // 最近日报
	RecentSendLogs []model.SendLog // 最近发送记录
}

// GetDashboardStats 获取仪表盘统计数据
func (s *ReportService) GetDashboardStats() (*DashboardStats, error) {
	stats := &DashboardStats{}

	// 总数
	s.db.Model(&model.Report{}).Count(&stats.TotalReports)

	// 各状态数量
	s.db.Model(&model.Report{}).Where("status = ?", model.ReportStatusDraft).Count(&stats.DraftCount)
	s.db.Model(&model.Report{}).Where("status = ?", model.ReportStatusReady).Count(&stats.ReadyCount)
	s.db.Model(&model.Report{}).Where("status = ?", model.ReportStatusSent).Count(&stats.SentCount)

	// 今日日报
	today := s.todayStr()
	var todayReport model.Report
	result := s.db.Where("date = ?", today).First(&todayReport)
	if result.Error == nil {
		stats.TodayReport = &todayReport
		stats.TodayExists = true
	}

	// 最近 7 条日报
	s.db.Order("date DESC").Limit(7).Find(&stats.RecentReports)

	// 最近 5 条发送记录
	s.db.Order("sent_at DESC").Limit(5).Find(&stats.RecentSendLogs)

	return stats, nil
}

// GetMonthlyReports 获取某月的所有日报（用于日历视图）
func (s *ReportService) GetMonthlyReports(year, month int) ([]model.Report, error) {
	startDate := fmt.Sprintf("%04d-%02d-01", year, month)
	// 计算月末
	t := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.Local)
	endDate := t.Format("2006-01-02")

	var reports []model.Report
	err := s.db.Where("date >= ? AND date <= ?", startDate, endDate).
		Order("date ASC").
		Find(&reports).Error
	return reports, err
}

// ==================== 发送记录 ====================

// CreateSendLog 创建发送记录
func (s *ReportService) CreateSendLog(log *model.SendLog) error {
	return s.db.Create(log).Error
}

// GetSendLogs 获取日报的发送记录
func (s *ReportService) GetSendLogs(reportID uint) ([]model.SendLog, error) {
	var logs []model.SendLog
	err := s.db.Where("report_id = ?", reportID).
		Order("sent_at DESC").
		Find(&logs).Error
	return logs, err
}

// GetAllSendLogs 获取所有发送记录（分页）
func (s *ReportService) GetAllSendLogs(page, pageSize int) ([]model.SendLog, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	var total int64
	s.db.Model(&model.SendLog{}).Count(&total)

	var logs []model.SendLog
	err := s.db.Order("sent_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&logs).Error
	return logs, total, err
}

// ==================== 辅助方法 ====================

// todayStr 返回今天的日期字符串（yyyy-MM-dd）
func (s *ReportService) todayStr() string {
	return time.Now().Format("2006-01-02")
}

// weekdayCN 将英文星期转换为中文
func weekdayCN(w time.Weekday) string {
	weekdays := map[time.Weekday]string{
		time.Sunday:    "周日",
		time.Monday:    "周一",
		time.Tuesday:   "周二",
		time.Wednesday: "周三",
		time.Thursday:  "周四",
		time.Friday:    "周五",
		time.Saturday:  "周六",
	}
	if name, ok := weekdays[w]; ok {
		return name
	}
	return "未知"
}

// IsWorkday 判断指定日期是否为工作日（简单判断周一到周五）
// 注意：此方法不考虑中国法定节假日调休，完整的节假日判断请使用 HolidayService
func IsWorkday(t time.Time) bool {
	day := t.Weekday()
	return day != time.Saturday && day != time.Sunday
}

// IsTodayWorkday 判断今天是否为工作日
func IsTodayWorkday() bool {
	return IsWorkday(time.Now())
}

// FormatDateCN 格式化日期为中文格式（如：2026年3月24日 周一）
func FormatDateCN(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	weekday := weekdayCN(t.Weekday())
	return fmt.Sprintf("%d年%d月%d日 %s", t.Year(), t.Month(), t.Day(), weekday)
}
