package fileparser

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Source-of-Intelligence/soi-rag/pkg/document"
)

// JSONParser JSON 文件解析器
type JSONParser struct{}

func NewJSONParser() *JSONParser { return &JSONParser{} }

func (p *JSONParser) Name() string { return "json" }

func (p *JSONParser) Parse(reader io.Reader, source string) (*document.Document, error) {
	doc := newDocument(extractTitle(source), source, document.DocTypeJSON)

	raw, err := io.ReadAll(reader)
	if err != nil {
		return doc, fmt.Errorf("read json: %w", err)
	}

	// 解析任意 JSON
	var data interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return doc, fmt.Errorf("parse json: %w", err)
	}

	// 以美化格式输出为 CodeBlock
	pretty, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		pretty = raw
	}

	doc.AddElement(&document.CodeBlock{
		Language: "json",
		Code:     string(pretty),
	})

	doc.Metadata["type"] = detectJSONType(data)
	doc.Metadata["bytes_size"] = len(raw)

	return doc, nil
}

func detectJSONType(v interface{}) string {
	switch v.(type) {
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}
