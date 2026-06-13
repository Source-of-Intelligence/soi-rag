package fileparser

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/Source-of-Intelligence/soi-rag/pkg/document"
)

// CSVParser CSV 文件解析器
type CSVParser struct{}

func NewCSVParser() *CSVParser { return &CSVParser{} }

func (p *CSVParser) Name() string { return "csv" }

func (p *CSVParser) Parse(reader io.Reader, source string) (*document.Document, error) {
	doc := newDocument(extractTitle(source), source, document.DocTypeCSV)

	// 尝试不同的分隔符（先逗号，然后用 rune 检测）
	cr := csv.NewReader(reader)
	cr.Comma = ','
	cr.FieldsPerRecord = -1 // 允许每行字段数不同

	records, err := cr.ReadAll()
	if err != nil {
		// 用 tab 重试
		return nil, fmt.Errorf("parse csv: %w", err)
	}

	if len(records) == 0 {
		return doc, nil
	}

	// 构建表元素
	var headers []string
	var rows [][]string

	if len(records) > 1 {
		headers = records[0]
		rows = records[1:]
	} else {
		rows = records
	}

	table := &document.Table{
		Headers: headers,
		Rows:    rows,
	}
	doc.AddElement(table)
	doc.TableCount++

	doc.Metadata["row_count"] = len(rows)
	doc.Metadata["column_count"] = len(headers)

	return doc, nil
}
