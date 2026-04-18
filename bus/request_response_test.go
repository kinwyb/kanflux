package bus

import (
	"context"
	"testing"
	"time"
)

func TestRequestResponseManagerCreateRequest(t *testing.T) {
	mgr := NewRequestResponseManager(30 * time.Second)

	requestID, responseChan := mgr.CreateRequest()

	if requestID == "" {
		t.Error("Expected non-empty request ID")
	}

	if responseChan == nil {
		t.Error("Expected non-nil response channel")
	}

	// Verify request is stored
	mgr.mu.Lock()
	_, exists := mgr.requests[requestID]
	mgr.mu.Unlock()

	if !exists {
		t.Error("Expected request to be stored in manager")
	}
}

func TestRequestResponseManagerHandleResponse(t *testing.T) {
	mgr := NewRequestResponseManager(30 * time.Second)

	requestID, responseChan := mgr.CreateRequest()

	// Create response message
	response := &OutboundMessage{
		IsResponse: true,
		ResponseID: requestID,
		Result:     "test result",
	}

	// Handle response
	handled := mgr.HandleResponse(response)

	if !handled {
		t.Error("Expected response to be handled")
	}

	// Receive response
	select {
	case resp := <-responseChan:
		if resp.Result != "test result" {
			t.Errorf("Expected result 'test result', got '%s'", resp.Result)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive response")
	}
}

func TestRequestResponseManagerHandleResponseNotResponse(t *testing.T) {
	mgr := NewRequestResponseManager(30 * time.Second)

	// Create a non-response message
	msg := &OutboundMessage{
		IsResponse: false,
	}

	handled := mgr.HandleResponse(msg)

	if handled {
		t.Error("Expected non-response message to not be handled")
	}
}

func TestRequestResponseManagerHandleResponseUnknownRequestID(t *testing.T) {
	mgr := NewRequestResponseManager(30 * time.Second)

	// Create response for unknown request ID
	response := &OutboundMessage{
		IsResponse: true,
		ResponseID: "unknown-request-id",
	}

	handled := mgr.HandleResponse(response)

	if handled {
		t.Error("Expected response with unknown request ID to not be handled")
	}
}

func TestRequestResponseManagerWaitForResponseTimeout(t *testing.T) {
	mgr := NewRequestResponseManager(100 * time.Millisecond)

	requestID, responseChan := mgr.CreateRequest()

	// Wait without sending response (should timeout)
	ctx := context.Background()
	resp, err := mgr.WaitForResponse(ctx, requestID, responseChan)

	if err != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded error, got %v", err)
	}

	if resp != nil {
		t.Error("Expected nil response on timeout")
	}
}

func TestRequestResponseManagerCleanupStaleRequests(t *testing.T) {
	mgr := NewRequestResponseManager(100 * time.Millisecond)

	// Create a request
	requestID, _ := mgr.CreateRequest()

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Cleanup
	mgr.CleanupStaleRequests()

	// Verify request was removed
	mgr.mu.Lock()
	_, exists := mgr.requests[requestID]
	mgr.mu.Unlock()

	if exists {
		t.Error("Expected stale request to be removed")
	}
}