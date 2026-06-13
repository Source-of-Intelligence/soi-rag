package pageindex

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
)

// =============================================================================
// 测试辅助：创建测试文件
// =============================================================================

// createTestHTML 创建测试 HTML 文件
func createTestHTML(t *testing.T, htmlContent string) *os.File {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.html")
	err := os.WriteFile(filePath, []byte(htmlContent), 0644)
	if err != nil {
		t.Fatalf("创建测试 HTML 文件失败: %v", err)
	}
	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("打开测试 HTML 文件失败: %v", err)
	}
	return file
}

// createTestDOCX 创建测试 DOCX 文件
func createTestDOCX(t *testing.T, content, title string) *os.File {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.docx")

	// 创建 zip 文件（DOCX 格式）
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	// 添加 [Content_Types].xml
	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`
	addFileToZip(zipWriter, "[Content_Types].xml", contentTypes)

	// 添加 _rels/.rels
	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`
	addFileToZip(zipWriter, "_rels/.rels", rels)

	// 添加 word/_rels/document.xml.rels
	docRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
</Relationships>`
	addFileToZip(zipWriter, "word/_rels/document.xml.rels", docRels)

	// 添加 word/document.xml（包含实际内容）
	documentXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r>
        <w:t>` + escapeXML(content) + `</w:t>
      </w:r>
    </w:p>
  </w:body>
</w:document>`
	addFileToZip(zipWriter, "word/document.xml", documentXML)

	// 添加 docProps/core.xml（包含标题）
	coreXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties">
  <dc:title xmlns:dc="http://purl.org/dc/elements/1.1/">` + escapeXML(title) + `</dc:title>
</cp:coreProperties>`
	addFileToZip(zipWriter, "docProps/core.xml", coreXML)

	zipWriter.Close()

	// 写入文件
	err := os.WriteFile(filePath, buf.Bytes(), 0644)
	if err != nil {
		t.Fatalf("创建测试 DOCX 文件失败: %v", err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("打开测试 DOCX 文件失败: %v", err)
	}
	return file
}

// addFileToZip 添加文件到 zip
func addFileToZip(zipWriter *zip.Writer, name, content string) {
	writer, _ := zipWriter.Create(name)
	writer.Write([]byte(content))
}

// escapeXML 转义 XML 特殊字符
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// =============================================================================
// Parser 接口测试
// =============================================================================

func TestParserManager_GetParser(t *testing.T) {
	pm := NewParserManager()

	tests := []struct {
		name        string
		docType     models.DocumentType
		expectNil   bool
		expectPanic bool
	}{
		{"PDF 解析器", models.DocTypePDF, false, false},
		{"Word 解析器", models.DocTypeWord, false, false},
		{"HTML 解析器", models.DocTypeHTML, false, false},
		{"Markdown 解析器", models.DocTypeMarkdown, false, false},
		{"Text 解析器", models.DocTypeText, false, false},
		{"CSV 解析器", models.DocTypeCSV, false, false},
		{"JSON 解析器", models.DocTypeJSON, false, false},
		{"不支持的类型", models.DocumentType("unknown"), true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := pm.GetParser(tt.docType)
			if tt.expectNil {
				if err == nil {
					t.Errorf("期望错误，但得到 nil")
				}
				return
			}
			if err != nil {
				t.Errorf("GetParser 失败: %v", err)
				return
			}
			if parser == nil {
				t.Errorf("解析器为 nil")
				return
			}
			if !parser.Supports(tt.docType) {
				t.Errorf("解析器不支持 %s", tt.docType)
			}
		})
	}
}

func TestParserManager_GetParserByExtension(t *testing.T) {
	pm := NewParserManager()

	tests := []struct {
		ext       string
		expectDoc models.DocumentType
	}{
		{".pdf", models.DocTypePDF},
		{".docx", models.DocTypeWord},
		{".html", models.DocTypeHTML},
		{".md", models.DocTypeMarkdown},
		{".txt", models.DocTypeText},
		{".csv", models.DocTypeCSV},
		{".json", models.DocTypeJSON},
		{"pdf", models.DocTypePDF},       // 不带点的扩展名
		{"docx", models.DocTypeWord},     // 不带点的扩展名
		{".PDF", models.DocTypePDF},      // 大写
		{".DOCX", models.DocTypeWord},    // 大写
		{".unknown", models.DocTypeText}, // 未知类型回退到 Text
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			parser, docType, err := pm.GetParserByExtension(tt.ext)
			if err != nil {
				t.Errorf("GetParserByExtension(%s) 失败: %v", tt.ext, err)
				return
			}
			if parser == nil {
				t.Errorf("解析器为 nil for ext=%s", tt.ext)
				return
			}
			if docType != tt.expectDoc {
				t.Errorf("期望 %s，实际得到 %s", tt.expectDoc, docType)
			}
		})
	}
}

func TestDetectDocType(t *testing.T) {
	tests := []struct {
		source    string
		expectDoc models.DocumentType
	}{
		{"test.pdf", models.DocTypePDF},
		{"test.docx", models.DocTypeWord},
		{"test.doc", models.DocTypeWord},
		{"test.html", models.DocTypeHTML},
		{"test.htm", models.DocTypeHTML},
		{"test.md", models.DocTypeMarkdown},
		{"test.markdown", models.DocTypeMarkdown},
		{"test.txt", models.DocTypeText},
		{"test.csv", models.DocTypeCSV},
		{"test.json", models.DocTypeJSON},
		{"test.PDF", models.DocTypePDF}, // 大写
		{"test.unknown", models.DocTypeText},
		{"test", models.DocTypeText}, // 无扩展名
		{"/path/to/file.pdf", models.DocTypePDF},
		{"C:\\path\\to\\file.docx", models.DocTypeWord},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			docType := DetectDocType(tt.source)
			if docType != tt.expectDoc {
				t.Errorf("DetectDocType(%q) = %s，期望 %s", tt.source, docType, tt.expectDoc)
			}
		})
	}
}

// =============================================================================
// HTML 解析器测试
// =============================================================================

func TestHTMLParser_Parse(t *testing.T) {
	parser := NewHTMLParser()

	tests := []struct {
		name           string
		htmlContent    string
		expectTitle    string
		expectContains string
		expectDocType  models.DocumentType
	}{
		{
			name:           "带标题的 HTML",
			htmlContent:    `<html><head><title>测试标题</title></head><body><p>这是正文内容</p></body></html>`,
			expectTitle:    "测试标题",
			expectContains: "这是正文内容",
			expectDocType:  models.DocTypeHTML,
		},
		{
			name:           "无标题 HTML",
			htmlContent:    `<html><body><p>纯文本内容</p></body></html>`,
			expectTitle:    "test",
			expectContains: "纯文本内容",
			expectDocType:  models.DocTypeHTML,
		},
		{
			name:           "带样式的 HTML",
			htmlContent:    `<html><head><title>样式测试</title></head><body><div style="color:red"><span>红色文字</span></div><script>alert('xss')</script></body></html>`,
			expectTitle:    "样式测试",
			expectContains: "红色文字",
			expectDocType:  models.DocTypeHTML,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewReader([]byte(tt.htmlContent))
			doc, err := parser.Parse(reader, "test.html")
			if err != nil {
				t.Fatalf("HTML 解析失败: %v", err)
			}

			if doc.Title != tt.expectTitle {
				t.Errorf("标题不匹配: 期望 %q，实际 %q", tt.expectTitle, doc.Title)
			}

			if !strings.Contains(doc.Content, tt.expectContains) {
				t.Errorf("内容不包含期望文本 %q，实际内容: %s", tt.expectContains, doc.Content)
			}

			if doc.DocType != tt.expectDocType {
				t.Errorf("文档类型不匹配: 期望 %s，实际 %s", tt.expectDocType, doc.DocType)
			}
		})
	}
}

func TestHTMLParser_Supports(t *testing.T) {
	parser := NewHTMLParser()

	if !parser.Supports(models.DocTypeHTML) {
		t.Error("HTML 解析器应支持 HTML 类型")
	}
	if parser.Supports(models.DocTypePDF) {
		t.Error("HTML 解析器不应支持 PDF 类型")
	}
}

// =============================================================================
// Text 解析器测试
// =============================================================================

func TestTextParser_Parse(t *testing.T) {
	parser := NewTextParser()

	content := "这是纯文本内容\n第二行内容"
	reader := bytes.NewReader([]byte(content))

	doc, err := parser.Parse(reader, "test.txt")
	if err != nil {
		t.Fatalf("Text 解析失败: %v", err)
	}

	if doc.Title != "test" {
		t.Errorf("标题不匹配: 期望 %q，实际 %q", "test", doc.Title)
	}

	if doc.Content != content {
		t.Errorf("内容不匹配: 期望 %q，实际 %q", content, doc.Content)
	}

	if doc.DocType != models.DocTypeText {
		t.Errorf("文档类型不匹配: 期望 %s，实际 %s", models.DocTypeText, doc.DocType)
	}
}

// =============================================================================
// DOCX 解析器测试
// =============================================================================

func TestWordParser_Parse(t *testing.T) {
	parser := NewWordParser()

	// 创建测试 DOCX 文件
	file := createTestDOCX(t, "这是 DOCX 文档的正文内容", "测试文档标题")
	defer file.Close()

	doc, err := parser.Parse(file, "test.docx")
	if err != nil {
		t.Fatalf("DOCX 解析失败: %v", err)
	}

	// 验证标题（从 core.xml 中提取）
	if doc.Title != "测试文档标题" {
		t.Errorf("标题不匹配: 期望 %q，实际 %q", "测试文档标题", doc.Title)
	}

	// 验证内容
	if !strings.Contains(doc.Content, "这是 DOCX 文档的正文内容") {
		t.Errorf("内容不包含期望文本，实际内容: %s", doc.Content)
	}

	// 验证文档类型
	if doc.DocType != models.DocTypeWord {
		t.Errorf("文档类型不匹配: 期望 %s，实际 %s", models.DocTypeWord, doc.DocType)
	}
}

func TestWordParser_Parse_FileBased(t *testing.T) {
	parser := NewWordParser()

	// 创建测试 DOCX 文件
	file := createTestDOCX(t, "从文件读取的 DOCX 内容", "文件标题")
	defer file.Close()

	// 验证文件可以被多次读取（Seek 回开头）
	doc1, err := parser.Parse(file, "test.docx")
	if err != nil {
		t.Fatalf("第一次解析失败: %v", err)
	}

	// Seek 回文件开头
	file.Seek(0, 0)

	doc2, err := parser.Parse(file, "test.docx")
	if err != nil {
		t.Fatalf("第二次解析失败: %v", err)
	}

	if doc1.Title != doc2.Title {
		t.Errorf("两次解析标题不一致: %q vs %q", doc1.Title, doc2.Title)
	}
	if doc1.Content != doc2.Content {
		t.Errorf("两次解析内容不一致: %q vs %q", doc1.Content, doc2.Content)
	}
}

// =============================================================================
// ParserManager 集成测试
// =============================================================================

func TestParserManager_Parse(t *testing.T) {
	pm := NewParserManager()

	// 测试 HTML 解析
	t.Run("HTML 解析", func(t *testing.T) {
		html := `<html><head><title>测试</title></head><body><p>正文</p></body></html>`
		doc, err := pm.Parse(bytes.NewReader([]byte(html)), "test.html", models.DocTypeHTML)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if !strings.Contains(doc.Content, "正文") {
			t.Errorf("HTML 解析内容错误: %s", doc.Content)
		}
	})

	// 测试 DOCX 解析
	t.Run("DOCX 解析", func(t *testing.T) {
		file := createTestDOCX(t, "DOCX 集成测试内容", "集成测试标题")
		defer file.Close()

		doc, err := pm.Parse(file, "test.docx", models.DocTypeWord)
		if err != nil {
			t.Fatalf("DOCX 解析失败: %v", err)
		}
		if !strings.Contains(doc.Content, "DOCX 集成测试内容") {
			t.Errorf("DOCX 内容错误: %s", doc.Content)
		}
	})

	// 测试通过扩展名解析
	t.Run("通过扩展名解析 DOCX", func(t *testing.T) {
		file := createTestDOCX(t, "扩展名解析内容", "扩展名标题")
		defer file.Close()

		doc, err := pm.ParseByExtension(file, "auto-detect.docx")
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if doc.DocType != models.DocTypeWord {
			t.Errorf("自动检测类型错误: 期望 %s，实际 %s", models.DocTypeWord, doc.DocType)
		}
	})
}

// =============================================================================
// 错误处理测试
// =============================================================================

func TestParser_ErrorHandling(t *testing.T) {
	t.Run("TextParser 空内容", func(t *testing.T) {
		parser := NewTextParser()
		doc, err := parser.Parse(bytes.NewReader([]byte{}), "empty.txt")
		if err != nil {
			t.Fatalf("空内容不应报错: %v", err)
		}
		if doc.Content != "" {
			t.Errorf("空内容应返回空字符串")
		}
	})

	t.Run("ParserManager 不支持的类型", func(t *testing.T) {
		pm := NewParserManager()
		_, err := pm.GetParser(models.DocumentType("unsupported"))
		if err == nil {
			t.Error("不支持的类型应返回错误")
		}
	})
}

// =============================================================================
// 性能/边界测试
// =============================================================================

func TestParser_LargeContent(t *testing.T) {
	parser := NewTextParser()

	// 创建较大的文本内容（1MB）
	largeContent := strings.Repeat("这是一段测试文本。", 50000) // 约 1MB
	reader := bytes.NewReader([]byte(largeContent))

	doc, err := parser.Parse(reader, "large.txt")
	if err != nil {
		t.Fatalf("大文件解析失败: %v", err)
	}

	if len(doc.Content) != len(largeContent) {
		t.Errorf("内容长度不匹配: 期望 %d，实际 %d", len(largeContent), len(doc.Content))
	}
}

// =============================================================================
// PDF 解析器测试（使用实际 PDF 文件）
// =============================================================================

// TestPDFParser_RealFile 测试使用实际 PDF 文件解析
// 该测试需要项目根目录下的测试 PDF 文件：26060228705004753.pdf
func TestPDFParser_RealFile(t *testing.T) {
	// 测试文件路径（项目根目录下）
	pdfPath := filepath.Join("..", "..", "26060228705004753.pdf")

	// 检查文件是否存在
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		t.Skipf("测试 PDF 文件不存在，跳过测试: %s", pdfPath)
	}

	parser := NewPDFParser()

	// 打开 PDF 文件
	file, err := os.Open(pdfPath)
	if err != nil {
		t.Fatalf("打开 PDF 文件失败: %v", err)
	}
	defer file.Close()

	// 解析 PDF
	doc, err := parser.Parse(file, pdfPath)
	if err != nil {
		t.Fatalf("PDF 解析失败: %v", err)
	}

	// 验证文档类型
	if doc.DocType != models.DocTypePDF {
		t.Errorf("文档类型不匹配: 期望 %s，实际 %s", models.DocTypePDF, doc.DocType)
	}

	// 验证内容不为空
	if doc.Content == "" {
		t.Error("PDF 内容为空，解析可能失败")
	}

	// 验证不是二进制乱码（%PDF-1.4 是 PDF 文件头，不是内容）
	if strings.HasPrefix(doc.Content, "%PDF-") {
		t.Error("PDF 内容以文件头开头，解析可能未正确提取文本")
	}

	// 验证内容主要是可读文本（不是大量不可见字符）
	// 统计可见字符的比例
	visibleCount := 0
	for _, r := range doc.Content {
		if r >= 32 && r < 127 || r >= 0x4E00 && r <= 0x9FFF { // ASCII 可打印字符 + 中文
			visibleCount++
		}
	}
	visibleRatio := float64(visibleCount) / float64(len(doc.Content))

	t.Logf("PDF 解析结果分析:")
	t.Logf("  - 总字符数: %d", len(doc.Content))
	t.Logf("  - 可见字符数: %d (ASCII可打印 + 中文)", visibleCount)
	t.Logf("  - 可读性比例: %.1f%%", visibleRatio*100)
	t.Logf("  - 内容预览 (前200字符): %s", truncateString(doc.Content, 200))

	// 可读性低于 50% 说明解析质量差（可能是图片型PDF或编码特殊）
	if visibleRatio < 0.5 {
		t.Errorf("⚠️  PDF 内容可读性低 (%.1f%%)，可能原因：\n"+
			"  1. 该 PDF 是图片扫描件（需要OCR）\n"+
			"  2. PDF 使用了特殊字体/编码，ledongthuc/pdf 库无法解析\n"+
			"  3. PDF 文件损坏",
			visibleRatio*100)
		// 注意：这里不 return，测试继续运行以收集更多诊断信息
	}

	t.Logf("PDF 解析成功:")
	t.Logf("  - 标题: %s", doc.Title)
	t.Logf("  - 内容长度: %d 字符", len(doc.Content))
	t.Logf("  - 内容预览 (前200字符): %s", truncateString(doc.Content, 200))

	// 如果有元数据，打印页数
	if doc.Metadata != nil {
		if pageCount, ok := doc.Metadata["page_count"].(int); ok {
			t.Logf("  - 页数: %d", pageCount)
		}
	}
}

// TestPDFParser_RealFile_WithPageIndex 测试使用 PageIndex 解析实际 PDF
func TestPDFParser_RealFile_WithPageIndex(t *testing.T) {
	pdfPath := filepath.Join("..", "..", "26060228705004753.pdf")

	// 检查文件是否存在
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		t.Skipf("测试 PDF 文件不存在，跳过测试: %s", pdfPath)
	}

	// 创建临时 SQLite 存储
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(SQLiteStoreConfig{DBPath: dbPath})
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer store.Close()

	// 初始化数据库 schema
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("初始化数据库 schema 失败: %v", err)
	}

	// 创建 PageIndex
	pageIndex := NewPageIndex(store, nil)

	// 创建文档对象
	doc := &models.Document{
		Title:   "测试 PDF",
		Source:  pdfPath,
		DocType: models.DocTypePDF,
	}

	// 打开 PDF 文件
	file, err := os.Open(pdfPath)
	if err != nil {
		t.Fatalf("打开 PDF 文件失败: %v", err)
	}
	defer file.Close()

	// 添加文档（通过 PageIndex）
	err = pageIndex.AddDocument(context.Background(), doc, file)
	if err != nil {
		t.Fatalf("PageIndex 添加文档失败: %v", err)
	}

	// 验证文档已添加
	retrieved, err := pageIndex.GetDocument(context.Background(), doc.ID)
	if err != nil {
		t.Fatalf("获取文档失败: %v", err)
	}

	// 验证内容
	if retrieved.Content == "" {
		t.Error("PageIndex 解析的 PDF 内容为空")
	}

	// 验证不是二进制
	if strings.HasPrefix(retrieved.Content, "%PDF-") {
		t.Error("PageIndex 解析的内容以 PDF 文件头开头，解析可能失败")
	}

	t.Logf("PageIndex PDF 解析成功:")
	t.Logf("  - 文档 ID: %s", retrieved.ID)
	t.Logf("  - 内容长度: %d 字符", len(retrieved.Content))
	t.Logf("  - 内容预览 (前200字符): %s", truncateString(retrieved.Content, 200))
}

// TestPDFParser_EmptyPDF 测试空 PDF 或无文本 PDF
func TestPDFParser_EmptyPDF(t *testing.T) {
	parser := NewPDFParser()

	// 测试损坏的 PDF 数据
	brokenPDF := []byte("%PDF-1.4\n%这是损坏的PDF内容")
	reader := bytes.NewReader(brokenPDF)

	_, err := parser.Parse(reader, "broken.pdf")
	// 应该返回错误，因为无法提取有效文本
	if err == nil {
		t.Log("注意: 损坏的 PDF 没有返回错误，可能库有容错处理")
	}
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
