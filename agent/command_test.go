package agent

import (
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantCmd    string
		wantArgs   []string
		wantIsCmd  bool
	}{
		{
			name:       "simple command",
			content:    "/help",
			wantCmd:    "help",
			wantArgs:   nil,
			wantIsCmd:  true,
		},
		{
			name:       "command with args",
			content:    "/search hello world",
			wantCmd:    "search",
			wantArgs:   []string{"hello", "world"},
			wantIsCmd:  true,
		},
		{
			name:       "command with quoted args",
			content:    "/search \"hello world\" test",
			wantCmd:    "search",
			wantArgs:   []string{"hello world", "test"},
			wantIsCmd:  true,
		},
		{
			name:       "command with single quote",
			content:    "/search 'hello world'",
			wantCmd:    "search",
			wantArgs:   []string{"hello world"},
			wantIsCmd:  true,
		},
		{
			name:       "not a command",
			content:    "hello world",
			wantCmd:    "",
			wantArgs:   nil,
			wantIsCmd:  false,
		},
		{
			name:       "empty content",
			content:    "",
			wantCmd:    "",
			wantArgs:   nil,
			wantIsCmd:  false,
		},
		{
			name:       "leading whitespace",
			content:    "   /help",
			wantCmd:    "help",
			wantArgs:   nil,
			wantIsCmd:  true,
		},
		{
			name:       "tab before command",
			content:    "\t/help arg1",
			wantCmd:    "help",
			wantArgs:   []string{"arg1"},
			wantIsCmd:  true,
		},
		{
			name:       "newline before command",
			content:    "\n/help",
			wantCmd:    "help",
			wantArgs:   nil,
			wantIsCmd:  true,
		},
		{
			name:       "empty args after space",
			content:    "/help   ",
			wantCmd:    "help",
			wantArgs:   nil,
			wantIsCmd:  true,
		},
		{
			name:       "mixed whitespace in args",
			content:    "/cmd  arg1   arg2",
			wantCmd:    "cmd",
			wantArgs:   []string{"arg1", "arg2"},
			wantIsCmd:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args, isCmd := ParseCommand(tt.content)
			if cmd != tt.wantCmd {
				t.Errorf("ParseCommand() cmd = %v, want %v", cmd, tt.wantCmd)
			}
			if isCmd != tt.wantIsCmd {
				t.Errorf("ParseCommand() isCmd = %v, want %v", isCmd, tt.wantIsCmd)
			}
			// Compare args
			if len(args) != len(tt.wantArgs) {
				t.Errorf("ParseCommand() args length = %v, want %v", len(args), len(tt.wantArgs))
			}
			for i := range args {
				if args[i] != tt.wantArgs[i] {
					t.Errorf("ParseCommand() args[%d] = %v, want %v", i, args[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestTrimLeadingWhitespace(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
	}{
		{"no whitespace", "hello", "hello"},
		{"leading space", "  hello", "hello"},
		{"leading tab", "\thello", "hello"},
		{"leading newline", "\nhello", "hello"},
		{"mixed whitespace", " \t\n hello", "hello"},
		{"all whitespace", "   ", ""},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimLeadingWhitespace(tt.input)
			if got != tt.want {
				t.Errorf("trimLeadingWhitespace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindFirstSpace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"no space", "hello", -1},
		{"space at end", "hello ", 5},
		{"space in middle", "hello world", 5},
		{"tab", "hello\tworld", 5},
		{"multiple spaces", "hello  world", 5},
		{"empty string", "", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findFirstSpace(tt.input)
			if got != tt.want {
				t.Errorf("findFirstSpace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single arg", "hello", []string{"hello"}},
		{"multiple args", "hello world", []string{"hello", "world"}},
		{"quoted arg", "\"hello world\"", []string{"hello world"}},
		{"single quote", "'hello world'", []string{"hello world"}},
		{"mixed", "hello \"world test\" end", []string{"hello", "world test", "end"}},
		{"extra whitespace", "  hello   world  ", []string{"hello", "world"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseArgs(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseArgs() length = %v, want %v", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseArgs()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}