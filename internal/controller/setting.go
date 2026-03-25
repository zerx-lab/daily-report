package controller

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zerx-lab/daily-report/internal/model"
	"github.com/zerx-lab/daily-report/internal/service"
	"gorm.io/gorm"
)

// SettingController 系统设置控制器
type SettingController struct {
	db        *gorm.DB
	emailSvc  *service.EmailService
	siyuanSvc *service.SiyuanService
	scheduler *service.Scheduler
}

// NewSettingController 创建设置控制器实例
func NewSettingController(
	db *gorm.DB,
	emailSvc *service.EmailService,
	siyuanSvc *service.SiyuanService,
	scheduler *service.Scheduler,
) *SettingController {
	return &SettingController{
		db:        db,
		emailSvc:  emailSvc,
		siyuanSvc: siyuanSvc,
		scheduler: scheduler,
	}
}

// ==================== 设置首页 ====================

// Index 设置首页（汇总所有设置分类入口）
func (c *SettingController) Index(ctx *gin.Context) {
	// 加载各分类当前设置值
	smtpSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategorySMTP)
	siyuanSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategorySiyuan)
	emailSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategoryEmail)
	generalSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategoryGeneral)
	scheduleSettings, _ := model.GetSettingsMapByCategory(c.db, model.CategorySchedule)

	flash := extractFlash(ctx)

	ctx.HTML(http.StatusOK, "settings.html", gin.H{
		"title":    "系统设置",
		"active":   "settings",
		"smtp":     smtpSettings,
		"siyuan":   siyuanSettings,
		"email":    emailSettings,
		"general":  generalSettings,
		"schedule": scheduleSettings,
		"flash":    flash,
	})
}

// ==================== SMTP 设置 ====================

// SMTP 显示 SMTP 设置页面
func (c *SettingController) SMTP(ctx *gin.Context) {
	settings, _ := model.GetSettingsMapByCategory(c.db, model.CategorySMTP)
	flash := extractFlash(ctx)

	ctx.HTML(http.StatusOK, "settings.html", gin.H{
		"title":  "SMTP 邮件设置",
		"active": "settings",
		"tab":    "smtp",
		"smtp":   settings,
		"flash":  flash,
	})
}

// SaveSMTP 保存 SMTP 设置
func (c *SettingController) SaveSMTP(ctx *gin.Context) {
	kvPairs := map[string]string{
		model.KeySMTPHost:     strings.TrimSpace(ctx.PostForm("host")),
		model.KeySMTPPort:     strings.TrimSpace(ctx.PostForm("port")),
		model.KeySMTPUsername: strings.TrimSpace(ctx.PostForm("username")),
		model.KeySMTPFrom:     strings.TrimSpace(ctx.PostForm("from")),
		model.KeySMTPFromAddr: strings.TrimSpace(ctx.PostForm("from_addr")),
	}

	// 密码仅在非空时更新（避免前端展示 placeholder 时覆盖）
	password := ctx.PostForm("password")
	if password != "" {
		kvPairs[model.KeySMTPPassword] = password
	}

	// TLS 复选框特殊处理：勾选时提交 "on" 或 "true"，未勾选时字段不存在
	if ctx.PostForm("use_tls") != "" {
		kvPairs[model.KeySMTPUseTLS] = "true"
	} else {
		kvPairs[model.KeySMTPUseTLS] = "false"
	}

	if err := model.BatchUpsertSettings(c.db, model.CategorySMTP, kvPairs); err != nil {
		log.Printf("[设置] 保存 SMTP 设置失败: %v\n", err)
		flashRedirect(ctx, "error", "保存失败: "+err.Error(), "/settings#smtp")
		return
	}

	flashRedirect(ctx, "success", "SMTP 设置保存成功", "/settings#smtp")
}

// TestSMTP 测试 SMTP 连接
func (c *SettingController) TestSMTP(ctx *gin.Context) {
	if c.emailSvc == nil {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    -1,
			"message": "邮件服务未初始化",
		})
		return
	}

	// 如果请求体中带有测试收件人，则发送测试邮件
	testTo := strings.TrimSpace(ctx.PostForm("test_to"))
	if testTo != "" {
		if err := c.emailSvc.SendTestEmail(testTo); err != nil {
			ctx.JSON(http.StatusOK, gin.H{
				"code":    -1,
				"message": "测试邮件发送失败: " + err.Error(),
			})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": fmt.Sprintf("测试邮件已发送至 %s，请查收", testTo),
		})
		return
	}

	// 仅测试 SMTP 连接
	if err := c.emailSvc.TestSMTP(); err != nil {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    -1,
			"message": "SMTP 连接测试失败: " + err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "SMTP 连接测试成功，服务器通信正常",
	})
}

// ==================== 思源笔记设置 ====================

// Siyuan 显示思源笔记设置页面
func (c *SettingController) Siyuan(ctx *gin.Context) {
	settings, _ := model.GetSettingsMapByCategory(c.db, model.CategorySiyuan)
	flash := extractFlash(ctx)

	ctx.HTML(http.StatusOK, "settings.html", gin.H{
		"title":  "思源笔记设置",
		"active": "settings",
		"tab":    "siyuan",
		"siyuan": settings,
		"flash":  flash,
	})
}

// SaveSiyuan 保存思源笔记设置
func (c *SettingController) SaveSiyuan(ctx *gin.Context) {
	kvPairs := map[string]string{
		model.KeySiyuanBaseURL:  strings.TrimSpace(ctx.PostForm("base_url")),
		model.KeySiyuanAPIToken: strings.TrimSpace(ctx.PostForm("api_token")),
		model.KeySiyuanAvID:     strings.TrimSpace(ctx.PostForm("av_id")),
		model.KeySiyuanBlockID:  strings.TrimSpace(ctx.PostForm("block_id")),
		model.KeySiyuanKeyID:    strings.TrimSpace(ctx.PostForm("key_id")),
		model.KeySiyuanNotebook: strings.TrimSpace(ctx.PostForm("notebook_id")),
	}

	if err := model.BatchUpsertSettings(c.db, model.CategorySiyuan, kvPairs); err != nil {
		log.Printf("[设置] 保存思源笔记设置失败: %v\n", err)
		flashRedirect(ctx, "error", "保存失败: "+err.Error(), "/settings#siyuan")
		return
	}

	flashRedirect(ctx, "success", "思源笔记设置保存成功", "/settings#siyuan")
}

// TestSiyuan 测试思源笔记连接
func (c *SettingController) TestSiyuan(ctx *gin.Context) {
	if c.siyuanSvc == nil {
		ctx.JSON(http.StatusOK, gin.H{
			"code":    -1,
			"message": "思源笔记服务未初始化",
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
		"message": fmt.Sprintf("连接成功，思源笔记版本: %s", version),
		"data": gin.H{
			"version": version,
		},
	})
}

// ==================== 邮件内容设置 ====================

// Email 显示邮件内容设置页面
func (c *SettingController) Email(ctx *gin.Context) {
	settings, _ := model.GetSettingsMapByCategory(c.db, model.CategoryEmail)
	flash := extractFlash(ctx)

	ctx.HTML(http.StatusOK, "settings.html", gin.H{
		"title":  "邮件内容设置",
		"active": "settings",
		"tab":    "email",
		"email":  settings,
		"flash":  flash,
	})
}

// SaveEmail 保存邮件内容设置
func (c *SettingController) SaveEmail(ctx *gin.Context) {
	kvPairs := map[string]string{
		model.KeyEmailRecipients: strings.TrimSpace(ctx.PostForm("recipients")),
		model.KeyEmailCc:         strings.TrimSpace(ctx.PostForm("cc")),
		model.KeyEmailSubject:    strings.TrimSpace(ctx.PostForm("subject")),
	}

	// 校验收件人不能为空
	if kvPairs[model.KeyEmailRecipients] == "" {
		flashRedirect(ctx, "warning", "收件人不能为空", "/settings#email")
		return
	}

	if err := model.BatchUpsertSettings(c.db, model.CategoryEmail, kvPairs); err != nil {
		log.Printf("[设置] 保存邮件设置失败: %v\n", err)
		flashRedirect(ctx, "error", "保存失败: "+err.Error(), "/settings#email")
		return
	}

	flashRedirect(ctx, "success", "邮件内容设置保存成功", "/settings#email")
}

// ==================== 通用设置 ====================

// General 显示通用设置页面
func (c *SettingController) General(ctx *gin.Context) {
	settings, _ := model.GetSettingsMapByCategory(c.db, model.CategoryGeneral)
	flash := extractFlash(ctx)

	ctx.HTML(http.StatusOK, "settings.html", gin.H{
		"title":   "通用设置",
		"active":  "settings",
		"tab":     "general",
		"general": settings,
		"flash":   flash,
	})
}

// SaveGeneral 保存通用设置
func (c *SettingController) SaveGeneral(ctx *gin.Context) {
	kvPairs := map[string]string{
		model.KeyGeneralAppName:  strings.TrimSpace(ctx.PostForm("app_name")),
		model.KeyGeneralTimezone: strings.TrimSpace(ctx.PostForm("timezone")),
	}

	if kvPairs[model.KeyGeneralAppName] == "" {
		kvPairs[model.KeyGeneralAppName] = "日报助手"
	}
	if kvPairs[model.KeyGeneralTimezone] == "" {
		kvPairs[model.KeyGeneralTimezone] = "Asia/Shanghai"
	}

	if err := model.BatchUpsertSettings(c.db, model.CategoryGeneral, kvPairs); err != nil {
		log.Printf("[设置] 保存通用设置失败: %v\n", err)
		flashRedirect(ctx, "error", "保存失败: "+err.Error(), "/settings#general")
		return
	}

	flashRedirect(ctx, "success", "通用设置保存成功", "/settings#general")
}

// ==================== 辅助函数 ====================

// FlashMessage 闪存消息
type FlashMessage struct {
	Level   string // success / error / warning / info
	Message string
}

// extractFlash 从 URL query 参数中提取闪存消息
func extractFlash(ctx *gin.Context) *FlashMessage {
	level := ctx.Query("flash_level")
	msg := ctx.Query("flash_msg")
	if level != "" && msg != "" {
		return &FlashMessage{
			Level:   level,
			Message: msg,
		}
	}
	return nil
}

// flashRedirect 设置闪存消息并执行 302 重定向
func flashRedirect(ctx *gin.Context, level, message, url string) {
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	redirectURL := fmt.Sprintf("%s%sflash_level=%s&flash_msg=%s", url, sep, level, message)
	ctx.Redirect(http.StatusFound, redirectURL)
}
