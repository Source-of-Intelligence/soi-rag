package fileparser

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/document"
)

// MarkdownParser Markdown 文档解析器
type MarkdownParser struct{}

func NewMarkdownParser() *MarkdownParser { return &MarkdownParser{} }

func (p *MarkdownParser) Name() string { return "markdown" }

var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
var fencedRe = regexp.MustCompile("^```(\\w+)?$")

func (p *MarkdownParser) Parse(reader io.Reader, source string) (*document.Document, error) {
	doc := newDocument(extractTitle(source), source, document.DocTypeMarkdown)

	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var sectionStack []*document.Section // 章节栈，用于处理嵌套
	var paraBuf strings.Builder
	inCodeBlock := false
	codeLang := ""
	codeBuf := strings.Builder{}

	// addEl 向当前激活的上下文添加元素
	addEl := func(el document.Element) {
		if len(sectionStack) > 0 {
			sectionStack[len(sectionStack)-1].AddElement(el)
		} else {
			doc.AddElement(el)
		}
	}

	addParagraph := func() {
		if paraBuf.Len() > 0 {
			para := strings.TrimSpace(paraBuf.String())
			if para != "" {
				addEl(&document.Paragraph{Content: para})
				doc.ParaCount++
			}
			paraBuf.Reset()
		}
	}

	for scanner.Scan() {
		line := scanner.Text()

		// --- 代码块 ---
		if m := fencedRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if inCodeBlock {
				// 结束代码块
				codeEl := &document.CodeBlock{
					Language: codeLang,
					Code:     strings.TrimSpace(codeBuf.String()),
				}
				addEl(codeEl)
				codeBuf.Reset()
				codeLang = ""
				inCodeBlock = false
			} else {
				// 开始代码块
				inCodeBlock = true
				codeLang = m[1]
			}
			continue
		}
		if inCodeBlock {
			codeBuf.WriteString(line)
			codeBuf.WriteRune('\n')
			continue
		}

		// --- 标题 ---
		if m := headingRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			addParagraph()
			level := len(m[1])
			title := strings.TrimSpace(m[2])

			// 文档第一个 H1 作为文档标题
			if doc.Title == extractTitle(source) && level == 1 &&
				len(doc.Sections) == 0 && len(doc.Elements) == 0 && paraBuf.Len() == 0 {
				doc.Title = title
				continue
			}

			sec := &document.Section{Level: level, Title: title}

			// 退栈到合适的父级别
			for len(sectionStack) > 0 && sectionStack[len(sectionStack)-1].Level >= level {
				sectionStack = sectionStack[:len(sectionStack)-1]
			}

			if len(sectionStack) == 0 {
				doc.AddSection(sec)
			} else {
				sectionStack[len(sectionStack)-1].AddSubSection(sec)
			}
			sectionStack = append(sectionStack, sec)
			continue
		}

		// --- 列表项（简化版，单行）---
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
			itemText := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* "), "+ ")
			listEl := &document.List{
				Ordered: false,
				Items:   []*document.ListItem{{Content: itemText}},
			}
			addEl(listEl)
			continue
		}

		// --- 水平线 ---
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			addEl(&document.Separator{Style: "horizontal"})
			continue
		}

		// 累积到段落缓冲
		if paraBuf.Len() > 0 {
			paraBuf.WriteRune(' ')
		}
		paraBuf.WriteString(strings.TrimSpace(line))
	}

	addParagraph()
	if err := scanner.Err(); err != nil {
		return doc, fmt.Errorf("read markdown: %w", err)
	}

	return doc, nil
}
