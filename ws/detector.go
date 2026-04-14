package ws

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Detector WebSocket 状态检测器
type Detector struct {
	config *ServerConfig
}

// NewDetector 创建检测器
func NewDetector(cfg *ServerConfig) *Detector {
	if cfg == nil {
		cfg = &ServerConfig{}
	}
	cfg.SetDefaults()

	return &Detector{
		config: cfg,
	}
}

// IsRunning 检测 WebSocket 服务是否已运行
// 尝试连接，成功返回 true
func (d *Detector) IsRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return d.IsRunningWithContext(ctx)
}

// IsRunningWithContext 检测 WebSocket 服务是否已运行（带 context）
func (d *Detector) IsRunningWithContext(ctx context.Context) bool {
	url := d.config.URL()

	// 尝试连接
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 2 * time.Second

	header := http.Header{}
	if d.config.AuthToken != "" {
		header.Set("Authorization", "Bearer "+d.config.AuthToken)
	}

	wsConn, _, err := dialer.Dial(url, header)
	if err != nil {
		return false
	}

	// 连接成功，立即关闭
	wsConn.Close()
	return true
}

// TryConnect 尝试连接并返回客户端
// 如果服务未运行，返回错误
func (d *Detector) TryConnect(ctx context.Context) (*Client, error) {
	url := d.config.URL()

	clientCfg := &ClientConfig{
		URL:       url,
		AuthToken: d.config.AuthToken,
	}

	client := NewClient(clientCfg)
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect WebSocket at %s: %w", url, err)
	}

	return client, nil
}

// TryConnectWithTimeout 尝试连接（带超时）
func (d *Detector) TryConnectWithTimeout(timeout time.Duration) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return d.TryConnect(ctx)
}

// GetConfig 获取配置
func (d *Detector) GetConfig() *ServerConfig {
	return d.config
}

// GetURL 获取 WebSocket URL
func (d *Detector) GetURL() string {
	return d.config.URL()
}

// WaitForRunning 等待 WebSocket 服务启动
// 返回是否成功启动
func (d *Detector) WaitForRunning(ctx context.Context, maxWait time.Duration) bool {
	timeout := time.NewTimer(maxWait)
	defer timeout.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-timeout.C:
			return false
		case <-ticker.C:
			if d.IsRunningWithContext(ctx) {
				return true
			}
		}
	}
}

// WaitForRunningWithConnect 等待服务启动并连接
func (d *Detector) WaitForRunningWithConnect(ctx context.Context, maxWait time.Duration) (*Client, error) {
	if !d.WaitForRunning(ctx, maxWait) {
		return nil, fmt.Errorf("WebSocket service not started within %v", maxWait)
	}

	return d.TryConnect(ctx)
}