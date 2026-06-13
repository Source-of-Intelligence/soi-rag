package fileparser

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/document"
)

// HTMLParser HTML 文档解析器（基于简单的正则 + 有限的 XML 解析）
type HTMLParser struct{}

func NewHTMLParser() *HTMLParser { return &HTMLParser{} }

func (p *HTMLParser) Name() string { return "html" }

// 简单的 HTML 解析策略：先移除 script/style，再提取标题，最后把内容按 <p>/<h>/<table>/<li> 等标签拆分
func (p *HTMLParser) Parse(reader io.Reader, source string) (*document.Document, error) {
	doc := newDocument(extractTitle(source), source, document.DocTypeHTML)

	rawBytes, err := io.ReadAll(reader)
	if err != nil {
		return doc, fmt.Errorf("read html: %w", err)
	}
	htmlContent := string(rawBytes)

	// --- 1. 编码标准化（简单处理 BOM）---
	htmlContent = strings.TrimPrefix(htmlContent, "\uFEFF")

	// --- 2. 移除 <script> 与 <style> 块（包含内部内容）---
	htmlContent = removeBlockTag(htmlContent, "script")
	htmlContent = removeBlockTag(htmlContent, "style")
	htmlContent = removeBlockTag(htmlContent, "noscript")

	// --- 3. 提取 <title> ---
	titleRe := regexp.MustCompile(`(?i)<title>([\s\S]*?)</title>`)
	if m := titleRe.FindStringSubmatch(htmlContent); m != nil {
		doc.Title = strings.TrimSpace(stripTags(m[1]))
	}

	// --- 4. 移除换行 & 合并空白（先把 <br>/<hr> 转成换行）---
	htmlContent = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(htmlContent, "\n")
	htmlContent = regexp.MustCompile(`(?i)<hr\s*/?>`).ReplaceAllString(htmlContent, "\n---\n")

	// --- 5. 按块级标签切片并解析 ---
	blocks := splitByBlockTags(htmlContent)

	for _, b := range blocks {
		el := parseHTMLBlock(b)
		if el != nil && !el.IsEmpty() {
			doc.AddElement(el)
			switch el.Type() {
			case document.ElemParagraph, document.ElemHeading:
				doc.ParaCount++
			case document.ElemTable:
				doc.TableCount++
			case document.ElemImage:
				doc.ImageCount++
			}
		}
	}

	// 如果元素太少，回退到纯文本
	if len(doc.Elements) == 0 {
		plain := stripTags(htmlContent)
		plain = strings.TrimSpace(plain)
		if plain != "" {
			// 按空行拆成段落
			lines := strings.Split(plain, "\n")
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
	}

	return doc, nil
}

// removeBlockTag 移除 <tagName ...> 到 </tagName> 的整块（含中间内容）
func removeBlockTag(s, tagName string) string {
	re := regexp.MustCompile(`(?i)<` + tagName + `\b[^>]*>[\s\S]*?</` + tagName + `>`)
	return re.ReplaceAllString(s, "")
}

// stripTags 移除所有 HTML 标签，保留纯文本
func stripTags(s string) string {
	var sb strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
			sb.WriteRune(' ')
		default:
			if !inTag {
				sb.WriteRune(r)
			}
		}
	}
	// 清理多余空白
	result := sb.String()
	result = regexp.MustCompile(`[ \t]+`).ReplaceAllString(result, " ")
	return strings.TrimSpace(result)
}

// splitByBlockTags 把 HTML 文本按主要块级标签切成片段
func splitByBlockTags(s string) []string {
	blockRe := regexp.MustCompile(`(?i)<(h[1-6]|p|div|li|ul|ol|table|pre|blockquote|article|section)\b`)

	idxs := blockRe.FindAllStringIndex(s, -1)
	if len(idxs) == 0 {
		return []string{s}
	}

	var blocks []string
	for _, pair := range idxs {
		tagName := strings.ToLower(strings.TrimSpace(s[pair[0]+1 : pair[1]]))
		// 找到匹配的结束标签
		endRe := regexp.MustCompile(`(?i)</` + tagName + `>`)
		endMatch := endRe.FindStringIndex(s[pair[1]:])
		endIdx := pair[1]
		if endMatch != nil {
			endIdx = pair[1] + endMatch[1]
		} else {
			endIdx = len(s)
		}
		blocks = append(blocks, s[pair[0]:endIdx])
	}
	return blocks
}

// parseHTMLBlock 把一个块级标签解析为 Element
func parseHTMLBlock(block string) document.Element {
	block = strings.TrimSpace(block)
	if block == "" {
		return nil
	}

	// 确定标签名
	tagRe := regexp.MustCompile(`(?i)^<([a-zA-Z0-9]+)[^>]*>`)
	m := tagRe.FindStringSubmatch(block)
	if m == nil {
		// 纯文本
		t := stripTags(block)
		if t == "" {
			return nil
		}
		return &document.Paragraph{Content: t}
	}
	tagName := strings.ToLower(m[1])

	switch {
	case tagName == "h1" || tagName == "h2" || tagName == "h3" ||
		tagName == "h4" || tagName == "h5" || tagName == "h6":
		level := int(tagName[1] - '0')
		return &document.Heading{Level: level, Content: stripTags(block)}

	case tagName == "p" || tagName == "div" || tagName == "blockquote" ||
		tagName == "article" || tagName == "section":
		text := stripTags(block)
		if text == "" {
			return nil
		}
		return &document.Paragraph{Content: text}

	case tagName == "li":
		text := stripTags(block)
		if text == "" {
			return nil
		}
		return &document.List{
			Ordered: false,
			Items:   []*document.ListItem{{Content: text}},
		}

	case tagName == "ul" || tagName == "ol":
		items := extractListItems(block)
		if len(items) == 0 {
			return nil
		}
		listItems := make([]*document.ListItem, 0, len(items))
		for _, it := range items {
			listItems = append(listItems, &document.ListItem{Content: it})
		}
		return &document.List{
			Ordered: tagName == "ol",
			Items:   listItems,
		}

	case tagName == "pre":
		text := stripTags(block)
		if text == "" {
			return nil
		}
		return &document.CodeBlock{Code: text}

	case tagName == "table":
		return parseHTMLTable(block)

	default:
		text := stripTags(block)
		if text == "" {
			return nil
		}
		return &document.Paragraph{Content: text}
	}
}

// extractListItems 从 <ul>/<ol> 中提取所有 <li> 的文本
func extractListItems(block string) []string {
	re := regexp.MustCompile(`(?i)<li[^>]*>([\s\S]*?)</li>`)
	matches := re.FindAllStringSubmatch(block, -1)
	var items []string
	for _, m := range matches {
		t := strings.TrimSpace(stripTags(m[1]))
		if t != "" {
			items = append(items, t)
		}
	}
	return items
}

// parseHTMLTable 简易解析 HTML 表格
func parseHTMLTable(block string) *document.Table {
	trRe := regexp.MustCompile(`(?i)<tr[^>]*>([\s\S]*?)</tr>`)
	cellRe := regexp.MustCompile(`(?i)<(th|td)[^>]*>([\s\S]*?)</(th|td)>`)

	rows := trRe.FindAllStringSubmatch(block, -1)
	if len(rows) == 0 {
		return nil
	}

	table := &document.Table{}
	firstRow := true
	for _, tr := range rows {
		trContent := tr[1]
		cells := cellRe.FindAllStringSubmatch(trContent, -1)
		if len(cells) == 0 {
			continue
		}
		rowValues := make([]string, 0, len(cells))
		for _, c := range cells {
			cellTag := strings.ToLower(c[1])
			cellText := strings.TrimSpace(stripTags(c[2]))
			if firstRow && cellTag == "th" {
				table.Headers = append(table.Headers, cellText)
			} else {
				rowValues = append(rowValues, cellText)
			}
		}
		if firstRow {
			if len(table.Headers) > 0 {
				firstRow = false
				continue
			}
			table.Headers = rowValues
			firstRow = false
			continue
		}
		if len(rowValues) > 0 {
			table.Rows = append(table.Rows, rowValues)
		}
	}

	if len(table.Headers) == 0 && len(table.Rows) == 0 {
		return nil
	}
	return table
}

// decodeHTML 解码常见 HTML 实体（&amp; 等）
func decodeHTML(s string) string {
	type entity struct {
		src string
		dst string
	}
	entities := []entity{
		{"&nbsp;", " "},
		{"&amp;", "&"},
		{"&lt;", "<"},
		{"&gt;", ">"},
		{"&quot;", "\""},
		{"&#39;", "'"},
		{"&apos;", "'"},
		{"&ldquo;", "“"},
		{"&rdquo;", "”"},
		{"&hellip;", "…"},
	}
	for _, e := range entities {
		s = strings.ReplaceAll(s, e.src, e.dst)
		s = strings.ReplaceAll(s, strings.ToLower(e.src), e.dst)
	}
	return s
}
