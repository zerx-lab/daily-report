package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/zerx-lab/daily-report/internal/model"
	"gorm.io/gorm"
)

// Scheduler 定时任务调度器
type Scheduler struct {
	mu        sync.RWMutex
	db        *gorm.DB
	scheduler gocron.Scheduler
	jobs      map[string]gocron.Job // 任务名称 -> gocron Job
	loc       *time.Location

	// 依赖的服务（由外部注入）
	reportSvc *ReportService
	emailSvc  *EmailService
	siyuanSvc *SiyuanService
	aiSvc     *AIService

	running bool
}

// NewScheduler 创建调度器实例
func NewScheduler(db *gorm.DB, reportSvc *ReportService, emailSvc *EmailService, siyuanSvc *SiyuanService, aiSvc *AIService) (*Scheduler, error) {
	// 从数据库获取时区设置
	tzName := model.GetSettingValue(db, model.CategoryGeneral, model.KeyGeneralTimezone, "Asia/Shanghai")
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		log.Printf("[调度器] 加载时区 %s 失败，使用 Asia/Shanghai: %v\n", tzName, err)
		loc, _ = time.LoadLocation("Asia/Shanghai")
	}

	s, err := gocron.NewScheduler(
		gocron.WithLocation(loc),
	)
	if err != nil {
		return nil, fmt.Errorf("创建调度器失败: %w", err)
	}

	return &Scheduler{
		db:        db,
		scheduler: s,
		jobs:      make(map[string]gocron.Job),
		loc:       loc,
		reportSvc: reportSvc,
		emailSvc:  emailSvc,
		siyuanSvc: siyuanSvc,
		aiSvc:     aiSvc,
	}, nil
}

// Start 启动调度器并加载所有任务
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("调度器已在运行中")
	}

	// 从数据库加载定时任务并注册
	if err := s.loadAndRegisterJobs(); err != nil {
		return fmt.Errorf("加载定时任务失败: %w", err)
	}

	// 启动 gocron 调度器
	s.scheduler.Start()
	s.running = true

	log.Println("[调度器] 启动成功")
	s.printJobsSummary()

	// 监听 context 取消信号，用于优雅关闭
	go func() {
		<-ctx.Done()
		log.Println("[调度器] 收到停止信号，正在关闭...")
		_ = s.Stop()
	}()

	return nil
}

// Stop 停止调度器
func (s *Scheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	if err := s.scheduler.Shutdown(); err != nil {
		return fmt.Errorf("关闭调度器失败: %w", err)
	}

	s.running = false
	s.jobs = make(map[string]gocron.Job)
	log.Println("[调度器] 已停止")
	return nil
}

// IsRunning 调度器是否在运行
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// loadAndRegisterJobs 从数据库读取设置，注册定时任务
func (s *Scheduler) loadAndRegisterJobs() error {
	scheduleSettings, err := model.GetSettingsMapByCategory(s.db, model.CategorySchedule)
	if err != nil {
		return fmt.Errorf("读取定时任务配置失败: %w", err)
	}

	// 注册自动创建日报任务
	createEnabled := scheduleSettings[model.KeyScheduleCreateEnabled] == "true"
	createCron := scheduleSettings[model.KeyScheduleCreateCron]
	if createCron == "" {
		createCron = "0 30 8 * * 1-5"
	}
	if createEnabled {
		if err := s.registerJob("auto_create_report", createCron, s.jobAutoCreateReport); err != nil {
			log.Printf("[调度器] 注册自动创建任务失败: %v\n", err)
		}
	} else {
		log.Println("[调度器] 自动创建日报任务未启用")
		s.updateTaskRecord("auto_create_report", createCron, false, nil)
	}

	// 注册自动发送日报任务
	sendEnabled := scheduleSettings[model.KeyScheduleSendEnabled] == "true"
	sendCron := scheduleSettings[model.KeyScheduleSendCron]
	if sendCron == "" {
		sendCron = "0 0 18 * * 1-5"
	}
	if sendEnabled {
		if err := s.registerJob("auto_send_report", sendCron, s.jobAutoSendReport); err != nil {
			log.Printf("[调度器] 注册自动发送任务失败: %v\n", err)
		}
	} else {
		log.Println("[调度器] 自动发送日报任务未启用")
		s.updateTaskRecord("auto_send_report", sendCron, false, nil)
	}

	// 注册自动同步思源笔记任务
	syncEnabled := scheduleSettings[model.KeyScheduleSyncEnabled] == "true"
	syncCron := scheduleSettings[model.KeyScheduleSyncCron]
	if syncCron == "" {
		syncCron = "0 50 21 * * *"
	}
	if syncEnabled {
		if err := s.registerJob("auto_sync_siyuan", syncCron, s.jobAutoSyncFromSiyuan); err != nil {
			log.Printf("[调度器] 注册自动同步思源任务失败: %v\n", err)
		}
	} else {
		log.Println("[调度器] 自动同步思源笔记任务未启用")
		s.updateTaskRecord("auto_sync_siyuan", syncCron, false, nil)
	}

	return nil
}

// registerJob 注册一个 cron 任务
func (s *Scheduler) registerJob(name, cronExpr string, taskFunc func()) error {
	// 如果已存在同名任务，先移除
	if existing, ok := s.jobs[name]; ok {
		s.scheduler.RemoveJob(existing.ID())
		delete(s.jobs, name)
	}

	job, err := s.scheduler.NewJob(
		gocron.CronJob(cronExpr, true), // 6 字段 cron（含秒）
		gocron.NewTask(taskFunc),
		gocron.WithName(name),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	)
	if err != nil {
		return fmt.Errorf("注册任务 [%s] 失败 (cron=%s): %w", name, cronExpr, err)
	}

	s.jobs[name] = job
	nextRun, _ := job.NextRun()
	log.Printf("[调度器] 任务注册成功: %s (cron=%s, 下次执行=%s)\n", name, cronExpr, nextRun.Format("2006-01-02 15:04:05"))

	// 更新数据库中的任务状态
	s.updateTaskRecord(name, cronExpr, true, &nextRun)
	return nil
}

// Reload 重新加载调度配置（用于设置变更后刷新）
func (s *Scheduler) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return fmt.Errorf("调度器未运行，无法重新加载")
	}

	log.Println("[调度器] 重新加载定时任务配置...")

	// 移除所有现有任务
	for name, job := range s.jobs {
		s.scheduler.RemoveJob(job.ID())
		log.Printf("[调度器] 移除旧任务: %s\n", name)
	}
	s.jobs = make(map[string]gocron.Job)

	// 重新加载
	if err := s.loadAndRegisterJobs(); err != nil {
		return fmt.Errorf("重新加载任务失败: %w", err)
	}

	s.printJobsSummary()
	return nil
}

// EnableJob 启用指定任务
func (s *Scheduler) EnableJob(taskType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var settingKey, cronKey, jobName string
	switch taskType {
	case "create":
		settingKey = model.KeyScheduleCreateEnabled
		cronKey = model.KeyScheduleCreateCron
		jobName = "auto_create_report"
	case "send":
		settingKey = model.KeyScheduleSendEnabled
		cronKey = model.KeyScheduleSendCron
		jobName = "auto_send_report"
	case "sync":
		settingKey = model.KeyScheduleSyncEnabled
		cronKey = model.KeyScheduleSyncCron
		jobName = "auto_sync_siyuan"
	default:
		return fmt.Errorf("未知的任务类型: %s", taskType)
	}

	// 更新数据库设置
	if err := model.UpsertSetting(s.db, model.CategorySchedule, settingKey, "true"); err != nil {
		return fmt.Errorf("更新设置失败: %w", err)
	}

	// 获取 cron 表达式
	cronExpr := model.GetSettingValue(s.db, model.CategorySchedule, cronKey, "")
	if cronExpr == "" {
		return fmt.Errorf("cron 表达式为空，无法启用")
	}

	// 注册对应任务
	var taskFunc func()
	switch taskType {
	case "create":
		taskFunc = s.jobAutoCreateReport
	case "send":
		taskFunc = s.jobAutoSendReport
	case "sync":
		taskFunc = s.jobAutoSyncFromSiyuan
	}

	if err := s.registerJob(jobName, cronExpr, taskFunc); err != nil {
		return err
	}

	log.Printf("[调度器] 任务 %s 已启用\n", jobName)
	return nil
}

// DisableJob 禁用指定任务
func (s *Scheduler) DisableJob(taskType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var settingKey, jobName string
	switch taskType {
	case "create":
		settingKey = model.KeyScheduleCreateEnabled
		jobName = "auto_create_report"
	case "send":
		settingKey = model.KeyScheduleSendEnabled
		jobName = "auto_send_report"
	case "sync":
		settingKey = model.KeyScheduleSyncEnabled
		jobName = "auto_sync_siyuan"
	default:
		return fmt.Errorf("未知的任务类型: %s", taskType)
	}

	// 更新数据库设置
	if err := model.UpsertSetting(s.db, model.CategorySchedule, settingKey, "false"); err != nil {
		return fmt.Errorf("更新设置失败: %w", err)
	}

	// 移除任务
	if job, ok := s.jobs[jobName]; ok {
		s.scheduler.RemoveJob(job.ID())
		delete(s.jobs, jobName)
		log.Printf("[调度器] 任务 %s 已禁用\n", jobName)
	}

	// 更新数据库记录
	s.updateTaskRecord(jobName, "", false, nil)
	return nil
}

// UpdateJobCron 更新任务的 cron 表达式
func (s *Scheduler) UpdateJobCron(taskType, cronExpr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cronKey, enabledKey, jobName string
	switch taskType {
	case "create":
		cronKey = model.KeyScheduleCreateCron
		enabledKey = model.KeyScheduleCreateEnabled
		jobName = "auto_create_report"
	case "send":
		cronKey = model.KeyScheduleSendCron
		enabledKey = model.KeyScheduleSendEnabled
		jobName = "auto_send_report"
	case "sync":
		cronKey = model.KeyScheduleSyncCron
		enabledKey = model.KeyScheduleSyncEnabled
		jobName = "auto_sync_siyuan"
	default:
		return fmt.Errorf("未知的任务类型: %s", taskType)
	}

	// 更新数据库中的 cron 表达式
	if err := model.UpsertSetting(s.db, model.CategorySchedule, cronKey, cronExpr); err != nil {
		return fmt.Errorf("更新 cron 表达式失败: %w", err)
	}

	// 如果任务已启用，则重新注册；否则仅更新 schedule_tasks 表记录
	enabled := model.GetSettingValue(s.db, model.CategorySchedule, enabledKey, "false")
	if enabled == "true" {
		var taskFunc func()
		switch taskType {
		case "create":
			taskFunc = s.jobAutoCreateReport
		case "send":
			taskFunc = s.jobAutoSendReport
		case "sync":
			taskFunc = s.jobAutoSyncFromSiyuan
		}
		if err := s.registerJob(jobName, cronExpr, taskFunc); err != nil {
			return err
		}
	} else {
		// 任务未启用时也需要更新 schedule_tasks 表的 cron_expr，否则页面显示不会变
		s.updateTaskRecord(jobName, cronExpr, false, nil)
	}

	log.Printf("[调度器] 任务 %s cron 表达式已更新: %s\n", jobName, cronExpr)
	return nil
}

// TriggerNow 立即触发指定任务（手动执行）
func (s *Scheduler) TriggerNow(taskType string) error {
	log.Printf("[调度器] 手动触发任务: %s\n", taskType)

	switch taskType {
	case "create":
		go s.jobAutoCreateReport()
	case "send":
		go s.jobAutoSendReport()
	case "sync":
		go s.jobAutoSyncFromSiyuan()
	default:
		return fmt.Errorf("未知的任务类型: %s", taskType)
	}

	return nil
}

// ==================== 定时任务执行逻辑 ====================

// jobAutoCreateReport 自动创建日报任务
func (s *Scheduler) jobAutoCreateReport() {
	now := time.Now().In(s.loc)
	dateStr := now.Format("2006-01-02")
	log.Printf("[定时任务] 开始执行自动创建日报: %s\n", dateStr)

	startTime := time.Now()
	var taskErr error
	defer func() {
		duration := time.Since(startTime)
		errMsg := ""
		if taskErr != nil {
			errMsg = taskErr.Error()
			log.Printf("[定时任务] 自动创建日报失败: %v\n", taskErr)
		} else {
			log.Printf("[定时任务] 自动创建日报完成 (耗时 %v)\n", duration)
		}
		s.recordTaskRun("auto_create_report", errMsg)
	}()

	// 检查是否需要跳过节假日
	if s.shouldSkipToday(now) {
		log.Println("[定时任务] 今天是节假日，跳过创建日报")
		return
	}

	// 检查今天是否已有日报
	if s.reportSvc != nil {
		existing, _ := s.reportSvc.GetByDate(dateStr)
		if existing != nil {
			log.Printf("[定时任务] 今天(%s)的日报已存在，跳过创建\n", dateStr)
			return
		}

		// 创建日报记录
		report, err := s.reportSvc.Create(dateStr, "")
		if err != nil {
			taskErr = fmt.Errorf("创建本地日报记录失败: %w", err)
			return
		}

		log.Printf("[定时任务] 本地日报记录创建成功: ID=%d, 日期=%s\n", report.ID, dateStr)

		// 同步到思源笔记
		if s.siyuanSvc != nil {
			err := s.siyuanSvc.CreateReportEntry("待填写")
			if err != nil {
				log.Printf("[定时任务] 同步思源笔记失败（不影响本地记录）: %v\n", err)
			} else {
				log.Println("[定时任务] 同步思源笔记成功")
				// 回填 SiyuanID，防止后续 AI/MCP 同步时误判为不存在而重复插入
				row, fetchErr := s.siyuanSvc.FetchReportByDate(dateStr)
				if fetchErr == nil && row != nil {
					now2 := time.Now()
					if dbErr := s.db.Model(&report).Updates(map[string]interface{}{
						"siyuan_id": row.RowID,
						"synced_at": &now2,
					}).Error; dbErr != nil {
						log.Printf("[定时任务] 回填 SiyuanID 失败: %v\n", dbErr)
					} else {
						log.Printf("[定时任务] SiyuanID 回填成功: %s\n", row.RowID)
					}
				} else if fetchErr != nil {
					log.Printf("[定时任务] 查询思源行 ID 失败，SiyuanID 未回填: %v\n", fetchErr)
				}
				// 触发云同步，确保新建日报条目上传到云端
				s.siyuanSvc.TriggerCloudSyncAsync()
			}
		}
	}
}

// jobAutoSendReport 自动发送日报任务
// jobAutoSyncFromSiyuan 定时从思源笔记同步数据到本地
func (s *Scheduler) jobAutoSyncFromSiyuan() {
	now := time.Now().In(s.loc)
	log.Printf("[定时任务] 开始执行自动同步思源笔记: %s\n", now.Format("2006-01-02 15:04:05"))

	startTime := time.Now()
	var taskErr error
	defer func() {
		duration := time.Since(startTime)
		errMsg := ""
		if taskErr != nil {
			errMsg = taskErr.Error()
			log.Printf("[定时任务] 自动同步思源笔记失败: %v\n", taskErr)
		} else {
			log.Printf("[定时任务] 自动同步思源笔记完成 (耗时 %v)\n", duration)
		}
		s.recordTaskRun("auto_sync_siyuan", errMsg)
	}()

	if s.siyuanSvc == nil {
		taskErr = fmt.Errorf("思源笔记服务未初始化")
		return
	}

	created, updated, err := s.siyuanSvc.SyncReportsToLocal()
	if err != nil {
		taskErr = fmt.Errorf("同步思源笔记数据失败: %w", err)
		return
	}

	log.Printf("[定时任务] 思源笔记同步结果: 新建 %d 条, 更新 %d 条\n", created, updated)
}

func (s *Scheduler) jobAutoSendReport() {
	now := time.Now().In(s.loc)
	dateStr := now.Format("2006-01-02")
	log.Printf("[定时任务] 开始执行自动发送日报: %s\n", dateStr)

	startTime := time.Now()
	var taskErr error
	defer func() {
		errMsg := ""
		if taskErr != nil {
			errMsg = taskErr.Error()
			log.Printf("[定时任务] 自动发送日报失败: %v\n", taskErr)
		} else {
			duration := time.Since(startTime)
			log.Printf("[定时任务] 自动发送日报完成 (耗时 %v)\n", duration)
		}
		s.recordTaskRun("auto_send_report", errMsg)
	}()

	// 检查是否需要跳过节假日
	if s.shouldSkipToday(now) {
		log.Println("[定时任务] 今天是节假日，跳过发送日报")
		return
	}

	// 获取所有非草稿日报（合并到一个 Excel 发送）
	if s.reportSvc == nil {
		taskErr = fmt.Errorf("日报服务未初始化")
		return
	}

	reports, err := s.reportSvc.GetAllNonDraftReports()
	if err != nil {
		taskErr = fmt.Errorf("获取非草稿日报失败: %w", err)
		return
	}
	if len(reports) == 0 {
		taskErr = fmt.Errorf("没有可发送的日报记录（所有日报均为草稿状态）")
		return
	}

	log.Printf("[定时任务] 查询到 %d 条非草稿日报，准备合并发送\n", len(reports))

	// 批量发送邮件（所有日报合并到一个 Excel 表格中）
	if s.emailSvc != nil {
		_, err = s.emailSvc.SendBatchReports(reports, model.EmailSendTypeAuto)
		if err != nil {
			taskErr = fmt.Errorf("批量发送日报邮件失败: %w", err)
			return
		}
	} else {
		taskErr = fmt.Errorf("邮件服务未初始化")
		return
	}
}

// ==================== 辅助方法 ====================

// shouldSkipToday 判断今天是否需要跳过（非工作日或节假日）
func (s *Scheduler) shouldSkipToday(now time.Time) bool {
	// 检查是否启用了节假日跳过
	skipHoliday := model.GetSettingValue(s.db, model.CategorySchedule, model.KeyScheduleSkipHoliday, "true")
	if skipHoliday != "true" {
		return false
	}

	weekday := now.Weekday()

	// 基本的周末判断
	if weekday == time.Saturday || weekday == time.Sunday {
		// TODO: 这里可以增加调休工作日判断（使用第三方节假日 API 或本地数据）
		// 目前仅做简单的周末跳过
		return true
	}

	// TODO: 接入中国法定节假日数据
	// 可以调用第三方 API 或使用本地节假日配置表来判断
	// 例如：国务院发布的节假日安排

	return false
}

// recordTaskRun 记录任务执行结果到数据库
func (s *Scheduler) recordTaskRun(taskName string, errMsg string) {
	now := time.Now().In(s.loc)

	var task model.ScheduleTask
	result := s.db.Where("task_name = ?", taskName).First(&task)
	if result.Error != nil {
		// 不存在则创建
		task = model.ScheduleTask{
			TaskType:   s.taskTypeFromName(taskName),
			TaskName:   taskName,
			CronExpr:   s.getJobCronExpr(taskName),
			Enabled:    true,
			LastRunAt:  &now,
			LastResult: errMsg,
		}
		if errMsg == "" {
			task.LastResult = "执行成功"
		}
		s.db.Create(&task)
		return
	}

	updates := map[string]interface{}{
		"last_run_at": &now,
		"last_result": "执行成功",
	}
	if errMsg != "" {
		updates["last_result"] = errMsg
	}
	s.db.Model(&task).Updates(updates)
}

// updateTaskRecord 更新任务记录
func (s *Scheduler) updateTaskRecord(taskName, cronExpr string, enabled bool, nextRun *time.Time) {
	var task model.ScheduleTask
	result := s.db.Where("task_name = ?", taskName).First(&task)
	taskType := s.taskTypeFromName(taskName)
	description := s.taskDescriptionFromName(taskName)
	if result.Error != nil {
		// 不存在则创建
		task = model.ScheduleTask{
			TaskType:    taskType,
			TaskName:    taskName,
			CronExpr:    cronExpr,
			Enabled:     enabled,
			Description: description,
		}
		s.db.Create(&task)
		return
	}

	updates := map[string]interface{}{
		"task_type":   taskType,
		"description": description,
		"enabled":     enabled,
	}
	if cronExpr != "" {
		updates["cron_expr"] = cronExpr
	}
	s.db.Model(&task).Updates(updates)
}

// taskDescriptionFromName 根据任务名获取任务描述
func (s *Scheduler) taskDescriptionFromName(name string) string {
	switch name {
	case "auto_create_report":
		return "自动创建日报"
	case "auto_send_report":
		return "自动发送日报"
	case "auto_sync_siyuan":
		return "自动同步思源笔记到本地"

	default:
		return "自动任务"
	}
}

// taskTypeFromName 根据任务名获取任务类型
func (s *Scheduler) taskTypeFromName(name string) string {
	switch name {
	case "auto_create_report":
		return "create"
	case "auto_send_report":
		return "send"
	case "auto_sync_siyuan":
		return "sync"

	default:
		return "other"
	}
}

// getJobCronExpr 获取任务的 cron 表达式
func (s *Scheduler) getJobCronExpr(jobName string) string {
	switch jobName {
	case "auto_create_report":
		return model.GetSettingValue(s.db, model.CategorySchedule, model.KeyScheduleCreateCron, "0 30 8 * * 1-5")
	case "auto_send_report":
		return model.GetSettingValue(s.db, model.CategorySchedule, model.KeyScheduleSendCron, "0 0 18 * * 1-5")
	case "auto_sync_siyuan":
		return model.GetSettingValue(s.db, model.CategorySchedule, model.KeyScheduleSyncCron, "0 50 21 * * *")
	default:
		return ""
	}
}

// GetJobsStatus 获取所有定时任务的状态（用于页面展示）
func (s *Scheduler) GetJobsStatus() []map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var tasks []model.ScheduleTask
	s.db.Order("task_name ASC").Find(&tasks)

	result := make([]map[string]interface{}, 0, len(tasks))
	for _, task := range tasks {
		item := map[string]interface{}{
			"id":          task.ID,
			"task_type":   task.TaskType,
			"task_name":   task.TaskName,
			"cron_expr":   task.CronExpr,
			"enabled":     task.Enabled,
			"last_run_at": task.LastRunAt,
			"last_result": task.LastResult,
			"description": task.Description,
		}

		// 如果任务已注册，获取下次运行时间
		if job, ok := s.jobs[task.TaskName]; ok {
			nextRun, err := job.NextRun()
			if err == nil {
				item["next_run_at"] = nextRun.Format("2006-01-02 15:04:05")
			}
		}

		result = append(result, item)
	}

	return result
}

// printJobsSummary 打印当前所有注册任务的摘要
func (s *Scheduler) printJobsSummary() {
	log.Printf("[调度器] 当前注册任务数: %d\n", len(s.jobs))
	for name, job := range s.jobs {
		nextRun, err := job.NextRun()
		if err != nil {
			log.Printf("  - %s (下次执行时间获取失败: %v)\n", name, err)
		} else {
			log.Printf("  - %s -> 下次执行: %s\n", name, nextRun.Format("2006-01-02 15:04:05"))
		}
	}
}

// ==================== 工具函数 ====================

// weekdayToChinese 将英文星期转为中文
func weekdayToChinese(w time.Weekday) string {
	switch w {
	case time.Monday:
		return "周一"
	case time.Tuesday:
		return "周二"
	case time.Wednesday:
		return "周三"
	case time.Thursday:
		return "周四"
	case time.Friday:
		return "周五"
	case time.Saturday:
		return "周六"
	case time.Sunday:
		return "周日"
	default:
		return ""
	}
}
