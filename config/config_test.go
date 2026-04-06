package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	// 创建临时测试文件
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.json")

	configContent := `{
  "providers": {
    "openai": {
      "api_key": "sk-test",
      "api_base_url": "https://api.openai.com/v1",
      "default_model": "gpt-4o"
    },
    "qwen": {
      "api_key": "sk-qwen",
      "api_base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
      "default_model": "qwen3.5-plus"
    }
  },
  "default_provider": "qwen",
  "agents": [
    {
      "name": "agent1",
      "workspace": "/path/to/ws1",
      "provider": "openai",
      "model": "gpt-4",
      "max_iteration": 20
    },
    {
      "name": "agent2",
      "workspace": "/path/to/ws2"
    }
  ]
}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// 验证 providers
	if len(cfg.Providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(cfg.Providers))
	}

	openai := cfg.Providers["openai"]
	if openai == nil {
		t.Error("openai provider not found")
	} else {
		if openai.APIKey != "sk-test" {
			t.Errorf("expected APIKey 'sk-test', got '%s'", openai.APIKey)
		}
		if openai.DefaultModel != "gpt-4o" {
			t.Errorf("expected DefaultModel 'gpt-4o', got '%s'", openai.DefaultModel)
		}
	}

	// 验证 default_provider
	if cfg.DefaultProvider != "qwen" {
		t.Errorf("expected default_provider 'qwen', got '%s'", cfg.DefaultProvider)
	}

	// 验证 agents
	if len(cfg.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(cfg.Agents))
	}

	agent1 := cfg.GetAgent("agent1")
	if agent1 == nil {
		t.Error("agent1 not found")
	} else {
		if agent1.Workspace != "/path/to/ws1" {
			t.Errorf("expected workspace '/path/to/ws1', got '%s'", agent1.Workspace)
		}
		if agent1.Provider != "openai" {
			t.Errorf("expected provider 'openai', got '%s'", agent1.Provider)
		}
	}
}

func TestResolveAgentConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.json")

	configContent := `{
  "providers": {
    "openai": {
      "api_key": "sk-openai",
      "api_base_url": "https://api.openai.com/v1",
      "default_model": "gpt-4o"
    },
    "qwen": {
      "api_key": "sk-qwen",
      "api_base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
      "default_model": "qwen3.5-plus"
    }
  },
  "default_provider": "qwen",
  "agents": [
    {
      "name": "explicit",
      "description": "Explicit agent description",
      "workspace": "/ws/explicit",
      "sub_agents": ["research", "code"],
      "provider": "openai",
      "model": "gpt-4"
    },
    {
      "name": "defaults",
      "workspace": "/ws/defaults"
    },
    {
      "name": "partial",
      "workspace": "/ws/partial",
      "provider": "openai"
    }
  ]
}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// 测试完全指定的 agent（包括描述和子 agent）
	explicit, err := cfg.ResolveAgentConfig("explicit")
	if err != nil {
		t.Fatalf("ResolveAgentConfig failed: %v", err)
	}
	if explicit.Provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", explicit.Provider)
	}
	if explicit.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", explicit.Model)
	}
	if explicit.APIKey != "sk-openai" {
		t.Errorf("expected APIKey 'sk-openai', got '%s'", explicit.APIKey)
	}
	if explicit.Description != "Explicit agent description" {
		t.Errorf("expected Description 'Explicit agent description', got '%s'", explicit.Description)
	}
	if len(explicit.SubAgents) != 2 {
		t.Errorf("expected 2 sub_agents, got %d", len(explicit.SubAgents))
	}
	if explicit.SubAgents[0] != "research" || explicit.SubAgents[1] != "code" {
		t.Errorf("expected sub_agents ['research', 'code'], got %v", explicit.SubAgents)
	}

	// 测试使用默认值的 agent（包括默认描述）
	defaults, err := cfg.ResolveAgentConfig("defaults")
	if err != nil {
		t.Fatalf("ResolveAgentConfig failed: %v", err)
	}
	if defaults.Provider != "qwen" {
		t.Errorf("expected provider 'qwen' (default), got '%s'", defaults.Provider)
	}
	if defaults.Model != "qwen3.5-plus" {
		t.Errorf("expected model 'qwen3.5-plus' (provider default), got '%s'", defaults.Model)
	}
	if defaults.APIKey != "sk-qwen" {
		t.Errorf("expected APIKey 'sk-qwen', got '%s'", defaults.APIKey)
	}
	// 测试默认描述
	if defaults.Description != "Agent defaults for general tasks" {
		t.Errorf("expected default Description, got '%s'", defaults.Description)
	}
	if len(defaults.SubAgents) != 0 {
		t.Errorf("expected 0 sub_agents, got %d", len(defaults.SubAgents))
	}

	// 测试部分指定的 agent
	partial, err := cfg.ResolveAgentConfig("partial")
	if err != nil {
		t.Fatalf("ResolveAgentConfig failed: %v", err)
	}
	if partial.Provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", partial.Provider)
	}
	if partial.Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o' (provider default), got '%s'", partial.Model)
	}
}

func TestResolveAgentConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		agent   string
		wantErr string
	}{
		{
			name: "missing workspace",
			content: `{
  "providers": {"p": {"api_key": "k", "default_model": "m"}},
  "default_provider": "p",
  "agents": [{"name": "bad", "workspace": ""}]
}`,
			agent:   "bad",
			wantErr: "workspace is required",
		},
		{
			name: "agent not found",
			content: `{
  "providers": {"p": {"api_key": "k"}},
  "agents": [{"name": "exists", "workspace": "/ws"}]
}`,
			agent:   "notexists",
			wantErr: "not found",
		},
		{
			name: "provider not found",
			content: `{
  "providers": {"p": {"api_key": "k"}},
  "agents": [{"name": "bad", "workspace": "/ws", "provider": "unknown"}]
}`,
			agent:   "bad",
			wantErr: "provider 'unknown' not found",
		},
		{
			name: "no default provider",
			content: `{
  "providers": {"p": {"api_key": "k"}},
  "agents": [{"name": "bad", "workspace": "/ws"}]
}`,
			agent:   "bad",
			wantErr: "no default_provider",
		},
		{
			name: "no model and no default",
			content: `{
  "providers": {"p": {"api_key": "k"}},
  "default_provider": "p",
  "agents": [{"name": "bad", "workspace": "/ws"}]
}`,
			agent:   "bad",
			wantErr: "no default_model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "test.json")
			if err := os.WriteFile(configPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}

			_, err = cfg.ResolveAgentConfig(tt.agent)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing '%s', got '%s'", tt.wantErr, err.Error())
			}
		})
	}
}

func TestMaxIterationDefault(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.json")

	configContent := `{
  "providers": {"p": {"api_key": "k", "default_model": "m"}},
  "default_provider": "p",
  "agents": [{"name": "test", "workspace": "/ws"}]
}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	resolved, err := cfg.ResolveAgentConfig("test")
	if err != nil {
		t.Fatalf("ResolveAgentConfig failed: %v", err)
	}

	if resolved.MaxIteration != 10 {
		t.Errorf("expected default MaxIteration 10, got %d", resolved.MaxIteration)
	}
}

func TestGetDefaultAgentName(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.json")

	configContent := `{
  "providers": {"p": {"api_key": "k", "default_model": "m"}},
  "default_provider": "p",
  "agents": [{"name": "first", "workspace": "/ws1"}, {"name": "second", "workspace": "/ws2"}]
}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// 默认 agent 应该是第一个
	if cfg.GetDefaultAgentName() != "first" {
		t.Errorf("expected default agent name 'first', got '%s'", cfg.GetDefaultAgentName())
	}

	// 获取所有 agent 名称
	names := cfg.GetAllAgentNames()
	if len(names) != 2 {
		t.Errorf("expected 2 agent names, got %d", len(names))
	}
	if names[0] != "first" || names[1] != "second" {
		t.Errorf("expected names ['first', 'second'], got %v", names)
	}
}