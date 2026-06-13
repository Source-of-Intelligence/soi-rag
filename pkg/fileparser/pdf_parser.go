package fileparser

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/document"
	"github.com/ledongthuc/pdf"
)

// PDFParser PDF 文件解析器
// 使用 ledongthuc/pdf 库按页提取文本
type PDFParser struct{}

func NewPDFParser() *PDFParser { return &PDFParser{} }

func (p *PDFParser) Name() string { return "pdf" }

func (p *PDFParser) Parse(reader io.Reader, source string) (*document.Document, error) {
	doc := newDocument(extractTitle(source), source, document.DocTypePDF)

	// 先把 reader 读入内存（ledongthuc/pdf 需要 bytes.Reader）
	data, err := io.ReadAll(reader)
	if err != nil {
		return doc, fmt.Errorf("read pdf: %w", err)
	}

	// 创建 PDF Reader
	pdfReader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return doc, fmt.Errorf("parse pdf: %w", err)
	}

	totalPages := pdfReader.NumPage()
	doc.PageCount = totalPages
	doc.Metadata["page_count"] = totalPages

	// 逐页处理
	var allText strings.Builder
	emptyPages := 0

	for pageNum := 1; pageNum <= totalPages; pageNum++ {
		page := pdfReader.Page(pageNum)
		if page.V.IsNull() {
			// 空页
			doc.AddPage(&document.Page{
				PageNumber: pageNum,
			})
			emptyPages++
			continue
		}

		pageContent := extractPageText(page, pageNum)
		if pageContent == "" {
			// 尝试从 V 对象中提取（图片型 PDF）
			pageContent = extractFromV(page, pageNum)
			if pageContent == "" {
				emptyPages++
			}
		}

		allText.WriteString(pageContent)
		allText.WriteString("\n")

		// 创建 Page 对象
		dp := &document.Page{PageNumber: pageNum}

		// 解析页面内容中的元素（按空行分段落）
		elements := splitPDFPageIntoElements(pageContent, pageNum)
		for _, el := range elements {
			dp.AddElement(el)
			if el.Type() == document.ElemParagraph {
				doc.ParaCount++
			}
		}

		doc.AddPage(dp)
	}

	// 元数据
	doc.Metadata["empty_pages"] = emptyPages
	doc.Metadata["total_text_chars"] = allText.Len()

	if emptyPages > totalPages/2 {
		doc.Metadata["warning"] = "多数页面无可提取文本，可能是扫描件或图片型 PDF"
	}

	return doc, nil
}

// extractPageText 综合使用多种方法从单页提取文本
func extractPageText(page pdf.Page, _ int) string {
	var combined strings.Builder

	// 方法 1：GetPlainText（直接返回字符串，需要 font map）
	fonts := make(map[string]*pdf.Font)
	pt, err := page.GetPlainText(fonts)
	if err == nil {
		pt = strings.TrimSpace(pt)
		if pt != "" {
			combined.WriteString(pt)
		}
	}

	// 方法 2：GetTextByRow（保留行结构）
	rows, err := page.GetTextByRow()
	if err == nil && len(rows) > 0 {
		var rowBuf strings.Builder
		for _, row := range rows {
			var line strings.Builder
			for _, w := range row.Content {
				line.WriteString(w.S)
				line.WriteRune(' ')
			}
			tr := strings.TrimSpace(line.String())
			if tr != "" {
				rowBuf.WriteString(tr)
				rowBuf.WriteString("\n")
			}
		}
		rowText := strings.TrimSpace(rowBuf.String())
		// 如果 rowText 比 GetPlainText 更丰富，优先使用
		if len(rowText) > combined.Len()*2/3 {
			combined.Reset()
			combined.WriteString(rowText)
		}
	}

	return combined.String()
}

// extractFromV 当页对象可访问时尝试从 V 内提取内容（主要用于诊断）
func extractFromV(page pdf.Page, _ int) string {
	_ = page
	return ""
}

// splitPDFPageIntoElements 把一页 PDF 文本切成 Paragraph / Heading 元素
func splitPDFPageIntoElements(text string, pageNum int) []document.Element {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var result []document.Element

	// 按双换行或页内大间隔切分成段落
	rawParas := strings.Split(text, "\n\n")

	for _, rp := range rawParas {
		para := strings.TrimSpace(rp)
		if para == "" {
			continue
		}

		// 判断是否是标题：单行、长度适中、可能全大写/中文大字号
		lines := strings.Split(para, "\n")
		// 去掉每行首尾空白，但保留换行
		cleanedLines := make([]string, 0, len(lines))
		for _, l := range lines {
			t := strings.TrimSpace(l)
			if t != "" {
				cleanedLines = append(cleanedLines, t)
			}
		}
		if len(cleanedLines) == 0 {
			continue
		}

		content := strings.Join(cleanedLines, "\n")

		// 简单启发：如果单行、长度 < 80、不包含句号/问号/感叹号结尾，视为标题
		if len(cleanedLines) == 1 && len(content) < 80 &&
			!strings.HasSuffix(content, "。") &&
			!strings.HasSuffix(content, ".") &&
			!strings.HasSuffix(content, "!") &&
			!strings.HasSuffix(content, "?") &&
			!strings.HasSuffix(content, "！") &&
			!strings.HasSuffix(content, "？") {
			result = append(result, &document.Heading{
				Level:   3,
				Content: content,
				Position: document.Position{
					PageNumber: pageNum,
				},
			})
			continue
		}

		result = append(result, &document.Paragraph{
			Content: content,
			Position: document.Position{
				PageNumber: pageNum,
			},
		})
	}

	return result
}
