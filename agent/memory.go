package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MemoryStore 记忆存储
type MemoryStore struct {
	baseDIR string
}

// NewMemoryStore 创建记忆存储
func NewMemoryStore(workspace string) (*MemoryStore, error) {
	baseDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &MemoryStore{
		baseDIR: baseDir,
	}, nil
}

// ReadToday 读取今日笔记
func (m *MemoryStore) ReadToday() (string, error) {
	path, _ := m.todyFilePath()

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return string(content), nil
}

// AppendToday 追加到今日笔记
func (m *MemoryStore) AppendToday(content string) error {
	// 确保目录存在
	path, err := m.todyFilePath()
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// 如果文件不为空，添加换行
	if info, err := file.Stat(); err == nil && info.Size() > 0 {
		if _, err := file.WriteString("\n\n"); err != nil {
			return err
		}
	}

	// 写入内容
	if _, err := file.WriteString(content); err != nil {
		return err
	}

	return nil
}

func (m *MemoryStore) todyFilePath() (string, error) {
	// 确保目录存在
	memoryDir := filepath.Join(m.baseDIR, "days")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return "", err
	}

	// 追加内容
	today := time.Now().Format("2006-01-02")
	path := filepath.Join(memoryDir, today+".md")
	return path, nil
}

// ReadLongTerm 读取长期记忆
func (m *MemoryStore) ReadLongTerm() (string, error) {
	path := m.longTermFilePath()

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return string(content), nil
}

func (m *MemoryStore) longTermFilePath() string {
	return filepath.Join(m.baseDIR, "MEMORY.md")
}

// AppendLongTerm 追加到长期记忆
func (m *MemoryStore) AppendLongTerm(content string) error {

	path := m.longTermFilePath()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// 如果文件不为空，添加换行
	if info, err := file.Stat(); err == nil && info.Size() > 0 {
		if _, err := file.WriteString("\n\n"); err != nil {
			return err
		}
	}

	// 写入内容
	if _, err := file.WriteString(content); err != nil {
		return err
	}

	return nil
}

// GetMemoryContext 获取格式化的记忆上下文
func (m *MemoryStore) GetMemoryContext() (string, error) {
	var parts []string

	// 读取长期记忆
	longTerm, err := m.ReadLongTerm()
	if err != nil {
		return "", err
	}
	if longTerm != "" {
		parts = append(parts, "## Long-term Memory\n\n"+longTerm)
	}

	// 读取今日笔记
	today, err := m.ReadToday()
	if err != nil {
		return "", err
	}
	if today != "" {
		parts = append(parts, "## Today's Notes\n\n"+today)
	}

	if len(parts) == 0 {
		return "", nil
	}

	return strings.Join(parts, "\n\n---\n\n"), nil
}

// ReadBootstrapFile 读取 bootstrap 文件
func (m *MemoryStore) ReadBootstrapFile(filename string) (string, error) {
	path := filepath.Join(m.baseDIR, filename)

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return string(content), nil
}

// EnsureBootstrapFiles 确保 bootstrap 文件存在
func (m *MemoryStore) EnsureBootstrapFiles() error {

	// bootstrap 文件列表
	bootstrapFiles := []string{
		"IDENTITY.md",
		"AGENTS.md",
		"SOUL.md",
		"USER.md",
	}

	for _, filename := range bootstrapFiles {
		path := filepath.Join(m.baseDIR, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// 创建默认内容
			var defaultContent string
			switch filename {
			case "IDENTITY.md":
				defaultContent = "# Identity\n\nThis file defines the agent's identity and character."
			case "AGENTS.md":
				defaultContent = "# Agent Configuration\n\nThis file defines the agent's capabilities and configuration."
			case "SOUL.md":
				defaultContent = "# Agent Soul\n\nThis file defines the agent's personality and core principles."
			case "USER.md":
				defaultContent = "# User Information\n\nThis file contains information about the user."
			}

			if err := os.WriteFile(path, []byte(defaultContent), 0644); err != nil {
				return fmt.Errorf("failed to create bootstrap file %s: %w", filename, err)
			}
		}
	}

	return nil
}
