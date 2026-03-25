package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/zerx-lab/daily-report/internal/model"
	"gorm.io/gorm"
)

// AIService AI 大模型服务（使用 OpenAI 官方 SDK，兼容国内厂商）
type AIService struct {
	db        *gorm.DB
	reportSvc *ReportService
	outingSvc *OutingService
	emailSvc  *EmailService
}

// NewAIService 创建 AI 服务实例
func NewAIService(db *gorm.DB, reportSvc *ReportService, outingSvc *OutingService, emailSvc *EmailService) *AIService {
	return &AIService{
		db:        db,
		reportSvc: reportSvc,
		outingSvc: outingSvc,
		emailSvc:  emailSvc,
	}
}

// ==================== 配置加载 ====================

// aiConfig 运行时 AI 配置
type aiConfig struct {
	BaseURL      string
	ApiKey       string
	Model        string
	MaxTokens    int64
	Temperature  float64
	SystemPrompt string
}

// loadConfig 从数据库加载 AI 配置
func (s *AIService) loadConfig() (*aiConfig, error) {
	m, err := model.GetSettingsMapByCategory(s.db, model.CategoryAI)
	if err != nil {
		return nil, fmt.Errorf("加载 AI 配置失败: %w", err)
	}

	cfg := &aiConfig{
		BaseURL:      m[model.KeyAIBaseURL],
		ApiKey:       m[model.KeyAIApiKey],
		Model:        m[model.KeyAIModel],
		SystemPrompt: m[model.KeyAISystemPrompt],
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.deepseek.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "deepseek-chat"
	}

	// 解析 max_tokens
	if v, err := strconv.ParseInt(m[model.KeyAIMaxTokens], 10, 64); err == nil && v > 0 {
		cfg.MaxTokens = v
	} else {
		cfg.MaxTokens = 2048
	}

	// 解析 temperature
	if v, err := strconv.ParseFloat(m[model.KeyAITemperature], 64); err == nil {
		cfg.Temperature = v
	} else {
		cfg.Temperature = 0.7
	}

	if cfg.ApiKey == "" {
		return nil, fmt.Errorf("AI API Key 未配置，请在系统设置中填写")
	}

	return cfg, nil
}

// newClient 创建 OpenAI 兼容客户端
func (s *AIService) newClient(cfg *aiConfig) openai.Client {
	return openai.NewClient(
		option.WithAPIKey(cfg.ApiKey),
		option.WithBaseURL(cfg.BaseURL),
	)
}

// ==================== 系统提示词 ====================

// defaultSystemPrompt 内置的系统提示词
func (s *AIService) defaultSystemPrompt() string {
	now := time.Now()
	today := now.Format("2006-01-02")
	weekday := weekdayCN(now.Weekday())

	return fmt.Sprintf(`你是一个智能日报助手机器人，帮助用户通过自然语言管理工作日报和外出申请。

## 当前信息
- 当前时间：%s %s %s
- 今天日期：%s（%s）

## 你的能力
你可以通过调用工具来完成以下操作：
1. **日报管理**：创建日报、追加日报内容、查看今日日报、查看最近日报
2. **外出申请**：创建外出办事申请
3. **邮件发送**：发送日报邮件、发送外出申请邮件

## 工作规则
1. 当用户描述工作内容时，先调用 get_today_report 查看今日日报是否已存在：
   - 如果不存在：调用 create_or_update_report 创建新日报
   - 如果已存在且有内容：理解现有内容，将新内容追加到已有内容后面，调用 create_or_update_report 更新
   - 追加时保持格式一致，使用换行分隔不同工作条目
2. 日期默认为今天，除非用户明确指定其他日期
3. 帮用户整理工作内容为简洁的条目格式，每条一行，不需要序号或符号前缀
4. 外出申请需要：外出时间、返回时间、外出地点、外出事由
5. 如果用户说"明天"、"后天"等相对日期，需要正确计算实际日期
6. 回复用户时使用友好的中文，适当使用 emoji 增加可读性
7. 完成操作后，向用户确认操作结果，包含关键信息摘要
8. **重要**：当用户只说"发送"而没有明确指定发送什么时，必须先确认用户要发送的是日报还是外出申请，绝对不要默认发送日报
9. 发送外出申请时，如果用户没有提供 ID，直接调用 send_outing（不传 id），系统会自动发送最近一条待发送的外出申请
10. 发送日报邮件会将所有非草稿状态的日报汇总到一个 Excel 表格中发送，而不是只发送某一天的`,
		today, weekday, now.Format("15:04"),
		today, weekday,
	)
}

// ==================== 工具定义 ====================

// buildTools 构建 AI 可用的工具列表
func (s *AIService) buildTools() []openai.ChatCompletionToolUnionParam {
	return []openai.ChatCompletionToolUnionParam{
		// 查看今日日报
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        "get_today_report",
			Description: openai.String("查看今日日报的内容和状态。在用户描述工作内容时，必须先调用此工具检查今日日报是否已存在。"),
			Parameters: openai.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{},
			},
		}),

		// 创建或更新日报
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        "create_or_update_report",
			Description: openai.String("创建或更新指定日期的日报。如果该日期日报不存在则创建，已存在则更新内容。content 应包含完整的日报内容（已有内容+新内容）。"),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"date": map[string]any{
						"type":        "string",
						"description": "日报日期，格式 yyyy-MM-dd，如 2026-03-24。默认今天。",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "完整的日报内容文本。多条工作内容用换行分隔，每条一行。",
					},
				},
				"required": []string{"date", "content"},
			},
		}),

		// 查看最近日报列表
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        "list_recent_reports",
			Description: openai.String("查看最近几天的日报列表，包含日期、状态和内容摘要。"),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "查看条数，默认 5，最大 20。",
					},
				},
			},
		}),

		// 发送日报邮件（批量发送所有非草稿日报）
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        "send_report",
			Description: openai.String("发送日报邮件。会将所有非草稿状态的日报汇总到一个 Excel 附件中批量发送，无需指定日期。"),
			Parameters: openai.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{},
			},
		}),

		// 创建外出申请
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        "create_outing",
			Description: openai.String("创建外出办事申请。申请人和部门会自动从系统设置中读取。"),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"out_time": map[string]any{
						"type":        "string",
						"description": "外出时间，格式 yyyy-MM-dd HH:mm，如 2026-03-20 09:00",
					},
					"return_time": map[string]any{
						"type":        "string",
						"description": "预计返回时间，格式 yyyy-MM-dd HH:mm，如 2026-03-20 18:00",
					},
					"destination": map[string]any{
						"type":        "string",
						"description": "外出地点",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "外出事由",
					},
					"remarks": map[string]any{
						"type":        "string",
						"description": "备注说明（可选）",
					},
				},
				"required": []string{"out_time", "return_time", "destination", "reason"},
			},
		}),

		// 发送外出申请邮件
		openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        "send_outing",
			Description: openai.String("发送外出申请邮件。如果不传 id，会自动发送最近一条待发送的外出申请。"),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "integer",
						"description": "外出申请的 ID。可选，不传则自动发送最近一条待发送的外出申请。",
					},
				},
			},
		}),
	}
}

// ==================== 核心对话方法 ====================

// Chat 处理用户消息并返回 AI 回复（支持多轮 tool calling）
func (s *AIService) Chat(ctx context.Context, userID string, userMessage string) (string, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		return "", err
	}

	client := s.newClient(cfg)

	// 构建系统提示词
	systemPrompt := cfg.SystemPrompt
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = s.defaultSystemPrompt()
	}

	// 初始化消息列表
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
	}

	// 加载对话记忆
	memoryCount := s.getMemoryCount()
	if memoryCount > 0 && userID != "" {
		history, err := model.GetRecentMessages(s.db, userID, memoryCount)
		if err != nil {
			log.Printf("[AI] 加载对话记忆失败: %v\n", err)
		} else if len(history) > 0 {
			for _, msg := range history {
				switch msg.Role {
				case "user":
					messages = append(messages, openai.UserMessage(msg.Content))
				case "assistant":
					messages = append(messages, openai.AssistantMessage(msg.Content))
				}
			}
			log.Printf("[AI] 加载了 %d 条对话记忆 (user=%s)\n", len(history), userID)
		}
	}

	// 追加当前用户消息
	messages = append(messages, openai.UserMessage(userMessage))

	tools := s.buildTools()
	modelName := openai.ChatModel(cfg.Model)

	// 工具调用循环（最多 10 轮，防止无限循环）
	for round := 0; round < 10; round++ {
		params := openai.ChatCompletionNewParams{
			Model:    modelName,
			Messages: messages,
			Tools:    tools,
		}

		if cfg.MaxTokens > 0 {
			params.MaxTokens = openai.Int(cfg.MaxTokens)
		}

		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", fmt.Errorf("调用 AI 接口失败: %w", err)
		}

		if len(completion.Choices) == 0 {
			return "", fmt.Errorf("AI 返回了空的回复")
		}

		choice := completion.Choices[0]

		// 如果没有工具调用，直接返回文本回复
		if len(choice.Message.ToolCalls) == 0 {
			content := choice.Message.Content
			if content == "" {
				content = "操作完成。"
			}
			// 保存对话记忆
			if userID != "" {
				_ = model.SaveChatMessage(s.db, userID, "user", userMessage)
				_ = model.SaveChatMessage(s.db, userID, "assistant", content)
			}
			log.Printf("[AI] 对话完成，共 %d 轮工具调用，token 使用: %d\n", round, completion.Usage.TotalTokens)
			return content, nil
		}

		// 有工具调用，先把 assistant 消息加入历史
		messages = append(messages, choice.Message.ToParam())

		// 逐个执行工具调用
		for _, toolCall := range choice.Message.ToolCalls {
			funcName := toolCall.Function.Name
			funcArgs := toolCall.Function.Arguments

			log.Printf("[AI] 执行工具调用: %s(%s)\n", funcName, funcArgs)

			result := s.executeTool(funcName, funcArgs)

			// 把工具执行结果加入消息历史
			messages = append(messages, openai.ToolMessage(result, toolCall.ID))
		}
	}

	content := "抱歉，处理过程过于复杂，请简化你的请求后重试。"
	// 保存对话记忆
	if userID != "" {
		_ = model.SaveChatMessage(s.db, userID, "user", userMessage)
		_ = model.SaveChatMessage(s.db, userID, "assistant", content)
	}
	return content, nil
}

// getMemoryCount 获取 AI 记忆条数配置
func (s *AIService) getMemoryCount() int {
	v := model.GetSettingValue(s.db, model.CategoryAI, model.KeyAIMemoryCount, "20")
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 20
	}
	return n
}

// ClearMemory 清除指定用户的对话记忆
func (s *AIService) ClearMemory(userID string) (int64, error) {
	return model.ClearChatMessages(s.db, userID)
}

// ClearAllMemory 清除所有用户的对话记忆（定时任务使用）
func (s *AIService) ClearAllMemory() (int64, error) {
	return model.ClearAllChatMessages(s.db)
}

// ==================== 工具执行器 ====================

// executeTool 根据工具名称和参数执行对应的业务操作
func (s *AIService) executeTool(name, argsJSON string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(`{"error": "参数解析失败: %s"}`, err.Error())
	}

	switch name {
	case "get_today_report":
		return s.toolGetTodayReport()
	case "create_or_update_report":
		return s.toolCreateOrUpdateReport(args)
	case "list_recent_reports":
		return s.toolListRecentReports(args)
	case "send_report":
		return s.toolSendReport(args)
	case "create_outing":
		return s.toolCreateOuting(args)
	case "send_outing":
		return s.toolSendOuting(args)
	default:
		return fmt.Sprintf(`{"error": "未知的工具: %s"}`, name)
	}
}

// ==================== 各工具实现 ====================

// toolGetTodayReport 查看今日日报
func (s *AIService) toolGetTodayReport() string {
	report, err := s.reportSvc.GetToday()
	if err != nil {
		return `{"exists": false, "message": "今日日报不存在"}`
	}

	result := map[string]any{
		"exists":  true,
		"id":      report.ID,
		"date":    report.Date,
		"weekday": report.Weekday,
		"content": report.Content,
		"status":  report.Status.String(),
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// toolCreateOrUpdateReport 创建或更新日报
func (s *AIService) toolCreateOrUpdateReport(args map[string]any) string {
	date, _ := args["date"].(string)
	content, _ := args["content"].(string)

	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	if strings.TrimSpace(content) == "" {
		return `{"error": "日报内容不能为空"}`
	}

	// 尝试获取已存在的日报
	existing, err := s.reportSvc.GetByDate(date)
	if err == nil && existing != nil {
		// 日报已存在，更新内容
		report, err := s.reportSvc.Update(existing.ID, content)
		if err != nil {
			return fmt.Sprintf(`{"error": "更新日报失败: %s"}`, err.Error())
		}
		result := map[string]any{
			"action":  "updated",
			"id":      report.ID,
			"date":    report.Date,
			"weekday": report.Weekday,
			"content": report.Content,
			"status":  report.Status.String(),
		}
		data, _ := json.Marshal(result)
		return string(data)
	}

	// 日报不存在，创建新的
	report, err := s.reportSvc.Create(date, content)
	if err != nil {
		return fmt.Sprintf(`{"error": "创建日报失败: %s"}`, err.Error())
	}
	result := map[string]any{
		"action":  "created",
		"id":      report.ID,
		"date":    report.Date,
		"weekday": report.Weekday,
		"content": report.Content,
		"status":  report.Status.String(),
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// toolListRecentReports 查看最近日报列表
func (s *AIService) toolListRecentReports(args map[string]any) string {
	limit := 5
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	if limit > 20 {
		limit = 20
	}

	reports, err := s.reportSvc.ListRecent(limit)
	if err != nil {
		return fmt.Sprintf(`{"error": "查询日报列表失败: %s"}`, err.Error())
	}

	items := make([]map[string]any, 0, len(reports))
	for _, r := range reports {
		// 内容摘要，最多 100 字
		contentSummary := r.Content
		runes := []rune(contentSummary)
		if len(runes) > 100 {
			contentSummary = string(runes[:100]) + "..."
		}

		items = append(items, map[string]any{
			"id":      r.ID,
			"date":    r.Date,
			"weekday": r.Weekday,
			"status":  r.Status.String(),
			"content": contentSummary,
		})
	}

	result := map[string]any{
		"total":   len(items),
		"reports": items,
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// toolSendReport 发送日报邮件（批量发送所有非草稿日报，与定时任务一致）
func (s *AIService) toolSendReport(args map[string]any) string {
	// 获取所有非草稿日报
	reports, err := s.reportSvc.GetAllNonDraftReports()
	if err != nil {
		return fmt.Sprintf(`{"error": "获取日报列表失败: %s"}`, err.Error())
	}
	if len(reports) == 0 {
		return `{"error": "没有可发送的日报记录（所有日报均为草稿状态）"}`
	}

	// 批量发送（所有日报合并到一个 Excel 表格中）
	_, sendErr := s.emailSvc.SendBatchReports(reports, model.EmailSendTypeManual)
	if sendErr != nil {
		return fmt.Sprintf(`{"error": "发送失败: %s"}`, sendErr.Error())
	}

	return fmt.Sprintf(`{"success": true, "message": "日报邮件发送成功，共 %d 条日报", "count": %d}`, len(reports), len(reports))
}

// toolCreateOuting 创建外出申请
func (s *AIService) toolCreateOuting(args map[string]any) string {
	outTimeStr, _ := args["out_time"].(string)
	returnTimeStr, _ := args["return_time"].(string)
	destination, _ := args["destination"].(string)
	reason, _ := args["reason"].(string)
	remarks, _ := args["remarks"].(string)

	// 解析时间
	outTime, err := parseFlexibleTime(outTimeStr)
	if err != nil {
		return fmt.Sprintf(`{"error": "外出时间格式错误: %s"}`, err.Error())
	}
	returnTime, err := parseFlexibleTime(returnTimeStr)
	if err != nil {
		return fmt.Sprintf(`{"error": "返回时间格式错误: %s"}`, err.Error())
	}

	// 从设置中读取固定的申请人和部门
	applicant := model.GetSettingValue(s.db, model.CategoryOuting, model.KeyOutingApplicant, "")
	department := model.GetSettingValue(s.db, model.CategoryOuting, model.KeyOutingDepartment, "")

	if applicant == "" {
		return `{"error": "系统未配置申请人姓名，请在设置 - 外出申请中配置"}`
	}
	if department == "" {
		return `{"error": "系统未配置部门名称，请在设置 - 外出申请中配置"}`
	}

	outing := &model.OutingRequest{
		Applicant:   applicant,
		Department:  department,
		OutTime:     outTime,
		ReturnTime:  returnTime,
		Destination: destination,
		Reason:      reason,
		Remarks:     remarks,
	}

	created, err := s.outingSvc.Create(outing)
	if err != nil {
		return fmt.Sprintf(`{"error": "创建外出申请失败: %s"}`, err.Error())
	}

	result := map[string]any{
		"success":     true,
		"id":          created.ID,
		"applicant":   created.Applicant,
		"department":  created.Department,
		"out_time":    created.FormatOutTime(),
		"return_time": created.FormatReturnTime(),
		"destination": created.Destination,
		"reason":      created.Reason,
		"status":      created.Status.String(),
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// toolSendOuting 发送外出申请邮件
func (s *AIService) toolSendOuting(args map[string]any) string {
	var outing *model.OutingRequest
	var err error

	idFloat, ok := args["id"].(float64)
	if ok && idFloat > 0 {
		// 指定了 ID，按 ID 查找
		id := uint(idFloat)
		outing, err = s.outingSvc.GetByID(id)
		if err != nil {
			return fmt.Sprintf(`{"error": "找不到外出申请(id=%d)"}`, id)
		}
	} else {
		// 未指定 ID，自动查找最近一条待发送的外出申请
		outings, listErr := s.outingSvc.ListRecent(10)
		if listErr != nil || len(outings) == 0 {
			return `{"error": "没有找到外出申请记录，请先创建外出申请"}`
		}
		found := false
		for i := range outings {
			if outings[i].Status == model.OutingStatusReady || outings[i].Status == model.OutingStatusFailed {
				outing = &outings[i]
				found = true
				break
			}
		}
		if !found {
			return `{"error": "没有待发送的外出申请，所有外出申请都已发送"}`
		}
	}

	// 1. 获取外出申请独立的收件人配置
	toStr := model.GetSettingValue(s.db, model.CategoryOuting, model.KeyOutingRecipients, "")
	ccStr := model.GetSettingValue(s.db, model.CategoryOuting, model.KeyOutingCc, "")
	toList := splitAndTrimList(toStr)
	ccList := splitAndTrimList(ccStr)
	if len(toList) == 0 {
		return `{"error": "未配置外出申请收件人，请在系统设置 - 外出申请中配置"}`
	}

	// 2. 获取 SMTP 配置
	smtpCfg, err := s.emailSvc.GetSMTPConfig()
	if err != nil {
		return fmt.Sprintf(`{"error": "SMTP 配置错误: %s"}`, err.Error())
	}

	// 3. 渲染邮件主题
	subjectTmpl := model.GetSettingValue(s.db, model.CategoryOuting, model.KeyOutingSubject, "外出申请 - {{.Applicant}} {{.OutDate}}")
	subject := s.renderOutingSubject(subjectTmpl, outing)

	// 4. 渲染邮件正文
	body := s.renderOutingEmailBody(outing)

	// 5. 创建邮件日志
	now := time.Now()
	emailLog := &model.EmailLog{
		LogType:    model.LogTypeOuting,
		OutingID:   &outing.ID,
		Subject:    subject,
		Recipients: strings.Join(toList, ","),
		CcList:     strings.Join(ccList, ","),
		Content:    body,
		Status:     model.EmailStatusSending,
		SendType:   model.EmailSendTypeManual,
		SentAt:     &now,
	}
	s.db.Create(emailLog)

	// 6. 发送邮件
	msg := &EmailMessage{
		To:      toList,
		Cc:      ccList,
		Subject: subject,
		Body:    body,
	}

	sendErr := s.emailSvc.SendCustom(smtpCfg, msg)
	if sendErr != nil {
		s.db.Model(emailLog).Updates(map[string]interface{}{
			"status":    model.EmailStatusFailed,
			"error_msg": sendErr.Error(),
		})
		s.outingSvc.UpdateStatus(outing.ID, model.OutingStatusFailed)
		return fmt.Sprintf(`{"error": "发送外出申请邮件失败: %s"}`, sendErr.Error())
	}

	// 发送成功
	s.db.Model(emailLog).Updates(map[string]interface{}{
		"status":  model.EmailStatusSuccess,
		"sent_at": now,
	})
	s.outingSvc.UpdateStatus(outing.ID, model.OutingStatusSent)

	return fmt.Sprintf(`{"success": true, "message": "外出申请邮件发送成功", "id": %d}`, outing.ID)
}

// splitAndTrimList 按逗号或换行分隔并去除空白
func splitAndTrimList(s string) []string {
	s = strings.ReplaceAll(s, "\n", ",")
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// renderOutingSubject 渲染外出申请邮件主题
func (s *AIService) renderOutingSubject(tmpl string, outing *model.OutingRequest) string {
	t, err := template.New("subject").Parse(tmpl)
	if err != nil {
		return fmt.Sprintf("外出申请 - %s %s", outing.Applicant, outing.OutTime.Format("2006-01-02"))
	}

	data := map[string]string{
		"Applicant":   outing.Applicant,
		"Department":  outing.Department,
		"Destination": outing.Destination,
		"Reason":      outing.Reason,
		"OutDate":     outing.OutTime.Format("2006-01-02"),
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Sprintf("外出申请 - %s %s", outing.Applicant, outing.OutTime.Format("2006-01-02"))
	}
	return buf.String()
}

// renderOutingEmailBody 生成外出申请 HTML 邮件正文
func (s *AIService) renderOutingEmailBody(outing *model.OutingRequest) string {
	applicant := html.EscapeString(outing.Applicant)
	department := html.EscapeString(outing.Department)
	outTime := html.EscapeString(outing.FormatOutTime())
	returnTime := html.EscapeString(outing.FormatReturnTime())
	destination := html.EscapeString(outing.Destination)
	reason := html.EscapeString(outing.Reason)
	remarks := html.EscapeString(outing.Remarks)

	const cellFont = "font-size: 16px; font-family: 楷体;"
	const labelCell = "border-right: 1px solid windowtext; border-bottom: 1px solid windowtext; border-left: 1px solid windowtext; border-top: none; padding: 0px 7px;"
	const valueCell = "border-top: none; border-left: none; border-bottom: 1px solid windowtext; border-right: 1px solid windowtext; padding: 0px 7px;"
	const pStyle = "margin: 0px; text-align: justify; font-size: 14px; font-family: Calibri, sans-serif;"

	var buf bytes.Buffer
	buf.WriteString(`<table border="1" cellpadding="0" cellspacing="0" style="width:631px;border-collapse:collapse;border:none;"><tbody>`)
	buf.WriteString(`<tr><td colspan="4" valign="top" width="631" style="border:1px solid windowtext;padding:0 7px;"><p style="` + pStyle + `"><span style="` + cellFont + `">外出申请表</span></p></td></tr>`)
	buf.WriteString(`<tr><td valign="top" width="130" style="` + labelCell + `"><p style="` + pStyle + `"><span style="` + cellFont + `">申请人</span></p></td>`)
	buf.WriteString(`<td valign="top" width="227" style="` + valueCell + `"><p style="` + pStyle + `"><span style="` + cellFont + `">&nbsp;` + applicant + `</span></p></td>`)
	buf.WriteString(`<td valign="top" width="66" style="` + valueCell + `"><p style="` + pStyle + `"><span style="` + cellFont + `">部门</span></p></td>`)
	buf.WriteString(`<td valign="top" width="208" style="` + valueCell + `"><p style="` + pStyle + `"><span style="` + cellFont + `">&nbsp;` + department + `</span></p></td></tr>`)

	rows := []struct{ label, value string }{
		{"申请外出时间", outTime},
		{"预计返回时间", returnTime},
		{"外出地点", destination},
		{"外出事由", reason},
		{"备注说明", remarks},
	}
	for _, row := range rows {
		buf.WriteString(`<tr><td valign="top" width="130" style="` + labelCell + `"><p style="` + pStyle + `"><span style="` + cellFont + `">` + row.label + `</span></p></td>`)
		buf.WriteString(`<td colspan="3" valign="top" width="501" style="` + valueCell + `"><p style="` + pStyle + `"><span style="` + cellFont + `">&nbsp;` + row.value + `</span></p></td></tr>`)
	}

	buf.WriteString(`</tbody></table>`)
	return buf.String()
}

// ==================== 辅助方法 ====================

// parseFlexibleTime 灵活解析时间字符串，支持多种格式
func parseFlexibleTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("时间不能为空")
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")
	if loc == nil {
		loc = time.Local
	}

	formats := []string{
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006/01/02 15:04",
		"2006/01/02 15:04:05",
		"2006年01月02日 15:04",
		"2006年1月2日 15:04",
		"2006年01月02日15时04分",
		"2006年1月2日15时04分",
		"01-02 15:04",
		"1月2日 15:04",
	}

	for _, layout := range formats {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			// 如果年份为0（只有月日），自动补今年
			if t.Year() == 0 {
				t = t.AddDate(time.Now().Year(), 0, 0)
			}
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("无法解析时间: %s", s)
}

// TestConnection 测试 AI 接口连通性
func (s *AIService) TestConnection(ctx context.Context) (string, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		return "", err
	}

	client := s.newClient(cfg)
	modelName := openai.ChatModel(cfg.Model)

	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: modelName,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("你好，请用一句话介绍自己。"),
		},
		MaxTokens: openai.Int(100),
	})
	if err != nil {
		return "", fmt.Errorf("AI 接口连接失败: %w", err)
	}

	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("AI 返回了空的回复")
	}

	reply := completion.Choices[0].Message.Content
	info := fmt.Sprintf("模型: %s | 回复: %s", completion.Model, reply)
	return info, nil
}
