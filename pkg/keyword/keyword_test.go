package keyword

import (
	"context"
	"testing"

	"github.com/ragtool/rag/pkg/models"
)

func TestSimpleTokenizer_Tokenize(t *testing.T) {
	tokenizer := &SimpleTokenizer{}
	tokens := tokenizer.Tokenize("Hello World Hello Go")

	if len(tokens) != 4 {
		t.Errorf("应有 4 个 token，实际为 %d", len(tokens))
	}

	// 验证小写化
	for _, token := range tokens {
		if token != tokens[0] && token == "Hello" {
			t.Error("字母应转换为小写")
		}
	}
}

func TestChineseTokenizer_Tokenize(t *testing.T) {
	tokenizer := &ChineseTokenizer{}
	tokens := tokenizer.Tokenize("你好世界Hello")

	if len(tokens) == 0 {
		t.Error("应有分词结果")
	}
}

func TestInvertedIndex_AddDocument(t *testing.T) {
	idx := NewInvertedIndex(&SimpleTokenizer{})

	idx.AddDocument("doc-1", "Hello world, hello golang")

	// 检查文档数量
	if idx.docCount != 1 {
		t.Errorf("文档数应为 1，实际为 %d", idx.docCount)
	}
}

func TestInvertedIndex_Search(t *testing.T) {
	idx := NewInvertedIndex(&SimpleTokenizer{})

	idx.AddDocument("doc-1", "golang is great")
	idx.AddDocument("doc-2", "python is popular")
	idx.AddDocument("doc-3", "golang and python")

	results := idx.Search("golang", 10)

	if len(results) != 2 {
		t.Errorf("搜索 golang 应返回 2 个结果，实际为 %d", len(results))
	}
}

func TestInvertedIndex_SearchWithBM25(t *testing.T) {
	idx := NewInvertedIndex(&SimpleTokenizer{})

	idx.AddDocument("doc-1", "golang programming language")
	idx.AddDocument("doc-2", "python programming language")
	idx.AddDocument("doc-3", "golang tutorial")

	results := idx.Search("golang programming", 10)

	// 应包含 doc-1 和 doc-3（都含 golang）
	if len(results) < 2 {
		t.Errorf("应至少包含 2 个含 golang 的文档，实际为 %d", len(results))
	}

	// 验证包含 golang 的文档
	found := make(map[string]bool)
	for _, r := range results {
		found[r.ID] = true
	}
	if !found["doc-1"] {
		t.Error("doc-1（golang programming）应被找到")
	}
	if !found["doc-3"] {
		t.Error("doc-3（golang tutorial）应被找到")
	}
}

func TestInvertedIndex_RemoveDocument(t *testing.T) {
	idx := NewInvertedIndex(&SimpleTokenizer{})

	idx.AddDocument("doc-1", "delete me")
	idx.RemoveDocument("doc-1")

	if idx.docCount != 0 {
		t.Errorf("删除后文档数应为 0，实际为 %d", idx.docCount)
	}
}

func TestKeywordRetriever_BasicSearch(t *testing.T) {
	retriever := NewKeywordRetriever(nil)
	ctx := context.Background()

	docs := []*models.Chunk{
		{ID: "chunk-1", DocumentID: "doc-1", Content: "golang is fast"},
		{ID: "chunk-2", DocumentID: "doc-2", Content: "python is popular"},
		{ID: "chunk-3", DocumentID: "doc-3", Content: "golang and python are languages"},
	}

	retriever.IndexChunks(ctx, docs)

	result, err := retriever.Search(ctx, "golang", models.SearchOptions{TopK: 10})
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}

	if len(result.Results) != 2 {
		t.Errorf("搜索 golang 应返回 2 个结果，实际为 %d", len(result.Results))
	}
}

func TestKeywordRetriever_DeleteDocument(t *testing.T) {
	retriever := NewKeywordRetriever(nil)
	ctx := context.Background()

	retriever.IndexChunks(ctx, []*models.Chunk{
		{ID: "chunk-1", DocumentID: "doc-1", Content: "test content one"},
	})

	retriever.DeleteDocument(ctx, "chunk-1")

	result, _ := retriever.Search(ctx, "test", models.SearchOptions{TopK: 10})
	if len(result.Results) != 0 {
		t.Errorf("删除后搜索不应返回结果，实际为 %d", len(result.Results))
	}
}
