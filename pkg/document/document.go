// Package document 定义了通用的结构化文档模型。
// 所有文件解析器（PDF/DOCX/HTML/Markdown 等）输出统一的 Document 结构，
// 上层的 RAG 引擎、搜索引擎直接消费此结构化数据。
package document

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ============================================================================
// 文档类型
// ============================================================================

// DocumentType 文档文件类型
type DocumentType string

const (
	DocTypePDF      DocumentType = "pdf"
	DocTypeWord     DocumentType = "docx"
	DocTypeHTML     DocumentType = "html"
	DocTypeMarkdown DocumentType = "markdown"
	DocTypeText     DocumentType = "text"
	DocTypeCSV      DocumentType = "csv"
	DocTypeJSON     DocumentType = "json"
	DocTypeUnknown  DocumentType = "unknown"
)

// ============================================================================
// 内容元素类型
// ============================================================================

// ElementType 内容元素类型
type ElementType string

const (
	ElemParagraph ElementType = "paragraph"
	ElemHeading   ElementType = "heading"
	ElemTable     ElementType = "table"
	ElemList      ElementType = "list"
	ElemListItem  ElementType = "list_item"
	ElemImage     ElementType = "image"
	ElemCodeBlock ElementType = "code_block"
	ElemSeparator ElementType = "separator"
)

// ============================================================================
// 位置信息
// ============================================================================

// Position 元素在原始文档中的位置信息
type Position struct {
	PageNumber int    `json:"page_number,omitempty"` // 页码（PDF/DOCX 等分页文档）
	LineStart  int    `json:"line_start,omitempty"`  // 起始行（文本文档）
	LineEnd    int    `json:"line_end,omitempty"`    // 结束行
	XPath      string `json:"xpath,omitempty"`       // XPath 或结构路径（HTML/DOCX）
}

// ============================================================================
// 内容元素
// ============================================================================

// Element 文档内容元素（统一接口）
type Element interface {
	Type() ElementType // 元素类型
	Text() string      // 纯文本表示（用于索引/检索）
	String() string    // 人类可读字符串
	IsEmpty() bool     // 是否为空
}

// Heading 标题元素
type Heading struct {
	Level    int      `json:"level"`            // 标题层级（1-6）
	Content  string   `json:"text"`             // 标题文本
	Anchor   string   `json:"anchor,omitempty"` // HTML anchor / DOCX bookmark
	Position Position `json:"position,omitempty"`
}

func (h *Heading) Type() ElementType { return ElemHeading }
func (h *Heading) Text() string      { return h.Content }
func (h *Heading) IsEmpty() bool     { return strings.TrimSpace(h.Content) == "" }
func (h *Heading) String() string {
	prefix := strings.Repeat("#", h.Level)
	return fmt.Sprintf("%s %s", prefix, h.Content)
}

// Paragraph 段落元素
type Paragraph struct {
	Content  string   `json:"text"`
	Position Position `json:"position,omitempty"`
}

func (p *Paragraph) Type() ElementType { return ElemParagraph }
func (p *Paragraph) Text() string      { return p.Content }
func (p *Paragraph) IsEmpty() bool     { return strings.TrimSpace(p.Content) == "" }
func (p *Paragraph) String() string    { return p.Content }

// Table 表格元素
type Table struct {
	Headers  []string   `json:"headers,omitempty"`
	Rows     [][]string `json:"rows,omitempty"`
	Caption  string     `json:"caption,omitempty"`
	Position Position   `json:"position,omitempty"`
}

func (t *Table) Type() ElementType { return ElemTable }
func (t *Table) IsEmpty() bool     { return len(t.Rows) == 0 && len(t.Headers) == 0 }
func (t *Table) Text() string {
	var sb strings.Builder
	if t.Caption != "" {
		sb.WriteString(t.Caption)
		sb.WriteString("\n")
	}
	if len(t.Headers) > 0 {
		sb.WriteString(strings.Join(t.Headers, " | "))
		sb.WriteString("\n")
	}
	for _, row := range t.Rows {
		sb.WriteString(strings.Join(row, " | "))
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}
func (t *Table) String() string {
	var sb strings.Builder
	if t.Caption != "" {
		sb.WriteString("Table: ")
		sb.WriteString(t.Caption)
		sb.WriteString("\n")
	}
	if len(t.Headers) > 0 {
		sb.WriteString("| ")
		sb.WriteString(strings.Join(t.Headers, " | "))
		sb.WriteString(" |\n|")
		for range t.Headers {
			sb.WriteString("---|")
		}
		sb.WriteString("\n")
	}
	for _, row := range t.Rows {
		sb.WriteString("| ")
		sb.WriteString(strings.Join(row, " | "))
		sb.WriteString(" |\n")
	}
	return strings.TrimSpace(sb.String())
}

// ListItem 列表项
type ListItem struct {
	Content  string   `json:"text"`
	Children []*List  `json:"children,omitempty"` // 嵌套列表
	Position Position `json:"position,omitempty"`
}

// List 列表元素
type List struct {
	Ordered  bool        `json:"ordered"` // true=有序列表(ol), false=无序列表(ul)
	Items    []*ListItem `json:"items,omitempty"`
	Position Position    `json:"position,omitempty"`
}

func (l *List) Type() ElementType { return ElemList }
func (l *List) IsEmpty() bool     { return len(l.Items) == 0 }

func (l *List) Text() string {
	var sb strings.Builder
	for i, item := range l.Items {
		if l.Ordered {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Content))
		} else {
			sb.WriteString(fmt.Sprintf("- %s\n", item.Content))
		}
	}
	return strings.TrimSpace(sb.String())
}

func (l *List) String() string { return l.Text() }

// Image 图片元素
type Image struct {
	Alt      string   `json:"alt,omitempty"` // 替代文本
	Src      string   `json:"src,omitempty"` // 图片路径/URL
	Width    int      `json:"width,omitempty"`
	Height   int      `json:"height,omitempty"`
	Caption  string   `json:"caption,omitempty"`
	Position Position `json:"position,omitempty"`
}

func (i *Image) Type() ElementType { return ElemImage }
func (i *Image) IsEmpty() bool     { return i.Src == "" && i.Alt == "" }
func (i *Image) Text() string {
	if i.Alt != "" {
		return fmt.Sprintf("[Image: %s]", i.Alt)
	}
	return fmt.Sprintf("[Image: %s]", i.Src)
}
func (i *Image) String() string { return i.Text() }

// CodeBlock 代码块元素
type CodeBlock struct {
	Language string   `json:"language,omitempty"`
	Code     string   `json:"code"`
	Position Position `json:"position,omitempty"`
}

func (c *CodeBlock) Type() ElementType { return ElemCodeBlock }
func (c *CodeBlock) IsEmpty() bool     { return strings.TrimSpace(c.Code) == "" }
func (c *CodeBlock) Text() string      { return c.Code }
func (c *CodeBlock) String() string {
	return fmt.Sprintf("```%s\n%s\n```", c.Language, c.Code)
}

// Separator 分隔符元素（如水平线、分页符）
type Separator struct {
	Style    string   `json:"style,omitempty"`
	Position Position `json:"position,omitempty"`
}

func (s *Separator) Type() ElementType { return ElemSeparator }
func (s *Separator) IsEmpty() bool     { return false }
func (s *Separator) Text() string      { return "" }
func (s *Separator) String() string    { return "---" }

// ============================================================================
// 章节（Section）
// ============================================================================

// Section 文档章节（由标题与内容组成的逻辑块）
type Section struct {
	Level       int        `json:"level"` // 章节层级（1=顶级）
	Title       string     `json:"title"` // 章节标题
	TitleAnchor string     `json:"anchor,omitempty"`
	Elements    []Element  `json:"elements,omitempty"`     // 章节内的内容元素
	SubSections []*Section `json:"sub_sections,omitempty"` // 子章节
}

// AddElement 添加内容元素
func (s *Section) AddElement(el Element) {
	if el != nil && !el.IsEmpty() {
		s.Elements = append(s.Elements, el)
	}
}

// AddSubSection 添加子章节
func (s *Section) AddSubSection(sub *Section) {
	if sub != nil {
		s.SubSections = append(s.SubSections, sub)
	}
}

// Text 返回章节的全部纯文本
func (s *Section) Text() string {
	var sb strings.Builder
	if s.Title != "" {
		sb.WriteString(s.Title)
		sb.WriteString("\n\n")
	}
	for _, el := range s.Elements {
		t := el.Text()
		if t != "" {
			sb.WriteString(t)
			sb.WriteString("\n\n")
		}
	}
	for _, sub := range s.SubSections {
		sb.WriteString(sub.Text())
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// ============================================================================
// 页面（Page）— 对于分页文档（PDF/DOCX）
// ============================================================================

// Page 单页内容
type Page struct {
	PageNumber int       `json:"page_number"`
	Elements   []Element `json:"elements,omitempty"` // 页内元素
	WordCount  int       `json:"word_count,omitempty"`
}

// AddElement 添加元素
func (p *Page) AddElement(el Element) {
	if el != nil && !el.IsEmpty() {
		p.Elements = append(p.Elements, el)
		p.WordCount += len(strings.Fields(el.Text()))
	}
}

// Text 返回页面全部纯文本
func (p *Page) Text() string {
	var sb strings.Builder
	for _, el := range p.Elements {
		t := el.Text()
		if t != "" {
			sb.WriteString(t)
			sb.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

// ============================================================================
// 顶层文档（Document）
// ============================================================================

// Document 通用结构化文档
type Document struct {
	// 基础信息
	ID       string       `json:"id,omitempty"`
	Title    string       `json:"title"`
	Source   string       `json:"source"`             // 源路径/URL
	DocType  DocumentType `json:"doc_type"`           // 文件类型
	Language string       `json:"language,omitempty"` // 语言（zh/en 等）

	// 统计信息
	PageCount  int `json:"page_count,omitempty"`
	WordCount  int `json:"word_count,omitempty"`
	ParaCount  int `json:"paragraph_count,omitempty"`
	TableCount int `json:"table_count,omitempty"`
	ImageCount int `json:"image_count,omitempty"`

	// 结构化内容
	Pages    []*Page    `json:"pages,omitempty"`    // 按页组织（PDF/DOCX）
	Sections []*Section `json:"sections,omitempty"` // 按章节组织（HTML/Markdown）
	Elements []Element  `json:"elements,omitempty"` // 顶层元素（无章节结构的文档）

	// 元数据
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// 时间
	ParsedAt time.Time `json:"parsed_at"`
}

// ============================================================================
// 便捷方法
// ============================================================================

// RawText 提取文档的全部纯文本（用于关键词索引/向量嵌入）
func (d *Document) RawText() string {
	var sb strings.Builder

	// 1. 从 Sections 提取
	for _, sec := range d.Sections {
		t := sec.Text()
		if t != "" {
			sb.WriteString(t)
			sb.WriteString("\n\n")
		}
	}

	// 2. 从 Pages 提取（如果没有 Sections）
	if len(d.Sections) == 0 {
		for _, page := range d.Pages {
			t := page.Text()
			if t != "" {
				sb.WriteString(t)
				sb.WriteString("\n\n")
			}
		}
	}

	// 3. 从顶层 Elements 提取
	for _, el := range d.Elements {
		t := el.Text()
		if t != "" {
			sb.WriteString(t)
			sb.WriteString("\n\n")
		}
	}

	return strings.TrimSpace(sb.String())
}

// IsEmpty 文档是否为空
func (d *Document) IsEmpty() bool {
	return len(d.Pages) == 0 && len(d.Sections) == 0 && len(d.Elements) == 0 &&
		d.RawText() == ""
}

// AddPage 添加页面
func (d *Document) AddPage(page *Page) {
	if page != nil {
		d.Pages = append(d.Pages, page)
		d.PageCount = len(d.Pages)
	}
}

// AddSection 添加章节
func (d *Document) AddSection(sec *Section) {
	if sec != nil {
		d.Sections = append(d.Sections, sec)
	}
}

// AddElement 添加顶层元素
func (d *Document) AddElement(el Element) {
	if el != nil && !el.IsEmpty() {
		d.Elements = append(d.Elements, el)
	}
}

// CountElements 统计各类元素数量
func (d *Document) CountElements() {
	d.WordCount = len(strings.Fields(d.RawText()))
	// 其他字段在解析时填充
}

// ============================================================================
// 调试输出：PrettyPrint
// ============================================================================

// PrettyPrint 以人类可读的格式打印文档结构
func (d *Document) PrettyPrint(indent string) string {
	var sb strings.Builder

	sb.WriteString(indent)
	sb.WriteString("=== Document ===\n")
	sb.WriteString(fmt.Sprintf("%sTitle: %s\n", indent, d.Title))
	sb.WriteString(fmt.Sprintf("%sType: %s\n", indent, d.DocType))
	sb.WriteString(fmt.Sprintf("%sSource: %s\n", indent, d.Source))
	if d.Language != "" {
		sb.WriteString(fmt.Sprintf("%sLang: %s\n", indent, d.Language))
	}
	sb.WriteString(fmt.Sprintf("%sPages: %d\n", indent, d.PageCount))
	sb.WriteString(fmt.Sprintf("%sWords: %d\n", indent, d.WordCount))
	if d.ParaCount > 0 {
		sb.WriteString(fmt.Sprintf("%sParagraphs: %d\n", indent, d.ParaCount))
	}
	if d.TableCount > 0 {
		sb.WriteString(fmt.Sprintf("%sTables: %d\n", indent, d.TableCount))
	}
	if d.ImageCount > 0 {
		sb.WriteString(fmt.Sprintf("%sImages: %d\n", indent, d.ImageCount))
	}

	// Metadata
	if len(d.Metadata) > 0 {
		sb.WriteString(fmt.Sprintf("%sMetadata:\n", indent))
		for k, v := range d.Metadata {
			sb.WriteString(fmt.Sprintf("%s  - %s: %v\n", indent, k, v))
		}
	}

	// Pages 结构
	if len(d.Pages) > 0 {
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("%s--- Pages (%d) ---\n", indent, len(d.Pages)))
		for i, page := range d.Pages {
			if i >= 20 { // 最多显示 20 页
				sb.WriteString(fmt.Sprintf("%s  ... (%d more pages)\n",
					indent, len(d.Pages)-20))
				break
			}
			sb.WriteString(fmt.Sprintf("%s  Page %d (%d elements, %d words)\n",
				indent, page.PageNumber, len(page.Elements), page.WordCount))
			for j, el := range page.Elements {
				if j >= 10 { // 每页最多显示 10 个元素
					sb.WriteString(fmt.Sprintf("%s    ... (%d more elements)\n",
						indent, len(page.Elements)-10))
					break
				}
				sb.WriteString(fmt.Sprintf("%s    [%s] %s\n",
					indent, el.Type(), truncate(el.String(), 100)))
			}
		}
	}

	// Sections 结构
	if len(d.Sections) > 0 {
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("%s--- Sections (%d) ---\n", indent, len(d.Sections)))
		for _, sec := range d.Sections {
			printSection(&sb, sec, indent+"  ")
		}
	}

	// 顶层 Elements
	if len(d.Elements) > 0 {
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("%s--- Top Elements (%d) ---\n", indent, len(d.Elements)))
		for i, el := range d.Elements {
			if i >= 20 {
				sb.WriteString(fmt.Sprintf("%s  ... (%d more)\n", indent, len(d.Elements)-20))
				break
			}
			sb.WriteString(fmt.Sprintf("%s  [%s] %s\n",
				indent, el.Type(), truncate(el.String(), 100)))
		}
	}

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("%s--- Full Text Preview (%d chars) ---\n",
		indent, len(d.RawText())))
	text := d.RawText()
	if len(text) > 500 {
		text = text[:500] + "..."
	}
	sb.WriteString(indent)
	sb.WriteString("  ")
	sb.WriteString(strings.ReplaceAll(text, "\n", "\n"+indent+"  "))
	sb.WriteString("\n")

	return sb.String()
}

func printSection(sb *strings.Builder, sec *Section, indent string) {
	sb.WriteString(fmt.Sprintf("%s[H%d] %s\n", indent, sec.Level, truncate(sec.Title, 100)))
	for i, el := range sec.Elements {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("%s  ... (%d more elements)\n",
				indent, len(sec.Elements)-10))
			break
		}
		sb.WriteString(fmt.Sprintf("%s  [%s] %s\n",
			indent, el.Type(), truncate(el.String(), 100)))
	}
	for _, sub := range sec.SubSections {
		printSection(sb, sub, indent+"  ")
	}
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return "(empty)"
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ============================================================================
// JSON 序列化
// ============================================================================

// ToJSON 将文档序列化为 JSON 字符串（带缩进）
func (d *Document) ToJSON() (string, error) {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", fmt.Errorf("serialize document: %w", err)
	}
	return string(data), nil
}
