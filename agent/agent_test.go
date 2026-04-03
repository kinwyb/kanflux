package agent

import (
	"context"
	"os"
	"testing"

	"github.com/kinwyb/kanflux/agent/tools"
	"github.com/kinwyb/kanflux/providers"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestNewAgent(t *testing.T) {
	ctx := context.Background()
	apiKey := os.Getenv("OPENAI_API_KEY")
	llm, err := providers.NewOpenAI(ctx, "https://dashscope.aliyuncs.com/compatible-mode/v1",
		"qwen3.5-plus", apiKey)
	if err != nil {
		t.Fatal(err)
	}
	workSpace := "/Users/wangyingbin/Downloads/bodacli"
	//cnt := NewContextBuilder(workSpace)
	//t.Log(cnt.BuildSystemPrompt())
	//return
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	//web := tools.NewWebTool("", "", 600)
	//ts := web.GetTools()
	//for _, t1 := range ts {
	//	reg.Register(t1)
	//}
	cfg := &Config{
		LLM:          llm,
		Workspace:    workSpace,
		MaxIteration: 10,
		ToolRegister: reg,
		SkillDirs:    []string{"/Users/wangyingbin/Downloads/bodacli/skills"},
	}
	agent, err := NewAgent(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	content, err := agent.Prompt(ctx, []adk.Message{schema.UserMessage("我叫什么")}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(content)
}
