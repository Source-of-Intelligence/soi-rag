package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/document"
	"github.com/Source-of-Intelligence/soi-rag/pkg/fileparser"
)

// ============================================================================
// 工具：扫描当前目录的文件
// ============================================================================

func scanDirectory(dir string) []string {
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Printf("扫描目录失败: %v\n", err)
		return files
	}
	supportedExts := map[string]bool{
		".pdf": true, ".docx": true, ".doc": true,
		".html": true, ".htm": true, ".md": true, ".markdown": true,
		".txt": true, ".csv": true, ".json": true,
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if supportedExts[ext] {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files
}

// ============================================================================
// 工具：漂亮打印文档结构
// ============================================================================

func printDocument(doc *document.Document, filePath string) {
	fmt.Println("================================================================================")
	fmt.Printf("  文件: %s\n", filePath)
	fmt.Printf("  类型: %s\n", doc.DocType)
	fmt.Printf("  标题: %s\n", doc.Title)
	fmt.Printf("  页数: %d\n", doc.PageCount)
	fmt.Printf("  段落数: %d\n", doc.ParaCount)
	fmt.Printf("  表格数: %d\n", doc.TableCount)
	fmt.Printf("  图片数: %d\n", doc.ImageCount)
	fmt.Printf("  总字符数: %d\n", len(doc.RawText()))

	if len(doc.Metadata) > 0 {
		fmt.Printf("  元数据:")
		for k, v := range doc.Metadata {
			fmt.Printf("    - %s = %v\n", k, v)
		}
	}
	fmt.Println("--------------------------------------------------------------------------------")

	// 结构化数据：Pages
	if len(doc.Pages) > 0 {
		fmt.Println("  ▣ 结构化内容 — 分页 (Pages):")
		for i, page := range doc.Pages {
			if i >= 10 {
				fmt.Printf("  ... (+%d 页省略)\n", len(doc.Pages)-10)
				break
			}
			fmt.Printf("  ├─ Page %d (%d 个元素, %d words)\n",
				page.PageNumber, len(page.Elements), page.WordCount)

			for j, el := range page.Elements {
				if j >= 6 {
					fmt.Printf("  │     ... (+%d elements)\n", len(page.Elements)-6)
					break
				}
				marker := " "
				if j == len(page.Elements)-1 {
					marker = "└"
				} else {
					marker = "├"
				}
				fmt.Printf("  │  %s [%s] %s\n",
					marker, el.Type(), truncateForDisplay(el.String(), 78))
			}
			fmt.Println("  │")
		}
		fmt.Println("--------------------------------------------------------------------------------")
	}

	// 结构化数据：Sections
	if len(doc.Sections) > 0 {
		fmt.Println("  ▣ 结构化内容 — 章节 (Sections):")
		for i, sec := range doc.Sections {
			if i >= 20 {
				fmt.Printf("  ... (+%d 章节省略)\n", len(doc.Sections)-20)
				break
			}
			printSection(sec, "  ├─", 0)
		}
		fmt.Println("--------------------------------------------------------------------------------")
	}

	// 顶层 Elements
	if len(doc.Elements) > 0 {
		fmt.Println("  ▣ 顶层元素:")
		for i, el := range doc.Elements {
			if i >= 30 {
				fmt.Printf("  ... (+%d 个元素省略)\n", len(doc.Elements)-30)
				break
			}
			fmt.Printf("  ├─ [%s] %s\n", el.Type(), truncateForDisplay(el.String(), 80))
		}
		fmt.Println("--------------------------------------------------------------------------------")
	}

	// 全文预览
	raw := doc.RawText()
	previewLimit := 600
	if len(raw) > previewLimit {
		raw = raw[:previewLimit] + "..."
	}
	fmt.Println("  ▣ 全文纯文本预览:")
	fmt.Println("  " + strings.Repeat("—", 76))
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		if len(l) > 0 {
			fmt.Println("  " + l)
		}
	}
	fmt.Println("  " + strings.Repeat("—", 76))

	fmt.Println("================================================================================")
	fmt.Println()
}

func printSection(sec *document.Section, prefix string, depth int) {
	if depth > 4 {
		return
	}
	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s %s[H%d] %s\n", indent, prefix, sec.Level,
		truncateForDisplay(sec.Title, 80))

	for i, el := range sec.Elements {
		if i >= 6 {
			fmt.Printf("%s      ... (+%d elements)\n", indent, len(sec.Elements)-6)
			break
		}
		fmt.Printf("%s      ├ [%s] %s\n", indent, el.Type(),
			truncateForDisplay(el.String(), 70))
	}

	for j, sub := range sec.SubSections {
		childPrefix := "├─"
		if j == len(sec.SubSections)-1 {
			childPrefix = "└─"
		}
		printSection(sub, childPrefix, depth+1)
	}
}

// truncateForDisplay 截断字符串用于显示
func truncateForDisplay(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	// 把换行换成 ↵ 以便单行显示
	s = strings.ReplaceAll(s, "\n", " ↵ ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")

	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// ============================================================================
// 主入口
// ============================================================================

func main() {
	fmt.Println("========== soi-rag 文件解析器 — 结构化数据诊断工具 ==========")
	fmt.Println()

	// 支持两种模式：
	//   1) go run . <file1> <file2> ...  — 解析指定文件
	//   2) go run .                       — 扫描当前目录
	args := os.Args[1:]

	var files []string
	if len(args) > 0 {
		for _, a := range args {
			if _, err := os.Stat(a); err == nil {
				files = append(files, a)
			} else {
				fmt.Printf("  ! 跳过不存在的文件: %s\n", a)
			}
		}
	} else {
		files = scanDirectory("./cmd/test")
	}

	if len(files) == 0 {
		fmt.Println("未找到可解析的文件。支持: .pdf / .docx / .doc / .html / .md / .txt / .csv / .json")
		fmt.Println("用法: go run . <文件路径>  或  直接 go run . 扫描当前目录")
		return
	}

	fmt.Printf("共发现 %d 个文件，开始解析...\n", len(files))
	fmt.Println()

	// 创建解析管理器
	pm := fileparser.NewManager()

	for i, f := range files {
		fmt.Printf("[%d/%d] ", i+1, len(files))
		doc, err := pm.ParseFromPath(f)
		if err != nil {
			fmt.Printf("解析失败: %v\n\n", err)
			continue
		}
		printDocument(doc, f)
	}

	// 如果只有一个文件，同时输出 JSON
	if len(files) == 1 {
		doc, err := pm.ParseFromPath(files[0])
		if err == nil {
			jsonPath := "parsed_output.json"
			jbytes, jerr := json.MarshalIndent(doc, "", "  ")
			if jerr == nil {
				if werr := os.WriteFile(jsonPath, jbytes, 0644); werr == nil {
					fmt.Printf("已写入 JSON 结构化数据到: %s\n", jsonPath)
				}
			}
		}
	}

	fmt.Println("========== 完成 ==========")
}
