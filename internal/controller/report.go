package controller

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zerx-lab/daily-report/internal/model"
	"github.com/zerx-lab/daily-report/internal/service"
	"gorm.io/gorm"
)

// ReportController 日报管理控制器
type ReportController struct {
	db        *gorm.DB
	reportSvc *service.ReportService
	emailSvc  *service.EmailService
	siyuanSvc *service.SiyuanService
	scheduler *service.Scheduler
}

// NewReportController 创建日报控制器实例
func NewReportController(
	db *gorm.DB,
	reportSvc *service.ReportService,
	emailSvc *service.EmailService,
	siyuanSvc *service.SiyuanService,
	scheduler *service.Scheduler,
) *ReportController {
	return &ReportController{
		db:        db,
		reportSvc: reportSvc,
		emailSvc:  emailSvc,
		siyuanSvc: siyuanSvc,
		scheduler: scheduler,
	}
}

// ==================== 仪表盘 ====================

// Dashboard 仪表盘首页
func (c *ReportController) Dashboard(ctx *gin.Context) {
	stats, err := c.reportSvc.GetDashboardStats()
	if err != nil {
		log.Printf("[控制器] 获取仪表盘数据失败: %v\n", err)
		stats = &service.DashboardStats{}
	}

	// 获取最近的邮件发送日志
	recentLogs, _ := c.emailSvc.GetRecentLogs(5)

	// 获取定时任务状态
	var jobsStatus []map[string]interface{}
	if c.scheduler != nil {
		jobsStatus = c.scheduler.GetJobsStatus()
	}

	// 今天的日期信息
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)
	todayStr := now.Format("2006-01-02")
	weekday := service.GetWeekdayChinese(now.Weekday())
	isWorkday := service.IsWorkday(now)

	ctx.HTML(http.StatusOK, "dashboard.html", gin.H{
		"title":       "仪表盘",
		"active":      "dashboard",
		"stats":       stats,
		"recentLogs":  recentLogs,
		"jobsStatus":  jobsStatus,
		"today":       todayStr,
		"weekday":     weekday,
		"isWorkday":   isWorkday,
		"currentTime": now.Format("2006-01-02 15:04:05"),
	})
}

// ==================== 日报列表 ====================

// List 日报列表页面
func (c *ReportController) List(ctx *gin.Context) {
	var query model.ReportListQuery
	if err := ctx.ShouldBindQuery(&query); err != nil {
		log.Printf("[控制器] 绑定查询参数失败: %v\n", err)
	}
	query.Normalize()

	reports, pagination, err := c.reportSvc.List(&query)
	if err != nil {
		log.Printf("[控制器] 查询日报列表失败: %v\n", err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{
			"title":   "错误",
			"message": "查询日报列表失败",
			"error":   err.Error(),
		})
		return
	}

	ctx.HTML(http.StatusOK, "reports.html", gin.H{
		"title":      "日报列表",
		"active":     "reports",
		"reports":    reports,
		"pagination": pagination,
		"query":      query,
	})
}

// ==================== 创建日报 ====================

// CreateForm 创建日报表单页面
func (c *ReportController) CreateForm(ctx *gin.Context) {
	// 默认日期为今天
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)
	todayStr := now.Format("2006-01-02")
	weekday := service.GetWeekdayChinese(now.Weekday())

	ctx.HTML(http.StatusOK, "report_edit.html", gin.H{
		"title":   "创建日报",
		"active":  "reports",
		"isNew":   true,
		"date":    todayStr,
		"weekday": weekday,
		"report":  nil,
	})
}

// Create 创建日报（POST）
func (c *ReportController) Create(ctx *gin.Context) {
	date := strings.TrimSpace(ctx.PostForm("date"))
	content := ctx.PostForm("content")

	if date == "" {
		c.flashAndRedirect(ctx, "error", "日期不能为空", "/reports/new")
		return
	}

	// 验证日期格式
	if _, err := time.Parse("2006-01-02", date); err != nil {
		c.flashAndRedirect(ctx, "error", "日期格式无效，应为 yyyy-MM-dd", "/reports/new")
		return
	}

	report, err := c.reportSvc.Create(date, content)
	if err != nil {
		log.Printf("[控制器] 创建日报失败: %v\n", err)
		// 如果已存在，尝试获取并跳转到编辑页面
		existing, getErr := c.reportSvc.GetByDate(date)
		if getErr == nil && existing != nil {
			c.flashAndRedirect(ctx, "warning", fmt.Sprintf("日期 %s 的日报已存在，已跳转到编辑页面", date), fmt.Sprintf("/reports/%d/edit", existing.ID))
			return
		}
		c.flashAndRedirect(ctx, "error", "创建日报失败: "+err.Error(), "/reports/new")
		return
	}

	// 同步到思源笔记（异步，不阻塞）
	if c.siyuanSvc != nil {
		go func(r *model.Report) {
			if syncErr := c.siyuanSvc.SyncLocalToSiyuan(r.ID); syncErr != nil {
				log.Printf("[控制器] 同步思源笔记失败(异步): %v\n", syncErr)
			}
		}(report)
	}

	c.flashAndRedirect(ctx, "success", fmt.Sprintf("日报 %s 创建成功", date), fmt.Sprintf("/reports/%d/edit", report.ID))
}

// CreateToday 快速创建今日日报
func (c *ReportController) CreateToday(ctx *gin.Context) {
	report, created, err := c.reportSvc.CreateTodayIfNotExist("待填写")
	if err != nil {
		c.flashAndRedirect(ctx, "error", "创建今日日报失败: "+err.Error(), "/")
		return
	}

	if created {
		c.flashAndRedirect(ctx, "success", "今日日报创建成功", fmt.Sprintf("/reports/%d/edit", report.ID))
	} else {
		c.flashAndRedirect(ctx, "info", "今日日报已存在", fmt.Sprintf("/reports/%d/edit", report.ID))
	}
}

// ==================== 编辑日报 ====================

// EditForm 编辑日报表单页面
func (c *ReportController) EditForm(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		c.flashAndRedirect(ctx, "error", "无效的日报 ID", "/reports")
		return
	}

	report, err := c.reportSvc.GetByID(id)
	if err != nil {
		c.flashAndRedirect(ctx, "error", "日报不存在", "/reports")
		return
	}

	// 获取该日报的发送记录
	sendLogs, _ := c.emailSvc.GetSendLogsByReportID(report.ID)

	ctx.HTML(http.StatusOK, "report_edit.html", gin.H{
		"title":    fmt.Sprintf("编辑日报 - %s", report.Date),
		"active":   "reports",
		"isNew":    false,
		"report":   report,
		"date":     report.Date,
		"weekday":  report.Weekday,
		"sendLogs": sendLogs,
	})
}

// Update 更新日报（POST）
func (c *ReportController) Update(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		c.flashAndRedirect(ctx, "error", "无效的日报 ID", "/reports")
		return
	}

	content := ctx.PostForm("content")

	report, err := c.reportSvc.Update(id, content)
	if err != nil {
		log.Printf("[控制器] 更新日报失败: %v\n", err)
		c.flashAndRedirect(ctx, "error", "更新日报失败: "+err.Error(), fmt.Sprintf("/reports/%d/edit", id))
		return
	}

	// 异步同步到思源笔记
	if c.siyuanSvc != nil {
		go func(r *model.Report) {
			if syncErr := c.siyuanSvc.SyncLocalToSiyuan(r.ID); syncErr != nil {
				log.Printf("[控制器] 同步思源笔记失败(异步): %v\n", syncErr)
			}
		}(report)
	}

	c.flashAndRedirect(ctx, "success", "日报更新成功", fmt.Sprintf("/reports/%d/edit", id))
}

// ==================== 删除日报 ====================

// Delete 删除日报（POST）
func (c *ReportController) Delete(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		c.jsonError(ctx, http.StatusBadRequest, "无效的日报 ID")
		return
	}

	if err := c.reportSvc.Delete(id); err != nil {
		c.jsonError(ctx, http.StatusInternalServerError, "删除日报失败: "+err.Error())
		return
	}

	// 判断请求来源决定响应方式
	if c.isAjax(ctx) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "删除成功",
		})
	} else {
		c.flashAndRedirect(ctx, "success", "日报已删除", "/reports")
	}
}

// ==================== 发送日报邮件 ====================

// Send 手动发送日报邮件（POST）
func (c *ReportController) Send(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		c.jsonOrFlash(ctx, http.StatusBadRequest, "error", "无效的日报 ID", "/reports")
		return
	}

	report, err := c.reportSvc.GetByID(id)
	if err != nil {
		c.jsonOrFlash(ctx, http.StatusNotFound, "error", "日报不存在", "/reports")
		return
	}

	// 检查日报内容是否为空
	if strings.TrimSpace(report.Content) == "" || report.Content == "待填写" {
		c.jsonOrFlash(ctx, http.StatusBadRequest, "error", "日报内容为空，请先填写", fmt.Sprintf("/reports/%d/edit", id))
		return
	}

	// 调用邮件服务发送
	logID, sendErr := c.emailSvc.SendReport(report, model.EmailSendTypeManual)
	if sendErr != nil {
		log.Printf("[控制器] 发送日报邮件失败: %v\n", sendErr)
		c.jsonOrFlash(ctx, http.StatusInternalServerError, "error", "发送失败: "+sendErr.Error(), fmt.Sprintf("/reports/%d/edit", id))
		return
	}

	msg := fmt.Sprintf("日报发送成功（日志 ID: %d）", logID)
	c.jsonOrFlash(ctx, http.StatusOK, "success", msg, fmt.Sprintf("/reports/%d/edit", id))
}

// ==================== 思源笔记同步 ====================

// SyncFromSiyuan 从思源笔记同步数据到本地（POST）
func (c *ReportController) SyncFromSiyuan(ctx *gin.Context) {
	if c.siyuanSvc == nil {
		c.jsonOrFlash(ctx, http.StatusServiceUnavailable, "error", "思源笔记服务未配置", "/")
		return
	}

	created, updated, err := c.siyuanSvc.SyncReportsToLocal()
	if err != nil {
		c.jsonOrFlash(ctx, http.StatusInternalServerError, "error", "同步失败: "+err.Error(), "/")
		return
	}

	msg := fmt.Sprintf("同步完成：新增 %d 条，更新 %d 条", created, updated)
	c.jsonOrFlash(ctx, http.StatusOK, "success", msg, "/reports")
}

// SyncAllFromSiyuan 全局同步：从思源笔记拉取日报和外出申请数据到本地（POST，返回 JSON）
func (c *ReportController) SyncAllFromSiyuan(ctx *gin.Context) {
	if c.siyuanSvc == nil {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    -1,
			"message": "思源笔记服务未配置",
		})
		return
	}

	type syncItem struct {
		Name    string `json:"name"`
		Created int    `json:"created"`
		Updated int    `json:"updated"`
		Error   string `json:"error,omitempty"`
	}

	items := make([]syncItem, 0, 2)

	// 1. 同步日报
	rCreated, rUpdated, rErr := c.siyuanSvc.SyncReportsToLocal()
	item := syncItem{Name: "日报", Created: rCreated, Updated: rUpdated}
	if rErr != nil {
		item.Error = rErr.Error()
		log.Printf("[全局同步] 日报同步失败: %v\n", rErr)
	}
	items = append(items, item)

	// 2. 同步外出申请（AV 未配置时跳过，不视为错误）
	oCreated, oUpdated, oErr := c.siyuanSvc.SyncOutingsToLocal()
	oItem := syncItem{Name: "外出申请", Created: oCreated, Updated: oUpdated}
	if oErr != nil {
		oItem.Error = oErr.Error()
		log.Printf("[全局同步] 外出申请同步失败: %v\n", oErr)
	}
	items = append(items, oItem)

	// 汇总消息
	totalCreated := rCreated + oCreated
	totalUpdated := rUpdated + oUpdated
	hasError := rErr != nil || oErr != nil

	var msgParts []string
	if rErr == nil {
		msgParts = append(msgParts, fmt.Sprintf("日报: 新增 %d / 更新 %d", rCreated, rUpdated))
	} else {
		msgParts = append(msgParts, "日报: 同步失败")
	}
	if oErr == nil && (oCreated > 0 || oUpdated > 0) {
		msgParts = append(msgParts, fmt.Sprintf("外出申请: 新增 %d / 更新 %d", oCreated, oUpdated))
	} else if oErr != nil && !strings.Contains(oErr.Error(), "未配置") {
		msgParts = append(msgParts, "外出申请: 同步失败")
	}

	msg := strings.Join(msgParts, "；")
	if msg == "" {
		msg = "同步完成，无数据变更"
	}

	code := 0
	if hasError && totalCreated == 0 && totalUpdated == 0 {
		code = -1
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":    code,
		"message": msg,
		"data": gin.H{
			"items":        items,
			"totalCreated": totalCreated,
			"totalUpdated": totalUpdated,
		},
	})
}

// PingSiyuan 测试思源笔记连接（GET/POST）
func (c *ReportController) PingSiyuan(ctx *gin.Context) {
	if c.siyuanSvc == nil {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    -1,
			"message": "思源笔记服务未配置",
		})
		return
	}

	version, err := c.siyuanSvc.Ping()
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    -1,
			"message": "连接失败: " + err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "连接成功",
		"data": gin.H{
			"version": version,
		},
	})
}

// ==================== 邮件相关 ====================

// TestSMTP 测试 SMTP 连接（POST）
func (c *ReportController) TestSMTP(ctx *gin.Context) {
	if err := c.emailSvc.TestSMTP(); err != nil {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    -1,
			"message": "SMTP 连接失败: " + err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "SMTP 连接成功",
	})
}

// SendTestEmail 发送测试邮件（POST）
func (c *ReportController) SendTestEmail(ctx *gin.Context) {
	to := strings.TrimSpace(ctx.PostForm("to"))

	// 如果表单方式取不到，尝试从 JSON body 中解析
	if to == "" {
		var req struct {
			To string `json:"to"`
		}
		if err := ctx.ShouldBindJSON(&req); err == nil {
			to = strings.TrimSpace(req.To)
		}
	}

	if to == "" {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    -1,
			"message": "收件人地址不能为空",
		})
		return
	}

	if err := c.emailSvc.SendTestEmail(to); err != nil {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    -1,
			"message": "发送失败: " + err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "测试邮件发送成功，请检查收件箱",
	})
}

// SendLogs 邮件发送日志列表页面
func (c *ReportController) SendLogs(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	logs, total, err := c.emailSvc.GetSendLogs(page, pageSize)
	if err != nil {
		log.Printf("[控制器] 查询发送日志失败: %v\n", err)
	}

	totalPage := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPage++
	}
	if totalPage == 0 {
		totalPage = 1
	}

	ctx.HTML(http.StatusOK, "send_logs.html", gin.H{
		"title":  "发送记录",
		"active": "logs",
		"logs":   logs,
		"pagination": &model.Pagination{
			Page:      page,
			PageSize:  pageSize,
			Total:     total,
			TotalPage: totalPage,
		},
	})
}

// SendLogDetail 发送日志详情（AJAX）
func (c *ReportController) SendLogDetail(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "无效的日志 ID"})
		return
	}

	logEntry, err := c.emailSvc.GetSendLogByID(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"code": -1, "message": "日志不存在"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": logEntry,
	})
}

// ==================== 定时任务管理 ====================

// ScheduleList 定时任务列表页面
func (c *ReportController) ScheduleList(ctx *gin.Context) {
	var jobsStatus []map[string]interface{}
	if c.scheduler != nil {
		jobsStatus = c.scheduler.GetJobsStatus()
	}

	// 获取调度器设置
	scheduleSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategorySchedule)

	ctx.HTML(http.StatusOK, "schedules.html", gin.H{
		"title":     "定时任务",
		"active":    "schedules",
		"jobs":      jobsStatus,
		"settings":  scheduleSettings,
		"isRunning": c.scheduler != nil && c.scheduler.IsRunning(),
	})
}

// ScheduleToggle 启用/禁用定时任务（POST）
func (c *ReportController) ScheduleToggle(ctx *gin.Context) {
	taskType := ctx.PostForm("task_type") // "create" 或 "send"
	action := ctx.PostForm("action")      // "enable" 或 "disable"

	if taskType == "" || action == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "参数不完整"})
		return
	}

	if c.scheduler == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": -1, "message": "调度器未初始化"})
		return
	}

	var err error
	switch action {
	case "enable":
		err = c.scheduler.EnableJob(taskType)
	case "disable":
		err = c.scheduler.DisableJob(taskType)
	default:
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "无效的操作: " + action})
		return
	}

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": fmt.Sprintf("任务 %s 已%s", taskType, map[string]string{"enable": "启用", "disable": "禁用"}[action]),
	})
}

// ScheduleUpdateCron 更新定时任务的 cron 表达式（POST）
func (c *ReportController) ScheduleUpdateCron(ctx *gin.Context) {
	taskType := ctx.PostForm("task_type")
	cronExpr := strings.TrimSpace(ctx.PostForm("cron_expr"))

	if taskType == "" || cronExpr == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "参数不完整"})
		return
	}

	if c.scheduler == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": -1, "message": "调度器未初始化"})
		return
	}

	if err := c.scheduler.UpdateJobCron(taskType, cronExpr); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "Cron 表达式已更新",
	})
}

// ScheduleTrigger 手动触发定时任务（POST）
func (c *ReportController) ScheduleTrigger(ctx *gin.Context) {
	taskType := ctx.PostForm("task_type")
	if taskType == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "任务类型不能为空"})
		return
	}

	if c.scheduler == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": -1, "message": "调度器未初始化"})
		return
	}

	if err := c.scheduler.TriggerNow(taskType); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "任务已触发执行",
	})
}

// ==================== 设置页面 ====================

// Settings 系统设置页面
func (c *ReportController) Settings(ctx *gin.Context) {
	// 获取所有分类的设置
	smtpSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategorySMTP)
	siyuanSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategorySiyuan)
	emailSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategoryEmail)
	generalSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategoryGeneral)
	scheduleSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategorySchedule)
	outingSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategoryOuting)

	ctx.HTML(http.StatusOK, "settings.html", gin.H{
		"title":    "系统设置",
		"active":   "settings",
		"smtp":     smtpSettings,
		"siyuan":   siyuanSettings,
		"email":    emailSettings,
		"general":  generalSettings,
		"schedule": scheduleSettings,
		"outing":   outingSettings,
	})
}

// SaveSettings 保存系统设置（POST）
func (c *ReportController) SaveSettings(ctx *gin.Context) {
	category := ctx.PostForm("category")
	if category == "" {
		c.flashAndRedirect(ctx, "error", "设置分类不能为空", "/settings")
		return
	}

	// 定义每个分类的允许字段
	allowedKeys := map[string][]string{
		model.CategorySMTP:     {model.KeySMTPHost, model.KeySMTPPort, model.KeySMTPUsername, model.KeySMTPPassword, model.KeySMTPFrom, model.KeySMTPFromAddr, model.KeySMTPUseTLS},
		model.CategorySiyuan:   {model.KeySiyuanBaseURL, model.KeySiyuanAPIToken, model.KeySiyuanAvID, model.KeySiyuanBlockID, model.KeySiyuanKeyID, model.KeySiyuanContentID, model.KeySiyuanNotebook},
		model.CategoryEmail:    {model.KeyEmailRecipients, model.KeyEmailCc, model.KeyEmailSubject},
		model.CategoryGeneral:  {model.KeyGeneralAppName, model.KeyGeneralTimezone},
		model.CategorySchedule: {model.KeyScheduleCreateEnabled, model.KeyScheduleCreateCron, model.KeyScheduleSendEnabled, model.KeyScheduleSendCron, model.KeyScheduleSyncEnabled, model.KeyScheduleSyncCron, model.KeyScheduleSkipHoliday},
		model.CategoryOuting:   {model.KeyOutingRecipients, model.KeyOutingCc, model.KeyOutingSubject, model.KeyOutingApplicant, model.KeyOutingDepartment, model.KeyOutingAvID, model.KeyOutingBlockID, model.KeyOutingKeyOutTime, model.KeyOutingKeyReturnTime, model.KeyOutingKeyDestination, model.KeyOutingKeyReason, model.KeyOutingKeyRemarks},
	}

	keys, ok := allowedKeys[category]
	if !ok {
		c.flashAndRedirect(ctx, "error", "未知的设置分类: "+category, "/settings")
		return
	}

	// 解析表单数据，只收集表单中实际提交的字段
	// 避免同分类下不同表单（如外出申请的邮件配置和思源配置）互相覆盖
	_ = ctx.Request.ParseForm()
	kvPairs := make(map[string]string)
	for _, key := range keys {
		if _, submitted := ctx.Request.PostForm[key]; submitted {
			kvPairs[key] = ctx.PostForm(key)
		}
	}

	// checkbox 特殊处理：未勾选时表单不会提交该字段，需要显式设为 "false"
	// 按分类定义 checkbox 字段，确保只处理当前分类的 checkbox
	categoryCheckboxKeys := map[string][]string{
		model.CategorySMTP:     {model.KeySMTPUseTLS},
		model.CategorySchedule: {model.KeyScheduleCreateEnabled, model.KeyScheduleSendEnabled, model.KeyScheduleSyncEnabled, model.KeyScheduleSkipHoliday},
	}
	if cbKeys, hasCB := categoryCheckboxKeys[category]; hasCB {
		for _, key := range cbKeys {
			if val, exists := kvPairs[key]; exists {
				// 表单提交了该字段，值为 "on" 或 "true" 都视为勾选
				if val == "on" || val == "true" {
					kvPairs[key] = "true"
				}
			} else {
				// 未勾选，显式设为 false
				kvPairs[key] = "false"
			}
		}
	}

	// 批量保存
	if err := model.BatchUpsertSettings(c.db, category, kvPairs); err != nil {
		c.flashAndRedirect(ctx, "error", "保存设置失败: "+err.Error(), "/settings")
		return
	}

	// 如果是调度相关设置变更，尝试重新加载调度器
	if category == model.CategorySchedule && c.scheduler != nil {
		go func() {
			if err := c.scheduler.Reload(); err != nil {
				log.Printf("[控制器] 重新加载调度器失败: %v\n", err)
			}
		}()
	}

	c.flashAndRedirect(ctx, "success", "设置保存成功", "/settings#"+category)
}

// ==================== API 接口（JSON 响应） ====================

// APIListReports 日报列表接口（JSON）
func (c *ReportController) APIListReports(ctx *gin.Context) {
	var query model.ReportListQuery
	_ = ctx.ShouldBindQuery(&query)
	query.Normalize()

	reports, pagination, err := c.reportSvc.List(&query)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":       0,
		"data":       reports,
		"pagination": pagination,
	})
}

// APIGetReport 获取单条日报接口（JSON）
func (c *ReportController) APIGetReport(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "无效的 ID"})
		return
	}

	report, err := c.reportSvc.GetByID(id)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"code": -1, "message": "日报不存在"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": report,
	})
}

// APICreateReport 创建日报接口（JSON）
func (c *ReportController) APICreateReport(ctx *gin.Context) {
	var req struct {
		Date    string `json:"date" binding:"required"`
		Content string `json:"content"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "参数错误: " + err.Error()})
		return
	}

	report, err := c.reportSvc.Create(req.Date, req.Content)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": report,
	})
}

// APIUpdateReport 更新日报接口（JSON）
func (c *ReportController) APIUpdateReport(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "无效的 ID"})
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "参数错误: " + err.Error()})
		return
	}

	report, err := c.reportSvc.Update(id, req.Content)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": report,
	})
}

// APIDeleteReport 删除日报接口（JSON）
func (c *ReportController) APIDeleteReport(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "无效的 ID"})
		return
	}

	if err := c.reportSvc.Delete(id); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "删除成功",
	})
}

// APISendReport 发送日报接口（JSON）
func (c *ReportController) APISendReport(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "无效的 ID"})
		return
	}

	report, err := c.reportSvc.GetByID(id)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"code": -1, "message": "日报不存在"})
		return
	}

	logID, sendErr := c.emailSvc.SendReport(report, model.EmailSendTypeManual)
	if sendErr != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": sendErr.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "发送成功",
		"data": gin.H{
			"log_id": logID,
		},
	})
}

// ==================== 辅助方法 ====================

// parseID 从 URL 参数解析 ID
func (c *ReportController) parseID(ctx *gin.Context) (uint, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("无效的 ID: %s", idStr)
	}
	return uint(id), nil
}

// isAjax 判断是否为 AJAX 请求
func (c *ReportController) isAjax(ctx *gin.Context) bool {
	return ctx.GetHeader("X-Requested-With") == "XMLHttpRequest" ||
		strings.Contains(ctx.GetHeader("Accept"), "application/json") ||
		ctx.Query("format") == "json"
}

// flashAndRedirect 设置闪存消息并重定向
func (c *ReportController) flashAndRedirect(ctx *gin.Context, level, message, url string) {
	// 使用 query parameter 传递 flash 消息（简单实现）
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	redirectURL := fmt.Sprintf("%s%sflash_level=%s&flash_msg=%s", url, sep, level, message)
	ctx.Redirect(http.StatusFound, redirectURL)
}

// jsonError 返回 JSON 格式的错误响应
func (c *ReportController) jsonError(ctx *gin.Context, code int, message string) {
	ctx.JSON(code, gin.H{
		"code":    -1,
		"message": message,
	})
}

// jsonOrFlash 根据请求类型返回 JSON 或重定向
func (c *ReportController) jsonOrFlash(ctx *gin.Context, httpCode int, level, message, redirectURL string) {
	if c.isAjax(ctx) {
		code := 0
		if level == "error" {
			code = -1
		}
		ctx.JSON(httpCode, gin.H{
			"code":    code,
			"message": message,
		})
	} else {
		c.flashAndRedirect(ctx, level, message, redirectURL)
	}
}
