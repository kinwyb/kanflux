package rag

// DocumentInfo 文档信息 (用于 parser 输出)
type DocumentInfo struct {
	ID         string         `json:"id"`
	SourcePath string         `json:"source_path"`
	Content    string         `json:"content"`
	Metadata   map[string]any `json:"metadata"`
	ModTime    int64          `json:"mod_time"`
}