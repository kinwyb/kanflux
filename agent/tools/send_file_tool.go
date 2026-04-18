package tools

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kinwyb/kanflux/bus"
)

// SendFileTool 发送文件工具
// 允许 LLM 调用工具发送文件给用户
type SendFileTool struct {
	bus         *bus.MessageBus
	responseMgr *bus.RequestResponseManager
}

// NewSendFileTool 创建发送文件工具
func NewSendFileTool(bus *bus.MessageBus, responseMgr *bus.RequestResponseManager) *SendFileTool {
	return &SendFileTool{
		bus:         bus,
		responseMgr: responseMgr,
	}
}

// Name 返回工具名称
func (t *SendFileTool) Name() string {
	return "send_file"
}

// Description 返回工具描述
func (t *SendFileTool) Description() string {
	return "发送文件给用户。通过指定的文件路径发送文件，可选添加说明文字。"
}

// Parameters 返回参数定义
func (t *SendFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "要发送的文件的绝对路径",
			},
			"caption": map[string]interface{}{
				"type":        "string",
				"description": "文件说明文字（可选）",
			},
		},
		"required": []string{"file_path"},
	}
}

// Execute 执行工具
func (t *SendFileTool) Execute(ctx context.Context, params map[string]interface{}) (string, error) {
	// 获取参数
	filePath, ok := params["file_path"].(string)
	if !ok || filePath == "" {
		return "", fmt.Errorf("file_path 参数缺失或无效")
	}

	caption := ""
	if c, ok := params["caption"].(string); ok {
		caption = c
	}

	// 验证文件存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("文件不存在: %s", filePath)
	}

	// 从 context 获取 channel 和 chat_id
	channel, ok := ctx.Value("channel").(string)
	if !ok || channel == "" {
		return "", fmt.Errorf("无法获取 channel 信息")
	}

	chatID, ok := ctx.Value("chat_id").(string)
	if !ok || chatID == "" {
		return "", fmt.Errorf("无法获取 chat_id 信息")
	}

	// 创建请求
	requestID, responseChan := t.responseMgr.CreateRequest()

	// 构建请求消息
	request := &bus.OutboundMessage{
		RequestID:   requestID,
		IsRequest:   true,
		RequestType: bus.RequestTypeSendFile,
		Channel:     channel,
		ChatID:      chatID,
		Content:     caption,
		Media: []bus.Media{
			{
				Type:     "document",
				URL:      filePath,
				Metadata: map[string]interface{}{"caption": caption},
			},
		},
		Timestamp: time.Now(),
	}

	// 发送请求
	if err := t.bus.PublishOutbound(ctx, request); err != nil {
		return "", fmt.Errorf("发送请求失败: %w", err)
	}

	// 等待响应
	response, err := t.responseMgr.WaitForResponse(ctx, requestID, responseChan)
	if err != nil {
		if err == context.DeadlineExceeded {
			return "", fmt.Errorf("等待响应超时")
		}
		return "", fmt.Errorf("等待响应失败: %w", err)
	}

	// 处理响应
	if response.Error != "" {
		return "", fmt.Errorf("发送失败: %s", response.Error)
	}

	return response.Result, nil
}

// ApprovalPrompt 返回审批提示内容
func (t *SendFileTool) ApprovalPrompt(argsJSON string) string {
	return fmt.Sprintf("是否允许发送文件？\n参数: %s", argsJSON)
}