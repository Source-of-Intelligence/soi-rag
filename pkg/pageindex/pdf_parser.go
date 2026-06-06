package pageindex

import (
	"bytes"
	"fmt"
	"io"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/ledongthuc/pdf"
)

// PDFParser PDF文档解析器
type PDFParser struct{}

// NewPDFParser 创建PDF解析器
func NewPDFParser() *PDFParser {
	return &PDFParser{}
}

// Parse 解析PDF文档
func (p *PDFParser) Parse(reader io.Reader, source string) (*models.Document, error) {
	// 读取全部内容到内存（pdf库需要文件或Seekable reader）
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("读取PDF失败: %w", err)
	}

	// 使用pdf库解析
	// 由于pdf.Open需要文件路径，我们使用NewReader直接从字节创建
	pdfReader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("创建PDF解析器失败: %w", err)
	}

	// 提取标题（从文件名）
	title := extractTitleFromSource(source)

	// 方法1：使用GetPlainText获取全部文本
	textReader, err := pdfReader.GetPlainText()
	if err != nil {
		return nil, fmt.Errorf("提取PDF文本失败: %w", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(textReader)
	content := buf.String()

	if content == "" {
		// 方法2：如果GetPlainText失败，尝试逐页提取
		content = extractTextByPage(pdfReader)
	}

	if content == "" {
		return nil, fmt.Errorf("PDF文档内容为空或无法提取")
	}

	// 构建元数据
	metadata := make(map[string]interface{})
	metadata["page_count"] = pdfReader.NumPage()

	doc := &models.Document{
		Title:    title,
		Content:  content,
		Source:   source,
		DocType:  models.DocTypePDF,
		Metadata: metadata,
	}

	return doc, nil
}

// extractTextByPage 逐页提取文本（备用方案）
func extractTextByPage(pdfReader *pdf.Reader) string {
	var contentBuilder bytes.Buffer
	numPages := pdfReader.NumPage()

	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page := pdfReader.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		// 按行提取文本
		rows, err := page.GetTextByRow()
		if err != nil {
			continue
		}

		if contentBuilder.Len() > 0 {
			contentBuilder.WriteString("\n\n")
		}
		contentBuilder.WriteString(fmt.Sprintf("--- 第 %d 页 ---\n", pageNum))

		for _, row := range rows {
			for _, word := range row.Content {
				contentBuilder.WriteString(word.S)
				contentBuilder.WriteString(" ")
			}
			contentBuilder.WriteString("\n")
		}
	}

	return contentBuilder.String()
}

// Supports 是否支持该类型
func (p *PDFParser) Supports(docType models.DocumentType) bool {
	return docType == models.DocTypePDF
}

// SupportedExtensions 返回支持的文件扩展名
func (p *PDFParser) SupportedExtensions() []string {
	return []string{".pdf"}
}
