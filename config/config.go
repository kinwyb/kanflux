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
	Name         string    `json:"name"`
	Type         AgentType `json:"type"`         // Agent 类型，默认 deep
	Description  string    `json:"description"`  // Agent 描述，未配置时使用默认描述
	Workspace    string    `json:"workspace"`    // 必须指定
	SubAgents    []string  `json:"sub_agents"`   // 子 agent 名称列表
	Provider     string    `json:"provider"`     // 未指定使用 default_provider
	Model        string    `json:"model"`        // 未指定使用供应商的 default_model
	MaxIteration int       `json:"max_iteration"` // 默认 10
	Streaming    bool      `json:"streaming"`    // 默认 true
}

// ResolvedAgentConfig 解析后的 agent 配置（包含最终确定的值）
type ResolvedAgentConfig struct {
	Name         string
	Type         AgentType // Agent 类型
	Description  string    // Agent 描述
	Workspace    string
	SubAgents    []string  // 子 agent 名称列表
	Provider     string
	Model        string
	APIKey       string
	APIBaseURL   string
	MaxIteration int
	Streaming    bool
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

	return &ResolvedAgentConfig{
		Name:         agent.Name,
		Type:         agentType,
		Description:  description,
		Workspace:    agent.Workspace,
		SubAgents:    agent.SubAgents,
		Provider:     providerName,
		Model:        model,
		APIKey:       provider.APIKey,
		APIBaseURL:   provider.APIBaseURL,
		MaxIteration: maxIteration,
		Streaming:    agent.Streaming,
	}, nil
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