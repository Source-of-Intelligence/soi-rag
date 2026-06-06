package query

import (
	"context"
	"fmt"
	"testing"
)

// errorRewriter 用于测试的错误改写器
type errorRewriter struct {
	errMsg string
}

func (r *errorRewriter) Rewrite(ctx context.Context, query string) ([]string, error) {
	return nil, fmt.Errorf("%s: %s", r.errMsg, query)
}

func (r *errorRewriter) Name() string {
	return "error_rewriter"
}

// TestSynonymRewriter 测试同义词扩展
func TestSynonymRewriter(t *testing.T) {
	ctx := context.Background()

	// 创建同义词改写器并添加同义词
	// 注意：SynonymRewriter 使用 strings.Fields 分词，中文连续字符不会被拆分
	// 所以同义词必须是独立的词（被空格分隔）
	rewriter := NewSynonymRewriter()
	rewriter.AddSynonym("database", "DB", "database")
	rewriter.AddSynonym("search", "search engine", "query")

	// 测试基本改写（英文用空格分隔）
	queries, err := rewriter.Rewrite(ctx, "database search")
	if err != nil {
		t.Fatalf("改写失败: %v", err)
	}

	// 应包含原始查询
	found := false
	for _, q := range queries {
		if q == "database search" {
			found = true
			break
		}
	}
	if !found {
		t.Error("改写结果应包含原始查询 'database search'")
	}

	// 验证名称
	if rewriter.Name() != "synonym" {
		t.Errorf("Name() 应返回 'synonym', 实际=%s", rewriter.Name())
	}

	// 测试无同义词的查询
	queries, err = rewriter.Rewrite(ctx, "hello world")
	if err != nil {
		t.Fatalf("改写失败: %v", err)
	}
	if len(queries) != 1 {
		t.Errorf("无同义词时应只返回原始查询, 实际数量=%d", len(queries))
	}

	// 测试空查询
	queries, err = rewriter.Rewrite(ctx, "")
	if err != nil {
		t.Fatalf("空查询改写失败: %v", err)
	}
	if len(queries) != 1 || queries[0] != "" {
		t.Errorf("空查询应返回包含空字符串的切片, 实际=%v", queries)
	}

	// 测试多个同义词
	rewriter2 := NewSynonymRewriter()
	rewriter2.AddSynonym("AI", "artificial intelligence", "machine intelligence")

	queries, err = rewriter2.Rewrite(ctx, "AI technology")
	if err != nil {
		t.Fatalf("多同义词改写失败: %v", err)
	}
	// 原始 + 2个同义词 = 3
	if len(queries) != 3 {
		t.Errorf("多同义词改写应返回 3 个查询, 实际=%d", len(queries))
	}
}

// TestCompositeRewriter 测试组合改写器
func TestCompositeRewriter(t *testing.T) {
	ctx := context.Background()

	rw1 := NewSynonymRewriter()
	rw1.AddSynonym("database", "DB")

	rw2 := NewSynonymRewriter()
	rw2.AddSynonym("DB", "DataBase")

	composite := NewCompositeRewriter(rw1, rw2)

	queries, err := composite.Rewrite(ctx, "database system")
	if err != nil {
		t.Fatalf("组合改写失败: %v", err)
	}

	// 原始 "database system" -> rw1 扩展 "DB system" -> rw2 扩展 "DataBase system"
	if len(queries) < 2 {
		t.Errorf("组合改写应产生更多结果, 实际=%d", len(queries))
	}

	// 验证名称
	if composite.Name() != "composite" {
		t.Errorf("Name() 应返回 'composite', 实际=%s", composite.Name())
	}
}

// TestCompositeRewriterError 测试错误处理
func TestCompositeRewriterError(t *testing.T) {
	ctx := context.Background()

	// 创建一个会报错的改写器
	errRewriter := &errorRewriter{errMsg: "改写失败"}

	rw := NewSynonymRewriter()
	rw.AddSynonym("test", "testing")

	// 错误改写器在前
	composite := NewCompositeRewriter(errRewriter, rw)

	queries, err := composite.Rewrite(ctx, "test query")
	// 应返回错误（部分改写器失败）
	if err == nil {
		t.Error("组合改写器应返回部分失败错误")
	} else {
		t.Logf("正确返回错误: %v", err)
	}

	// 但仍应有结果（保留原始查询）
	if len(queries) == 0 {
		t.Error("即使部分改写器失败，也应返回结果")
	}
}

// TestCompositeRewriterNilContext 测试 nil context
func TestCompositeRewriterNilContext(t *testing.T) {
	rw := NewSynonymRewriter()
	rw.AddSynonym("hello", "hi")

	composite := NewCompositeRewriter(rw)
	queries, err := composite.Rewrite(nil, "hello world")
	if err != nil {
		t.Fatalf("nil context 不应导致错误: %v", err)
	}
	if len(queries) < 1 {
		t.Error("应返回至少一个查询")
	}
}

// TestRewriterInterface 测试接口实现
func TestRewriterInterface(t *testing.T) {
	var _ QueryRewriter = (*SynonymRewriter)(nil)
	var _ QueryRewriter = (*CompositeRewriter)(nil)
	var _ QueryRewriter = (*HyDERewriter)(nil)
	var _ QueryRewriter = (*MultiQueryRewriter)(nil)
}

// TestErrorRewriterBehavior 测试错误改写器行为
func TestErrorRewriterBehavior(t *testing.T) {
	ctx := context.Background()
	errRw := &errorRewriter{errMsg: "test error"}

	_, err := errRw.Rewrite(ctx, "test query")
	if err == nil {
		t.Error("errorRewriter 应返回错误")
	}
	if errRw.Name() != "error_rewriter" {
		t.Errorf("Name() 应返回 'error_rewriter', 实际=%s", errRw.Name())
	}
}
