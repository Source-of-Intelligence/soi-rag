package pageindex

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
)

// WordParser Word文档解析器
type WordParser struct{}

// NewWordParser 创建Word解析器
func NewWordParser() *WordParser {
	return &WordParser{}
}

// Parse 解析Word文档
func (p *WordParser) Parse(reader io.Reader, source string) (*models.Document, error) {
	// 读取全部内容到内存
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("读取Word文档失败: %w", err)
	}

	// 解析docx文件（docx是zip格式）
	content, metadata, err := parseDocx(data)
	if err != nil {
		return nil, fmt.Errorf("解析Word文档失败: %w", err)
	}

	// 提取标题
	title := ""
	if t, ok := metadata["title"].(string); ok && t != "" {
		title = t
	} else {
		title = extractTitleFromSource(source)
	}

	doc := &models.Document{
		Title:    title,
		Content:  content,
		Source:   source,
		DocType:  models.DocTypeWord,
		Metadata: metadata,
	}

	return doc, nil
}

// Supports 是否支持该类型
func (p *WordParser) Supports(docType models.DocumentType) bool {
	return docType == models.DocTypeWord
}

// SupportedExtensions 返回支持的文件扩展名
func (p *WordParser) SupportedExtensions() []string {
	return []string{".docx", ".doc"}
}

// parseDocx 解析docx文件内容
func parseDocx(data []byte) (string, map[string]interface{}, error) {
	metadata := make(map[string]interface{})

	// docx是zip格式，打开zip reader
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, fmt.Errorf("打开docx文件失败: %w", err)
	}

	var documentContent string
	var corePropsContent string

	// 遍历zip中的文件
	for _, file := range zipReader.File {
		switch file.Name {
		case "word/document.xml":
			content, err := readFileFromZip(file)
			if err != nil {
				return "", nil, fmt.Errorf("读取document.xml失败: %w", err)
			}
			documentContent = content
		case "docProps/core.xml":
			content, err := readFileFromZip(file)
			if err != nil {
				// 忽略元数据读取错误
				continue
			}
			corePropsContent = content
		}
	}

	if documentContent == "" {
		return "", nil, fmt.Errorf("docx文件中未找到document.xml")
	}

	// 解析文档内容
	text := extractTextFromDocumentXML(documentContent)

	// 解析元数据
	if corePropsContent != "" {
		parseCoreProperties(corePropsContent, metadata)
	}

	return text, metadata, nil
}

// readFileFromZip 从zip文件中读取内容
func readFileFromZip(file *zip.File) (string, error) {
	rc, err := file.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// documentXML 表示document.xml的结构
type documentXML struct {
	Body bodyXML `xml:"body"`
}

type bodyXML struct {
	Paragraphs []paragraphXML `xml:"p"`
}

type paragraphXML struct {
	Runs []runXML `xml:"r"`
}

type runXML struct {
	Text textXML `xml:"t"`
}

type textXML struct {
	Content string `xml:",chardata"`
}

// extractTextFromDocumentXML 从document.xml中提取文本
func extractTextFromDocumentXML(xmlContent string) string {
	var doc documentXML
	if err := xml.Unmarshal([]byte(xmlContent), &doc); err != nil {
		// 如果XML解析失败，使用简单的正则提取
		return extractTextSimple(xmlContent)
	}

	var paragraphs []string
	for _, p := range doc.Body.Paragraphs {
		var runs []string
		for _, r := range p.Runs {
			if r.Text.Content != "" {
				runs = append(runs, r.Text.Content)
			}
		}
		if len(runs) > 0 {
			paragraphs = append(paragraphs, strings.Join(runs, ""))
		}
	}

	return strings.Join(paragraphs, "\n")
}

// extractTextSimple 简单的文本提取（备用方案）
func extractTextSimple(xmlContent string) string {
	// 简单提取<w:t>标签中的内容
	var result strings.Builder
	inText := false
	inTag := false

	for i := 0; i < len(xmlContent); i++ {
		if i+4 <= len(xmlContent) && xmlContent[i:i+4] == "<w:t" {
			// 找到<w:t标签的开始
			// 跳过属性直到>
			for j := i + 4; j < len(xmlContent); j++ {
				if xmlContent[j] == '>' {
					i = j
					inText = true
					break
				}
			}
			continue
		}

		if inText && i+6 <= len(xmlContent) && xmlContent[i:i+6] == "</w:t>" {
			inText = false
			i += 5
			continue
		}

		if inText && xmlContent[i] == '<' {
			inTag = true
		}

		if inText && xmlContent[i] == '>' {
			inTag = false
			continue
		}

		if inText && !inTag {
			result.WriteByte(xmlContent[i])
		}
	}

	return result.String()
}

// corePropertiesXML 表示core.xml的结构
type corePropertiesXML struct {
	Title       string `xml:"title"`
	Creator     string `xml:"creator"`
	Subject     string `xml:"subject"`
	Description string `xml:"description"`
	Keywords    string `xml:"keywords"`
	Created     string `xml:"created"`
	Modified    string `xml:"modified"`
}

// parseCoreProperties 解析文档核心属性
func parseCoreProperties(xmlContent string, metadata map[string]interface{}) {
	var props corePropertiesXML
	// 使用命名空间前缀的版本
	var propsWithNS struct {
		Title       string `xml:"http://purl.org/dc/elements/1.1/ title"`
		Creator     string `xml:"http://purl.org/dc/elements/1.1/ creator"`
		Subject     string `xml:"http://purl.org/dc/elements/1.1/ subject"`
		Description string `xml:"http://purl.org/dc/elements/1.1/ description"`
		Keywords    string `xml:"http://purl.org/dc/elements/1.1/ keywords"`
		Created     string `xml:"http://purl.org/dc/terms/ created"`
		Modified    string `xml:"http://purl.org/dc/terms/ modified"`
	}

	// 尝试带命名空间解析
	if err := xml.Unmarshal([]byte(xmlContent), &propsWithNS); err == nil {
		if propsWithNS.Title != "" {
			metadata["title"] = propsWithNS.Title
		}
		if propsWithNS.Creator != "" {
			metadata["author"] = propsWithNS.Creator
		}
		if propsWithNS.Subject != "" {
			metadata["subject"] = propsWithNS.Subject
		}
		if propsWithNS.Description != "" {
			metadata["description"] = propsWithNS.Description
		}
		if propsWithNS.Keywords != "" {
			metadata["keywords"] = propsWithNS.Keywords
		}
		if propsWithNS.Created != "" {
			metadata["created"] = propsWithNS.Created
		}
		if propsWithNS.Modified != "" {
			metadata["modified"] = propsWithNS.Modified
		}
		return
	}

	// 尝试不带命名空间解析
	if err := xml.Unmarshal([]byte(xmlContent), &props); err == nil {
		if props.Title != "" {
			metadata["title"] = props.Title
		}
		if props.Creator != "" {
			metadata["author"] = props.Creator
		}
		if props.Subject != "" {
			metadata["subject"] = props.Subject
		}
		if props.Description != "" {
			metadata["description"] = props.Description
		}
		if props.Keywords != "" {
			metadata["keywords"] = props.Keywords
		}
		if props.Created != "" {
			metadata["created"] = props.Created
		}
		if props.Modified != "" {
			metadata["modified"] = props.Modified
		}
	}
}
