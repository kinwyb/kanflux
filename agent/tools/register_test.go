package tools

import (
	"testing"
)

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry()
	web := NewWebTool("", "", 600)
	ts := web.GetTools()
	for _, t1 := range ts {
		reg.Register(t1)
	}
	v, e := reg.GetTools()
	if e != nil {
		t.Fatal(e)
	}
	t.Log(v)
}
