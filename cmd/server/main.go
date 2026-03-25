package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	dailyreport "github.com/zerx-lab/daily-report"
	"github.com/zerx-lab/daily-report/internal/config"
	"github.com/zerx-lab/daily-report/internal/model"
	"github.com/zerx-lab/daily-report/internal/router"
	"github.com/zerx-lab/daily-report/internal/service"
)

const (
	appName    = "日报自动化系统"
	appVersion = "1.0.0"
)

func main() {
	// ==================== 1. 解析命令行参数 ====================
	configPath := flag.String("config", "config/config.yaml", "配置文件路径")
	flag.Parse()

	printBanner()

	// ==================== 2. 加载配置文件 ====================
	log.Printf("[启动] 加载配置文件: %s\n", *configPath)
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[致命] 加载配置失败: %v\n", err)
	}
	log.Println("[启动] 配置加载成功")

	// ==================== 3. 初始化数据库 ====================
	log.Printf("[启动] 初始化数据库: %s\n", cfg.Database.Path)
	if err := model.InitDB(cfg.Database.Path); err != nil {
		log.Fatalf("[致命] 数据库初始化失败: %v\n", err)
	}
	db := model.GetDB()
	log.Println("[启动] 数据库初始化成功")

	// ==================== 4. 创建服务实例 ====================
	reportSvc := service.NewReportService(db)
	emailSvc := service.NewEmailService(db, dailyreport.TemplatesFS)
	siyuanSvc := service.NewSiyuanService(db)
	outingSvc := service.NewOutingService(db)
	log.Println("[启动] 业务服务初始化完成")

	// ==================== 5. 创建并启动定时调度器 ====================
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler, err := service.NewScheduler(db, reportSvc, emailSvc, siyuanSvc)
	if err != nil {
		log.Fatalf("[致命] 创建调度器失败: %v\n", err)
	}

	if err := scheduler.Start(ctx); err != nil {
		log.Printf("[警告] 调度器启动失败: %v\n", err)
		// 调度器启动失败不影响 Web 服务，继续运行
	} else {
		log.Println("[启动] 定时调度器启动成功")
	}

	// ==================== 6. 配置路由 ====================
	r := router.NewRouter(db, dailyreport.TemplatesFS, dailyreport.StaticFS, reportSvc, emailSvc, siyuanSvc, scheduler, outingSvc)
	engine := r.Setup(cfg.Server.Mode)
	log.Println("[启动] 路由配置完成")

	// ==================== 7. 启动 HTTP 服务器 ====================
	addr := cfg.Addr()
	srv := &http.Server{
		Addr:         addr,
		Handler:      engine,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 在 goroutine 中启动服务器
	go func() {
		log.Println("========================================")
		log.Printf("  %s v%s 启动成功", appName, appVersion)
		log.Printf("  运行模式: %s", cfg.Server.Mode)
		log.Printf("  监听地址: http://%s", addr)
		log.Printf("  访问地址: http://localhost:%d", cfg.Server.Port)
		log.Println("========================================")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[致命] HTTP 服务器启动失败: %v\n", err)
		}
	}()

	// ==================== 8. 优雅关闭 ====================
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	log.Printf("\n[关闭] 收到信号 %v，正在优雅关闭...\n", sig)

	// 取消 context，通知调度器停止
	cancel()

	// 给 HTTP 服务器 10 秒的关闭时间
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[关闭] HTTP 服务器关闭异常: %v\n", err)
	} else {
		log.Println("[关闭] HTTP 服务器已停止")
	}

	// 关闭数据库连接
	if err := model.CloseDB(); err != nil {
		log.Printf("[关闭] 数据库关闭异常: %v\n", err)
	} else {
		log.Println("[关闭] 数据库连接已关闭")
	}

	log.Println("[关闭] 所有资源已释放，程序退出")
}

// printBanner 打印启动横幅
func printBanner() {
	banner := `
 ____        _ _         ____                       _
|  _ \  __ _(_) |_   _  |  _ \ ___ _ __   ___  _ __| |_
| | | |/ _' | | | | | | | |_) / _ \ '_ \ / _ \| '__| __|
| |_| | (_| | | | |_| | |  _ <  __/ |_) | (_) | |  | |_
|____/ \__,_|_|_|\__, | |_| \_\___| .__/ \___/|_|   \__|
                 |___/             |_|                    `
	fmt.Println(banner)
	fmt.Printf("  %s v%s\n\n", appName, appVersion)
}
