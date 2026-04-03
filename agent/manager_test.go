package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kinwyb/kanflux/agent/tools"
	bus2 "github.com/kinwyb/kanflux/bus"
	"github.com/kinwyb/kanflux/providers"
	"github.com/kinwyb/kanflux/session"
)

func TestNewManager(t *testing.T) {
	ctx := context.Background()
	apiKey := os.Getenv("OPENAI_API_KEY")
	llm, err := providers.NewOpenAI(ctx, "https://dashscope.aliyuncs.com/compatible-mode/v1",
		"qwen3.5-122b-a10b", apiKey)
	if err != nil {
		t.Fatal(err)
	}
	workSpace := "~/Download/bodacli"
	cfg := &Config{
		LLM:          llm,
		Workspace:    workSpace,
		MaxIteration: 10,
		ToolRegister: tools.NewRegistry(),
		Streaming:    true,
	}
	cfg.ToolRegister.NeedApprove("read_file")
	cfg.ToolRegister.NeedApprove("write_file")
	cfg.ToolRegister.NeedApprove("ls")

	agent, err := NewAgent(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	sessionMgr, serr := session.NewManager(workSpace)
	if serr != nil {
		t.Fatal(serr)
	}
	bus := bus2.NewMessageBus(10)
	go func() {
		evs := bus.SubscribeLogEvent()
		for {
			select {
			case e := <-evs.Channel:
				t.Log(e.Message)
			}
		}
	}()
	manager := NewManager(bus, sessionMgr)
	manager.RegisterAgent("default", agent, true)
	manager.Start(ctx)
	chatID := "7751"
	messages := []string{"列出当前目录", "y", "查看agent__tui-session.jsonl", "y", "总结一下上面内容"}
	i := 0
	for {
		line := messages[i]
		inMsg := bus2.InboundMessage{
			ID:        generateID(),
			Channel:   "agent",
			AccountID: "",
			SenderID:  "",
			ChatID:    chatID,
			Content:   line,
			Media:     nil,
			Metadata:  nil,
			Timestamp: time.Now(),
		}
		err = bus.PublishInbound(ctx, &inMsg)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := bus.ConsumeOutbound(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Log(resp.Content)
		i++
		if i >= len(messages) {
			break
		}
	}
}
