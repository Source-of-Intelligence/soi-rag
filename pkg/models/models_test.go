package models

import (
	"testing"
)

func TestNewDocument(t *testing.T) {
	doc := NewDocument("Test Title", "Test Content", "test-source", DocTypeText)

	if doc.Title != "Test Title" {
		t.Errorf("Title 不匹配: %s", doc.Title)
	}
	if doc.Content != "Test Content" {
		t.Errorf("Content 不匹配: %s", doc.Content)
	}
	if doc.Source != "test-source" {
		t.Errorf("Source 不匹配: %s", doc.Source)
	}
	if doc.DocType != DocTypeText {
		t.Errorf("DocType 应为 text: %s", doc.DocType)
	}
	if doc.ID == "" {
		t.Error("ID 不应为空")
	}
	if doc.Status != DocStatusPending {
		t.Errorf("Status 应为 pending: %s", doc.Status)
	}
}

func TestDocument_IsValid(t *testing.T) {
	validDoc := NewDocument("Title", "Content", "source", DocTypeText)
	if !validDoc.IsValid() {
		t.Error("正常文档应 IsValid")
	}

	emptyContent := &Document{ID: "id", Content: ""}
	if emptyContent.IsValid() {
		t.Error("空内容文档应 IsValid=false")
	}

	emptyID := &Document{ID: "", Content: "content"}
	if emptyID.IsValid() {
		t.Error("空 ID 文档应 IsValid=false")
	}
}

func TestDocument_GetMetadataString(t *testing.T) {
	doc := NewDocument("Title", "Content", "source", DocTypeText)
	doc.Metadata["key1"] = "value1"
	doc.Metadata["key2"] = 123 // 非字符串

	if doc.GetMetadataString("key1") != "value1" {
		t.Errorf("key1 应为 value1，实际为 %s", doc.GetMetadataString("key1"))
	}
	if doc.GetMetadataString("key2") != "" {
		t.Errorf("key2 非字符串应返回空字符串")
	}
	if doc.GetMetadataString("nonexistent") != "" {
		t.Error("不存在的 key 应返回空字符串")
	}
}

func TestNewChunk(t *testing.T) {
	chunk := NewChunk("doc-123", 0, "chunk content here")

	if chunk.DocumentID != "doc-123" {
		t.Errorf("DocumentID 不匹配: %s", chunk.DocumentID)
	}
	if chunk.Content != "chunk content here" {
		t.Errorf("Content 不匹配: %s", chunk.Content)
	}
	if chunk.ID == "" {
		t.Error("ID 不应为空")
	}
}

func TestDefaultSearchOptions(t *testing.T) {
	opts := DefaultSearchOptions()
	if opts.TopK != 10 {
		t.Errorf("TopK 默认值应为 10，实际为 %d", opts.TopK)
	}
}

func TestDefaultHybridOptions(t *testing.T) {
	opts := DefaultHybridOptions()
	if opts.TopK != 10 {
		t.Errorf("TopK 默认值应为 10，实际为 %d", opts.TopK)
	}
	if opts.FusionMethod != FusionMethodRRF {
		t.Errorf("FusionMethod 默认值应为 RRF: %s", opts.FusionMethod)
	}
	if opts.RRFK != 60 {
		t.Errorf("RRFK 默认值应为 60，实际为 %d", opts.RRFK)
	}
}

func TestNewEntity(t *testing.T) {
	entity := NewEntity("测试实体", "PERSON")

	if entity.Name != "测试实体" {
		t.Errorf("Name 不匹配: %s", entity.Name)
	}
	if entity.Type != "PERSON" {
		t.Errorf("Type 应为 PERSON: %s", entity.Type)
	}
	if entity.ID == "" {
		t.Error("ID 不应为空")
	}
}

func TestNewRelation(t *testing.T) {
	rel := NewRelation("entity-1", "WORKS_FOR", "entity-2")

	if rel.SourceID != "entity-1" {
		t.Errorf("SourceID 不匹配: %s", rel.SourceID)
	}
	if rel.Type != "WORKS_FOR" {
		t.Errorf("Type 不匹配: %s", rel.Type)
	}
	if rel.TargetID != "entity-2" {
		t.Errorf("TargetID 不匹配: %s", rel.TargetID)
	}
}

func TestDocumentTypes(t *testing.T) {
	types := []DocumentType{
		DocTypePDF, DocTypeWord, DocTypeMarkdown,
		DocTypeHTML, DocTypeText, DocTypeCSV, DocTypeJSON,
	}

	for _, dt := range types {
		if string(dt) == "" {
			t.Error("文档类型常量不应为空")
		}
	}
}

func TestDocumentStatus(t *testing.T) {
	statuses := []DocumentStatus{
		DocStatusPending, DocStatusProcessing,
		DocStatusIndexed, DocStatusFailed, DocStatusDeleted,
	}

	for _, ds := range statuses {
		if string(ds) == "" {
			t.Error("文档状态常量不应为空")
		}
	}
}

func TestRetrievalTypes(t *testing.T) {
	types := []RetrievalType{
		RetrievalTypeVector, RetrievalTypeKeyword,
		RetrievalTypeGraph, RetrievalTypeHybrid,
	}

	for _, rt := range types {
		if string(rt) == "" {
			t.Error("检索类型常量不应为空")
		}
	}
}

func TestFusionMethods(t *testing.T) {
	methods := []FusionMethod{
		FusionMethodRRF, FusionMethodWeighted,
	}

	for _, fm := range methods {
		if string(fm) == "" {
			t.Error("融合方法常量不应为空")
		}
	}
}
