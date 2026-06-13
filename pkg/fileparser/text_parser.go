package fileparser

import (
	"bufio"
	"io"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/document"
)

// TextParser 纯文本文件解析器
type TextParser struct{}

func NewTextParser() *TextParser { return &TextParser{} }

func (p *TextParser) Name() string { return "text" }

func (p *TextParser) Parse(reader io.Reader, source string) (*document.Document, error) {
	doc := newDocument(extractTitle(source), source, document.DocTypeText)

	scanner := bufio.NewScanner(reader)
	// 扩大默认缓冲（64KB）
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var sb strings.Builder
	lineNum := 0
	var para strings.Builder

	for scanner.Scan() {
		lineNum++
		line := strings.TrimRight(scanner.Text(), "\r\n")

		// 空行作为段落分隔符
		if strings.TrimSpace(line) == "" {
			if para.Len() > 0 {
				paraStr := strings.TrimSpace(para.String())
				if paraStr != "" {
					doc.AddElement(&document.Paragraph{
						Content: paraStr,
						Position: document.Position{
							LineStart: lineNum - countLines(paraStr),
							LineEnd:   lineNum - 1,
						},
					})
					doc.ParaCount++
				}
				para.Reset()
			}
			continue
		}

		if para.Len() > 0 {
			para.WriteRune('\n')
		}
		para.WriteString(line)
	}

	// 最后一段
	if para.Len() > 0 {
		paraStr := strings.TrimSpace(para.String())
		if paraStr != "" {
			doc.AddElement(&document.Paragraph{
				Content: paraStr,
				Position: document.Position{
					LineStart: lineNum - countLines(paraStr) + 1,
					LineEnd:   lineNum,
				},
			})
			doc.ParaCount++
		}
	}

	if err := scanner.Err(); err != nil {
		sb.WriteString("[scanner error]")
	}

	return doc, nil
}

func countLines(s string) int {
	n := strings.Count(s, "\n")
	return n + 1
}
