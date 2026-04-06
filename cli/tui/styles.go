package tui

import "github.com/charmbracelet/lipgloss"

// Styles TUI样式定义
type Styles struct {
	Title            lipgloss.Style
	Status           lipgloss.Style
	StatusProcessing lipgloss.Style
	Input            lipgloss.Style
	Help             lipgloss.Style
	ChatViewport     lipgloss.Style
	LogViewport      lipgloss.Style
	ChatBox          lipgloss.Style
	LogBox           lipgloss.Style
	ConfirmBox       lipgloss.Style // 确认框样式
	UserMessage      lipgloss.Style
	AssistantMessage lipgloss.Style
	ThinkingMessage  lipgloss.Style
	ErrorMessage     lipgloss.Style
	ToolMessage      lipgloss.Style
	LogInfo          lipgloss.Style
	LogDebug         lipgloss.Style
	LogWarn          lipgloss.Style
	LogError         lipgloss.Style
}

// NewStyles 创建样式
func NewStyles() Styles {
	return Styles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62")),
		Status: lipgloss.NewStyle().
			Foreground(lipgloss.Color("170")),
		StatusProcessing: lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")),
		Input: lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")),
		ChatViewport: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("99")),
		LogViewport: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("63")),
		ChatBox: lipgloss.NewStyle(),
		LogBox:  lipgloss.NewStyle(),
		ConfirmBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("214")).
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("15")).
			Padding(0, 2),
		UserMessage: lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")),
		AssistantMessage: lipgloss.NewStyle().
			Foreground(lipgloss.Color("99")),
		ThinkingMessage: lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")),
		ErrorMessage: lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")),
		ToolMessage: lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")),
		LogInfo: lipgloss.NewStyle().
			Foreground(lipgloss.Color("117")),
		LogDebug: lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")),
		LogWarn: lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")),
		LogError: lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")),
	}
}
