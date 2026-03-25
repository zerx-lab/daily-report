package router

import (
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zerx-lab/daily-report/internal/controller"
	"github.com/zerx-lab/daily-report/internal/model"
	"github.com/zerx-lab/daily-report/internal/service"
	"gorm.io/gorm"
)

// Router 路由管理器
type Router struct {
	engine      *gin.Engine
	db          *gorm.DB
	templateDir string
	staticDir   string

	// 服务层
	reportSvc *service.ReportService
	emailSvc  *service.EmailService
	siyuanSvc *service.SiyuanService
	scheduler *service.Scheduler
	outingSvc *service.OutingService

	// 控制器
	ctrl       *controller.ReportController
	outingCtrl *controller.OutingController
}

// NewRouter 创建路由实例
func NewRouter(
	db *gorm.DB,
	templateDir string,
	staticDir string,
	reportSvc *service.ReportService,
	emailSvc *service.EmailService,
	siyuanSvc *service.SiyuanService,
	scheduler *service.Scheduler,
	outingSvc *service.OutingService,
) *Router {
	return &Router{
		db:          db,
		templateDir: templateDir,
		staticDir:   staticDir,
		reportSvc:   reportSvc,
		emailSvc:    emailSvc,
		siyuanSvc:   siyuanSvc,
		scheduler:   scheduler,
		outingSvc:   outingSvc,
	}
}

// Setup 初始化并配置所有路由，返回 gin.Engine
func (r *Router) Setup(mode string) *gin.Engine {
	// 设置 Gin 运行模式
	switch strings.ToLower(mode) {
	case "release":
		gin.SetMode(gin.ReleaseMode)
	case "test":
		gin.SetMode(gin.TestMode)
	default:
		gin.SetMode(gin.DebugMode)
	}

	engine := gin.New()

	// 全局中间件
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())
	engine.Use(r.corsMiddleware())
	engine.Use(r.flashMiddleware())

	// 加载 HTML 模板
	r.loadTemplates(engine)

	// 注册静态文件
	engine.Static("/static", r.staticDir)

	// 初始化控制器
	r.ctrl = controller.NewReportController(
		r.db,
		r.reportSvc,
		r.emailSvc,
		r.siyuanSvc,
		r.scheduler,
	)
	r.outingCtrl = controller.NewOutingController(
		r.db,
		r.outingSvc,
		r.emailSvc,
		r.siyuanSvc,
	)

	// 注册路由
	r.registerRoutes(engine)

	r.engine = engine
	return engine
}

// GetEngine 获取 gin.Engine 实例
func (r *Router) GetEngine() *gin.Engine {
	return r.engine
}

// ==================== 模板加载 ====================

// loadTemplates 加载 HTML 模板并注册自定义模板函数
func (r *Router) loadTemplates(engine *gin.Engine) {
	funcMap := template.FuncMap{
		// ---------- 日期/时间格式化 ----------
		"formatDate": func(t time.Time) string {
			return t.Format("2006-01-02")
		},
		"formatDateTime": func(t time.Time) string {
			return t.Format("2006-01-02 15:04:05")
		},
		"formatDateTimePtr": func(t *time.Time) string {
			if t == nil {
				return "-"
			}
			return t.Format("2006-01-02 15:04:05")
		},
		"formatTimePtr": func(t *time.Time) string {
			if t == nil {
				return "-"
			}
			return t.Format("15:04:05")
		},
		"currentYear": func() int {
			return time.Now().Year()
		},

		// ---------- 字符串操作 ----------
		"truncate": func(s string, length int) string {
			runes := []rune(s)
			if len(runes) <= length {
				return s
			}
			return string(runes[:length]) + "..."
		},
		"nl2br": func(s string) template.HTML {
			escaped := template.HTMLEscapeString(s)
			return template.HTML(strings.ReplaceAll(escaped, "\n", "<br>"))
		},
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"contains": func(s, substr string) bool {
			return strings.Contains(s, substr)
		},
		"isEmpty": func(s string) bool {
			return strings.TrimSpace(s) == ""
		},
		"join": func(elems []string, sep string) string {
			return strings.Join(elems, sep)
		},
		"split": func(s, sep string) []string {
			return strings.Split(s, sep)
		},

		// ---------- 数学运算 ----------
		"add": func(a, b int) int {
			return a + b
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"mul": func(a, b int) int {
			return a * b
		},

		// ---------- 比较 ----------
		"eqInt": func(a, b int) bool {
			return a == b
		},
		"neInt": func(a, b int) bool {
			return a != b
		},

		// ---------- 日报状态 ----------
		"statusText": func(status model.ReportStatus) string {
			switch status {
			case model.ReportStatusDraft:
				return "草稿"
			case model.ReportStatusReady:
				return "待发送"
			case model.ReportStatusSent:
				return "已发送"
			case model.ReportStatusFailed:
				return "发送失败"
			default:
				return "未知"
			}
		},
		"statusBadge": func(status model.ReportStatus) string {
			switch status {
			case model.ReportStatusDraft:
				return "secondary"
			case model.ReportStatusReady:
				return "primary"
			case model.ReportStatusSent:
				return "success"
			case model.ReportStatusFailed:
				return "danger"
			default:
				return "secondary"
			}
		},

		// ---------- 外出申请状态 ----------
		"outingStatusText": func(status model.OutingStatus) string {
			return status.String()
		},
		"outingStatusBadge": func(status model.OutingStatus) string {
			switch status {
			case model.OutingStatusDraft:
				return "secondary"
			case model.OutingStatusReady:
				return "primary"
			case model.OutingStatusSent:
				return "success"
			case model.OutingStatusFailed:
				return "danger"
			default:
				return "secondary"
			}
		},

		// ---------- 邮件发送状态 ----------
		"emailStatusText": func(status int) string {
			switch status {
			case 0:
				return "待发送"
			case 1:
				return "发送中"
			case 2:
				return "成功"
			case 3:
				return "失败"
			default:
				return "未知"
			}
		},
		"emailStatusBadge": func(status int) string {
			switch status {
			case 0:
				return "warning"
			case 1:
				return "info"
			case 2:
				return "success"
			case 3:
				return "danger"
			default:
				return "secondary"
			}
		},

		// ---------- 分页辅助 ----------
		"pageRange": func(current, total int) []int {
			pages := make([]int, 0)
			start := current - 2
			if start < 1 {
				start = 1
			}
			end := start + 4
			if end > total {
				end = total
			}
			if end-start < 4 && start > 1 {
				start = end - 4
				if start < 1 {
					start = 1
				}
			}
			for i := start; i <= end; i++ {
				pages = append(pages, i)
			}
			return pages
		},
		"rowIndex": func(page, pageSize, index int) int {
			return (page-1)*pageSize + index + 1
		},

		// ---------- 布尔/其他 ----------
		"boolStr": func(b bool) string {
			if b {
				return "true"
			}
			return "false"
		},
		"dict": func(values ...interface{}) map[string]interface{} {
			// 模板中快捷创建 map：{{dict "key1" val1 "key2" val2}}
			d := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values)-1; i += 2 {
				key, ok := values[i].(string)
				if ok {
					d[key] = values[i+1]
				}
			}
			return d
		},
	}

	engine.SetFuncMap(funcMap)

	// 加载模板文件（templates 目录下所有 .html，含子目录）
	pattern := r.templateDir + "/**/*"
	engine.LoadHTMLGlob(pattern)
	log.Printf("[路由] HTML 模板已加载: %s\n", pattern)
}

// ==================== 路由注册 ====================

// registerRoutes 注册所有路由
func (r *Router) registerRoutes(engine *gin.Engine) {
	ctrl := r.ctrl

	// ====================== 页面路由（返回 HTML） ======================

	// --- 仪表盘/首页 ---
	engine.GET("/", ctrl.Dashboard)
	engine.GET("/dashboard", ctrl.Dashboard)

	// --- 快捷操作 ---
	engine.POST("/reports/create-today", ctrl.CreateToday)

	// --- 日报管理 ---
	reports := engine.Group("/reports")
	{
		reports.GET("", ctrl.List)               // 日报列表页
		reports.GET("/new", ctrl.CreateForm)     // 新建日报表单
		reports.POST("/new", ctrl.Create)        // 提交新建
		reports.GET("/:id/edit", ctrl.EditForm)  // 编辑日报表单
		reports.POST("/:id/edit", ctrl.Update)   // 提交更新
		reports.POST("/:id/delete", ctrl.Delete) // 删除日报
		reports.POST("/:id/send", ctrl.Send)     // 手动发送邮件
	}

	// --- 思源笔记同步 ---
	engine.POST("/sync/from-siyuan", ctrl.SyncFromSiyuan) // 从思源笔记拉取同步（仅日报）
	engine.POST("/sync/all", ctrl.SyncAllFromSiyuan)      // 全局同步（日报 + 外出申请）
	engine.POST("/sync/test-siyuan", ctrl.PingSiyuan)     // 测试思源连接

	// --- 邮件发送记录 ---
	engine.GET("/logs", ctrl.SendLogs)          // 发送记录列表
	engine.GET("/logs/:id", ctrl.SendLogDetail) // 发送记录详情

	// --- 定时任务管理 ---
	schedule := engine.Group("/schedule")
	{
		schedule.GET("", ctrl.ScheduleList)             // 定时任务列表页
		schedule.POST("/toggle", ctrl.ScheduleToggle)   // 启用/禁用
		schedule.POST("/cron", ctrl.ScheduleUpdateCron) // 更新 Cron 表达式
		schedule.POST("/trigger", ctrl.ScheduleTrigger) // 立即执行
	}

	// --- 外出申请管理 ---
	outings := engine.Group("/outings")
	{
		outings.GET("", r.outingCtrl.List)               // 外出申请列表页
		outings.GET("/new", r.outingCtrl.CreateForm)     // 新建外出申请表单
		outings.POST("/new", r.outingCtrl.Create)        // 提交新建
		outings.GET("/:id/edit", r.outingCtrl.EditForm)  // 编辑外出申请表单
		outings.POST("/:id/edit", r.outingCtrl.Update)   // 提交更新
		outings.POST("/:id/delete", r.outingCtrl.Delete) // 删除外出申请
		outings.POST("/:id/send", r.outingCtrl.Send)     // 发送外出申请邮件
	}

	// --- 系统设置 ---
	engine.GET("/settings", ctrl.Settings)      // 设置页面
	engine.POST("/settings", ctrl.SaveSettings) // 保存设置

	// --- SMTP 测试 ---
	engine.POST("/settings/test-smtp", ctrl.TestSMTP)       // 测试 SMTP 连接
	engine.POST("/settings/test-email", ctrl.SendTestEmail) // 发送测试邮件

	// ====================== API 路由（返回 JSON） ======================

	api := engine.Group("/api/v1")
	{
		// 日报 CRUD
		api.GET("/reports", ctrl.APIListReports)
		api.GET("/reports/:id", ctrl.APIGetReport)
		api.POST("/reports", ctrl.APICreateReport)
		api.PUT("/reports/:id", ctrl.APIUpdateReport)
		api.DELETE("/reports/:id", ctrl.APIDeleteReport)
		api.POST("/reports/:id/send", ctrl.APISendReport)

		// 同步
		api.POST("/sync/pull", ctrl.SyncFromSiyuan)
		api.POST("/sync/ping", ctrl.PingSiyuan)

		// 定时任务
		api.GET("/schedule/status", func(c *gin.Context) {
			if r.scheduler != nil {
				c.JSON(http.StatusOK, gin.H{
					"code": 0,
					"data": gin.H{
						"running": r.scheduler.IsRunning(),
						"jobs":    r.scheduler.GetJobsStatus(),
					},
				})
			} else {
				c.JSON(http.StatusOK, gin.H{
					"code":    -1,
					"message": "调度器未初始化",
				})
			}
		})
		api.POST("/schedule/trigger", ctrl.ScheduleTrigger)

		// 发送记录
		api.GET("/logs", func(c *gin.Context) {
			c.Request.Header.Set("Accept", "application/json")
			ctrl.SendLogs(c)
		})

		// 系统信息
		api.GET("/system/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status": "ok",
				"time":   time.Now().Format(time.RFC3339),
			})
		})
		api.GET("/system/info", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"data": gin.H{
					"app_name":    "日报管理系统",
					"version":     "1.0.0",
					"go_version":  "1.23",
					"server_time": time.Now().Format(time.RFC3339),
				},
			})
		})

		// 设置 API
		api.GET("/settings/:category", func(c *gin.Context) {
			c.Request.Header.Set("Accept", "application/json")
			ctrl.Settings(c)
		})
	}

	// ====================== 通用路由 ======================

	// 健康检查（无前缀）
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// 404 处理
	engine.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "接口不存在",
			})
			return
		}
		c.HTML(http.StatusNotFound, "404.html", gin.H{
			"title":   "页面不存在",
			"active":  "",
			"message": "您访问的页面不存在或已被移除",
		})
	})

	log.Println("[路由] 所有路由注册完成")
}

// ==================== 中间件 ====================

// corsMiddleware 跨域资源共享中间件
func (r *Router) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// flashMiddleware 简易闪存消息中间件（从 query 参数读取）
func (r *Router) flashMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 URL 参数中读取 flash 消息，注入到模板上下文
		flashLevel := c.Query("flash_level")
		flashMsg := c.Query("flash_msg")
		if flashLevel != "" && flashMsg != "" {
			c.Set("flash_level", flashLevel)
			c.Set("flash_msg", flashMsg)
		}
		c.Next()
	}
}
