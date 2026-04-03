package tui

import (
	"context"
	"os"

	"github.com/charmbracelet/bubbletea"
)

// Config TUI配置
type Config struct {
	Workspace    string
	Model        string
	APIKey       string
	APIBaseURL   string
	MaxIteration int
}

// App TUI应用
type App struct {
	program *tea.Program
	model   *Model
}

// NewApp 创建新的TUI应用
func NewApp(ctx context.Context, cfg *Config) (*App, error) {
	// 如果API密钥未提供，尝试从环境变量获取
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("OPENAI_API_KEY")
	}

	// 创建模型
	model, err := NewModel(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// 创建程序
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // 使用备用屏幕
		tea.WithMouseCellMotion(), // 启用鼠标支持
	)

	return &App{
		program: p,
		model:   model,
	}, nil
}

// Run 运行TUI应用
func (a *App) Run() error {
	_, err := a.program.Run()
	return err
}
