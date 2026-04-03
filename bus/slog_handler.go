package bus

import (
	"context"
	"log/slog"
	"sync"
)

// busLogHandler 是一个 slog.Handler，将日志输出转发到 MessageBus 的 LogEvent
type busLogHandler struct {
	bus    *MessageBus
	level  slog.Level
	source string
	mu     sync.Mutex
}

// newBusLogHandler 创建一个新的 busLogHandler
func newBusLogHandler(bus *MessageBus, level slog.Level, source string) *busLogHandler {
	return &busLogHandler{
		bus:    bus,
		level:  level,
		source: source,
	}
}

// Enabled 检查日志级别是否启用
func (h *busLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle 处理日志记录
func (h *busLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.bus == nil || h.bus.IsClosed() {
		return nil
	}

	// 确定日志级别字符串
	var levelStr string
	switch r.Level {
	case slog.LevelDebug:
		levelStr = LogLevelDebug
	case slog.LevelInfo:
		levelStr = LogLevelInfo
	case slog.LevelWarn:
		levelStr = LogLevelWarn
	case slog.LevelError:
		levelStr = LogLevelError
	default:
		levelStr = LogLevelInfo
	}

	// 构建日志消息
	event := &LogEvent{
		Level:     levelStr,
		Message:   r.Message,
		Source:    h.source,
		Timestamp: r.Time,
	}

	// 添加属性到消息中（可选扩展）
	if r.NumAttrs() > 0 {
		r.Attrs(func(attr slog.Attr) bool {
			// 可以将属性添加到 Metadata 或格式化到消息中
			// 这里简单地将属性追加到消息
			event.Message += " " + attr.Key + "=" + attr.Value.String()
			return true
		})
	}

	// 发布日志事件（非阻塞）
	select {
	case h.bus.logEvents <- event:
	default:
		// channel 满时丢弃
	}
	return nil
}

// WithAttrs 返回带有属性的新 handler
func (h *busLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// 返回一个新的 handler，属性在 Handle 中处理
	return h
}

// WithGroup 返回带有分组的新 handler
func (h *busLogHandler) WithGroup(name string) slog.Handler {
	// 返回一个新的 handler，分组名可以作为 source 的一部分
	return h
}

// SetSource 设置日志来源
func (h *busLogHandler) SetSource(source string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.source = source
}

// SetLevel 设置日志级别
func (h *busLogHandler) SetLevel(level slog.Level) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.level = level
}

// SetupDefaultLogger 设置 slog 默认 logger 使用 BusHandler
func SetupDefaultLogger(bus *MessageBus, level slog.Level, source string) {
	handler := newBusLogHandler(bus, level, source)
	slog.SetDefault(slog.New(handler))
}
