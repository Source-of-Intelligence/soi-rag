package fileparser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/document"
)

// WordParser DOCX 文件解析器
// DOCX 文件本质是 ZIP 包，内有 word/document.xml 等资源
type WordParser struct{}

func NewWordParser() *WordParser { return &WordParser{} }

func (p *WordParser) Name() string { return "docx" }

func (p *WordParser) Parse(reader io.Reader, source string) (*document.Document, error) {
	doc := newDocument(extractTitle(source), source, document.DocTypeWord)

	// 先读入内存
	data, err := io.ReadAll(reader)
	if err != nil {
		return doc, fmt.Errorf("read docx: %w", err)
	}

	// 以 zip 方式打开
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return doc, fmt.Errorf("open docx as zip: %w", err)
	}

	// 1. 提取 document.xml（主内容）
	var docXML []byte
	for _, f := range zipReader.File {
		if strings.HasSuffix(strings.ToLower(f.Name), "word/document.xml") {
			rc, err := f.Open()
			if err != nil {
				return doc, fmt.Errorf("open document.xml: %w", err)
			}
			docXML, _ = io.ReadAll(rc)
			rc.Close()
			break
		}
	}

	if len(docXML) == 0 {
		return doc, fmt.Errorf("docx: document.xml not found")
	}

	// 2. 尝试从 word/numbering.xml / word/styles.xml 提取可选元数据
	for _, f := range zipReader.File {
		if strings.HasSuffix(strings.ToLower(f.Name), "docprops/core.xml") {
			rc, err := f.Open()
			if err == nil {
				propsXML, _ := io.ReadAll(rc)
				rc.Close()
				if len(propsXML) > 0 {
					parseCoreProps(doc, propsXML)
				}
			}
			break
		}
	}

	// 3. 解析 document.xml
	parseWordDocument(doc, docXML)

	return doc, nil
}

// --- Word XML 解析 ---

// w:body 里的元素
type wordBody struct {
	XMLName  xml.Name `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main body"`
	Elements []any    `xml:",any"`
}

type wordDoc struct {
	XMLName xml.Name `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main document"`
	Body    wordBody `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main body"`
}

// <w:p> 段落
type wordParagraph struct {
	XMLName xml.Name `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main p"`
	PPr     *wPPr    `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pPr"`
	Elems   []any    `xml:",any"`
}

type wPPr struct {
	Style *wStyle `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pStyle"`
}

type wStyle struct {
	Val string `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main val,attr"`
}

// <w:r> run（带格式的文本段）
type wordRun struct {
	XMLName xml.Name `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main r"`
	Elems   []any    `xml:",any"`
}

// <w:t> 文本
type wordText struct {
	XMLName xml.Name `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main t"`
	Text    string   `xml:",chardata"`
}

// <w:br> 换行
type wordBr struct {
	XMLName xml.Name `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main br"`
}

// <w:tbl> 表格
type wordTable struct {
	XMLName xml.Name  `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tbl"`
	Rows    []wordRow `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tr"`
}

type wordRow struct {
	Cells []wordCell `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main tc"`
}

type wordCell struct {
	Paras []wordParagraph `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main p"`
}

func parseWordDocument(doc *document.Document, xmlData []byte) {
	var container wordDoc
	// decode 可能失败但我们仍尽可能提取
	_ = xml.Unmarshal(xmlData, &container)

	// 解析段落和表格
	pageCounter := 1
	currentPage := &document.Page{PageNumber: pageCounter}

	// 先尝试结构化解析
	if len(container.Body.Elements) > 0 {
		for _, rawEl := range container.Body.Elements {
			switch el := rawEl.(type) {
			case *xml.CharData:
				continue
			case *wordParagraph:
				// 解码段落里的文本
				text := extractParagraphText(el)
				if text == "" {
					continue
				}
				// 判断是否是标题段落
				if el.PPr != nil && el.PPr.Style != nil && strings.HasPrefix(el.PPr.Style.Val, "Heading") {
					level := parseHeadingLevel(el.PPr.Style.Val)
					doc.AddElement(&document.Heading{Level: level, Content: text})
				} else if el.PPr != nil && el.PPr.Style != nil && el.PPr.Style.Val == "Title" {
					// 文档标题
					if doc.Title == extractTitle(doc.Source) || doc.Title == "" {
						doc.Title = text
					} else {
						doc.AddElement(&document.Heading{Level: 1, Content: text})
					}
				} else {
					doc.AddElement(&document.Paragraph{Content: text})
					doc.ParaCount++
				}
			case *wordTable:
				table := parseWordTable(*el)
				if table != nil {
					doc.AddElement(table)
					doc.TableCount++
				}
			}
		}

		// 尝试检测页面分页
		doc.AddPage(currentPage)
	}

	// 如果结构化解析没有任何结果，回退到简单文本提取
	if len(doc.Elements) == 0 {
		plainText := extractWordPlainText(xmlData)
		lines := strings.Split(plainText, "\n")
		var para strings.Builder
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l == "" {
				if para.Len() > 0 {
					doc.AddElement(&document.Paragraph{Content: strings.TrimSpace(para.String())})
					doc.ParaCount++
					para.Reset()
				}
			} else {
				para.WriteString(l)
				para.WriteRune(' ')
			}
		}
		if para.Len() > 0 {
			doc.AddElement(&document.Paragraph{Content: strings.TrimSpace(para.String())})
			doc.ParaCount++
		}
	}

	_ = currentPage
	_ = pageCounter
}

// extractParagraphText 从段落中提取纯文本
func extractParagraphText(p *wordParagraph) string {
	var sb strings.Builder
	for _, e := range p.Elems {
		switch el := e.(type) {
		case *wordRun:
			for _, re := range el.Elems {
				switch t := re.(type) {
				case *wordText:
					sb.WriteString(t.Text)
				case *wordBr:
					sb.WriteString("\n")
				}
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

// parseHeadingLevel 把 "Heading1" / "Heading2" 等转为 1~6 的整数
func parseHeadingLevel(s string) int {
	s = strings.ToLower(s)
	switch s {
	case "heading1":
		return 1
	case "heading2":
		return 2
	case "heading3":
		return 3
	case "heading4":
		return 4
	case "heading5":
		return 5
	case "heading6":
		return 6
	default:
		return 2
	}
}

// parseWordTable 把 Word 表格转为 document.Table
func parseWordTable(t wordTable) *document.Table {
	if len(t.Rows) == 0 {
		return nil
	}
	table := &document.Table{}
	firstRow := true
	for _, row := range t.Rows {
		cells := make([]string, 0, len(row.Cells))
		for _, c := range row.Cells {
			var cellText strings.Builder
			for _, p := range c.Paras {
				pt := extractParagraphText(&p)
				if pt != "" {
					cellText.WriteString(pt)
					cellText.WriteString(" ")
				}
			}
			cells = append(cells, strings.TrimSpace(cellText.String()))
		}
		if firstRow {
			table.Headers = cells
			firstRow = false
		} else {
			table.Rows = append(table.Rows, cells)
		}
	}
	return table
}

// extractWordPlainText 用最简单的正则方式从 document.xml 中提取所有 <w:t> 文本
func extractWordPlainText(xmlData []byte) string {
	// 用正则匹配 <w:t> 和 </w:t> 之间的内容
	s := string(xmlData)
	var sb strings.Builder
	// 匹配 <w:p ...> 到 </w:p>，然后内部 <w:t> 文本
	paraRe := regexpMustCompile(`(?s)<w:p\b[^>]*>(.*?)</w:p>`)
	textRe := regexpMustCompile(`(?s)<w:t[^>]*>(.*?)</w:t>`)

	for _, pm := range paraRe.FindAllStringSubmatch(s, -1) {
		paraContent := pm[1]
		var paraText strings.Builder
		for _, tm := range textRe.FindAllStringSubmatch(paraContent, -1) {
			paraText.WriteString(tm[1])
			paraText.WriteString(" ")
		}
		if pt := strings.TrimSpace(paraText.String()); pt != "" {
			sb.WriteString(pt)
			sb.WriteString("\n\n")
		}
	}

	return strings.TrimSpace(sb.String())
}

// parseCoreProps 解析 docProps/core.xml 中的元数据
func parseCoreProps(doc *document.Document, xmlData []byte) {
	s := string(xmlData)
	if title := extractTag(s, "dc:title"); title != "" {
		doc.Metadata["title"] = title
	}
	if author := extractTag(s, "dc:creator"); author != "" {
		doc.Metadata["author"] = author
	}
	if subject := extractTag(s, "dc:subject"); subject != "" {
		doc.Metadata["subject"] = subject
	}
	if created := extractTag(s, "dcterms:created"); created != "" {
		doc.Metadata["created"] = created
	}
	if modified := extractTag(s, "dcterms:modified"); modified != "" {
		doc.Metadata["modified"] = modified
	}
}

func extractTag(s, tag string) string {
	reOpen := regexpMustCompile(`<` + regexpEscape(tag) + `[^>]*>(.*?)</` + regexpEscape(tag) + `>`)
	m := reOpen.FindStringSubmatch(s)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func regexpMustCompile(p string) *regexp.Regexp {
	return regexp.MustCompile(p)
}

func regexpEscape(s string) string {
	return strings.NewReplacer(
		`+`, `\+`, `*`, `\*`, `?`, `\?`, `(`, `\(`, `)`, `\)`,
		`[`, `\[`, `]`, `\]`, `{`, `\{`, `}`, `\}`, `\`, `\\`,
		`^`, `\^`, `$`, `\$`, `.`, `\.`, `|`, `\|`,
	).Replace(s)
}
