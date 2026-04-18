package tools

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mafredri/cdp"
	"github.com/mafredri/cdp/devtool"
	"github.com/mafredri/cdp/protocol/emulation"
	"github.com/mafredri/cdp/protocol/network"
	"github.com/mafredri/cdp/rpcc"
)

// ConnectionMode 浏览器连接模式
type ConnectionMode string

const (
	ModeAuto   ConnectionMode = "auto"   // 自动检测（优先尝试 relay，失败则尝试 direct）
	ModeDirect ConnectionMode = "direct" // 直接 CDP 连接
	ModeRelay  ConnectionMode = "relay"  // 通过 Relay 连接
)

// BrowserSessionManager 浏览器会话管理器 (使用 Chrome DevTools Protocol 或 Relay)
type BrowserSessionManager struct {
	mu             sync.RWMutex
	devt           *devtool.DevTools
	client         *cdp.Client
	conn           *rpcc.Conn
	cmd            interface{} // Chrome 进程（跨平台处理）
	ready          bool
	chromePath     string
	userDataDir    string
	remoteURL      string         // 远程 Chrome 实例 URL
	connectionMode ConnectionMode // 连接模式
	relayURL       string         // Relay URL
	relaySession   *RelaySessionManager // Relay 会话
}

var sessionManager *BrowserSessionManager

// GetBrowserSession 获取浏览器会话管理器（单例）
func GetBrowserSession() *BrowserSessionManager {
	if sessionManager == nil {
		sessionManager = &BrowserSessionManager{}
	}
	return sessionManager
}

// Start 启动浏览器会话
func (b *BrowserSessionManager) Start(timeout time.Duration) error {
	return b.StartWithMode(timeout, "", ModeAuto)
}

// StartWithMode 使用指定模式启动浏览器会话
func (b *BrowserSessionManager) StartWithMode(timeout time.Duration, relayURL string, mode ConnectionMode) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.ready {
		return nil
	}

	b.relayURL = relayURL
	b.connectionMode = mode

	// 根据模式决定连接方式
	switch mode {
	case ModeRelay:
		return b.startRelayMode(timeout)
	case ModeDirect:
		return b.startDirectMode(timeout)
	case ModeAuto:
		// 自动模式：优先尝试 relay，失败则尝试 direct
		if relayURL != "" {
			slog.Debug("Auto mode: trying Relay first")
			err := b.startRelayMode(timeout)
			if err == nil {
				return nil
			}
			slog.Warn("Relay connection failed, falling back to direct CDP", "error", err)
		}
		return b.startDirectMode(timeout)
	default:
		return fmt.Errorf("unknown connection mode: %s", mode)
	}
}

// startRelayMode 启动 Relay 模式
func (b *BrowserSessionManager) startRelayMode(timeout time.Duration) error {
	slog.Debug("Starting browser session with Relay", "relay_url", b.relayURL)

	relaySession := GetRelaySession()
	if err := relaySession.Start(b.relayURL, timeout); err != nil {
		return fmt.Errorf("failed to start relay session: %w", err)
	}

	b.relaySession = relaySession
	b.ready = true
	slog.Debug("Browser session started successfully with Relay")
	return nil
}

// startDirectMode 启动直接 CDP 模式
func (b *BrowserSessionManager) startDirectMode(timeout time.Duration) error {
	slog.Debug("Starting persistent browser session with Chrome DevTools Protocol")

	// 首先尝试连接到已运行的 Chrome 实例
	if err := b.tryConnectToExisting(); err == nil {
		b.ready = true
		slog.Debug("Connected to existing Chrome instance")
		return nil
	}

	slog.Debug("No existing Chrome found, starting new instance")

	// 查找 Chrome 可执行文件
	chromePath, err := b.findChrome()
	if err != nil {
		return fmt.Errorf("failed to find Chrome: %w", err)
	}
	b.chromePath = chromePath

	// 创建用户数据目录
	userDataDir, err := createTempDir("kanflux-chrome-")
	if err != nil {
		return fmt.Errorf("failed to create user data dir: %w", err)
	}
	b.userDataDir = userDataDir

	// 启动 Chrome
	if err := b.launchChrome(chromePath, userDataDir); err != nil {
		cleanupTempDir(userDataDir)
		return fmt.Errorf("failed to launch Chrome: %w", err)
	}

	// 等待 Chrome 启动
	select {
	case <-time.After(timeout):
		b.killChrome()
		cleanupTempDir(userDataDir)
		return fmt.Errorf("Chrome did not start within timeout")
	case <-time.After(3 * time.Second):
		// 继续连接
	}

	// 连接到 Chrome
	if err := b.connect(9222); err != nil {
		b.killChrome()
		cleanupTempDir(userDataDir)
		return fmt.Errorf("failed to connect to Chrome: %w", err)
	}

	b.ready = true
	slog.Debug("Browser session started successfully with Chrome DevTools Protocol")
	return nil
}

// tryConnectToExisting 尝试连接到已运行的 Chrome 实例
func (b *BrowserSessionManager) tryConnectToExisting() error {
	// 尝试连接默认端口
	for _, port := range []int{9222, 9223, 9224} {
		if err := b.connect(port); err == nil {
			b.remoteURL = fmt.Sprintf("http://localhost:%d", port)
			return nil
		}
	}
	return fmt.Errorf("no existing Chrome instance found")
}

// connect 连接到指定端口的 Chrome 实例
func (b *BrowserSessionManager) connect(port int) error {
	// 使用 devtool 包
	b.devt = devtool.New(fmt.Sprintf("http://localhost:%d", port))

	// 列出可用的页面
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pt, err := b.devt.Get(ctx, devtool.Page)
	if err != nil {
		// 如果没有页面，创建新标签页
		pt, err = b.devt.Create(ctx)
		if err != nil {
			return fmt.Errorf("failed to create page: %w", err)
		}
	}

	// 连接到 WebSocket
	conn, err := rpcc.DialContext(ctx, pt.WebSocketDebuggerURL)
	if err != nil {
		return fmt.Errorf("failed to dial WebSocket: %w", err)
	}

	b.conn = conn

	// 创建 CDP 客户端
	b.client = cdp.NewClient(conn)

	// 启用需要的域
	if err := b.client.DOM.Enable(ctx); err != nil {
		return fmt.Errorf("failed to enable DOM: %w", err)
	}
	if err := b.client.Page.Enable(ctx); err != nil {
		return fmt.Errorf("failed to enable Page: %w", err)
	}
	if err := b.client.Runtime.Enable(ctx); err != nil {
		return fmt.Errorf("failed to enable Runtime: %w", err)
	}
	if err := b.client.Network.Enable(ctx, network.NewEnableArgs()); err != nil {
		return fmt.Errorf("failed to enable Network: %w", err)
	}

	// 设置真实的 User-Agent 以避免被检测为自动化工具
	userAgent := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	if err := b.client.Emulation.SetUserAgentOverride(ctx, emulation.NewSetUserAgentOverrideArgs(userAgent)); err != nil {
		slog.Warn("Failed to set User-Agent", "error", err)
	}

	return nil
}

// IsReady 检查会话是否就绪
func (b *BrowserSessionManager) IsReady() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.ready
}

// GetClient 获取 CDP 客户端
func (b *BrowserSessionManager) GetClient() (*cdp.Client, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.ready {
		return nil, fmt.Errorf("browser session not ready")
	}

	return b.client, nil
}

// GetRelayClient 获取 Relay 客户端
func (b *BrowserSessionManager) GetRelayClient() *RelaySessionManager {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.relaySession
}

// GetConnectionMode 获取当前连接模式
func (b *BrowserSessionManager) GetConnectionMode() ConnectionMode {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connectionMode
}

// IsRelayMode 检查是否使用 Relay 模式
func (b *BrowserSessionManager) IsRelayMode() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.relaySession != nil && b.relaySession.IsReady()
}

// Stop 停止浏览器会话
func (b *BrowserSessionManager) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.ready {
		slog.Debug("Stopping browser session")

		// 停止 Relay 会话
		if b.relaySession != nil {
			b.relaySession.Stop()
			b.relaySession = nil
		}

		// 关闭连接
		if b.conn != nil {
			_ = b.conn.Close()
		}

		// 停止 Chrome 进程
		b.killChrome()

		// 清理临时目录
		if b.userDataDir != "" {
			cleanupTempDir(b.userDataDir)
		}

		b.ready = false
		b.client = nil
		b.conn = nil
		b.userDataDir = ""
		b.connectionMode = ModeAuto
		b.relayURL = ""
	}
}