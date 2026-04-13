package tui

import (
	"context"
	"os"

	"github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/channel"
	"github.com/kinwyb/kanflux/config"

	"github.com/charmbracelet/bubbletea"
)

// Config TUI配置
type Config struct {
	// 单 agent 模式（无配置文件时使用）
	Workspace    string
	Model        string
	APIKey       string
	APIBaseURL   string
	MaxIteration int

	// 多 agent 模式（有配置文件时使用）
	AppConfig    *config.Config // 完整配置
	DefaultAgent string         // 默认 agent 名称
}

// App TUI应用
type App struct {
	program    *tea.Program
	model      *Model
	channelMgr *channel.Manager
	tuiChannel *channel.TUIChannel
	bus        *bus.MessageBus
}

// NewApp 创建新的TUI应用
func NewApp(ctx context.Context, cfg *Config) (*App, error) {
	// 如果API密钥未提供，尝试从环境变量获取
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("OPENAI_API_KEY")
	}

	// 创建消息总线
	msgBus := bus.NewMessageBus(10)

	// 创建 channel manager
	channelMgr := channel.NewManager(msgBus)

	// 创建模型（传入 channel manager）
	model, err := NewModel(ctx, cfg, channelMgr)
	if err != nil {
		return nil, err
	}

	// 创建 TUIChannel
	tuiChannel, err := channel.NewTUIChannel(msgBus, model, &channel.TUIConfig{
		Enabled: true,
	})
	if err != nil {
		return nil, err
	}

	// 注册 TUIChannel
	if err := channelMgr.Register(tuiChannel); err != nil {
		return nil, err
	}

	// 启动 channel manager
	if err := channelMgr.StartAll(ctx); err != nil {
		return nil, err
	}

	// 创建程序
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // 使用备用屏幕
		tea.WithMouseCellMotion(), // 启用鼠标支持
	)

	return &App{
		program:    p,
		model:      model,
		channelMgr: channelMgr,
		tuiChannel: tuiChannel,
		bus:        msgBus,
	}, nil
}

// Run 运行TUI应用
func (a *App) Run() error {
	_, err := a.program.Run()
	return err
}

// Stop 停止应用
func (a *App) Stop() error {
	// 停止 channel manager
	return a.channelMgr.StopAll()
}

// GetChannelMgr 获取 channel manager
func (a *App) GetChannelMgr() *channel.Manager {
	return a.channelMgr
}