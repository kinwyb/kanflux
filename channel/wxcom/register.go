package wxcom

import (
	"context"
	"fmt"

	"github.com/kinwyb/kanflux/channel"
	"github.com/kinwyb/kanflux/config"
)

// WxComFactory 企业微信 Channel 工厂
type WxComFactory struct{}

func init() {
	// 在 init 时自动注册到全局注册表
	channel.RegisterFactory(&WxComFactory{})
}

// Type 返回 Channel 类型标识
func (f *WxComFactory) Type() string {
	return "wxcom"
}

// CreateFromConfig 从 ChannelsConfig 创建 Channel 实例
func (f *WxComFactory) CreateFromConfig(ctx context.Context, cfg *config.ChannelsConfig, common *channel.ChannelCommonParams) ([]channel.Channel, error) {
	// 提取 wxcom 配置
	if cfg.WxCom == nil || !cfg.WxCom.Enabled {
		return nil, nil
	}

	wxcomCfg := cfg.WxCom
	var channels []channel.Channel

	// 从 accounts 创建实例
	for accountID, accountCfg := range wxcomCfg.Accounts {
		if !accountCfg.Enabled {
			continue
		}

		// 验证必要字段
		if accountCfg.BotID == "" || accountCfg.Secret == "" {
			return nil, fmt.Errorf("account '%s': bot_id and secret are required", accountID)
		}

		// 转换配置
		wxConfig := f.toWxComConfig(&accountCfg)

		// 创建实例
		ch, err := NewWxComChannel(common.Bus, wxConfig)
		if err != nil {
			return nil, fmt.Errorf("account '%s': %w", accountID, err)
		}
		channels = append(channels, ch)
	}

	return channels, nil
}

// toWxComConfig 将 config.WxComAccountConfig 转换为 wxcom.WxComConfig
func (f *WxComFactory) toWxComConfig(cfg *config.WxComAccountConfig) *WxComConfig {
	return &WxComConfig{
		Enabled:           cfg.Enabled,
		BotID:             cfg.BotID,
		Secret:            cfg.Secret,
		WSURL:             cfg.WSURL,
		HeartbeatInterval: cfg.HeartbeatInterval,
		ReconnectInterval: cfg.ReconnectInterval,
		MaxReconnect:      cfg.MaxReconnect,
		RequestTimeout:    cfg.RequestTimeout,
	}
}