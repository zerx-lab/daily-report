package controller

import (
	"bytes"
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zerx-lab/daily-report/internal/model"
	"github.com/zerx-lab/daily-report/internal/service"
	"gorm.io/gorm"
)

// OutingController 外出申请控制器
type OutingController struct {
	db        *gorm.DB
	outingSvc *service.OutingService
	emailSvc  *service.EmailService
	siyuanSvc *service.SiyuanService
}

// NewOutingController 创建外出申请控制器实例
func NewOutingController(db *gorm.DB, outingSvc *service.OutingService, emailSvc *service.EmailService, siyuanSvc *service.SiyuanService) *OutingController {
	return &OutingController{
		db:        db,
		outingSvc: outingSvc,
		emailSvc:  emailSvc,
		siyuanSvc: siyuanSvc,
	}
}

// ==================== 列表页面 ====================

// List 外出申请列表页面
func (c *OutingController) List(ctx *gin.Context) {
	var query model.OutingListQuery
	if err := ctx.ShouldBindQuery(&query); err != nil {
		log.Printf("[外出申请] 绑定查询参数失败: %v\n", err)
	}
	query.Normalize()

	outings, pagination, err := c.outingSvc.List(&query)
	if err != nil {
		log.Printf("[外出申请] 查询列表失败: %v\n", err)
		ctx.HTML(http.StatusInternalServerError, "error.html", gin.H{
			"title":   "错误",
			"message": "查询外出申请列表失败",
			"error":   err.Error(),
		})
		return
	}

	ctx.HTML(http.StatusOK, "outings.html", gin.H{
		"title":      "外出申请",
		"active":     "outings",
		"outings":    outings,
		"pagination": pagination,
		"query":      query,
	})
}

// ==================== 创建外出申请 ====================

// CreateForm 新建外出申请表单页面
func (c *OutingController) CreateForm(ctx *gin.Context) {
	// 计算默认时间：外出时间取当前整点，返回时间取当天 18:00
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)
	outTime := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, loc)
	returnTime := time.Date(now.Year(), now.Month(), now.Day(), 18, 0, 0, 0, loc)

	ctx.HTML(http.StatusOK, "outing_edit.html", gin.H{
		"title":      "新建外出申请",
		"active":     "outings",
		"isNew":      true,
		"outTime":    outTime.Format("2006-01-02T15:04"),
		"returnTime": returnTime.Format("2006-01-02T15:04"),
	})
}

// Create 创建外出申请（POST）
func (c *OutingController) Create(ctx *gin.Context) {
	// 申请人和部门从设置中获取，不由表单提交
	applicant := model.GetSettingValue(c.db, model.CategoryOuting, model.KeyOutingApplicant, "")
	department := model.GetSettingValue(c.db, model.CategoryOuting, model.KeyOutingDepartment, "")
	outTimeStr := strings.TrimSpace(ctx.PostForm("out_time"))
	returnTimeStr := strings.TrimSpace(ctx.PostForm("return_time"))
	destination := strings.TrimSpace(ctx.PostForm("destination"))
	reason := strings.TrimSpace(ctx.PostForm("reason"))
	remarks := strings.TrimSpace(ctx.PostForm("remarks"))

	// 解析外出时间
	loc, _ := time.LoadLocation("Asia/Shanghai")
	outTime, err := time.ParseInLocation("2006-01-02T15:04", outTimeStr, loc)
	if err != nil {
		c.flashAndRedirect(ctx, "error", "外出时间格式无效", "/outings/new")
		return
	}

	// 解析返回时间
	returnTime, err := time.ParseInLocation("2006-01-02T15:04", returnTimeStr, loc)
	if err != nil {
		c.flashAndRedirect(ctx, "error", "返回时间格式无效", "/outings/new")
		return
	}

	req := &model.OutingRequest{
		Applicant:   applicant,
		Department:  department,
		OutTime:     outTime,
		ReturnTime:  returnTime,
		Destination: destination,
		Reason:      reason,
		Remarks:     remarks,
	}

	outing, err := c.outingSvc.Create(req)
	if err != nil {
		log.Printf("[外出申请] 创建失败: %v\n", err)
		c.flashAndRedirect(ctx, "error", "创建外出申请失败: "+err.Error(), "/outings/new")
		return
	}

	// 异步同步到思源笔记
	if c.siyuanSvc != nil {
		go func(id uint) {
			if err := c.siyuanSvc.SyncOutingToSiyuan(id); err != nil {
				log.Printf("[外出申请] 同步思源笔记失败(异步): %v\n", err)
			}
		}(outing.ID)
	}

	c.flashAndRedirect(ctx, "success", "外出申请创建成功", fmt.Sprintf("/outings/%d/edit", outing.ID))
}

// ==================== 编辑外出申请 ====================

// EditForm 编辑外出申请表单页面
func (c *OutingController) EditForm(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		c.flashAndRedirect(ctx, "error", "无效的外出申请 ID", "/outings")
		return
	}

	outing, err := c.outingSvc.GetByID(id)
	if err != nil {
		c.flashAndRedirect(ctx, "error", "外出申请不存在", "/outings")
		return
	}

	ctx.HTML(http.StatusOK, "outing_edit.html", gin.H{
		"title":           fmt.Sprintf("编辑外出申请 - %s", outing.Applicant),
		"active":          "outings",
		"isNew":           false,
		"outing":          outing,
		"outTimeValue":    outing.OutTime.Format("2006-01-02T15:04"),
		"returnTimeValue": outing.ReturnTime.Format("2006-01-02T15:04"),
	})
}

// Update 更新外出申请（POST）
func (c *OutingController) Update(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		c.flashAndRedirect(ctx, "error", "无效的外出申请 ID", "/outings")
		return
	}

	// 申请人和部门从设置中获取，不由表单提交
	applicant := model.GetSettingValue(c.db, model.CategoryOuting, model.KeyOutingApplicant, "")
	department := model.GetSettingValue(c.db, model.CategoryOuting, model.KeyOutingDepartment, "")
	outTimeStr := strings.TrimSpace(ctx.PostForm("out_time"))
	returnTimeStr := strings.TrimSpace(ctx.PostForm("return_time"))
	destination := strings.TrimSpace(ctx.PostForm("destination"))
	reason := strings.TrimSpace(ctx.PostForm("reason"))
	remarks := strings.TrimSpace(ctx.PostForm("remarks"))

	// 解析外出时间
	loc, _ := time.LoadLocation("Asia/Shanghai")
	outTime, err := time.ParseInLocation("2006-01-02T15:04", outTimeStr, loc)
	if err != nil {
		c.flashAndRedirect(ctx, "error", "外出时间格式无效", fmt.Sprintf("/outings/%d/edit", id))
		return
	}

	// 解析返回时间
	returnTime, err := time.ParseInLocation("2006-01-02T15:04", returnTimeStr, loc)
	if err != nil {
		c.flashAndRedirect(ctx, "error", "返回时间格式无效", fmt.Sprintf("/outings/%d/edit", id))
		return
	}

	req := &model.OutingRequest{
		Applicant:   applicant,
		Department:  department,
		OutTime:     outTime,
		ReturnTime:  returnTime,
		Destination: destination,
		Reason:      reason,
		Remarks:     remarks,
	}

	if _, err := c.outingSvc.Update(id, req); err != nil {
		log.Printf("[外出申请] 更新失败: %v\n", err)
		c.flashAndRedirect(ctx, "error", "更新外出申请失败: "+err.Error(), fmt.Sprintf("/outings/%d/edit", id))
		return
	}

	// 异步同步到思源笔记
	if c.siyuanSvc != nil {
		go func(outingID uint) {
			if err := c.siyuanSvc.SyncOutingToSiyuan(outingID); err != nil {
				log.Printf("[外出申请] 同步思源笔记失败(异步): %v\n", err)
			}
		}(id)
	}

	c.flashAndRedirect(ctx, "success", "外出申请更新成功", fmt.Sprintf("/outings/%d/edit", id))
}

// ==================== 删除外出申请 ====================

// Delete 删除外出申请（POST）
func (c *OutingController) Delete(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		c.jsonError(ctx, http.StatusBadRequest, "无效的外出申请 ID")
		return
	}

	// 删除前先获取思源 ID，用于异步删除思源侧记录
	outing, _ := c.outingSvc.GetByID(id)

	if err := c.outingSvc.Delete(id); err != nil {
		c.jsonError(ctx, http.StatusInternalServerError, "删除外出申请失败: "+err.Error())
		return
	}

	// 异步删除思源笔记中的对应行
	if c.siyuanSvc != nil && outing != nil && outing.SiyuanID != "" {
		go func(siyuanID string) {
			if err := c.siyuanSvc.DeleteOutingEntry(siyuanID); err != nil {
				log.Printf("[外出申请] 删除思源笔记条目失败(异步): %v\n", err)
			}
		}(outing.SiyuanID)
	}

	if c.isAjax(ctx) {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "删除成功",
		})
	} else {
		c.flashAndRedirect(ctx, "success", "外出申请已删除", "/outings")
	}
}

// ==================== 发送外出申请邮件 ====================

// Send 发送外出申请邮件（POST）
func (c *OutingController) Send(ctx *gin.Context) {
	id, err := c.parseID(ctx)
	if err != nil {
		c.jsonOrFlash(ctx, http.StatusBadRequest, "error", "无效的 ID", "/outings")
		return
	}

	outing, err := c.outingSvc.GetByID(id)
	if err != nil {
		c.jsonOrFlash(ctx, http.StatusNotFound, "error", "外出申请不存在", "/outings")
		return
	}

	// 1. 获取外出申请独立的收件人配置
	toStr := model.GetSettingValue(c.db, model.CategoryOuting, model.KeyOutingRecipients, "")
	ccStr := model.GetSettingValue(c.db, model.CategoryOuting, model.KeyOutingCc, "")
	toList := splitAndTrim(toStr)
	ccList := splitAndTrim(ccStr)
	if len(toList) == 0 {
		c.jsonOrFlash(ctx, http.StatusBadRequest, "error", "未配置外出申请收件人，请在系统设置中配置", "/outings")
		return
	}

	// 2. 获取 SMTP 配置（共用日报的 SMTP）
	smtpCfg, err := c.emailSvc.GetSMTPConfig()
	if err != nil {
		c.jsonOrFlash(ctx, http.StatusInternalServerError, "error", "SMTP 配置错误: "+err.Error(), fmt.Sprintf("/outings/%d/edit", id))
		return
	}

	// 3. 渲染邮件主题
	subjectTmpl := model.GetSettingValue(c.db, model.CategoryOuting, model.KeyOutingSubject, "外出申请 - {{.Applicant}} {{.OutDate}}")
	subject := renderOutingSubject(subjectTmpl, outing)

	// 4. 渲染邮件正文（HTML 表格）
	body := renderOutingEmailBody(outing)

	// 5. 创建邮件日志
	now := time.Now()
	emailLog := &model.EmailLog{
		ReportID:   0, // 外出申请不关联日报
		Subject:    subject,
		Recipients: strings.Join(toList, ","),
		CcList:     strings.Join(ccList, ","),
		Content:    body,
		Status:     model.EmailStatusSending,
		SendType:   model.EmailSendTypeManual,
		SentAt:     &now,
	}
	c.db.Create(emailLog)

	// 6. 发送邮件
	msg := &service.EmailMessage{
		To:      toList,
		Cc:      ccList,
		Subject: subject,
		Body:    body,
	}

	sendErr := c.emailSvc.SendCustom(smtpCfg, msg)

	if sendErr != nil {
		// 发送失败：更新日志和状态
		c.db.Model(emailLog).Updates(map[string]interface{}{
			"status":    model.EmailStatusFailed,
			"error_msg": sendErr.Error(),
		})
		c.outingSvc.UpdateStatus(id, model.OutingStatusFailed)
		log.Printf("[外出申请] 邮件发送失败(id=%d): %v\n", id, sendErr)
		c.jsonOrFlash(ctx, http.StatusInternalServerError, "error", "发送失败: "+sendErr.Error(), fmt.Sprintf("/outings/%d/edit", id))
		return
	}

	// 发送成功：更新日志和状态
	c.db.Model(emailLog).Updates(map[string]interface{}{
		"status":  model.EmailStatusSuccess,
		"sent_at": now,
	})
	c.outingSvc.UpdateStatus(id, model.OutingStatusSent)
	log.Printf("[外出申请] 邮件发送成功(id=%d, to=%v)\n", id, toList)

	c.jsonOrFlash(ctx, http.StatusOK, "success", "外出申请邮件发送成功", fmt.Sprintf("/outings/%d/edit", id))
}

// ==================== 邮件渲染 ====================

// outingSubjectData 邮件主题模板渲染数据
type outingSubjectData struct {
	Applicant   string // 申请人
	Department  string // 部门
	Destination string // 外出地点
	Reason      string // 外出事由
	OutDate     string // 外出日期（格式化后，如 2026-03-20）
}

// renderOutingSubject 使用 text/template 渲染邮件主题
func renderOutingSubject(tmpl string, outing *model.OutingRequest) string {
	t, err := template.New("subject").Parse(tmpl)
	if err != nil {
		log.Printf("[外出申请] 解析主题模板失败: %v，使用默认主题\n", err)
		return fmt.Sprintf("外出申请 - %s %s", outing.Applicant, outing.OutTime.Format("2006-01-02"))
	}

	data := outingSubjectData{
		Applicant:   outing.Applicant,
		Department:  outing.Department,
		Destination: outing.Destination,
		Reason:      outing.Reason,
		OutDate:     outing.OutTime.Format("2006-01-02"),
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		log.Printf("[外出申请] 渲染主题模板失败: %v，使用默认主题\n", err)
		return fmt.Sprintf("外出申请 - %s %s", outing.Applicant, outing.OutTime.Format("2006-01-02"))
	}

	return buf.String()
}

// renderOutingEmailBody 生成外出申请 HTML 邮件正文（内联样式，兼容邮件客户端）
func renderOutingEmailBody(outing *model.OutingRequest) string {
	// 转义用户输入，防止 XSS
	applicant := html.EscapeString(outing.Applicant)
	department := html.EscapeString(outing.Department)
	outTime := html.EscapeString(outing.FormatOutTime())
	returnTime := html.EscapeString(outing.FormatReturnTime())
	destination := html.EscapeString(outing.Destination)
	reason := html.EscapeString(outing.Reason)
	remarks := html.EscapeString(outing.Remarks)

	// 橙色高亮色
	const accentColor = "#E88B00"
	// 表头背景色
	const headerBg = "#FFF8EE"
	// 边框色
	const borderColor = "#D9D9D9"

	var buf bytes.Buffer

	// 邮件外层容器
	buf.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body style="margin:0; padding:0; background-color:#F5F5F5; font-family:'Microsoft YaHei','PingFang SC','Helvetica Neue',Arial,sans-serif;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color:#F5F5F5;">
<tr><td align="center" style="padding:30px 20px;">

<!-- 邮件内容卡片 -->
<table role="presentation" width="640" cellpadding="0" cellspacing="0" style="background-color:#FFFFFF; border-radius:8px; box-shadow:0 2px 8px rgba(0,0,0,0.08);">

<!-- 标题区域 -->
<tr>
<td style="padding:28px 32px 20px 32px; text-align:center; border-bottom:2px solid `)
	buf.WriteString(accentColor)
	buf.WriteString(`;">
<h2 style="margin:0; font-size:22px; font-weight:normal; color:#333333;">
<span style="font-weight:bold; color:`)
	buf.WriteString(accentColor)
	buf.WriteString(`;">外出</span>申请表
</h2>
</td>
</tr>

<!-- 表格内容区域 -->
<tr>
<td style="padding:24px 32px 32px 32px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse; border:1px solid `)
	buf.WriteString(borderColor)
	buf.WriteString(`;">

<!-- 第1行：申请人 / 部门 -->
<tr>
<td style="width:25%; padding:12px 16px; border:1px solid `)
	buf.WriteString(borderColor)
	buf.WriteString(`; background-color:`)
	buf.WriteString(headerBg)
	buf.WriteString(`; color:`)
	buf.WriteString(accentColor)
	buf.WriteString(`; font-size:14px; font-weight:bold; text-align:center; white-space:nowrap;">申请人</td>
<td style="width:25%; padding:12px 16px; border:1px solid `)
	buf.WriteString(borderColor)
	buf.WriteString(`; font-size:14px; color:#333333; text-align:center;">`)
	buf.WriteString(applicant)
	buf.WriteString(`</td>
<td style="width:25%; padding:12px 16px; border:1px solid `)
	buf.WriteString(borderColor)
	buf.WriteString(`; background-color:`)
	buf.WriteString(headerBg)
	buf.WriteString(`; color:`)
	buf.WriteString(accentColor)
	buf.WriteString(`; font-size:14px; font-weight:bold; text-align:center; white-space:nowrap;">部门</td>
<td style="width:25%; padding:12px 16px; border:1px solid `)
	buf.WriteString(borderColor)
	buf.WriteString(`; font-size:14px; color:#333333; text-align:center;">`)
	buf.WriteString(department)
	buf.WriteString(`</td>
</tr>

`)

	// 后续行使用辅助函数统一生成（标签列 + 值列跨3列）
	rows := []struct {
		label string
		value string
	}{
		{"申请外出时间", outTime},
		{"预计返回时间", returnTime},
		{"外出地点", destination},
		{"外出事由", reason},
		{"备注说明", remarks},
	}

	for _, row := range rows {
		buf.WriteString(`<tr>
<td style="width:25%; padding:12px 16px; border:1px solid `)
		buf.WriteString(borderColor)
		buf.WriteString(`; background-color:`)
		buf.WriteString(headerBg)
		buf.WriteString(`; color:`)
		buf.WriteString(accentColor)
		buf.WriteString(`; font-size:14px; font-weight:bold; text-align:center; white-space:nowrap;">`)
		buf.WriteString(row.label)
		buf.WriteString(`</td>
<td colspan="3" style="padding:12px 16px; border:1px solid `)
		buf.WriteString(borderColor)
		buf.WriteString(`; font-size:14px; color:#333333; line-height:1.6;">`)
		// 事由和备注可能含有换行，将换行转为 <br>
		val := strings.ReplaceAll(row.value, "\n", "<br>")
		buf.WriteString(val)
		buf.WriteString(`</td>
</tr>
`)
	}

	// 关闭表格和容器
	buf.WriteString(`
</table>
</td>
</tr>

<!-- 底部提示 -->
<tr>
<td style="padding:0 32px 24px 32px;">
<p style="margin:0; font-size:12px; color:#999999; text-align:center; line-height:1.6;">
</p>
</td>
</tr>

</table>
<!-- /邮件内容卡片 -->

</td></tr>
</table>
</body>
</html>`)

	return buf.String()
}

// ==================== 辅助方法 ====================

// parseID 从 URL 路径参数中解析 ID
func (c *OutingController) parseID(ctx *gin.Context) (uint, error) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("无效的 ID: %s", idStr)
	}
	return uint(id), nil
}

// isAjax 判断是否为 AJAX 请求
func (c *OutingController) isAjax(ctx *gin.Context) bool {
	return ctx.GetHeader("X-Requested-With") == "XMLHttpRequest" ||
		strings.Contains(ctx.GetHeader("Accept"), "application/json") ||
		ctx.Query("format") == "json"
}

// flashAndRedirect 设置闪存消息并重定向
func (c *OutingController) flashAndRedirect(ctx *gin.Context, level, message, url string) {
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	redirectURL := fmt.Sprintf("%s%sflash_level=%s&flash_msg=%s", url, sep, level, message)
	ctx.Redirect(http.StatusFound, redirectURL)
}

// jsonError 返回 JSON 格式的错误响应
func (c *OutingController) jsonError(ctx *gin.Context, code int, message string) {
	ctx.JSON(code, gin.H{
		"code":    -1,
		"message": message,
	})
}

// jsonOrFlash 根据请求类型返回 JSON 或重定向
func (c *OutingController) jsonOrFlash(ctx *gin.Context, httpCode int, level, message, redirectURL string) {
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

// splitAndTrim 按逗号分割字符串并去除空白项
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
