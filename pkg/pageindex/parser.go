package pageindex

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
)

// Parser 文档解析器接口
type Parser interface {
	Parse(reader io.Reader, source string) (*models.Document, error)
	Supports(docType models.DocumentType) bool
}

// ParseResult 解析结果
type ParseResult struct {
	Title    string
	Content  string
	Metadata map[string]interface{}
}

// TextParser 文本解析器
type TextParser struct{}

// NewTextParser 创建文本解析器
func NewTextParser() *TextParser {
	return &TextParser{}
}

// Parse 解析文本
func (p *TextParser) Parse(reader io.Reader, source string) (*models.Document, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("读取文本失败: %w", err)
	}

	title := extractTitleFromSource(source)

	doc := &models.Document{
		Title:    title,
		Content:  string(content),
		Source:   source,
		DocType:  models.DocTypeText,
		Metadata: make(map[string]interface{}),
	}

	return doc, nil
}

// Supports 是否支持该类型
func (p *TextParser) Supports(docType models.DocumentType) bool {
	return docType == models.DocTypeText
}

// MarkdownParser Markdown解析器
type MarkdownParser struct{}

// NewMarkdownParser 创建Markdown解析器
func NewMarkdownParser() *MarkdownParser {
	return &MarkdownParser{}
}

// Parse 解析Markdown
func (p *MarkdownParser) Parse(reader io.Reader, source string) (*models.Document, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("读取Markdown失败: %w", err)
	}

	contentStr := string(content)
	title := extractMarkdownTitle(contentStr)
	if title == "" {
		title = extractTitleFromSource(source)
	}

	// 提取元数据（简化版）
	metadata := extractMarkdownMetadata(contentStr)

	doc := &models.Document{
		Title:    title,
		Content:  contentStr,
		Source:   source,
		DocType:  models.DocTypeMarkdown,
		Metadata: metadata,
	}

	return doc, nil
}

// Supports 是否支持该类型
func (p *MarkdownParser) Supports(docType models.DocumentType) bool {
	return docType == models.DocTypeMarkdown
}

// HTMLParser HTML解析器（简化版）
type HTMLParser struct{}

// NewHTMLParser 创建HTML解析器
func NewHTMLParser() *HTMLParser {
	return &HTMLParser{}
}

// Parse 解析HTML
func (p *HTMLParser) Parse(reader io.Reader, source string) (*models.Document, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("读取HTML失败: %w", err)
	}

	contentStr := string(content)

	// 简单的HTML标签移除（生产环境应使用更完善的HTML解析库）
	text := stripHTMLTags(contentStr)
	title := extractHTMLTitle(contentStr)
	if title == "" {
		title = extractTitleFromSource(source)
	}

	doc := &models.Document{
		Title:    title,
		Content:  text,
		Source:   source,
		DocType:  models.DocTypeHTML,
		Metadata: make(map[string]interface{}),
	}

	return doc, nil
}

// Supports 是否支持该类型
func (p *HTMLParser) Supports(docType models.DocumentType) bool {
	return docType == models.DocTypeHTML
}

// CSVParser CSV解析器
type CSVParser struct{}

// NewCSVParser 创建CSV解析器
func NewCSVParser() *CSVParser {
	return &CSVParser{}
}

// Parse 解析CSV
func (p *CSVParser) Parse(reader io.Reader, source string) (*models.Document, error) {
	csvReader := csv.NewReader(reader)

	// 读取所有记录
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析CSV失败: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV文件为空")
	}

	// 将CSV转换为文本格式
	var sb strings.Builder
	headers := records[0]

	for i, record := range records[1:] {
		sb.WriteString(fmt.Sprintf("记录 %d:\n", i+1))
		for j, value := range record {
			if j < len(headers) {
				sb.WriteString(fmt.Sprintf("  %s: %s\n", headers[j], value))
			}
		}
		sb.WriteString("\n")
	}

	doc := &models.Document{
		Title:   extractTitleFromSource(source),
		Content: sb.String(),
		Source:  source,
		DocType: models.DocTypeCSV,
		Metadata: map[string]interface{}{
			"row_count":    len(records) - 1,
			"column_count": len(headers),
			"headers":      headers,
		},
	}

	return doc, nil
}

// Supports 是否支持该类型
func (p *CSVParser) Supports(docType models.DocumentType) bool {
	return docType == models.DocTypeCSV
}

// JSONParser JSON解析器
type JSONParser struct{}

// NewJSONParser 创建JSON解析器
func NewJSONParser() *JSONParser {
	return &JSONParser{}
}

// Parse 解析JSON
func (p *JSONParser) Parse(reader io.Reader, source string) (*models.Document, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("读取JSON失败: %w", err)
	}

	var data interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w", err)
	}

	// 将JSON转换为格式化的文本
	formattedJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("格式化JSON失败: %w", err)
	}

	doc := &models.Document{
		Title:   extractTitleFromSource(source),
		Content: string(formattedJSON),
		Source:  source,
		DocType: models.DocTypeJSON,
		Metadata: map[string]interface{}{
			"json_type": detectJSONType(data),
		},
	}

	return doc, nil
}

// Supports 是否支持该类型
func (p *JSONParser) Supports(docType models.DocumentType) bool {
	return docType == models.DocTypeJSON
}

// ParserManager 解析器管理器
type ParserManager struct {
	parsers      map[models.DocumentType]Parser
	extensionMap map[string]models.DocumentType // 扩展名 -> 文档类型映射
}

// NewParserManager 创建解析器管理器
func NewParserManager() *ParserManager {
	pm := &ParserManager{
		parsers:      make(map[models.DocumentType]Parser),
		extensionMap: make(map[string]models.DocumentType),
	}

	// 注册默认解析器
	pm.Register(models.DocTypeText, NewTextParser())
	pm.Register(models.DocTypeMarkdown, NewMarkdownParser())
	pm.Register(models.DocTypeHTML, NewHTMLParser())
	pm.Register(models.DocTypeCSV, NewCSVParser())
	pm.Register(models.DocTypeJSON, NewJSONParser())
	pm.Register(models.DocTypePDF, NewPDFParser())
	pm.Register(models.DocTypeWord, NewWordParser())

	return pm
}

// Register 注册解析器
func (pm *ParserManager) Register(docType models.DocumentType, parser Parser) {
	pm.parsers[docType] = parser

	// 如果解析器实现了 ExtensionParser 接口，注册扩展名映射
	if ep, ok := parser.(interface{ SupportedExtensions() []string }); ok {
		for _, ext := range ep.SupportedExtensions() {
			pm.extensionMap[ext] = docType
		}
	}
}

// RegisterWithExtensions 注册解析器并指定支持的扩展名
func (pm *ParserManager) RegisterWithExtensions(docType models.DocumentType, parser Parser, extensions []string) {
	pm.parsers[docType] = parser
	for _, ext := range extensions {
		pm.extensionMap[ext] = docType
	}
}

// GetParser 获取解析器
func (pm *ParserManager) GetParser(docType models.DocumentType) (Parser, error) {
	parser, ok := pm.parsers[docType]
	if !ok {
		return nil, fmt.Errorf("不支持的文档类型: %s", docType)
	}
	return parser, nil
}

// GetParserByExtension 根据文件扩展名获取解析器
func (pm *ParserManager) GetParserByExtension(ext string) (Parser, models.DocumentType, error) {
	// 标准化扩展名
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	docType, ok := pm.extensionMap[ext]
	if !ok {
		// 回退到 DetectDocType
		docType = DetectDocType(ext)
	}

	parser, err := pm.GetParser(docType)
	if err != nil {
		return nil, docType, fmt.Errorf("未找到扩展名 %s 对应的解析器: %w", ext, err)
	}

	return parser, docType, nil
}

// Parse 解析文档
func (pm *ParserManager) Parse(reader io.Reader, source string, docType models.DocumentType) (*models.Document, error) {
	parser, err := pm.GetParser(docType)
	if err != nil {
		return nil, err
	}
	return parser.Parse(reader, source)
}

// ParseByExtension 根据扩展名解析文档
func (pm *ParserManager) ParseByExtension(reader io.Reader, source string) (*models.Document, error) {
	ext := strings.ToLower(filepath.Ext(source))
	parser, _, err := pm.GetParserByExtension(ext)
	if err != nil {
		return nil, err
	}
	return parser.Parse(reader, source)
}

// SupportedExtensions 返回所有支持的文件扩展名
func (pm *ParserManager) SupportedExtensions() []string {
	extensions := make([]string, 0, len(pm.extensionMap))
	for ext := range pm.extensionMap {
		extensions = append(extensions, ext)
	}
	return extensions
}

// SupportedDocTypes 返回所有支持的文档类型
func (pm *ParserManager) SupportedDocTypes() []models.DocumentType {
	types := make([]models.DocumentType, 0, len(pm.parsers))
	for docType := range pm.parsers {
		types = append(types, docType)
	}
	return types
}

// DetectDocType 根据文件路径检测文档类型
func DetectDocType(source string) models.DocumentType {
	ext := strings.ToLower(filepath.Ext(source))
	switch ext {
	case ".pdf":
		return models.DocTypePDF
	case ".docx", ".doc":
		return models.DocTypeWord
	case ".md", ".markdown":
		return models.DocTypeMarkdown
	case ".html", ".htm":
		return models.DocTypeHTML
	case ".txt":
		return models.DocTypeText
	case ".csv":
		return models.DocTypeCSV
	case ".json":
		return models.DocTypeJSON
	default:
		return models.DocTypeText
	}
}

// 辅助函数

// extractTitleFromSource 从源路径提取标题
func extractTitleFromSource(source string) string {
	base := filepath.Base(source)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// extractMarkdownTitle 提取Markdown标题
func extractMarkdownTitle(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

// extractMarkdownMetadata 提取Markdown元数据（YAML front matter）
func extractMarkdownMetadata(content string) map[string]interface{} {
	metadata := make(map[string]interface{})

	// 检查是否有YAML front matter
	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) >= 3 {
			// 简单解析YAML front matter
			lines := strings.Split(parts[1], "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if idx := strings.Index(line, ":"); idx > 0 {
					key := strings.TrimSpace(line[:idx])
					value := strings.TrimSpace(line[idx+1:])
					metadata[key] = value
				}
			}
		}
	}

	return metadata
}

// stripHTMLTags 移除HTML标签
func stripHTMLTags(html string) string {
	var result strings.Builder
	inTag := false

	for _, r := range html {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				result.WriteRune(r)
			}
		}
	}

	// 清理多余空白
	text := result.String()
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}

	return strings.Join(cleaned, "\n")
}

// extractHTMLTitle 提取HTML标题
func extractHTMLTitle(html string) string {
	// 简单的标题提取
	startIdx := strings.Index(html, "<title>")
	if startIdx == -1 {
		startIdx = strings.Index(html, "<TITLE>")
	}
	if startIdx == -1 {
		return ""
	}

	startIdx += len("<title>")
	endIdx := strings.Index(html[startIdx:], "</title>")
	if endIdx == -1 {
		endIdx = strings.Index(html[startIdx:], "</TITLE>")
	}
	if endIdx == -1 {
		return ""
	}

	return strings.TrimSpace(html[startIdx : startIdx+endIdx])
}

// detectJSONType 检测JSON类型
func detectJSONType(data interface{}) string {
	switch data.(type) {
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "value"
	}
}
