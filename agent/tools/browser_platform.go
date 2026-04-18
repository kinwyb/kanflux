package tools

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// createTempDir 创建临时目录
func createTempDir(prefix string) (string, error) {
	return os.MkdirTemp("", prefix)
}

// cleanupTempDir 清理临时目录
func cleanupTempDir(path string) {
	_ = os.RemoveAll(path)
}

// findChrome 查找 Chrome 可执行文件
func (b *BrowserSessionManager) findChrome() (string, error) {
	// 常见 Chrome 路径
	paths := chromePaths()

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// 尝试通过命令查找
	for _, cmd := range chromeCommands() {
		if path, err := exec.LookPath(cmd); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("Chrome not found in common locations")
}

// chromePaths 返回平台相关的 Chrome 路径
func chromePaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "linux":
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium-browser",
			"/usr/bin/chromium",
			"/snap/bin/chromium",
		}
	case "windows":
		return []string{
			"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
			"C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
			os.Getenv("LOCALAPPDATA") + "\\Google\\Chrome\\Application\\chrome.exe",
		}
	default:
		return []string{}
	}
}

// chromeCommands 返回平台相关的 Chrome 命令名称
func chromeCommands() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"google-chrome", "chromium"}
	case "linux":
		return []string{"google-chrome", "google-chrome-stable", "chromium-browser", "chromium"}
	case "windows":
		return []string{"chrome"}
	default:
		return []string{"google-chrome", "chromium"}
	}
}

// launchChrome 启动 Chrome 进程
func (b *BrowserSessionManager) launchChrome(chromePath, userDataDir string) error {
	args := []string{
		"--headless=new",
		"--no-sandbox",
		"--disable-setuid-sandbox",
		"--disable-dev-shm-usage",
		"--disable-gpu",
		"--disable-software-rasterizer",
		"--remote-debugging-port=9222",
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
	}

	cmd := exec.Command(chromePath, args...)
	if err := cmd.Start(); err != nil {
		return err
	}

	b.cmd = cmd
	return nil
}

// killChrome 停止 Chrome 进程
func (b *BrowserSessionManager) killChrome() {
	if b.cmd != nil {
		if cmd, ok := b.cmd.(*exec.Cmd); ok && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		b.cmd = nil
	}
}