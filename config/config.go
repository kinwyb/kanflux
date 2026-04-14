package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config 顶层配置
type Config struct {
	Providers       map[string]*ProviderConfig `json:"providers"`
	DefaultProvider string                     `json:"default_provider"`
	Agents          []*AgentConfig             `json:"agents"`
	Tools           *ToolsConfig               `json:"tools"` // 工具配置
	// 公共知识库配置
	KnowledgeBases map[string]*KnowledgeBaseConfig `json:"knowledge_bases"` // 公共知识库，多个 agent 可共用
	// Channel 配置
	Channels *ChannelsConfig `json:"channels"` // 通道配置
	// WebSocket 配置
	WebSocket *WebSocketConfig `json:"websocket"` // WebSocket 服务配置
}

// KnowledgeBaseConfig 公共知识库配置
type KnowledgeBaseConfig struct {
	Paths         []KnowledgePathConfig `json:"paths"`          // 知识库路径列表
	Embedding     *EmbeddingConfig      `json:"embedding"`      // Embedding 配置
	RAGConfig     *RAGConfigOptions     `json:"rag_config"`     // RAG 详细配置
}

// EmbeddingConfig Embedding 配置
type EmbeddingConfig struct {
	Provider   string `json:"provider"`    // Provider 名称，为空时使用 default_provider
	Model      string `json:"model"`       // Embedding 模型名称
	APIKey     string `json:"api_key"`     // 可选，为空时使用 provider 的 api_key
	APIBaseURL string `json:"api_base_url"`// 可选，为空时使用 provider 的 api_base_url
}

// ToolsConfig 工具配置
type ToolsConfig struct {
	Approval []string `json:"approval"` // 需要审批的工具名称列表
}

// ProviderConfig 供应商配置
type ProviderConfig struct {
	APIKey       string `json:"api_key"`
	APIBaseURL   string `json:"api_base_url"`
	DefaultModel string `json:"default_model"`
}

// AgentType agent 类型
type AgentType string

const (
	AgentTypeChatModel  AgentType = "chatmodel"  // 基础 ReAct 模式
	AgentTypeDeep       AgentType = "deep"       // 预构建 agent（规划+文件系统+子agent）
	AgentTypePlanExecute AgentType = "planexecute" // Plan-Execute-Replan 模式
	AgentTypeSupervisor AgentType = "supervisor" // 监督者模式
)

// AgentConfig agent 配置
type AgentConfig struct {
	Name           string              `json:"name"`
	Type           AgentType           `json:"type"`           // Agent 类型，默认 deep
	Description    string              `json:"description"`    // Agent 描述，未配置时使用默认描述
	Workspace      string              `json:"workspace"`      // 必须指定
	SubAgents      []string            `json:"sub_agents"`     // 子 agent 名称列表
	Provider       string              `json:"provider"`       // 未指定使用 default_provider
	Model          string              `json:"model"`          // 未指定使用供应商的 default_model
	MaxIteration   int                 `json:"max_iteration"`  // 默认 10
	Streaming      bool                `json:"streaming"`      // 默认 true
	Tools          []string            `json:"tools"`          // 允许使用的工具列表，空表示所有工具可用
	ToolsApproval  []string            `json:"tools_approval"` // 需要审批的工具列表，继承全局配置并追加
	// RAG 配置
	KnowledgePaths     []KnowledgePathConfig `json:"knowledge_paths"`     // 私有知识库路径配置
	KnowledgeBaseRefs  []string              `json:"knowledge_base_refs"` // 引用公共知识库名称列表
	Embedding          *EmbeddingConfig      `json:"embedding"`           // Embedding 配置（可独立于 agent provider）
	RAGConfig          *RAGConfigOptions     `json:"rag_config"`          // RAG 详细配置
	// Memoria 记忆摘要配置
	SummarizeModel     *EmbeddingConfig      `json:"summarize_model"`     // 记忆摘要模型配置（用于 Memoria）
}

// KnowledgePathConfig 知识库路径配置
type KnowledgePathConfig struct {
	Path       string   `json:"path"`       // 路径（相对于 workspace 或绝对路径）
	Extensions []string `json:"extensions"` // 文件扩展名过滤，如 ["md", "txt", "go"]
	Recursive  bool     `json:"recursive"`  // 是否递归子目录
	Exclude    []string `json:"exclude"`    // 排除模式，如 ["*.tmp", "test_*"]
}

// RAGConfigOptions RAG 详细配置选项
type RAGConfigOptions struct {
	ChunkSize      int            `json:"chunk_size"`      // 分块大小，默认 500
	ChunkOverlap   int            `json:"chunk_overlap"`   // 分块重叠，默认 50
	TopK           int            `json:"top_k"`           // 检索数量，默认 5
	ScoreThreshold float64        `json:"score_threshold"` // 相关性阈值，默认 0.5
	EnableWatcher  bool           `json:"enable_watcher"`  // 启用文件监控，默认 true
	StoreType      string         `json:"store_type"`      // 存储类型: json, redis, milvus
	StoreOptions   map[string]any `json:"store_options"`   // 存储额外配置
}

// ResolvedAgentConfig 解析后的 agent 配置（包含最终确定的值）
type ResolvedAgentConfig struct {
	Name           string
	Type           AgentType // Agent 类型
	Description    string    // Agent 描述
	Workspace      string
	SubAgents      []string  // 子 agent 名称列表
	Provider       string
	Model          string
	APIKey         string
	APIBaseURL     string
	MaxIteration   int
	Streaming      bool
	Tools          []string  // 允许使用的工具列表，空表示所有工具可用
	ToolsApproval  []string  // 需要审批的工具列表
	// RAG 配置
	KnowledgePaths      []KnowledgePathConfig // 知识库路径配置（私有 + 公共）
	RAGConfig           *RAGConfigOptions     // RAG 详细配置
	// Embedding 配置
	EmbeddingProvider   string // Embedding provider 名称
	EmbeddingModel      string // Embedding 模型名称
	EmbeddingAPIKey     string // Embedding API Key
	EmbeddingAPIBaseURL string // Embedding API Base URL
	// Memoria 记忆摘要配置
	SummarizeProvider   string // 记忆摘要 provider 名称
	SummarizeModel      string // 记忆摘要模型名称
	SummarizeAPIKey     string // 记忆摘要 API Key
	SummarizeAPIBaseURL string // 记忆摘要 API Base URL
}

// Load 从指定路径加载配置文件
func Load(path string) (*Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// LoadDefault 从默认路径加载配置文件
// 查找顺序: ./kanflux.json -> ~/.kanflux/config.json
func LoadDefault() (*Config, error) {
	// 查找默认路径
	paths := []string{
		"kanflux.json",                   // 当前目录
		filepath.Join(homeDir(), ".kanflux", "config.json"), // 用户目录
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return Load(path)
		}
	}

	return nil, errors.New("no config file found in default paths")
}

// homeDir 获取用户主目录
func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	// Windows
	if home := os.Getenv("USERPROFILE"); home != "" {
		return home
	}
	return ""
}

// GetAgent 获取指定名称的 agent 配置
func (c *Config) GetAgent(name string) *AgentConfig {
	for _, agent := range c.Agents {
		if agent.Name == name {
			return agent
		}
	}
	return nil
}

// ResolveAgentConfig 解析 agent 的最终配置
// 处理 provider/model 的默认值继承
func (c *Config) ResolveAgentConfig(name string) (*ResolvedAgentConfig, error) {
	agent := c.GetAgent(name)
	if agent == nil {
		return nil, fmt.Errorf("agent '%s' not found in config", name)
	}

	// workspace 必须指定
	if agent.Workspace == "" {
		return nil, fmt.Errorf("agent '%s': workspace is required", name)
	}

	// 确定 provider
	providerName := agent.Provider
	if providerName == "" {
		providerName = c.DefaultProvider
	}
	if providerName == "" {
		return nil, fmt.Errorf("agent '%s': no provider specified and no default_provider defined", name)
	}

	// 获取 provider 配置
	provider := c.Providers[providerName]
	if provider == nil {
		return nil, fmt.Errorf("agent '%s': provider '%s' not found", name, providerName)
	}

	// 确定 model
	model := agent.Model
	if model == "" {
		model = provider.DefaultModel
	}
	if model == "" {
		return nil, fmt.Errorf("agent '%s': no model specified and provider '%s' has no default_model", name, providerName)
	}

	// 设置默认值
	maxIteration := agent.MaxIteration
	if maxIteration == 0 {
		maxIteration = 10
	}

	// 设置默认描述
	description := agent.Description
	if description == "" {
		description = fmt.Sprintf("Agent %s for general tasks", name)
	}

	// 设置默认类型
	agentType := agent.Type
	if agentType == "" {
		agentType = AgentTypeDeep // 默认使用 DeepAgent
	}

	// 处理工具配置
	tools := agent.Tools // 空表示所有工具可用
	toolsApproval := agent.ToolsApproval

	// 合并全局工具审批配置
	if c.Tools != nil && len(c.Tools.Approval) > 0 {
		toolsApproval = mergeStringLists(c.Tools.Approval, toolsApproval)
	}

	// 合并知识库路径：私有路径 + 引用的公共知识库路径
	knowledgePaths := make([]KnowledgePathConfig, 0)
	// 1. 添加私有知识库路径
	knowledgePaths = append(knowledgePaths, agent.KnowledgePaths...)
	// 2. 添加引用的公共知识库路径
	for _, ref := range agent.KnowledgeBaseRefs {
		if kb, ok := c.KnowledgeBases[ref]; ok {
			knowledgePaths = append(knowledgePaths, kb.Paths...)
		}
	}

	// 解析 Embedding 配置
	embeddingProvider, embeddingModel, embeddingAPIKey, embeddingAPIBaseURL := c.resolveEmbeddingConfig(agent, providerName, provider)

	// 解析 SummarizeModel 配置（用于 Memoria）
	summarizeProvider, summarizeModel, summarizeAPIKey, summarizeAPIBaseURL := c.resolveSummarizeModelConfig(agent, providerName, provider, model)

	return &ResolvedAgentConfig{
		Name:                agent.Name,
		Type:                agentType,
		Description:         description,
		Workspace:           agent.Workspace,
		SubAgents:           agent.SubAgents,
		Provider:            providerName,
		Model:               model,
		APIKey:              provider.APIKey,
		APIBaseURL:          provider.APIBaseURL,
		MaxIteration:        maxIteration,
		Streaming:           agent.Streaming,
		Tools:               tools,
		ToolsApproval:       toolsApproval,
		KnowledgePaths:      knowledgePaths,
		RAGConfig:           c.resolveFinalRAGConfig(agent),
		EmbeddingProvider:   embeddingProvider,
		EmbeddingModel:      embeddingModel,
		EmbeddingAPIKey:     embeddingAPIKey,
		EmbeddingAPIBaseURL: embeddingAPIBaseURL,
		SummarizeProvider:   summarizeProvider,
		SummarizeModel:      summarizeModel,
		SummarizeAPIKey:     summarizeAPIKey,
		SummarizeAPIBaseURL: summarizeAPIBaseURL,
	}, nil
}

// resolveEmbeddingConfig 解析 Embedding 配置
// 优先级：agent.Embedding > 公共知识库.Embedding > agent.Provider
func (c *Config) resolveEmbeddingConfig(agent *AgentConfig, agentProviderName string, agentProvider *ProviderConfig) (provider, model, apiKey, apiBaseURL string) {
	// 默认使用 agent 的 provider
	provider = agentProviderName
	apiKey = agentProvider.APIKey
	apiBaseURL = agentProvider.APIBaseURL
	model = "" // 将在下面确定

	// 检查是否有公共知识库的 embedding 配置
	var kbEmbedding *EmbeddingConfig
	for _, ref := range agent.KnowledgeBaseRefs {
		if kb, ok := c.KnowledgeBases[ref]; ok && kb.Embedding != nil {
			kbEmbedding = kb.Embedding
			break
		}
	}

	// 优先级：agent.Embedding > kbEmbedding > agentProvider
	if agent.Embedding != nil {
		// agent 级别的 embedding 配置
		if agent.Embedding.Provider != "" {
			provider = agent.Embedding.Provider
			if p, ok := c.Providers[provider]; ok {
				apiKey = p.APIKey
				apiBaseURL = p.APIBaseURL
			}
		}
		if agent.Embedding.Model != "" {
			model = agent.Embedding.Model
		}
		if agent.Embedding.APIKey != "" {
			apiKey = agent.Embedding.APIKey
		}
		if agent.Embedding.APIBaseURL != "" {
			apiBaseURL = agent.Embedding.APIBaseURL
		}
	} else if kbEmbedding != nil {
		// 公共知识库的 embedding 配置
		if kbEmbedding.Provider != "" {
			provider = kbEmbedding.Provider
			if p, ok := c.Providers[provider]; ok {
				apiKey = p.APIKey
				apiBaseURL = p.APIBaseURL
			}
		}
		if kbEmbedding.Model != "" {
			model = kbEmbedding.Model
		}
		if kbEmbedding.APIKey != "" {
			apiKey = kbEmbedding.APIKey
		}
		if kbEmbedding.APIBaseURL != "" {
			apiBaseURL = kbEmbedding.APIBaseURL
		}
	}

	// 设置默认模型
	if model == "" {
		model = "text-embedding-3-small" // OpenAI 默认 embedding 模型
	}

	return
}

// resolveSummarizeModelConfig 解析 Memoria 记忆摘要模型配置
// 优先级：agent.SummarizeModel > agent.Provider（使用 agent 的主模型）
func (c *Config) resolveSummarizeModelConfig(agent *AgentConfig, agentProviderName string, agentProvider *ProviderConfig, agentModel string) (provider, model, apiKey, apiBaseURL string) {
	// 默认使用 agent 的 provider 和 model
	provider = agentProviderName
	model = agentModel
	apiKey = agentProvider.APIKey
	apiBaseURL = agentProvider.APIBaseURL

	// 如果配置了专门的 summarize_model
	if agent.SummarizeModel != nil {
		if agent.SummarizeModel.Provider != "" {
			provider = agent.SummarizeModel.Provider
			if p, ok := c.Providers[provider]; ok {
				apiKey = p.APIKey
				apiBaseURL = p.APIBaseURL
			}
		}
		if agent.SummarizeModel.Model != "" {
			model = agent.SummarizeModel.Model
		}
		if agent.SummarizeModel.APIKey != "" {
			apiKey = agent.SummarizeModel.APIKey
		}
		if agent.SummarizeModel.APIBaseURL != "" {
			apiBaseURL = agent.SummarizeModel.APIBaseURL
		}
	}

	return
}

// resolveFinalRAGConfig 解析最终的 RAG 配置
// 优先级：agent.RAGConfig > 公共知识库.RAGConfig > 默认值
func (c *Config) resolveFinalRAGConfig(agent *AgentConfig) *RAGConfigOptions {
	// 检查公共知识库的 RAG 配置
	var kbRAGConfig *RAGConfigOptions
	for _, ref := range agent.KnowledgeBaseRefs {
		if kb, ok := c.KnowledgeBases[ref]; ok && kb.RAGConfig != nil {
			kbRAGConfig = kb.RAGConfig
			break
		}
	}

	// 合并配置
	cfg := resolveRAGConfig(nil) // 默认值

	if kbRAGConfig != nil {
		cfg = resolveRAGConfig(kbRAGConfig)
	}

	if agent.RAGConfig != nil {
		cfg = resolveRAGConfig(agent.RAGConfig)
	}

	return cfg
}

// GetDefaultSkillDirs 获取默认的 skills 目录
// 优先级: 1. 工作区下的 skills 目录  2. 用户目录下的 ~/.kanflux/skills
func GetDefaultSkillDirs(workspace string) []string {
	var skillDirs []string

	// 1. 工作区下的 skills 目录
	workspaceSkills := filepath.Join(workspace, "skills")
	if _, err := os.Stat(workspaceSkills); err == nil {
		skillDirs = append(skillDirs, workspaceSkills)
	}

	// 2. 用户目录下的 ~/.kanflux/skills
	userSkills := filepath.Join(homeDir(), ".kanflux", "skills")
	if _, err := os.Stat(userSkills); err == nil {
		skillDirs = append(skillDirs, userSkills)
	}

	return skillDirs
}

// GetDefaultAgentName 获取默认 agent 名称（第一个 agent）
func (c *Config) GetDefaultAgentName() string {
	if len(c.Agents) == 0 {
		return ""
	}
	return c.Agents[0].Name
}

// GetAllAgentNames 获取所有 agent 名称
func (c *Config) GetAllAgentNames() []string {
	names := make([]string, 0, len(c.Agents))
	for _, agent := range c.Agents {
		names = append(names, agent.Name)
	}
	return names
}

// mergeStringLists 合并两个字符串列表，去除重复项
func mergeStringLists(list1, list2 []string) []string {
	result := make([]string, 0, len(list1)+len(list2))
	seen := make(map[string]bool)

	for _, item := range list1 {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	for _, item := range list2 {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

// resolveRAGConfig 解析 RAG 配置，设置默认值
func resolveRAGConfig(cfg *RAGConfigOptions) *RAGConfigOptions {
	if cfg == nil {
		return &RAGConfigOptions{
			ChunkSize:      500,
			ChunkOverlap:   50,
			TopK:           5,
			ScoreThreshold: 0.5,
			EnableWatcher:  true,
		}
	}

	// 设置默认值
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 500
	}
	if cfg.ChunkOverlap < 0 {
		cfg.ChunkOverlap = 0
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if cfg.ScoreThreshold <= 0 || cfg.ScoreThreshold > 1 {
		cfg.ScoreThreshold = 0.5
	}

	return cfg
}

// ChannelsConfig 通道配置
type ChannelsConfig struct {
	Telegram       *TelegramChannelConfig         `json:"telegram"`
	WhatsApp       *WhatsAppChannelConfig         `json:"whatsapp"`
	Feishu         *FeishuChannelConfig           `json:"feishu"`
	CLI            *CLIChannelConfig              `json:"cli"`
	WxCom          *WxComChannelConfig            `json:"wxcom"`
	ThreadBindings []ThreadBindingConfig          `json:"thread_bindings"`
}

// WxComChannelConfig 企业微信通道配置
type WxComChannelConfig struct {
	Enabled  bool                          `json:"enabled"`
	Accounts map[string]WxComAccountConfig `json:"accounts"` // 所有账号从这里配置
}

// WxComAccountConfig 企业微信账号配置
type WxComAccountConfig struct {
	Enabled           bool     `json:"enabled"`
	BotID             string   `json:"bot_id"`
	Secret            string   `json:"secret"`
	WSURL             string   `json:"ws_url,omitempty"`              // 可选，自定义 WebSocket 地址
	HeartbeatInterval int      `json:"heartbeat_interval,omitempty"`  // 可选，心跳间隔(ms)
	ReconnectInterval int      `json:"reconnect_interval,omitempty"`  // 可选，重连延迟(ms)
	MaxReconnect      int      `json:"max_reconnect,omitempty"`       // 可选，最大重连次数
	RequestTimeout    int      `json:"request_timeout,omitempty"`     // 可选，请求超时(ms)
	AllowedIDs        []string `json:"allowed_ids"`
}

// BaseChannelConfig 通道基础配置
type BaseChannelConfig struct {
	Enabled    bool     `json:"enabled"`
	AccountID  string   `json:"account_id"` // 账号ID
	Name       string   `json:"name"`       // 账号显示名称
	AllowedIDs []string `json:"allowed_ids"`
}

// ChannelAccountConfig 通道账号配置（支持多账号）
type ChannelAccountConfig struct {
	Enabled    bool     `json:"enabled"`
	Name       string   `json:"name"`       // 账号显示名称
	AllowedIDs []string `json:"allowed_ids"`
	// Telegram 专用
	Token string `json:"token"`
	// WhatsApp 专用
	BridgeURL string `json:"bridge_url"`
	// Feishu 专用
	AppID             string `json:"app_id"`
	AppSecret         string `json:"app_secret"`
	EncryptKey        string `json:"encrypt_key"`
	VerificationToken string `json:"verification_token"`
	WebhookPort       int    `json:"webhook_port"`
}

// TelegramChannelConfig Telegram 通道配置
type TelegramChannelConfig struct {
	Enabled    bool                          `json:"enabled"`
	Token      string                        `json:"token"`
	AllowedIDs []string                      `json:"allowed_ids"`
	Accounts   map[string]ChannelAccountConfig `json:"accounts"` // 多账号配置
}

// WhatsAppChannelConfig WhatsApp 通道配置
type WhatsAppChannelConfig struct {
	Enabled    bool                          `json:"enabled"`
	BridgeURL  string                        `json:"bridge_url"`
	AllowedIDs []string                      `json:"allowed_ids"`
	Accounts   map[string]ChannelAccountConfig `json:"accounts"` // 多账号配置
}

// FeishuChannelConfig 飞书通道配置
type FeishuChannelConfig struct {
	Enabled           bool                          `json:"enabled"`
	AppID             string                        `json:"app_id"`
	AppSecret         string                        `json:"app_secret"`
	EncryptKey        string                        `json:"encrypt_key"`
	VerificationToken string                        `json:"verification_token"`
	WebhookPort       int                           `json:"webhook_port"`
	AllowedIDs        []string                      `json:"allowed_ids"`
	Accounts          map[string]ChannelAccountConfig `json:"accounts"` // 多账号配置
}

// CLIChannelConfig CLI 通道配置
type CLIChannelConfig struct {
	Enabled    bool     `json:"enabled"`
	AllowedIDs []string `json:"allowed_ids"` // CLI 通常不需要限制
}

// ThreadBindingConfig 会话绑定配置
type ThreadBindingConfig struct {
	SessionKey   string `json:"session_key"`   // Channel:ChatID (如 "tui:chat123")
	TargetChannel string `json:"target_channel"` // 目标通道名称 (如 "telegram")
	TargetAgent   string `json:"target_agent"`   // 可选：指定 agent
	Priority      int    `json:"priority"`       // 优先级
}

// WebSocketConfig WebSocket 服务配置
type WebSocketConfig struct {
	Enabled      bool   `json:"enabled"`        // 是否启用，默认 true
	Port         int    `json:"port"`           // WebSocket 端口，默认 8765
	Host         string `json:"host"`           // 主机地址，默认 localhost
	Path         string `json:"path"`           // WebSocket 路径，默认 /ws
	AuthToken    string `json:"auth_token"`     // 认证 token（可选）
	ReadTimeout  int    `json:"read_timeout"`   // 读超时（秒），默认 60
	WriteTimeout int    `json:"write_timeout"`  // 写超时（秒），默认 60
}

// GetChannelConfig 获取通道配置
func (c *Config) GetChannelConfig(channelType string) interface{} {
	if c.Channels == nil {
		return nil
	}

	switch channelType {
	case "telegram":
		return c.Channels.Telegram
	case "whatsapp":
		return c.Channels.WhatsApp
	case "feishu":
		return c.Channels.Feishu
	case "cli":
		return c.Channels.CLI
	default:
		return nil
	}
}

// GetThreadBindings 获取会话绑定配置
func (c *Config) GetThreadBindings() []ThreadBindingConfig {
	if c.Channels == nil {
		return nil
	}
	return c.Channels.ThreadBindings
}