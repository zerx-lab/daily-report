package service

import (
	"bytes"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xuri/excelize/v2"
	"github.com/zerx-lab/daily-report/internal/model"
	"gorm.io/gorm"
)

// EmailService 邮件发送服务
type EmailService struct {
	db            *gorm.DB
	templatesFS   embed.FS
	emailTemplate *template.Template
	mu            sync.Mutex
}

// Attachment 邮件附件
type Attachment struct {
	Filename    string // 附件文件名（支持中文）
	ContentType string // MIME 类型
	Data        []byte // 附件原始字节
}

// EmailMessage 待发送的邮件消息
type EmailMessage struct {
	To          []string     // 收件人列表
	Cc          []string     // 抄送列表
	Subject     string       // 邮件主题
	Body        string       // HTML 正文（为空时不发送正文部分）
	Attachments []Attachment // 附件列表
}

// EmailTemplateData 邮件模板渲染数据
type EmailTemplateData struct {
	Date     string // 日期，如 2026-03-24
	Weekday  string // 星期几
	Content  string // 日报正文（HTML）
	Author   string // 作者/发送人
	AppName  string // 应用名称
	Year     int    // 当前年份
	SentAt   string // 发送时间
	ReportID uint   // 日报 ID
}

// SMTPConfig 运行时 SMTP 配置（从数据库设置加载）
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	FromName string
	FromAddr string
	UseTLS   bool
}

// NewEmailService 创建邮件服务实例
func NewEmailService(db *gorm.DB, templatesFS embed.FS) *EmailService {
	svc := &EmailService{
		db:          db,
		templatesFS: templatesFS,
	}

	// 加载邮件 HTML 模板
	if err := svc.loadTemplates(); err != nil {
		log.Printf("[邮件服务] 加载模板失败（将使用内置模板）: %v\n", err)
	}

	return svc
}

// loadTemplates 从嵌入文件系统加载邮件 HTML 模板
func (s *EmailService) loadTemplates() error {
	tmpl, err := template.New("email").Funcs(template.FuncMap{
		"nl2br": func(text string) template.HTML {
			escaped := template.HTMLEscapeString(text)
			return template.HTML(strings.ReplaceAll(escaped, "\n", "<br>"))
		},
		"safeHTML": func(text string) template.HTML {
			return template.HTML(text)
		},
	}).ParseFS(s.templatesFS, "templates/email/*.html")
	if err != nil {
		return fmt.Errorf("解析嵌入邮件模板失败: %w", err)
	}
	s.emailTemplate = tmpl
	return nil
}

// ReloadTemplates 重新加载模板（运行时热更新）
func (s *EmailService) ReloadTemplates() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadTemplates()
}

// GetSMTPConfig 从数据库加载 SMTP 配置
func (s *EmailService) GetSMTPConfig() (*SMTPConfig, error) {
	settings, err := model.GetSMTPSettings(s.db)
	if err != nil {
		return nil, fmt.Errorf("获取 SMTP 设置失败: %w", err)
	}

	port, _ := strconv.Atoi(settings.Port)
	if port == 0 {
		port = 587
	}

	useTLS := strings.ToLower(settings.UseTLS) == "true" || settings.UseTLS == "1"

	cfg := &SMTPConfig{
		Host:     settings.Host,
		Port:     port,
		Username: settings.Username,
		Password: settings.Password,
		FromName: settings.From,
		FromAddr: settings.FromAddr,
		UseTLS:   useTLS,
	}

	if cfg.Host == "" {
		return nil, fmt.Errorf("SMTP 服务器地址未配置")
	}
	if cfg.FromAddr == "" {
		cfg.FromAddr = cfg.Username
	}
	if cfg.FromName == "" {
		cfg.FromName = "日报系统"
	}

	return cfg, nil
}

// GetRecipients 从数据库获取收件人列表
func (s *EmailService) GetRecipients() (to []string, cc []string, err error) {
	toStr := model.GetSettingValue(s.db, model.CategoryEmail, model.KeyEmailRecipients, "")
	ccStr := model.GetSettingValue(s.db, model.CategoryEmail, model.KeyEmailCc, "")

	to = splitAndTrim(toStr)
	cc = splitAndTrim(ccStr)

	if len(to) == 0 {
		return nil, nil, fmt.Errorf("未配置收件人邮箱")
	}
	return to, cc, nil
}

// RenderSubject 渲染邮件主题
func (s *EmailService) RenderSubject(report *model.Report) (string, error) {
	if report == nil {
		return "", fmt.Errorf("日报记录不能为空")
	}

	return s.renderSubject(report.Date, report.Weekday)
}

// renderSubject 按日期渲染邮件主题
func (s *EmailService) renderSubject(date, weekday string) (string, error) {
	subjectTmpl := model.GetSettingValue(s.db, model.CategoryEmail, model.KeyEmailSubject, "{{.Date}} 工作日报")
	appName := model.GetSettingValue(s.db, model.CategoryGeneral, model.KeyGeneralAppName, "日报助手")

	tmpl, err := template.New("subject").Parse(subjectTmpl)
	if err != nil {
		return "", fmt.Errorf("解析主题模板失败: %w", err)
	}

	data := EmailTemplateData{
		Date:    date,
		Weekday: weekday,
		AppName: appName,
		Author:  appName,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("渲染主题模板失败: %w", err)
	}

	return buf.String(), nil
}

// getReportWeekday 根据日期字符串推导星期几
func getReportWeekday(date string) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		return ""
	}

	weekdays := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	return weekdays[int(t.Weekday())]
}

// getBatchReportDisplayDate 获取批量发送时展示用的日期
func getBatchReportDisplayDate(reports []*model.Report) string {
	var latest time.Time

	for _, report := range reports {
		if report == nil {
			continue
		}

		date := strings.TrimSpace(report.Date)
		if date == "" {
			continue
		}

		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}

		if latest.IsZero() || t.After(latest) {
			latest = t
		}
	}

	if latest.IsZero() {
		return time.Now().Format("2006-01-02")
	}

	return latest.Format("2006-01-02")
}

// RenderBody 渲染邮件正文 HTML
func (s *EmailService) RenderBody(report *model.Report) (string, error) {
	appName := model.GetSettingValue(s.db, model.CategoryGeneral, model.KeyGeneralAppName, "日报助手")
	now := time.Now()

	data := EmailTemplateData{
		Date:     report.Date,
		Weekday:  report.Weekday,
		Content:  report.Content,
		Author:   appName,
		AppName:  appName,
		Year:     now.Year(),
		SentAt:   now.Format("2006-01-02 15:04:05"),
		ReportID: report.ID,
	}

	s.mu.Lock()
	tmpl := s.emailTemplate
	s.mu.Unlock()

	// 尝试使用文件模板
	if tmpl != nil {
		if t := tmpl.Lookup("daily_report.html"); t != nil {
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				log.Printf("[邮件服务] 文件模板渲染失败，回退到内置模板: %v\n", err)
			} else {
				return buf.String(), nil
			}
		}
	}

	// 使用内置默认模板
	return s.renderBuiltinTemplate(data)
}

// renderBuiltinTemplate 使用内置 HTML 模板渲染邮件正文
func (s *EmailService) renderBuiltinTemplate(data EmailTemplateData) (string, error) {
	const builtinTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Date}} 工作日报</title>
</head>
<body style="margin:0;padding:0;background-color:#f5f5f5;font-family:'Microsoft YaHei','PingFang SC','Helvetica Neue',Arial,sans-serif;">
<table width="100%" cellpadding="0" cellspacing="0" style="background-color:#f5f5f5;padding:20px 0;">
<tr><td align="center">
<table width="640" cellpadding="0" cellspacing="0" style="background-color:#ffffff;border-radius:8px;overflow:hidden;box-shadow:0 2px 8px rgba(0,0,0,0.08);">
  <!-- 头部 -->
  <tr>
    <td style="background:linear-gradient(135deg,#667eea 0%,#764ba2 100%);padding:24px 32px;">
      <h1 style="margin:0;color:#ffffff;font-size:22px;font-weight:600;">📋 工作日报</h1>
      <p style="margin:8px 0 0;color:rgba(255,255,255,0.85);font-size:14px;">{{.Date}} {{.Weekday}}</p>
    </td>
  </tr>
  <!-- 正文 -->
  <tr>
    <td style="padding:28px 32px;">
      <div style="font-size:15px;line-height:1.8;color:#333333;white-space:pre-wrap;">{{.Content}}</div>
    </td>
  </tr>
  <!-- 分隔线 -->
  <tr>
    <td style="padding:0 32px;">
      <hr style="border:none;border-top:1px solid #eee;margin:0;">
    </td>
  </tr>
  <!-- 底部 -->
  <tr>
    <td style="padding:16px 32px 24px;text-align:center;">
      <p style="margin:0;font-size:12px;color:#999999;">
        由 {{.AppName}} 自动发送 · {{.SentAt}}
      </p>
    </td>
  </tr>
</table>
</td></tr>
</table>
</body>
</html>`

	tmpl, err := template.New("builtin_email").Parse(builtinTemplate)
	if err != nil {
		return "", fmt.Errorf("解析内置模板失败: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("渲染内置模板失败: %w", err)
	}
	return buf.String(), nil
}

// GenerateExcelReport 将日报内容生成 Excel 附件（格式：日期/姓名/当日完成工作）
//
// Excel 格式与样本文件一致：
//   - Sheet 名称：{作者}日报
//   - 第 1 行：表头（日期 / 姓名 / 当日完成工作）
//   - 第 2 行：数据（MM-DD-YY 格式日期 / 作者名 / 工作内容）
//
// 返回值：(Excel字节, 文件名, 错误)
func (s *EmailService) GenerateExcelReport(report *model.Report) ([]byte, string, error) {
	author := model.GetSettingValue(s.db, model.CategoryGeneral, model.KeyGeneralAppName, "日报")

	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("[邮件服务] 关闭 Excel 对象失败: %v\n", err)
		}
	}()

	sheetName := author + "日报"

	// 将默认 Sheet1 重命名
	if err := f.SetSheetName("Sheet1", sheetName); err != nil {
		return nil, "", fmt.Errorf("设置 Sheet 名称失败: %w", err)
	}

	// 写入表头
	headers := []interface{}{"日期", "姓名", "当日完成工作"}
	if err := f.SetSheetRow(sheetName, "A1", &headers); err != nil {
		return nil, "", fmt.Errorf("写入 Excel 表头失败: %w", err)
	}

	// 将日期从 "2006-01-02" 格式转换为中文格式（如：2026年3月24日）
	excelDate := report.Date
	if t, err := time.Parse("2006-01-02", report.Date); err == nil {
		excelDate = t.Format("2006年1月2日")
	}

	// 写入数据行
	row := []interface{}{excelDate, author, report.Content}
	if err := f.SetSheetRow(sheetName, "A2", &row); err != nil {
		return nil, "", fmt.Errorf("写入 Excel 数据行失败: %w", err)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, "", fmt.Errorf("生成 Excel 字节流失败: %w", err)
	}

	filename := fmt.Sprintf("%s-日报-%s.xlsx", author, report.Date)
	log.Printf("[邮件服务] 生成 Excel 附件: %s (%d bytes)\n", filename, buf.Len())
	return buf.Bytes(), filename, nil
}

// GenerateBatchExcelReport 将多条日报内容生成一个 Excel 附件（每条日报一行）
//
// Excel 格式：
//   - Sheet 名称：{作者}日报
//   - 第 1 行：表头（日期 / 姓名 / 当日完成工作）
//   - 第 2~N 行：各条日报数据（按日期升序）
//
// 返回值：(Excel字节, 文件名, 错误)
func (s *EmailService) GenerateBatchExcelReport(reports []*model.Report) ([]byte, string, error) {
	if len(reports) == 0 {
		return nil, "", fmt.Errorf("日报列表为空，无法生成 Excel")
	}

	author := model.GetSettingValue(s.db, model.CategoryGeneral, model.KeyGeneralAppName, "日报")

	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("[邮件服务] 关闭 Excel 对象失败: %v\n", err)
		}
	}()

	sheetName := author + "日报"

	// 将默认 Sheet1 重命名
	if err := f.SetSheetName("Sheet1", sheetName); err != nil {
		return nil, "", fmt.Errorf("设置 Sheet 名称失败: %w", err)
	}

	// 写入表头
	headers := []interface{}{"日期", "姓名", "当日完成工作"}
	if err := f.SetSheetRow(sheetName, "A1", &headers); err != nil {
		return nil, "", fmt.Errorf("写入 Excel 表头失败: %w", err)
	}

	// 写入每条日报数据（每条一行）
	for i, report := range reports {
		excelDate := report.Date
		if t, err := time.Parse("2006-01-02", report.Date); err == nil {
			excelDate = t.Format("2006年1月2日")
		}

		rowNum := i + 2 // 从第 2 行开始（第 1 行是表头）
		cell := fmt.Sprintf("A%d", rowNum)
		row := []interface{}{excelDate, author, report.Content}
		if err := f.SetSheetRow(sheetName, cell, &row); err != nil {
			return nil, "", fmt.Errorf("写入 Excel 第 %d 行失败: %w", rowNum, err)
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, "", fmt.Errorf("生成 Excel 字节流失败: %w", err)
	}

	// 文件名仅使用展示日期，避免出现无效范围或时间串
	displayDate := getBatchReportDisplayDate(reports)
	filename := fmt.Sprintf("%s-日报-%s.xlsx", author, displayDate)

	log.Printf("[邮件服务] 生成批量 Excel 附件: %s (%d 条日报, %d bytes)\n", filename, len(reports), buf.Len())
	return buf.Bytes(), filename, nil
}

// SendBatchReports 批量发送日报邮件（将多条日报合并到一个 Excel 附件中一次性发送）
//   - reports: 要发送的日报列表（所有非草稿日报）
//   - sendType: 发送方式（0=手动，1=自动）
//
// 返回发送日志 ID 和可能的错误
func (s *EmailService) SendBatchReports(reports []*model.Report, sendType int) (uint, error) {
	if len(reports) == 0 {
		return 0, fmt.Errorf("没有需要发送的日报")
	}

	// 过滤掉内容为空的日报
	var validReports []*model.Report
	for _, r := range reports {
		if strings.TrimSpace(r.Content) != "" && r.Content != "待填写" {
			validReports = append(validReports, r)
		}
	}
	if len(validReports) == 0 {
		return 0, fmt.Errorf("所有日报内容均为空，无法发送")
	}

	// 1. 加载 SMTP 配置
	smtpCfg, err := s.GetSMTPConfig()
	if err != nil {
		return 0, fmt.Errorf("SMTP 配置错误: %w", err)
	}

	// 2. 获取收件人
	toList, ccList, err := s.GetRecipients()
	if err != nil {
		return 0, fmt.Errorf("收件人配置错误: %w", err)
	}

	// 3. 渲染邮件主题（批量发送仅展示一个日期）
	displayDate := getBatchReportDisplayDate(validReports)
	subject, err := s.renderSubject(displayDate, getReportWeekday(displayDate))
	if err != nil {
		return 0, fmt.Errorf("渲染邮件主题失败: %w", err)
	}

	// 4. 生成批量 Excel 附件
	excelData, excelFilename, err := s.GenerateBatchExcelReport(validReports)
	if err != nil {
		return 0, fmt.Errorf("生成 Excel 附件失败: %w", err)
	}

	// 5. 收集所有日报 ID 用于日志记录
	reportIDs := make([]string, 0, len(validReports))
	for _, r := range validReports {
		reportIDs = append(reportIDs, fmt.Sprintf("%d", r.ID))
	}

	// 6. 创建发送日志记录（状态：发送中）
	now := time.Now()
	emailLog := &model.EmailLog{
		ReportID:   &validReports[0].ID, // 关联第一条日报
		Subject:    subject,
		Recipients: strings.Join(toList, ","),
		CcList:     strings.Join(ccList, ","),
		Content:    fmt.Sprintf("[批量 Excel 附件] %s (共 %d 条日报, IDs: %s)", excelFilename, len(validReports), strings.Join(reportIDs, ",")),
		Status:     model.EmailStatusSending,
		SendType:   sendType,
		SentAt:     &now,
	}
	if err := s.db.Create(emailLog).Error; err != nil {
		return 0, fmt.Errorf("创建发送日志失败: %w", err)
	}

	// 7. 构建邮件消息（无正文，仅 Excel 附件）
	msg := &EmailMessage{
		To:      toList,
		Cc:      ccList,
		Subject: subject,
		Body:    "",
		Attachments: []Attachment{
			{
				Filename:    excelFilename,
				ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
				Data:        excelData,
			},
		},
	}

	// 8. 通过 SMTP 发送
	sendErr := s.sendSMTP(smtpCfg, msg)

	// 9. 更新发送日志和日报状态
	if sendErr != nil {
		s.db.Model(emailLog).Updates(map[string]interface{}{
			"status":    model.EmailStatusFailed,
			"error_msg": sendErr.Error(),
		})
		log.Printf("[邮件服务] 批量发送失败 (%d 条日报): %v\n", len(validReports), sendErr)
		return emailLog.ID, fmt.Errorf("邮件发送失败: %w", sendErr)
	}

	// 发送成功，更新日志状态
	s.db.Model(emailLog).Updates(map[string]interface{}{
		"status":  model.EmailStatusSuccess,
		"sent_at": now,
	})

	// 更新所有日报状态为已发送
	sentAt := time.Now()
	for _, r := range validReports {
		s.db.Model(r).Updates(map[string]interface{}{
			"status":  model.ReportStatusSent,
			"sent_at": &sentAt,
		})
	}

	log.Printf("[邮件服务] 批量发送成功 (%d 条日报, to=%v)\n", len(validReports), toList)
	return emailLog.ID, nil
}

// SendReport 发送日报邮件（核心方法）
//   - report: 要发送的日报记录
//   - sendType: 发送方式（0=手动，1=自动）
//
// 返回发送日志 ID 和可能的错误
func (s *EmailService) SendReport(report *model.Report, sendType int) (uint, error) {
	if report == nil {
		return 0, fmt.Errorf("日报记录不能为空")
	}
	if strings.TrimSpace(report.Content) == "" {
		return 0, fmt.Errorf("日报内容为空，无法发送")
	}

	// 1. 加载 SMTP 配置
	smtpCfg, err := s.GetSMTPConfig()
	if err != nil {
		return 0, fmt.Errorf("SMTP 配置错误: %w", err)
	}

	// 2. 获取收件人
	toList, ccList, err := s.GetRecipients()
	if err != nil {
		return 0, fmt.Errorf("收件人配置错误: %w", err)
	}

	// 3. 渲染邮件主题
	subject, err := s.RenderSubject(report)
	if err != nil {
		return 0, fmt.Errorf("渲染邮件主题失败: %w", err)
	}

	// 4. 生成 Excel 附件（替代 HTML 正文）
	excelData, excelFilename, err := s.GenerateExcelReport(report)
	if err != nil {
		return 0, fmt.Errorf("生成 Excel 附件失败: %w", err)
	}

	// 5. 创建发送日志记录（状态：发送中）
	now := time.Now()
	emailLog := &model.EmailLog{
		ReportID:   &report.ID,
		Subject:    subject,
		Recipients: strings.Join(toList, ","),
		CcList:     strings.Join(ccList, ","),
		Content:    "[Excel 附件] " + excelFilename, // 记录附件名而非 HTML
		Status:     model.EmailStatusSending,
		SendType:   sendType,
		SentAt:     &now,
	}
	if err := s.db.Create(emailLog).Error; err != nil {
		return 0, fmt.Errorf("创建发送日志失败: %w", err)
	}

	// 6. 构建邮件消息（无正文，仅 Excel 附件）
	msg := &EmailMessage{
		To:      toList,
		Cc:      ccList,
		Subject: subject,
		Body:    "", // 不需要邮件正文
		Attachments: []Attachment{
			{
				Filename:    excelFilename,
				ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
				Data:        excelData,
			},
		},
	}

	// 7. 通过 SMTP 发送
	sendErr := s.sendSMTP(smtpCfg, msg)

	// 8. 更新发送日志和日报状态
	if sendErr != nil {
		// 发送失败
		s.db.Model(emailLog).Updates(map[string]interface{}{
			"status":    model.EmailStatusFailed,
			"error_msg": sendErr.Error(),
		})
		log.Printf("[邮件服务] 发送失败 (report_id=%d): %v\n", report.ID, sendErr)
		return emailLog.ID, fmt.Errorf("邮件发送失败: %w", sendErr)
	}

	// 发送成功
	s.db.Model(emailLog).Updates(map[string]interface{}{
		"status":  model.EmailStatusSuccess,
		"sent_at": now,
	})

	// 更新日报状态为已发送
	sentAt := time.Now()
	s.db.Model(report).Updates(map[string]interface{}{
		"status":  model.ReportStatusSent,
		"sent_at": &sentAt,
	})

	log.Printf("[邮件服务] 发送成功 (report_id=%d, to=%v)\n", report.ID, toList)
	return emailLog.ID, nil
}

// SendCustom 发送自定义邮件（公共方法，供外部控制器调用）
func (s *EmailService) SendCustom(cfg *SMTPConfig, msg *EmailMessage) error {
	if cfg == nil {
		return fmt.Errorf("SMTP 配置不能为空")
	}
	if msg == nil {
		return fmt.Errorf("邮件消息不能为空")
	}
	if len(msg.To) == 0 {
		return fmt.Errorf("收件人列表不能为空")
	}
	return s.sendSMTP(cfg, msg)
}

// sendSMTP 底层 SMTP 发送实现
func (s *EmailService) sendSMTP(cfg *SMTPConfig, msg *EmailMessage) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	// 构建邮件头和正文
	mailBody := s.buildMIMEMessage(cfg, msg)

	// 收集所有收件人地址（To + Cc）
	allRecipients := make([]string, 0, len(msg.To)+len(msg.Cc))
	allRecipients = append(allRecipients, msg.To...)
	allRecipients = append(allRecipients, msg.Cc...)

	// 认证信息
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)

	if cfg.UseTLS || cfg.Port == 465 {
		// SSL/TLS 直连（端口 465）
		return s.sendWithTLS(addr, auth, cfg, mailBody, allRecipients)
	}

	if cfg.Port == 587 {
		// STARTTLS（端口 587）
		return s.sendWithSTARTTLS(addr, auth, cfg, mailBody, allRecipients)
	}

	// 普通发送
	return smtp.SendMail(addr, auth, cfg.FromAddr, allRecipients, mailBody)
}

// sendWithTLS 使用 TLS 直连发送（端口 465）
func (s *EmailService) sendWithTLS(addr string, auth smtp.Auth, cfg *SMTPConfig, body []byte, recipients []string) error {
	tlsConfig := &tls.Config{
		ServerName: cfg.Host,
		MinVersion: tls.VersionTLS12,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS 连接失败: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}
	defer client.Close()

	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP 认证失败: %w", err)
	}

	if err := client.Mail(cfg.FromAddr); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}

	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("设置收件人 %s 失败: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("打开数据写入失败: %w", err)
	}

	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("关闭数据写入失败: %w", err)
	}

	return client.Quit()
}

// sendWithSTARTTLS 使用 STARTTLS 发送（端口 587）
func (s *EmailService) sendWithSTARTTLS(addr string, auth smtp.Auth, cfg *SMTPConfig, body []byte, recipients []string) error {
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	defer client.Close()

	// 发送 EHLO
	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("EHLO 失败: %w", err)
	}

	// 启用 STARTTLS
	tlsConfig := &tls.Config{
		ServerName: cfg.Host,
		MinVersion: tls.VersionTLS12,
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS 失败: %w", err)
		}
	}

	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP 认证失败: %w", err)
	}

	if err := client.Mail(cfg.FromAddr); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}

	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("设置收件人 %s 失败: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("打开数据写入失败: %w", err)
	}

	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("关闭数据写入失败: %w", err)
	}

	return client.Quit()
}

// buildMIMEMessage 构建 MIME 格式的邮件消息
//
// - 无附件时：Content-Type: text/html
// - 有附件时：Content-Type: multipart/mixed（正文为空则省略正文 part）
func (s *EmailService) buildMIMEMessage(cfg *SMTPConfig, msg *EmailMessage) []byte {
	var buf bytes.Buffer

	// ——— 公共邮件头 ———
	fromHeader := fmt.Sprintf("%s <%s>", cfg.FromName, cfg.FromAddr)
	buf.WriteString(fmt.Sprintf("From: %s\r\n", fromHeader))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(msg.To, ", ")))
	if len(msg.Cc) > 0 {
		buf.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(msg.Cc, ", ")))
	}
	buf.WriteString(fmt.Sprintf("Subject: =?UTF-8?B?%s?=\r\n",
		base64.StdEncoding.EncodeToString([]byte(msg.Subject))))
	buf.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	buf.WriteString("MIME-Version: 1.0\r\n")

	if len(msg.Attachments) == 0 {
		// ——— 纯 HTML 邮件（无附件）———
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: base64\r\n")
		buf.WriteString("\r\n")
		writeBase64Lines(&buf, []byte(msg.Body))
		return buf.Bytes()
	}

	// ——— multipart/mixed（带附件）———
	boundary := fmt.Sprintf("daily_report_%d", time.Now().UnixNano())
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
	buf.WriteString("\r\n")

	// 正文 part（仅在非空时写入）
	if strings.TrimSpace(msg.Body) != "" {
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: base64\r\n")
		buf.WriteString("\r\n")
		writeBase64Lines(&buf, []byte(msg.Body))
		buf.WriteString("\r\n")
	}

	// 附件 parts
	for _, att := range msg.Attachments {
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString(fmt.Sprintf("Content-Type: %s\r\n", att.ContentType))
		// 使用 RFC 2047 encoded-word 编码中文文件名，兼容主流邮件客户端
		encodedName := fmt.Sprintf("=?UTF-8?B?%s?=",
			base64.StdEncoding.EncodeToString([]byte(att.Filename)))
		buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", encodedName))
		buf.WriteString("Content-Transfer-Encoding: base64\r\n")
		buf.WriteString("\r\n")
		writeBase64Lines(&buf, att.Data)
		buf.WriteString("\r\n")
	}

	// 结束边界
	buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	return buf.Bytes()
}

// writeBase64Lines 将 data 以 Base64 编码写入 buf，每 76 字符换行（RFC 2045）
func writeBase64Lines(buf *bytes.Buffer, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		buf.WriteString(encoded[i:end])
		buf.WriteString("\r\n")
	}
}

// TestSMTP 测试 SMTP 连接和认证
func (s *EmailService) TestSMTP() error {
	cfg, err := s.GetSMTPConfig()
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	if cfg.UseTLS || cfg.Port == 465 {
		tlsConfig := &tls.Config{
			ServerName: cfg.Host,
			MinVersion: tls.VersionTLS12,
		}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("TLS 连接失败: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return fmt.Errorf("创建客户端失败: %w", err)
		}
		defer client.Close()

		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("认证失败: %w", err)
		}

		return client.Quit()
	}

	// STARTTLS 或普通连接
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer client.Close()

	if cfg.Port == 587 {
		tlsConfig := &tls.Config{
			ServerName: cfg.Host,
			MinVersion: tls.VersionTLS12,
		}
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS 失败: %w", err)
			}
		}
	}

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("认证失败: %w", err)
	}

	return client.Quit()
}

// SendTestEmail 发送测试邮件
func (s *EmailService) SendTestEmail(to string) error {
	cfg, err := s.GetSMTPConfig()
	if err != nil {
		return err
	}

	msg := &EmailMessage{
		To:      []string{to},
		Subject: "日报系统 - 测试邮件",
		Body: fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="UTF-8"></head>
<body style="font-family:'Microsoft YaHei',sans-serif;padding:40px;background:#f5f5f5;">
<div style="max-width:600px;margin:0 auto;background:#fff;border-radius:8px;padding:32px;box-shadow:0 2px 8px rgba(0,0,0,0.08);">
<h2 style="color:#667eea;margin-top:0;">✅ 测试邮件发送成功</h2>
<p>如果您看到这封邮件，说明 SMTP 配置正确。</p>
<hr style="border:none;border-top:1px solid #eee;">
<p style="color:#999;font-size:12px;">发送时间：%s<br>SMTP 服务器：%s:%d</p>
</div>
</body></html>`, time.Now().Format("2006-01-02 15:04:05"), cfg.Host, cfg.Port),
	}

	return s.sendSMTP(cfg, msg)
}

// GetSendLogs 获取邮件发送日志列表
func (s *EmailService) GetSendLogs(page, pageSize int) ([]model.EmailLog, int64, error) {
	var logs []model.EmailLog
	var total int64

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	s.db.Model(&model.EmailLog{}).Count(&total)

	err := s.db.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&logs).Error

	return logs, total, err
}

// GetSendLogByID 根据 ID 获取发送日志
func (s *EmailService) GetSendLogByID(id uint) (*model.EmailLog, error) {
	var log model.EmailLog
	err := s.db.First(&log, id).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}

// GetSendLogsByReportID 获取某个日报的所有发送记录
func (s *EmailService) GetSendLogsByReportID(reportID uint) ([]model.EmailLog, error) {
	var logs []model.EmailLog
	err := s.db.Where("report_id = ?", reportID).
		Order("created_at DESC").
		Find(&logs).Error
	return logs, err
}

// GetRecentLogs 获取最近的发送记录
func (s *EmailService) GetRecentLogs(limit int) ([]model.EmailLog, error) {
	var logs []model.EmailLog
	if limit <= 0 {
		limit = 10
	}
	err := s.db.Order("created_at DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// GetSendStats 获取发送统计信息
func (s *EmailService) GetSendStats() (total int64, success int64, failed int64, err error) {
	s.db.Model(&model.EmailLog{}).Count(&total)
	s.db.Model(&model.EmailLog{}).Where("status = ?", model.EmailStatusSuccess).Count(&success)
	s.db.Model(&model.EmailLog{}).Where("status = ?", model.EmailStatusFailed).Count(&failed)
	return
}

// ==================== 工具函数 ====================

// splitAndTrim 按逗号分隔字符串并去除空白
func splitAndTrim(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// base64Encode 将字符串编码为 Base64
func base64Encode(s string) string {
	const base64Table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	src := []byte(s)
	var buf bytes.Buffer

	for i := 0; i < len(src); i += 3 {
		var b0, b1, b2 byte
		b0 = src[i]
		if i+1 < len(src) {
			b1 = src[i+1]
		}
		if i+2 < len(src) {
			b2 = src[i+2]
		}

		buf.WriteByte(base64Table[b0>>2])
		buf.WriteByte(base64Table[((b0&0x03)<<4)|(b1>>4)])

		if i+1 < len(src) {
			buf.WriteByte(base64Table[((b1&0x0F)<<2)|(b2>>6)])
		} else {
			buf.WriteByte('=')
		}

		if i+2 < len(src) {
			buf.WriteByte(base64Table[b2&0x3F])
		} else {
			buf.WriteByte('=')
		}
	}

	return buf.String()
}
