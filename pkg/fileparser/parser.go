// Package fileparser 提供从各种文件格式中提取结构化文档的能力。
// 所有解析器输出统一的 document.Document 结构，供上层 RAG/检索引擎消费。
package fileparser

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/Source-of-Intelligence/soi-rag/pkg/document"
)

// ============================================================================
// 解析器接口
// ============================================================================

// Parser 文件解析器接口
type Parser interface {
	// Parse 从 reader 解析内容为结构化文档，source 用于错误提示与标题
	Parse(reader io.Reader, source string) (*document.Document, error)

	// Name 返回解析器名称（用于调试）
	Name() string
}

// ============================================================================
// 解析器管理器
// ============================================================================

// ParserManager 根据文件扩展名选择合适的解析器
type ParserManager struct {
	parsers  map[string]Parser // 扩展名 -> 解析器
	fallback Parser            // 回退解析器
}

// NewManager 创建一个默认的解析器管理器，注册所有支持的格式
func NewManager() *ParserManager {
	pm := &ParserManager{
		parsers:  make(map[string]Parser),
		fallback: NewTextParser(),
	}

	// 注册 PDF
	pdf := NewPDFParser()
	pm.register(pdf, ".pdf")

	// 注册 DOCX
	docx := NewWordParser()
	pm.register(docx, ".docx", ".doc")

	// 注册 HTML
	html := NewHTMLParser()
	pm.register(html, ".html", ".htm", ".xhtml")

	// 注册 Markdown
	md := NewMarkdownParser()
	pm.register(md, ".md", ".markdown", ".mkd")

	// 注册 CSV
	csv := NewCSVParser()
	pm.register(csv, ".csv")

	// 注册 JSON
	json := NewJSONParser()
	pm.register(json, ".json")

	// 注册纯文本
	txt := NewTextParser()
	pm.register(txt, ".txt", ".text", ".log")

	return pm
}

func (pm *ParserManager) register(p Parser, extensions ...string) {
	for _, ext := range extensions {
		pm.parsers[strings.ToLower(ext)] = p
	}
}

// GetParserByExtension 根据扩展名返回对应的解析器
func (pm *ParserManager) GetParserByExtension(ext string) Parser {
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if p, ok := pm.parsers[ext]; ok {
		return p
	}
	return pm.fallback
}

// GetParserBySource 根据文件路径选择解析器
func (pm *ParserManager) GetParserBySource(source string) Parser {
	ext := strings.ToLower(filepath.Ext(source))
	if ext == "" {
		return pm.fallback
	}
	if p, ok := pm.parsers[ext]; ok {
		return p
	}
	return pm.fallback
}

// ParseFromPath 打开文件并解析
func (pm *ParserManager) ParseFromPath(path string) (*document.Document, error) {
	f, err := openFile(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	parser := pm.GetParserBySource(path)
	doc, err := parser.Parse(f, path)
	if err != nil {
		return nil, err
	}
	doc.CountElements()
	return doc, nil
}

// ParseByReader 使用指定扩展名的解析器来解析 reader
func (pm *ParserManager) ParseByReader(reader io.Reader, source string) (*document.Document, error) {
	parser := pm.GetParserBySource(source)
	doc, err := parser.Parse(reader, source)
	if err != nil {
		return nil, err
	}
	doc.CountElements()
	return doc, nil
}

// SupportedExtensions 返回当前管理器支持的所有扩展名
func (pm *ParserManager) SupportedExtensions() []string {
	exts := make([]string, 0, len(pm.parsers))
	for k := range pm.parsers {
		exts = append(exts, k)
	}
	return exts
}

// ============================================================================
// 公共辅助函数
// ============================================================================

// newDocument 构建一个基础 Document 骨架
func newDocument(title, source string, docType document.DocumentType) *document.Document {
	return &document.Document{
		Title:    title,
		Source:   source,
		DocType:  docType,
		Metadata: make(map[string]interface{}),
		ParsedAt: time.Now(),
	}
}

// extractTitle 从 source 路径提取文件名（去掉扩展名）
func extractTitle(source string) string {
	if source == "" {
		return "Untitled"
	}
	base := filepath.Base(source)
	ext := filepath.Ext(base)
	title := strings.TrimSuffix(base, ext)
	if title == "" {
		title = base
	}
	return title
}
