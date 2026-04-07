package rag

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Parser 文档解析器
type Parser struct {
	supportedExtensions map[string]bool
}

// NewParser 创建解析器
func NewParser() *Parser {
	return &Parser{
		supportedExtensions: map[string]bool{
			".md":   true,
			".txt":  true,
			".go":   true,
			".json": true,
			".yaml": true,
			".yml":  true,
			".mod":  true,
			".sum":  true,
			".ts":   true,
			".tsx":  true,
			".js":   true,
			".jsx":  true,
			".py":   true,
			".rs":   true,
			".java": true,
			".c":    true,
			".cpp":  true,
			".h":    true,
			".hpp":  true,
			".sh":   true,
			".bash": true,
			".zsh":  true,
			".sql":  true,
			".html": true,
			".css":  true,
			".xml":  true,
			".ini":  true,
			".toml": true,
		},
	}
}

// SupportedExtensions 返回支持的扩展名列表
func (p *Parser) SupportedExtensions() []string {
	exts := make([]string, 0, len(p.supportedExtensions))
	for ext := range p.supportedExtensions {
		exts = append(exts, ext)
	}
	return exts
}

// IsSupported 检查文件扩展名是否支持
func (p *Parser) IsSupported(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return p.supportedExtensions[ext]
}

// ParseFile 解析文件
func (p *Parser) ParseFile(path string) (*DocumentInfo, error) {
	if !p.IsSupported(path) {
		return nil, fmt.Errorf("unsupported file type: %s", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	// 获取文件修改时间
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file %s: %w", path, err)
	}

	doc := &DocumentInfo{
		ID:         GenerateDocumentID(path),
		SourcePath: path,
		Content:    string(content),
		Metadata: map[string]any{
			"extension":  filepath.Ext(path),
			"filename":   filepath.Base(path),
			"size":       len(content),
		},
		ModTime:  info.ModTime().Unix(),
	}

	return doc, nil
}

// ParseFiles 解析多个文件
func (p *Parser) ParseFiles(paths []string) ([]*DocumentInfo, error) {
	var docs []*DocumentInfo
	var errors []string

	for _, path := range paths {
		doc, err := p.ParseFile(path)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		docs = append(docs, doc)
	}

	if len(errors) > 0 {
		return docs, fmt.Errorf("some files failed to parse: %s", strings.Join(errors, "; "))
	}

	return docs, nil
}

// FileScanner 文件扫描器
type FileScanner struct {
	parser      *Parser
	extensions  []string // 过滤的扩展名
	exclude     []string // 排除模式
}

// NewFileScanner 创建文件扫描器
func NewFileScanner(parser *Parser, extensions []string, exclude []string) *FileScanner {
	return &FileScanner{
		parser:     parser,
		extensions: extensions,
		exclude:    exclude,
	}
}

// ScanDir 扫描目录获取所有支持的文件
func (s *FileScanner) ScanDir(dir string, recursive bool) ([]string, error) {
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			if !recursive && path != dir {
				return filepath.SkipDir
			}
			return nil
		}

		// 检查扩展名
		ext := strings.ToLower(filepath.Ext(path))
		if len(s.extensions) > 0 {
			found := false
			for _, allowed := range s.extensions {
				if ext == "."+strings.ToLower(allowed) || ext == strings.ToLower(allowed) {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		} else {
			// 使用 parser 默认支持的扩展名
			if !s.parser.IsSupported(path) {
				return nil
			}
		}

		// 检查排除模式
		for _, pattern := range s.exclude {
		 matched, _ := filepath.Match(pattern, filepath.Base(path))
			if matched {
				return nil
			}
			// 也检查完整路径匹配
			matchedFull, _ := filepath.Match(pattern, path)
			if matchedFull {
				return nil
			}
		}

		files = append(files, path)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory %s: %w", dir, err)
	}

	return files, nil
}

// shouldIncludeFile 检查文件是否应该被包含
func (s *FileScanner) shouldIncludeFile(path string) bool {
	// 检查扩展名
	ext := strings.ToLower(filepath.Ext(path))
	if len(s.extensions) > 0 {
		found := false
		for _, allowed := range s.extensions {
			if ext == "."+strings.ToLower(allowed) || ext == strings.ToLower(allowed) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// 检查排除模式
	for _, pattern := range s.exclude {
		matched, _ := filepath.Match(pattern, filepath.Base(path))
		if matched {
			return false
		}
	}

	return true
}