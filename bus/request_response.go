package bus

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// RequestResponseManager 管理请求-响应模式的匹配
type RequestResponseManager struct {
	mu       sync.Mutex
	requests map[string]*pendingRequest // requestID -> pendingRequest
	timeout  time.Duration
}

// pendingRequest 等待响应的请求
type pendingRequest struct {
	responseChan chan *OutboundMessage
	createdAt    time.Time
}

// NewRequestResponseManager 创建请求响应管理器
func NewRequestResponseManager(timeout time.Duration) *RequestResponseManager {
	return &RequestResponseManager{
		requests: make(map[string]*pendingRequest),
		timeout:  timeout,
	}
}

// CreateRequest 创建请求，返回 requestID 和响应 channel
func (r *RequestResponseManager) CreateRequest() (string, chan *OutboundMessage) {
	requestID := uuid.New().String()
	responseChan := make(chan *OutboundMessage, 1)

	r.mu.Lock()
	r.requests[requestID] = &pendingRequest{
		responseChan: responseChan,
		createdAt:    time.Now(),
	}
	r.mu.Unlock()

	return requestID, responseChan
}

// WaitForResponse 等待响应（带超时）
func (r *RequestResponseManager) WaitForResponse(ctx context.Context, requestID string, responseChan chan *OutboundMessage) (*OutboundMessage, error) {
	// 创建超时 context
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// 清理请求记录
	defer func() {
		r.mu.Lock()
		if req, exists := r.requests[requestID]; exists {
			close(req.responseChan)
			delete(r.requests, requestID)
		}
		r.mu.Unlock()
	}()

	select {
	case resp := <-responseChan:
		return resp, nil
	case <-timeoutCtx.Done():
		return nil, context.DeadlineExceeded
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// HandleResponse 处理响应消息，匹配到对应请求
// 返回 true 表示成功匹配并处理，false 表示没有找到对应的请求
func (r *RequestResponseManager) HandleResponse(msg *OutboundMessage) bool {
	if !msg.IsResponse || msg.ResponseID == "" {
		return false
	}

	r.mu.Lock()
	req, exists := r.requests[msg.ResponseID]
	r.mu.Unlock()

	if !exists {
		return false
	}

	// 发送响应（非阻塞）
	select {
	case req.responseChan <- msg:
		return true
	default:
		return false
	}
}

// CleanupStaleRequests 清理超时的请求
func (r *RequestResponseManager) CleanupStaleRequests() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for requestID, req := range r.requests {
		if now.Sub(req.createdAt) > r.timeout {
			close(req.responseChan)
			delete(r.requests, requestID)
		}
	}
}