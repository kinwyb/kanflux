package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/kinwyb/kanflux/scheduler"
)

// SchedulerTool 定时任务管理工具
type SchedulerTool struct {
	scheduler scheduler.Accessor
	name      string
}

// NewSchedulerTool 创建定时任务管理工具
func NewSchedulerTool(schedulerAccessor scheduler.Accessor) *SchedulerTool {
	return &SchedulerTool{
		scheduler: schedulerAccessor,
		name:      "scheduler",
	}
}

// Name 返回工具名称
func (t *SchedulerTool) Name() string {
	return t.name
}

// Description 返回工具描述
func (t *SchedulerTool) Description() string {
	return `Provides comprehensive management of scheduled tasks (cron jobs).

## Operations

- **list**: List all scheduled tasks with their details (status, next run time, etc.)
- **add**: Create a new scheduled task with cron expression
- **update**: Modify an existing task's configuration
- **remove**: Delete a task by its ID
- **trigger**: Manually execute a task immediately (regardless of schedule)
- **status**: Get detailed status of a specific task

## Parameters

- **action** (required): The operation to perform: "list", "add", "update", "remove", "trigger", "status"

### For "add" and "update" actions:
- **id**: Unique task identifier (required for add)
- **name**: Human-readable task name
- **description**: Task description
- **enabled**: Whether task is active (default: true)
- **cron**: Cron expression in 5-field format (minute hour day month weekday)
  - Example: "*/5 * * * *" = every 5 minutes
  - Example: "0 9 * * *" = daily at 9:00
  - Example: "0 9 * * 1-5" = weekdays at 9:00
- **channel**: Target channel (tui, telegram, feishu, wxcom)
- **chat_id**: Target chat/conversation ID
- **prompt**: The prompt content to be processed by the agent

### For "remove" and "trigger" and "status" actions:
- **id**: The task ID to operate on

## Cron Expression Format
Standard 5-field format: minute hour day-of-month month day-of-week
- Field ranges: minute (0-59), hour (0-23), day (1-31), month (1-12), weekday (0-7, 0 and 7 are Sunday)
- Special characters: * (any), */n (every n), , (list), - (range)

## Notes
- Tasks send prompts to specified channels for agent processing
- Use "trigger" to test a task without waiting for its schedule
- Tasks maintain execution history (success/fail counts, last result)`
}

// Parameters 返回参数定义
func (t *SchedulerTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Required. Action: 'list', 'add', 'update', 'remove', 'trigger', 'status'",
				"enum":        []string{"list", "add", "update", "remove", "trigger", "status"},
			},
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Task unique identifier. Required for add, update, remove, trigger, status actions.",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Task name. Used in add and update actions.",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Task description. Used in add and update actions.",
			},
			"enabled": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether task is enabled. Default: true. Used in add and update actions.",
				"default":     true,
			},
			"cron": map[string]interface{}{
				"type":        "string",
				"description": "Cron expression (5-field format: minute hour day month weekday). Used in add and update actions.",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Target channel (tui, telegram, feishu, wxcom). Used in add and update actions.",
			},
			"account_id": map[string]interface{}{
				"type":        "string",
				"description": "Account ID for multi-account scenarios. Used in add and update actions.",
			},
			"chat_id": map[string]interface{}{
				"type":        "string",
				"description": "Target chat/conversation ID. Used in add and update actions.",
			},
			"agent_name": map[string]interface{}{
				"type":        "string",
				"description": "Optional agent name to use. Used in add and update actions.",
			},
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Prompt content for agent processing. Used in add and update actions.",
			},
		},
		"required": []string{"action"},
	}
}

// Execute 执行工具
func (t *SchedulerTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	action, _ := params["action"].(string)
	if action == "" {
		return "", fmt.Errorf("action is required")
	}

	// 检查 scheduler 是否可用
	if t.scheduler == nil {
		return "", fmt.Errorf("scheduler not available")
	}

	switch action {
	case "list":
		return t.handleList()
	case "add":
		return t.handleAdd(params)
	case "update":
		return t.handleUpdate(params)
	case "remove":
		return t.handleRemove(params)
	case "trigger":
		return t.handleTrigger(params)
	case "status":
		return t.handleStatus(params)
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

// handleList 处理列出任务操作
func (t *SchedulerTool) handleList() (string, error) {
	// 尝试使用 GetTaskInfoList（如果 Scheduler 实现了这个方法）
	if s, ok := t.scheduler.(*scheduler.Scheduler); ok {
		infoList := s.GetTaskInfoList()
		if len(infoList) == 0 {
			return "No scheduled tasks configured", nil
		}

		result := fmt.Sprintf("Scheduled tasks (total: %d):\n\n", len(infoList))
		for _, info := range infoList {
			result += formatTaskInfo(info)
			result += "\n"
		}
		return result, nil
	}

	// 回退到使用 ListTaskDetails
	details := t.scheduler.ListTaskDetails()
	if len(details) == 0 {
		return "No scheduled tasks configured", nil
	}

	result := fmt.Sprintf("Scheduled tasks (total: %d):\n\n", len(details))
	for _, detail := range details {
		result += formatTaskDetail(detail)
		result += "\n"
	}
	return result, nil
}

// handleAdd 处理添加任务操作
func (t *SchedulerTool) handleAdd(params map[string]interface{}) (string, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return "", fmt.Errorf("task id is required for 'add' action")
	}

	cron, _ := params["cron"].(string)
	if cron == "" {
		return "", fmt.Errorf("cron expression is required for 'add' action")
	}

	channel, _ := params["channel"].(string)
	if channel == "" {
		channel = "tui" // 默认使用 tui channel
	}

	chatID, _ := params["chat_id"].(string)
	if chatID == "" {
		chatID = "default" // 默认 chat
	}

	prompt, _ := params["prompt"].(string)
	if prompt == "" {
		return "", fmt.Errorf("prompt is required for 'add' action")
	}

	name, _ := params["name"].(string)
	if name == "" {
		name = id
	}

	description, _ := params["description"].(string)
	enabled := true // 默认启用
	if e, ok := params["enabled"].(bool); ok {
		enabled = e
	}

	config := &scheduler.TaskConfig{
		ID:          id,
		Name:        name,
		Description: description,
		Enabled:     enabled,
		Schedule: scheduler.ScheduleConfig{
			Cron: cron,
		},
		Target: scheduler.TargetConfig{
			Channel:   channel,
			AccountID: params["account_id"].(string),
			ChatID:    chatID,
			AgentName: params["agent_name"].(string),
		},
		Content: scheduler.ContentConfig{
			Prompt: prompt,
		},
	}

	if err := t.scheduler.AddTask(config); err != nil {
		return "", fmt.Errorf("failed to add task: %w", err)
	}

	return fmt.Sprintf("Task '%s' added successfully. Cron: %s, Channel: %s, ChatID: %s",
		id, cron, channel, chatID), nil
}

// handleUpdate 处理更新任务操作
func (t *SchedulerTool) handleUpdate(params map[string]interface{}) (string, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return "", fmt.Errorf("task id is required for 'update' action")
	}

	// 先获取当前任务详情
	details := t.scheduler.ListTaskDetails()
	var currentConfig *scheduler.TaskConfig
	for _, d := range details {
		if d.Config != nil && d.Config.ID == id {
			currentConfig = d.Config
			break
		}
	}

	if currentConfig == nil {
		return "", fmt.Errorf("task '%s' not found", id)
	}

	// 更新配置（保留未修改的字段）
	if name, ok := params["name"].(string); ok && name != "" {
		currentConfig.Name = name
	}
	if desc, ok := params["description"].(string); ok {
		currentConfig.Description = desc
	}
	if enabled, ok := params["enabled"].(bool); ok {
		currentConfig.Enabled = enabled
	}
	if cron, ok := params["cron"].(string); ok && cron != "" {
		currentConfig.Schedule.Cron = cron
	}
	if channel, ok := params["channel"].(string); ok && channel != "" {
		currentConfig.Target.Channel = channel
	}
	if accountID, ok := params["account_id"].(string); ok {
		currentConfig.Target.AccountID = accountID
	}
	if chatID, ok := params["chat_id"].(string); ok && chatID != "" {
		currentConfig.Target.ChatID = chatID
	}
	if agentName, ok := params["agent_name"].(string); ok {
		currentConfig.Target.AgentName = agentName
	}
	if prompt, ok := params["prompt"].(string); ok && prompt != "" {
		currentConfig.Content.Prompt = prompt
	}

	if err := t.scheduler.UpdateTask(currentConfig); err != nil {
		return "", fmt.Errorf("failed to update task: %w", err)
	}

	return fmt.Sprintf("Task '%s' updated successfully", id), nil
}

// handleRemove 处理删除任务操作
func (t *SchedulerTool) handleRemove(params map[string]interface{}) (string, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return "", fmt.Errorf("task id is required for 'remove' action")
	}

	if err := t.scheduler.RemoveTask(id); err != nil {
		return "", fmt.Errorf("failed to remove task: %w", err)
	}

	return fmt.Sprintf("Task '%s' removed successfully", id), nil
}

// handleTrigger 处理手动触发任务操作
func (t *SchedulerTool) handleTrigger(params map[string]interface{}) (string, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return "", fmt.Errorf("task id is required for 'trigger' action")
	}

	if err := t.scheduler.TriggerTask(id); err != nil {
		return "", fmt.Errorf("failed to trigger task: %w", err)
	}

	return fmt.Sprintf("Task '%s' triggered successfully. It will execute immediately.", id), nil
}

// handleStatus 处理获取任务状态操作
func (t *SchedulerTool) handleStatus(params map[string]interface{}) (string, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return "", fmt.Errorf("task id is required for 'status' action")
	}

	// 尝试使用 GetTaskInfo（如果 Scheduler 实现了这个方法）
	if s, ok := t.scheduler.(*scheduler.Scheduler); ok {
		info, err := s.GetTaskInfo(id)
		if err != nil {
			return "", fmt.Errorf("failed to get task info: %w", err)
		}

		result := formatTaskInfoFull(info)
		return result, nil
	}

	// 回退到使用 GetTaskStatus 和 ListTaskDetails
	state, err := t.scheduler.GetTaskStatus(id)
	if err != nil {
		return "", fmt.Errorf("failed to get task status: %w", err)
	}

	// 获取任务详细信息
	details := t.scheduler.ListTaskDetails()
	var detail *scheduler.TaskDetail
	for i := range details {
		if details[i].Config != nil && details[i].Config.ID == id {
			detail = &details[i]
			break
		}
	}

	result := fmt.Sprintf("Task '%s' status:\n", id)
	if detail != nil && detail.Config != nil {
		result += fmt.Sprintf("  Name: %s\n", detail.Config.Name)
		result += fmt.Sprintf("  Description: %s\n", detail.Config.Description)
		result += fmt.Sprintf("  Enabled: %v\n", detail.Config.Enabled)
		result += fmt.Sprintf("  Cron: %s\n", detail.Config.Schedule.Cron)
		result += fmt.Sprintf("  Channel: %s\n", detail.Config.Target.Channel)
		result += fmt.Sprintf("  ChatID: %s\n", detail.Config.Target.ChatID)
		result += fmt.Sprintf("  AgentName: %s\n", detail.Config.Target.AgentName)
	}
	result += fmt.Sprintf("  Running: %v\n", detail != nil && detail.IsRunning)
	if state != nil {
		result += fmt.Sprintf("  NextRun: %s\n", formatTime(state.NextRun))
		result += fmt.Sprintf("  LastRun: %s\n", formatTime(state.LastRun))
		result += fmt.Sprintf("  LastResult: %s\n", state.LastResult)
		if state.LastError != "" {
			result += fmt.Sprintf("  LastError: %s\n", state.LastError)
		}
		result += fmt.Sprintf("  SuccessCount: %d\n", state.SuccessCount)
		result += fmt.Sprintf("  FailCount: %d\n", state.FailCount)
	}

	return result, nil
}

// formatTaskInfo 格式化任务信息摘要
func formatTaskInfo(info scheduler.TaskInfo) string {
	result := fmt.Sprintf("  - ID: %s\n", info.ID)
	result += fmt.Sprintf("    Name: %s\n", info.Name)
	result += fmt.Sprintf("    Enabled: %v\n", info.Enabled)
	result += fmt.Sprintf("    Cron: %s\n", info.Cron)
	result += fmt.Sprintf("    Channel: %s, ChatID: %s\n", info.Channel, info.ChatID)
	result += fmt.Sprintf("    NextRun: %s\n", formatTime(info.NextRun))
	if info.IsRunning {
		result += "    Status: RUNNING\n"
	} else if info.LastError != "" {
		result += fmt.Sprintf("    Status: FAILED (error: %s)\n", info.LastError)
	} else if !info.LastRun.IsZero() {
		result += "    Status: IDLE\n"
	}

	if info.SuccessCount > 0 || info.FailCount > 0 {
		result += fmt.Sprintf("    Executions: %d success, %d fail\n",
			info.SuccessCount, info.FailCount)
	}

	return result
}

// formatTaskInfoFull 格式化完整的任务信息
func formatTaskInfoFull(info *scheduler.TaskInfo) string {
	result := fmt.Sprintf("Task '%s' status:\n", info.ID)
	result += fmt.Sprintf("  Name: %s\n", info.Name)
	result += fmt.Sprintf("  Description: %s\n", info.Description)
	result += fmt.Sprintf("  Enabled: %v\n", info.Enabled)
	result += fmt.Sprintf("  Cron: %s\n", info.Cron)
	result += fmt.Sprintf("  Channel: %s\n", info.Channel)
	result += fmt.Sprintf("  ChatID: %s\n", info.ChatID)
	result += fmt.Sprintf("  AgentName: %s\n", info.AgentName)
	result += fmt.Sprintf("  Running: %v\n", info.IsRunning)
	result += fmt.Sprintf("  NextRun: %s\n", formatTime(info.NextRun))
	result += fmt.Sprintf("  LastRun: %s\n", formatTime(info.LastRun))
	if info.LastResult != "" {
		result += fmt.Sprintf("  LastResult: %s\n", info.LastResult)
	}
	if info.LastError != "" {
		result += fmt.Sprintf("  LastError: %s\n", info.LastError)
	}
	result += fmt.Sprintf("  SuccessCount: %d\n", info.SuccessCount)
	result += fmt.Sprintf("  FailCount: %d\n", info.FailCount)
	return result
}

// formatTaskDetail 格式化任务详情
func formatTaskDetail(detail scheduler.TaskDetail) string {
	config := detail.Config
	if config == nil {
		return "  - [unknown task]"
	}

	result := fmt.Sprintf("  - ID: %s\n", config.ID)
	result += fmt.Sprintf("    Name: %s\n", config.Name)
	result += fmt.Sprintf("    Enabled: %v\n", config.Enabled)
	result += fmt.Sprintf("    Cron: %s\n", config.Schedule.Cron)
	result += fmt.Sprintf("    Channel: %s, ChatID: %s\n", config.Target.Channel, config.Target.ChatID)
	result += fmt.Sprintf("    NextRun: %s\n", formatTime(detail.NextRun))
	if detail.IsRunning {
		result += "    Status: RUNNING\n"
	} else if detail.State != nil && detail.State.LastError != "" {
		result += fmt.Sprintf("    Status: FAILED (error: %s)\n", detail.State.LastError)
	} else if !detail.LastRun.IsZero() {
		result += "    Status: IDLE\n"
	}

	if detail.State != nil {
		result += fmt.Sprintf("    Executions: %d success, %d fail\n",
			detail.State.SuccessCount, detail.State.FailCount)
	}

	return result
}

// formatTime 格式化时间
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Format("2006-01-02 15:04:05")
}