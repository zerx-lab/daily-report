package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/zerx-lab/daily-report/internal/model"
	"gorm.io/gorm"
)

// SiyuanService 思源笔记 API 交互服务
type SiyuanService struct {
	db         *gorm.DB
	httpClient *http.Client
}

// NewSiyuanService 创建思源笔记服务实例
func NewSiyuanService(db *gorm.DB) *SiyuanService {
	return &SiyuanService{
		db: db,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ==================== 内部数据结构 ====================

// siyuanRequest 通用请求体
type siyuanRequest struct {
	url  string
	body interface{}
}

// siyuanResponse 通用响应体
type siyuanResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// AddBlocksRequest 添加数据库行请求
type AddBlocksRequest struct {
	AVID       string           `json:"avID"`
	BlockID    string           `json:"blockID"`
	Srcs       []AddBlockSource `json:"srcs"`
	PreviousID string           `json:"previousID"` // 空字符串 = 插入顶部
}

// AddBlockSource 新增行数据源
type AddBlockSource struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	IsDetached bool   `json:"isDetached"`
}

// AppendDetachedBlocksRequest 底部追加行请求
type AppendDetachedBlocksRequest struct {
	AVID         string              `json:"avID"`
	BlocksValues [][]BlockValueEntry `json:"blocksValues"`
}

// BlockValueEntry 单个列值
type BlockValueEntry struct {
	KeyID      string      `json:"keyID"`
	Type       string      `json:"type"`
	IsDetached bool        `json:"isDetached"`
	Block      *BlockValue `json:"block,omitempty"`
}

// BlockValue 块内容值
type BlockValue struct {
	Content string `json:"content"`
}

// RenderAVRequest 渲染属性视图请求
type RenderAVRequest struct {
	ID       string `json:"id"`
	ViewID   string `json:"viewID"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

// RenderAVResponse 渲染属性视图响应
type RenderAVResponse struct {
	View AVView `json:"view"`
}

// AVView 属性视图
type AVView struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Rows     []AVRow `json:"rows"`
	RowCount int     `json:"rowCount"`
	PageSize int     `json:"pageSize"`
}

// AVRow 属性视图行
type AVRow struct {
	ID    string   `json:"id"`
	Cells []AVCell `json:"cells"`
}

// AVCell 属性视图单元格
type AVCell struct {
	ID        string   `json:"id"`
	KeyID     string   `json:"keyID"`
	ValueType string   `json:"valueType"`
	Value     *AVValue `json:"value"`
}

// AVValue 单元格值（多态）
type AVValue struct {
	ID      string          `json:"id"`
	KeyID   string          `json:"keyID"`
	BlockID string          `json:"blockID"`
	Type    string          `json:"type"`
	Block   *AVBlockValue   `json:"block,omitempty"`
	Created *AVCreatedValue `json:"created,omitempty"` // 创建时间（思源返回对象）
	Text    *AVTextValue    `json:"text,omitempty"`
}

// AVCreatedValue 创建时间类型值（思源 API 返回对象而非纯数字）
type AVCreatedValue struct {
	Content  int64 `json:"content"`  // 毫秒时间戳
	Content2 int64 `json:"content2"` // 毫秒时间戳（备用）
}

// AVBlockValue 块类型值
type AVBlockValue struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// AVTextValue 文本类型值
type AVTextValue struct {
	Content string `json:"content"`
}

// SetBlockAttrRequest 修改单元格请求
type SetBlockAttrRequest struct {
	AVID   string               `json:"avID"`
	KeyID  string               `json:"keyID"`
	ItemID string               `json:"rowID"`
	Value  SetBlockAttrReqValue `json:"value"`
}

// SetBlockAttrReqValue 修改单元格请求值
type SetBlockAttrReqValue struct {
	Block *BlockValue `json:"block,omitempty"`
}

// RemoveBlocksRequest 删除行请求
type RemoveBlocksRequest struct {
	AVID   string   `json:"avID"`
	SrcIDs []string `json:"srcIDs"`
}

// ==================== 配置获取 ====================

// getSiyuanConfig 从数据库设置中获取思源笔记配置
func (s *SiyuanService) getSiyuanConfig() (*model.SiyuanSettings, error) {
	settings, err := model.GetSiyuanSettings(s.db)
	if err != nil {
		return nil, fmt.Errorf("获取思源笔记配置失败: %w", err)
	}
	if settings.BaseURL == "" {
		return nil, fmt.Errorf("思源笔记地址(base_url)未配置")
	}
	if settings.APIToken == "" {
		return nil, fmt.Errorf("思源笔记 API Token 未配置")
	}
	return settings, nil
}

// ==================== HTTP 请求基础方法 ====================

// doRequest 发送 HTTP 请求到思源笔记 API
func (s *SiyuanService) doRequest(apiPath string, body interface{}) (*siyuanResponse, error) {
	cfg, err := s.getSiyuanConfig()
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(cfg.BaseURL, "/") + apiPath

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	log.Printf("[思源API] POST %s => %s\n", apiPath, string(jsonBody))

	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+cfg.APIToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求思源笔记失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("思源笔记返回异常状态码 %d: %s", resp.StatusCode, string(respBody))
	}

	var result siyuanResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w (原始响应: %s)", err, string(respBody))
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("思源笔记 API 返回错误 [code=%d]: %s", result.Code, result.Msg)
	}

	return &result, nil
}

// ==================== 核心 API 方法 ====================

// Ping 测试思源笔记连接是否正常
func (s *SiyuanService) Ping() (string, error) {
	resp, err := s.doRequest("/api/system/version", map[string]interface{}{})
	if err != nil {
		return "", err
	}
	// data 直接是版本号字符串
	var version string
	if err := json.Unmarshal(resp.Data, &version); err != nil {
		return "", fmt.Errorf("解析版本信息失败: %w", err)
	}
	return version, nil
}

// GetCurrentTime 获取思源笔记服务器当前时间
func (s *SiyuanService) GetCurrentTime() (int64, error) {
	resp, err := s.doRequest("/api/system/currentTime", map[string]interface{}{})
	if err != nil {
		return 0, err
	}
	var ts int64
	if err := json.Unmarshal(resp.Data, &ts); err != nil {
		return 0, fmt.Errorf("解析时间失败: %w", err)
	}
	return ts, nil
}

// CreateReportEntry 在数据库顶部新建一行日报记录
// content: 日报主键列的内容文本
// 返回: 新创建行的 ID（由思源自动生成，需从后续渲染中获取）
func (s *SiyuanService) CreateReportEntry(content string) error {
	cfg, err := s.getSiyuanConfig()
	if err != nil {
		return err
	}

	reqBody := AddBlocksRequest{
		AVID:    cfg.AvID,
		BlockID: cfg.BlockID,
		Srcs: []AddBlockSource{
			{
				ID:         "", // 留空，系统自动生成
				Content:    content,
				IsDetached: true, // 独立行，不绑定文档块
			},
		},
		PreviousID: "", // 空字符串 = 插入到顶部
	}

	_, err = s.doRequest("/api/av/addAttributeViewBlocks", reqBody)
	if err != nil {
		return fmt.Errorf("创建日报条目失败: %w", err)
	}

	log.Printf("[思源API] 成功在数据库顶部创建日报条目: %s\n", content)
	return nil
}

// AppendReportEntry 在数据库底部追加一行日报记录
func (s *SiyuanService) AppendReportEntry(content string) error {
	cfg, err := s.getSiyuanConfig()
	if err != nil {
		return err
	}

	reqBody := AppendDetachedBlocksRequest{
		AVID: cfg.AvID,
		BlocksValues: [][]BlockValueEntry{
			{
				{
					KeyID:      cfg.KeyID,
					Type:       "block",
					IsDetached: true,
					Block:      &BlockValue{Content: content},
				},
			},
		},
	}

	_, err = s.doRequest("/api/av/appendAttributeViewDetachedBlocksWithValues", reqBody)
	if err != nil {
		return fmt.Errorf("追加日报条目失败: %w", err)
	}

	log.Printf("[思源API] 成功在数据库底部追加日报条目: %s\n", content)
	return nil
}

// UpdateReportEntry 修改已有行的单元格内容
// rowID: 行 ID（从渲染结果中获取）
// content: 新的内容文本
func (s *SiyuanService) UpdateReportEntry(rowID, content string) error {
	cfg, err := s.getSiyuanConfig()
	if err != nil {
		return err
	}

	reqBody := SetBlockAttrRequest{
		AVID:   cfg.AvID,
		KeyID:  cfg.KeyID,
		ItemID: rowID,
		Value: SetBlockAttrReqValue{
			Block: &BlockValue{Content: content},
		},
	}

	_, err = s.doRequest("/api/av/setAttributeViewBlockAttr", reqBody)
	if err != nil {
		return fmt.Errorf("更新日报条目失败(rowID=%s): %w", rowID, err)
	}

	log.Printf("[思源API] 成功更新日报条目: rowID=%s\n", rowID)
	return nil
}

// DeleteReportEntries 删除数据库中的行
func (s *SiyuanService) DeleteReportEntries(rowIDs []string) error {
	if len(rowIDs) == 0 {
		return nil
	}

	cfg, err := s.getSiyuanConfig()
	if err != nil {
		return err
	}

	reqBody := RemoveBlocksRequest{
		AVID:   cfg.AvID,
		SrcIDs: rowIDs,
	}

	_, err = s.doRequest("/api/av/removeAttributeViewBlocks", reqBody)
	if err != nil {
		return fmt.Errorf("删除日报条目失败: %w", err)
	}

	log.Printf("[思源API] 成功删除 %d 条日报条目\n", len(rowIDs))
	return nil
}

// ==================== 数据查询方法 ====================

// SiyuanReportRow 思源笔记中的日报行数据
type SiyuanReportRow struct {
	RowID     string    // 行 ID
	Content   string    // 工作内容
	CreatedAt time.Time // 创建时间
}

// FetchAllReports 获取数据库中的所有日报数据
// page: 页码（从1开始）
// pageSize: 每页条数
func (s *SiyuanService) FetchAllReports(page, pageSize int) ([]SiyuanReportRow, int, error) {
	cfg, err := s.getSiyuanConfig()
	if err != nil {
		return nil, 0, err
	}

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	reqBody := RenderAVRequest{
		ID:       cfg.AvID,
		ViewID:   "",
		Page:     page,
		PageSize: pageSize,
	}

	resp, err := s.doRequest("/api/av/renderAttributeView", reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("获取日报数据失败: %w", err)
	}

	var renderResp RenderAVResponse
	if err := json.Unmarshal(resp.Data, &renderResp); err != nil {
		return nil, 0, fmt.Errorf("解析渲染结果失败: %w", err)
	}

	rows := make([]SiyuanReportRow, 0, len(renderResp.View.Rows))
	for _, row := range renderResp.View.Rows {
		item := SiyuanReportRow{
			RowID: row.ID,
		}
		for _, cell := range row.Cells {
			if cell.Value == nil {
				continue
			}
			switch {
			case cell.Value.Block != nil:
				item.Content = cell.Value.Block.Content
			case cell.Value.Created != nil:
				// 毫秒时间戳转时间
				ts := cell.Value.Created.Content
				item.CreatedAt = time.UnixMilli(ts)
			}
		}
		rows = append(rows, item)
	}

	return rows, renderResp.View.RowCount, nil
}

// FetchTodayReport 获取今天的日报数据
func (s *SiyuanService) FetchTodayReport() (*SiyuanReportRow, error) {
	rows, _, err := s.FetchAllReports(1, 50)
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")
	today := time.Now().In(loc).Format("2006-01-02")

	for _, row := range rows {
		rowDate := row.CreatedAt.In(loc).Format("2006-01-02")
		if rowDate == today {
			return &row, nil
		}
	}

	return nil, nil // 今天没有日报
}

// FetchReportByDate 获取指定日期的日报数据
func (s *SiyuanService) FetchReportByDate(date string) (*SiyuanReportRow, error) {
	rows, _, err := s.FetchAllReports(1, 100)
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")

	for _, row := range rows {
		rowDate := row.CreatedAt.In(loc).Format("2006-01-02")
		if rowDate == date {
			return &row, nil
		}
	}

	return nil, nil
}

// ==================== 同步方法 ====================

// SyncReportsToLocal 将思源笔记中的日报数据同步到本地 SQLite
func (s *SiyuanService) SyncReportsToLocal() (int, int, error) {
	rows, _, err := s.FetchAllReports(1, 200)
	if err != nil {
		return 0, 0, fmt.Errorf("从思源笔记获取数据失败: %w", err)
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")
	created, updated := 0, 0

	for _, row := range rows {
		date := row.CreatedAt.In(loc).Format("2006-01-02")
		weekday := weekdayChinese(row.CreatedAt.In(loc).Weekday())

		var report model.Report
		result := s.db.Where("date = ?", date).First(&report)

		now := time.Now()

		if result.Error != nil {
			// 本地不存在，创建新记录
			report = model.Report{
				Date:     date,
				Weekday:  weekday,
				Content:  row.Content,
				SiyuanID: row.RowID,
				SyncedAt: &now,
			}
			// 有内容的标记为已填写
			if strings.TrimSpace(row.Content) != "" && row.Content != "待填写" {
				report.Status = model.ReportStatusReady
			}
			if err := s.db.Create(&report).Error; err != nil {
				log.Printf("[同步] 创建日报记录失败(date=%s): %v\n", date, err)
				continue
			}
			created++
		} else {
			// 本地已存在，更新思源数据
			updates := map[string]interface{}{
				"siyuan_id": row.RowID,
				"synced_at": now,
			}
			// 仅在内容有变化时更新
			if row.Content != report.Content && strings.TrimSpace(row.Content) != "" {
				updates["content"] = row.Content
				if report.Status == model.ReportStatusDraft {
					updates["status"] = model.ReportStatusReady
				}
			}
			if err := s.db.Model(&report).Updates(updates).Error; err != nil {
				log.Printf("[同步] 更新日报记录失败(date=%s): %v\n", date, err)
				continue
			}
			updated++
		}
	}

	log.Printf("[同步] 思源笔记同步完成: 新建 %d 条, 更新 %d 条\n", created, updated)
	return created, updated, nil
}

// SyncLocalToSiyuan 将本地日报内容同步回思源笔记
func (s *SiyuanService) SyncLocalToSiyuan(reportID uint) error {
	var report model.Report
	if err := s.db.First(&report, reportID).Error; err != nil {
		return fmt.Errorf("查询日报失败: %w", err)
	}

	if report.SiyuanID == "" {
		// 本地日报没有对应的思源记录，先创建
		if err := s.CreateReportEntry(report.Content); err != nil {
			return fmt.Errorf("在思源笔记创建条目失败: %w", err)
		}
		// 创建后尝试获取对应行的 ID
		row, err := s.FetchReportByDate(report.Date)
		if err == nil && row != nil {
			now := time.Now()
			s.db.Model(&report).Updates(map[string]interface{}{
				"siyuan_id": row.RowID,
				"synced_at": &now,
			})
		}
		return nil
	}

	// 有思源记录 ID，直接更新内容
	if err := s.UpdateReportEntry(report.SiyuanID, report.Content); err != nil {
		return fmt.Errorf("更新思源笔记条目失败: %w", err)
	}

	now := time.Now()
	s.db.Model(&report).Update("synced_at", &now)
	return nil
}

// ==================== SQL 查询方法 ====================

// SQLQueryResult SQL 查询结果
type SQLQueryResult struct {
	Data json.RawMessage `json:"data"`
}

// ExecuteSQL 执行 SQL 查询（思源笔记内置 SQL）
func (s *SiyuanService) ExecuteSQL(stmt string) (json.RawMessage, error) {
	reqBody := map[string]string{
		"stmt": stmt,
	}

	resp, err := s.doRequest("/api/query/sql", reqBody)
	if err != nil {
		return nil, fmt.Errorf("SQL 查询失败: %w", err)
	}

	return resp.Data, nil
}

// ==================== 工具方法 ====================

// weekdayChinese 将英文星期几转换为中文
func weekdayChinese(w time.Weekday) string {
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

// GetWeekdayChinese 导出的星期几中文转换方法（供其他包使用）
func GetWeekdayChinese(w time.Weekday) string {
	return weekdayChinese(w)
}

// TodayDateStr 获取今天的日期字符串（上海时区）
func TodayDateStr() string {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	return time.Now().In(loc).Format("2006-01-02")
}

// TodayWeekday 获取今天的中文星期几
func TodayWeekday() string {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	return weekdayChinese(time.Now().In(loc).Weekday())
}
