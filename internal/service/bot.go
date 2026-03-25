package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/zerx-lab/daily-report/internal/model"
	"gorm.io/gorm"
)

// ==================== OneBot 11 协议数据结构 ====================

// OneBotEvent OneBot 11 通用事件
type OneBotEvent struct {
	Time        int64           `json:"time"`
	SelfID      int64           `json:"self_id"`
	PostType    string          `json:"post_type"`       // message / notice / request / meta_event
	MessageType string          `json:"message_type"`    // private / group（仅 message 事件）
	SubType     string          `json:"sub_type"`        // friend / group / normal 等
	MessageID   int64           `json:"message_id"`      // 消息 ID
	UserID      int64           `json:"user_id"`         // 发送者 QQ 号
	GroupID     int64           `json:"group_id"`        // 群号（仅群消息）
	RawMessage  string          `json:"raw_message"`     // 纯文本消息
	Message     json.RawMessage `json:"message"`         // 消息内容（可能是字符串或数组）
	Sender      OneBotSender    `json:"sender"`          // 发送者信息
	Font        int             `json:"font"`            // 字体
	MetaEvent   string          `json:"meta_event_type"` // 元事件类型
}

// OneBotSender 消息发送者
type OneBotSender struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Sex      string `json:"sex"`
	Age      int    `json:"age"`
	Card     string `json:"card"`  // 群名片
	Role     string `json:"role"`  // 群角色 owner/admin/member
	Title    string `json:"title"` // 群头衔
}

// OneBotAPIResponse OneBot API 响应
type OneBotAPIResponse struct {
	Status  string          `json:"status"`  // ok / failed
	RetCode int             `json:"retcode"` // 返回码
	Data    json.RawMessage `json:"data"`    // 响应数据
	Message string          `json:"message"` // 错误信息
	Echo    string          `json:"echo"`    // 回声标识
}

// OneBotSendMsgRequest 发送消息请求（WebSocket 通道使用）
type OneBotSendMsgRequest struct {
	Action string        `json:"action"`
	Params OneBotSendMsg `json:"params"`
	Echo   string        `json:"echo,omitempty"`
}

// OneBotSendMsg 发送消息参数
type OneBotSendMsg struct {
	MessageType string `json:"message_type"`       // private / group
	UserID      int64  `json:"user_id,omitempty"`  // 私聊时的 QQ 号
	GroupID     int64  `json:"group_id,omitempty"` // 群聊时的群号
	Message     string `json:"message"`            // 消息内容
	AutoEscape  bool   `json:"auto_escape"`        // 是否作为纯文本发送
}

// ==================== Bot 配置 ====================

// BotConfig 机器人运行时配置
type BotConfig struct {
	Enabled      bool
	APIURL       string  // NapCat OneBot HTTP API 地址（用于发送消息）
	AccessToken  string  // access_token 鉴权
	AllowedUsers []int64 // 允许的 QQ 号白名单
	WsEnabled    bool    // 是否额外启用反向 WebSocket（NapCat 连到日报系统）
	WsHost       string  // 反向 WebSocket 监听地址
	WsPort       int     // 反向 WebSocket 监听端口
	FwsEnabled   bool    // 是否启用正向 WebSocket（日报系统主动连到 NapCat）
	FwsURL       string  // NapCat WebSocket 服务器地址，如 ws://20.40.96.52:3001
	FwsToken     string  // 正向 WebSocket 的 access_token
}

// ==================== Bot 服务 ====================

// BotService QQ 机器人服务
//
// 消息接收支持三种通道（可同时启用）：
//  1. HTTP POST 事件上报（默认）—— NapCat 将事件 POST 到日报系统的 /onebot/v11/http 端点，
//     复用现有 Gin 路由，无需额外端口，配置最简单。
//  2. 反向 WebSocket（可选）—— NapCat 主动连接到独立的 ws://host:port/onebot/v11/ws 端点，
//     需要额外监听端口。适合需要更低延迟或已有 WS 配置的场景。
//  3. 正向 WebSocket（推荐用于跨网络）—— 日报系统主动连接到 NapCat 的 WebSocket 服务器，
//     日报系统在内网、NapCat 在公网时必须用此方式。自带断线自动重连。
//
// 消息发送始终通过 NapCat 的 HTTP API（/send_msg）；如有活跃的 WebSocket 连接，则优先走 WS。
type BotService struct {
	db         *gorm.DB
	aiSvc      *AIService
	httpClient *http.Client
	upgrader   websocket.Upgrader

	mu      sync.RWMutex
	config  *BotConfig
	wsConns map[*websocket.Conn]bool // 活跃的 WebSocket 连接（反向+正向共用）
	fwsConn *websocket.Conn          // 正向 WebSocket 连接（主动连到 NapCat）

	// 控制生命周期
	ctx      context.Context
	cancel   context.CancelFunc
	wsServer *http.Server
	running  bool
}

// NewBotService 创建机器人服务实例
func NewBotService(db *gorm.DB, aiSvc *AIService) *BotService {
	return &BotService{
		db:         db,
		aiSvc:      aiSvc,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // NapCat 连接不校验 Origin
			},
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
		},
		wsConns: make(map[*websocket.Conn]bool),
	}
}

// ==================== 配置加载 ====================

// LoadConfig 从数据库加载机器人配置
func (s *BotService) LoadConfig() (*BotConfig, error) {
	m, err := model.GetSettingsMapByCategory(s.db, model.CategoryBot)
	if err != nil {
		return nil, fmt.Errorf("加载机器人配置失败: %w", err)
	}

	cfg := &BotConfig{
		Enabled:     m[model.KeyBotEnabled] == "true",
		APIURL:      strings.TrimRight(m[model.KeyBotAPIURL], "/"),
		AccessToken: m[model.KeyBotAccessToken],
		WsEnabled:   m[model.KeyBotWsEnabled] == "true",
		WsHost:      m[model.KeyBotWsHost],
		FwsEnabled:  m[model.KeyBotFwsEnabled] == "true",
		FwsURL:      strings.TrimRight(m[model.KeyBotFwsURL], "/"),
		FwsToken:    m[model.KeyBotFwsToken],
	}

	if cfg.APIURL == "" {
		cfg.APIURL = "http://20.40.96.52:6099"
	}
	if cfg.WsHost == "" {
		cfg.WsHost = "0.0.0.0"
	}

	// 解析 WebSocket 端口
	if port, err := strconv.Atoi(m[model.KeyBotWsPort]); err == nil && port > 0 {
		cfg.WsPort = port
	} else {
		cfg.WsPort = 8788
	}

	// 解析允许的 QQ 号白名单
	usersStr := m[model.KeyBotAllowedUsers]
	if usersStr != "" {
		for _, s := range strings.Split(usersStr, ",") {
			s = strings.TrimSpace(s)
			if uid, err := strconv.ParseInt(s, 10, 64); err == nil && uid > 0 {
				cfg.AllowedUsers = append(cfg.AllowedUsers, uid)
			}
		}
	}

	return cfg, nil
}

// isUserAllowed 检查 QQ 号是否在白名单中
func (s *BotService) isUserAllowed(userID int64) bool {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	if cfg == nil {
		return false
	}

	// 白名单为空时不允许任何人（安全考虑）
	if len(cfg.AllowedUsers) == 0 {
		return false
	}

	for _, allowed := range cfg.AllowedUsers {
		if allowed == userID {
			return true
		}
	}
	return false
}

// ==================== 启动与停止 ====================

// Start 启动机器人服务
func (s *BotService) Start(parentCtx context.Context) error {
	cfg, err := s.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载机器人配置失败: %w", err)
	}

	if !cfg.Enabled {
		log.Println("[机器人] 机器人未启用，跳过启动")
		return nil
	}

	s.mu.Lock()
	s.config = cfg
	s.ctx, s.cancel = context.WithCancel(parentCtx)
	s.running = true
	s.mu.Unlock()

	log.Printf("[机器人] 机器人服务启动中... 发送消息 API: %s\n", cfg.APIURL)

	if len(cfg.AllowedUsers) > 0 {
		log.Printf("[机器人] QQ 号白名单: %v\n", cfg.AllowedUsers)
	} else {
		log.Println("[机器人] 警告: 未配置 QQ 号白名单，将不响应任何用户消息")
	}

	// HTTP POST 事件上报始终可用（通过 Gin 路由 /onebot/v11/http 注册，无需额外操作）
	log.Println("[机器人] HTTP 事件上报: 已就绪（NapCat 配置 HTTP 上报地址为本系统的 /onebot/v11/http）")

	// 正向 WebSocket（日报系统主动连到 NapCat WS 服务器）
	if cfg.FwsEnabled && cfg.FwsURL != "" {
		go s.startForwardWebSocket(cfg)
	} else if cfg.FwsEnabled && cfg.FwsURL == "" {
		log.Println("[机器人] 正向 WebSocket: 已启用但未填写 NapCat WS 服务器地址，跳过")
	} else {
		log.Println("[机器人] 正向 WebSocket: 未启用")
	}

	// 反向 WebSocket 作为可选的额外通道
	if cfg.WsEnabled {
		go s.startWebSocketServer(cfg)
	} else {
		log.Println("[机器人] 反向 WebSocket: 未启用")
	}

	log.Println("[机器人] 机器人服务启动完成")
	return nil
}

// Stop 停止机器人服务
func (s *BotService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	log.Println("[机器人] 正在停止机器人服务...")

	// 取消 context
	if s.cancel != nil {
		s.cancel()
	}

	// 关闭 WebSocket 服务器
	if s.wsServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := s.wsServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("[机器人] WebSocket 服务器关闭异常: %v\n", err)
		}
	}

	// 关闭正向 WebSocket 连接
	if s.fwsConn != nil {
		_ = s.fwsConn.Close()
		s.fwsConn = nil
	}

	// 关闭所有反向 WebSocket 连接
	for conn := range s.wsConns {
		_ = conn.Close()
		delete(s.wsConns, conn)
	}

	s.running = false
	log.Println("[机器人] 机器人服务已停止")
}

// Reload 重新加载配置并重启
func (s *BotService) Reload(parentCtx context.Context) error {
	s.Stop()
	// 短暂等待端口释放
	time.Sleep(500 * time.Millisecond)
	return s.Start(parentCtx)
}

// IsRunning 返回机器人是否正在运行
func (s *BotService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// GetConfig 获取当前配置（只读）
func (s *BotService) GetConfig() *BotConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// ==================== 正向 WebSocket 客户端（跨网络推荐） ====================

// startForwardWebSocket 主动连接到 NapCat 的 WebSocket 服务器，自带断线自动重连
func (s *BotService) startForwardWebSocket(cfg *BotConfig) {
	log.Printf("[机器人] 正向 WebSocket: 正在连接 %s\n", cfg.FwsURL)

	for {
		// 检查是否已取消
		s.mu.RLock()
		ctx := s.ctx
		s.mu.RUnlock()

		if ctx == nil {
			return
		}
		select {
		case <-ctx.Done():
			log.Println("[机器人] 正向 WebSocket: 收到停止信号，退出重连循环")
			return
		default:
		}

		// 构建连接 header（带 token 鉴权）
		header := http.Header{}
		if cfg.FwsToken != "" {
			header.Set("Authorization", "Bearer "+cfg.FwsToken)
		}

		// 拨号连接
		conn, _, err := websocket.DefaultDialer.Dial(cfg.FwsURL, header)
		if err != nil {
			log.Printf("[机器人] 正向 WebSocket: 连接失败: %v，5 秒后重试...\n", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		log.Printf("[机器人] 正向 WebSocket: 连接成功 %s\n", cfg.FwsURL)

		// 注册到连接池（与反向 WS 共用）
		s.mu.Lock()
		s.fwsConn = conn
		s.wsConns[conn] = true
		s.mu.Unlock()

		// 读取消息循环（阻塞直到连接断开）
		s.fwsReadLoop(conn)

		// 连接断开，从连接池移除
		s.mu.Lock()
		delete(s.wsConns, conn)
		s.fwsConn = nil
		s.mu.Unlock()

		log.Println("[机器人] 正向 WebSocket: 连接已断开，5 秒后自动重连...")
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// fwsReadLoop 正向 WebSocket 消息读取循环
func (s *BotService) fwsReadLoop(conn *websocket.Conn) {
	defer func() {
		_ = conn.Close()
	}()

	conn.SetPongHandler(func(string) error {
		return nil
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[机器人] 正向 WebSocket 读取错误: %v\n", err)
			}
			return
		}

		// 异步处理，wsConn 传入当前连接用于回复
		go s.handleOneBotEvent(message, conn)
	}
}

// ==================== HTTP POST 事件上报（主通道） ====================

// HandleHTTPEvent 处理 NapCat 通过 HTTP POST 上报的事件
// 由 Gin 路由直接调用，复用现有 Web 服务端口，无需额外配置
//
// NapCat 侧配置示例（napcat 网络配置 → HTTP 上报）：
//
//	地址: http://<日报系统IP>:<端口>/onebot/v11/http
//	密钥: （与 access_token 一致，可留空）
func (s *BotService) HandleHTTPEvent(w http.ResponseWriter, r *http.Request) {
	// 1. 校验是否启用
	s.mu.RLock()
	cfg := s.config
	running := s.running
	s.mu.RUnlock()

	if !running || cfg == nil {
		http.Error(w, `{"status":"failed","retcode":1,"message":"bot not running"}`, http.StatusServiceUnavailable)
		return
	}

	// 2. 验证 access_token
	if cfg.AccessToken != "" {
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("access_token")
		} else {
			token = strings.TrimPrefix(token, "Bearer ")
			token = strings.TrimPrefix(token, "Token ")
		}
		if token != cfg.AccessToken {
			log.Printf("[机器人] HTTP 事件上报鉴权失败 (来自 %s)\n", r.RemoteAddr)
			http.Error(w, `{"status":"failed","retcode":1403,"message":"access denied"}`, http.StatusForbidden)
			return
		}
	}

	// 3. 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[机器人] HTTP 事件上报: 读取请求体失败: %v\n", err)
		http.Error(w, `{"status":"failed","retcode":1,"message":"read body error"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 4. 异步处理事件（快速返回 204，避免 NapCat 超时重发）
	go s.handleOneBotEvent(body, nil)

	// 5. 返回空 JSON 表示收到（OneBot 11 规范）
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

// ==================== 反向 WebSocket 服务（可选通道） ====================

// startWebSocketServer 启动反向 WebSocket 服务端（NapCat 主动连接到这里）
func (s *BotService) startWebSocketServer(cfg *BotConfig) {
	addr := fmt.Sprintf("%s:%d", cfg.WsHost, cfg.WsPort)

	mux := http.NewServeMux()
	// 通用 WebSocket 端点
	mux.HandleFunc("/onebot/v11/ws", s.handleWebSocket)
	mux.HandleFunc("/onebot/v11/ws/", s.handleWebSocket)
	// 兼容直接连根路径
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/ws/", s.handleWebSocket)
	mux.HandleFunc("/", s.handleWebSocket)

	s.mu.Lock()
	s.wsServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  0, // WebSocket 长连接不设超时
		WriteTimeout: 30 * time.Second,
	}
	s.mu.Unlock()

	log.Printf("[机器人] 反向 WebSocket: 监听 ws://%s （NapCat 配置反向 WS 连接到此地址）\n", addr)

	if err := s.wsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("[机器人] WebSocket 服务器异常退出: %v\n", err)
	}
}

// handleWebSocket 处理 WebSocket 连接
func (s *BotService) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 验证 access_token
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	if cfg != nil && cfg.AccessToken != "" {
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("access_token")
		} else {
			token = strings.TrimPrefix(token, "Bearer ")
			token = strings.TrimPrefix(token, "Token ")
		}
		if token != cfg.AccessToken {
			log.Printf("[机器人] WebSocket 连接被拒绝: access_token 不匹配 (来自 %s)\n", r.RemoteAddr)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[机器人] WebSocket 升级失败: %v\n", err)
		return
	}

	log.Printf("[机器人] NapCat WebSocket 已连接: %s (路径: %s)\n", r.RemoteAddr, r.URL.Path)

	// 注册连接
	s.mu.Lock()
	s.wsConns[conn] = true
	s.mu.Unlock()

	// 启动读取循环
	s.wsReadLoop(conn)
}

// wsReadLoop WebSocket 消息读取循环
func (s *BotService) wsReadLoop(conn *websocket.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.wsConns, conn)
		s.mu.Unlock()
		_ = conn.Close()
		log.Println("[机器人] NapCat WebSocket 连接已断开")
	}()

	conn.SetPongHandler(func(string) error {
		return nil
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[机器人] WebSocket 读取错误: %v\n", err)
			}
			return
		}

		// 异步处理消息，避免阻塞读取循环
		go s.handleOneBotEvent(message, conn)
	}
}

// ==================== 事件处理（HTTP 和 WebSocket 共用） ====================

// handleOneBotEvent 处理 OneBot 事件（两个通道共用入口）
// wsConn 为 nil 表示事件来自 HTTP 上报，回复将通过 HTTP API 发送
func (s *BotService) handleOneBotEvent(data []byte, wsConn *websocket.Conn) {
	var event OneBotEvent
	if err := json.Unmarshal(data, &event); err != nil {
		log.Printf("[机器人] 解析事件失败: %v, 原始数据: %s\n", err, truncateBytes(data, 200))
		return
	}

	switch event.PostType {
	case "meta_event":
		s.handleMetaEvent(&event)
	case "message":
		s.handleMessageEvent(&event, wsConn)
	case "notice":
		// 通知事件（好友增加、群成员变动等），暂不处理
	case "request":
		// 请求事件（好友请求、群邀请等），暂不处理
	default:
		// 未知事件类型，忽略
	}
}

// handleMetaEvent 处理元事件（心跳、生命周期）
func (s *BotService) handleMetaEvent(event *OneBotEvent) {
	switch event.MetaEvent {
	case "lifecycle":
		log.Printf("[机器人] 生命周期事件: %s (self_id=%d)\n", event.SubType, event.SelfID)
	case "heartbeat":
		// 心跳事件，静默处理
	}
}

// handleMessageEvent 处理消息事件
func (s *BotService) handleMessageEvent(event *OneBotEvent, wsConn *websocket.Conn) {
	// 仅处理白名单用户的好友私聊消息，忽略群聊和群临时会话
	if !isDirectPrivateMessage(event) {
		return
	}

	// 检查用户是否在白名单中
	if !s.isUserAllowed(event.UserID) {
		return
	}

	// 提取纯文本消息
	text := strings.TrimSpace(event.RawMessage)
	if text == "" {
		return
	}

	senderName := event.Sender.Nickname
	if event.Sender.Card != "" {
		senderName = event.Sender.Card
	}

	source := "HTTP"
	if wsConn != nil {
		source = "WebSocket"
	}
	userID := strconv.FormatInt(event.UserID, 10)
	log.Printf("[机器人] 收到消息(%s): [%s(%s)] %s\n", source, senderName, userID, truncateStr(text, 100))

	// 处理 /new 命令：清除对话记忆
	if text == "/new" {
		cleared, err := s.aiSvc.ClearMemory(userID)
		var reply string
		if err != nil {
			log.Printf("[机器人] 清除对话记忆失败(user=%s): %v\n", userID, err)
			reply = "⚠️ 清除记忆失败: " + err.Error()
		} else {
			log.Printf("[机器人] 清除对话记忆成功(user=%s): %d 条\n", userID, cleared)
			reply = fmt.Sprintf("🧹 记忆已清除！共清除 %d 条对话记录。\n\n现在是全新的对话，请告诉我你需要什么帮助 😊", cleared)
		}
		s.sendReply(event, reply, wsConn)
		return
	}

	// 获取 context
	s.mu.RLock()
	ctx := s.ctx
	s.mu.RUnlock()

	if ctx == nil {
		ctx = context.Background()
	}

	// 设置单次对话超时（AI 调用可能较慢，给足时间）
	chatCtx, chatCancel := context.WithTimeout(ctx, 120*time.Second)
	defer chatCancel()

	reply, err := s.aiSvc.Chat(chatCtx, userID, text)
	if err != nil {
		log.Printf("[机器人] AI 处理消息失败: %v\n", err)
		reply = fmt.Sprintf("⚠️ 处理消息时出错: %s", err.Error())
	}

	if reply == "" {
		reply = "🤔 我没有理解你的意思，请再说一次。"
	}

	// 发送回复
	s.sendReply(event, reply, wsConn)
}

func isDirectPrivateMessage(event *OneBotEvent) bool {
	if event == nil {
		return false
	}

	if event.MessageType != "private" {
		return false
	}

	if event.GroupID != 0 {
		return false
	}

	subType := strings.ToLower(strings.TrimSpace(event.SubType))
	return subType == "" || subType == "friend"
}

// ==================== 消息发送 ====================

// sendReply 回复消息
// 优先使用传入的 wsConn（如事件来自 WebSocket），否则通过 HTTP API 发送
func (s *BotService) sendReply(event *OneBotEvent, text string, wsConn *websocket.Conn) {
	msg := OneBotSendMsg{
		Message:    text,
		AutoEscape: true, // 纯文本发送，避免 CQ 码注入
	}

	if event.MessageType == "group" {
		msg.MessageType = "group"
		msg.GroupID = event.GroupID
	} else {
		msg.MessageType = "private"
		msg.UserID = event.UserID
	}

	// 如果事件来自 WebSocket，优先通过该连接回复
	if wsConn != nil {
		if err := s.sendViaWebSocket(wsConn, msg); err != nil {
			log.Printf("[机器人] WebSocket 发送失败，降级到 HTTP API: %v\n", err)
			if httpErr := s.sendViaHTTP(msg); httpErr != nil {
				log.Printf("[机器人] HTTP API 也发送失败: %v\n", httpErr)
			}
		}
		return
	}

	// HTTP 上报的事件，通过 HTTP API 回复
	if err := s.sendViaHTTP(msg); err != nil {
		log.Printf("[机器人] HTTP API 发送失败: %v\n", err)
	}
}

// sendViaWebSocket 通过 WebSocket 发送消息
func (s *BotService) sendViaWebSocket(conn *websocket.Conn, msg OneBotSendMsg) error {
	req := OneBotSendMsgRequest{
		Action: "send_msg",
		Params: msg,
		Echo:   fmt.Sprintf("bot_%d", time.Now().UnixNano()),
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("WebSocket 写入失败: %w", err)
	}

	return nil
}

// sendViaHTTP 通过 HTTP API 发送消息（调用 NapCat 的 /send_msg 接口）
func (s *BotService) sendViaHTTP(msg OneBotSendMsg) error {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	if cfg == nil || cfg.APIURL == "" {
		return fmt.Errorf("HTTP API 地址未配置")
	}

	apiURL := cfg.APIURL + "/send_msg"
	body, _ := json.Marshal(msg)

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if cfg.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AccessToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("调用 NapCat API(%s) 失败: %w", apiURL, err)
	}
	defer resp.Body.Close()

	// 先读取完整响应体，再尝试 JSON 解析
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("读取响应失败(HTTP %d): %w", resp.StatusCode, err)
	}

	// HTTP 状态码异常时直接报错
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("NapCat API 返回 HTTP %d，地址: %s，响应: %s（请检查 API 地址是否正确，NapCat 的 OneBot HTTP 服务端口通常与 WebUI 端口不同）",
			resp.StatusCode, apiURL, truncateBytes(respBody, 200))
	}

	// 检查响应是否为 JSON（非 JSON 通常是打到了 WebUI 页面）
	trimmed := bytes.TrimSpace(respBody)
	if len(trimmed) == 0 {
		// 空响应视为成功（某些 OneBot 实现返回空 body）
		return nil
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return fmt.Errorf("NapCat API 返回了非 JSON 内容(HTTP %d)，响应: %s（大概率是 API 地址不对，当前地址 %s 可能指向了 NapCat WebUI 而不是 OneBot HTTP 服务端口）",
			resp.StatusCode, truncateBytes(respBody, 200), apiURL)
	}

	var apiResp OneBotAPIResponse
	if err := json.Unmarshal(trimmed, &apiResp); err != nil {
		return fmt.Errorf("解析 JSON 响应失败(HTTP %d): %w，原始响应: %s", resp.StatusCode, err, truncateBytes(respBody, 200))
	}

	if apiResp.RetCode != 0 {
		return fmt.Errorf("NapCat 返回错误: retcode=%d, message=%s", apiResp.RetCode, apiResp.Message)
	}

	return nil
}

// ==================== 主动发送接口（供外部调用） ====================

// SendMessage 主动发送私聊消息（如定时提醒等场景）
func (s *BotService) SendMessage(userID int64, text string) error {
	msg := OneBotSendMsg{
		MessageType: "private",
		UserID:      userID,
		Message:     text,
		AutoEscape:  true,
	}

	// 优先使用 WebSocket
	if conn := s.pickWSConn(); conn != nil {
		return s.sendViaWebSocket(conn, msg)
	}

	// 降级到 HTTP API
	return s.sendViaHTTP(msg)
}

// SendGroupMessage 主动发送群消息
func (s *BotService) SendGroupMessage(groupID int64, text string) error {
	msg := OneBotSendMsg{
		MessageType: "group",
		GroupID:     groupID,
		Message:     text,
		AutoEscape:  true,
	}

	if conn := s.pickWSConn(); conn != nil {
		return s.sendViaWebSocket(conn, msg)
	}

	return s.sendViaHTTP(msg)
}

// SendTestMessage 发送测试消息给指定 QQ 号（或白名单第一个用户）
// 返回发送结果详情，供设置页面测试按钮使用
func (s *BotService) SendTestMessage(targetQQ int64) (string, error) {
	s.mu.RLock()
	cfg := s.config
	running := s.running
	s.mu.RUnlock()

	if !running || cfg == nil {
		return "", fmt.Errorf("机器人服务未启动，请先启用并保存配置")
	}

	if cfg.APIURL == "" {
		return "", fmt.Errorf("NapCat HTTP API 地址未配置")
	}

	// 如果未指定目标，取白名单第一个
	if targetQQ <= 0 {
		if len(cfg.AllowedUsers) == 0 {
			return "", fmt.Errorf("未指定目标 QQ 号，且白名单为空")
		}
		targetQQ = cfg.AllowedUsers[0]
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	text := fmt.Sprintf("🤖 日报助手测试消息\n\n发送时间: %s\n状态: 消息通道正常 ✅\n\n如果你收到了这条消息，说明机器人→QQ 的发送链路工作正常。", now)

	msg := OneBotSendMsg{
		MessageType: "private",
		UserID:      targetQQ,
		Message:     text,
		AutoEscape:  true,
	}

	// 记录使用的通道
	channel := "HTTP API"

	// 优先 WebSocket
	if conn := s.pickWSConn(); conn != nil {
		channel = "WebSocket"
		if err := s.sendViaWebSocket(conn, msg); err != nil {
			// WS 失败，降级 HTTP
			channel = "HTTP API (WebSocket 降级)"
			if httpErr := s.sendViaHTTP(msg); httpErr != nil {
				return "", fmt.Errorf("WebSocket 发送失败(%v)，HTTP API 也失败(%v)", err, httpErr)
			}
		}
	} else {
		if err := s.sendViaHTTP(msg); err != nil {
			return "", fmt.Errorf("发送失败: %w", err)
		}
	}

	detail := fmt.Sprintf("测试消息已通过 %s 发送至 QQ %d", channel, targetQQ)
	log.Printf("[机器人] %s\n", detail)
	return detail, nil
}

// pickWSConn 随机取一个活跃的 WebSocket 连接（用于主动发送）
func (s *BotService) pickWSConn() *websocket.Conn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for c := range s.wsConns {
		return c
	}
	return nil
}

// ==================== 辅助方法 ====================

// truncateStr 截断字符串
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// truncateBytes 截断字节切片（用于日志）
func truncateBytes(b []byte, maxLen int) string {
	if len(b) <= maxLen {
		return string(b)
	}
	return string(b[:maxLen]) + "..."
}
