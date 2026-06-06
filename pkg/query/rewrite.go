package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/llm"
)

// QueryRewriter 查询改写器接口
type QueryRewriter interface {
	Rewrite(ctx context.Context, query string) ([]string, error)
	Name() string
}

// SynonymRewriter 同义词扩展改写器
type SynonymRewriter struct {
	synonyms map[string][]string // 词 -> 同义词列表
}

// NewSynonymRewriter 创建同义词改写器
func NewSynonymRewriter() *SynonymRewriter {
	return &SynonymRewriter{
		synonyms: make(map[string][]string),
	}
}

// AddSynonym 添加同义词
func (r *SynonymRewriter) AddSynonym(word string, synonyms ...string) {
	r.synonyms[word] = append(r.synonyms[word], synonyms...)
}

// Rewrite 改写查询
func (r *SynonymRewriter) Rewrite(ctx context.Context, query string) ([]string, error) {
	queries := []string{query}

	// 对每个词检查是否有同义词
	words := strings.Fields(query)
	for _, word := range words {
		if syns, ok := r.synonyms[word]; ok {
			for _, syn := range syns {
				newQuery := strings.ReplaceAll(query, word, syn)
				queries = append(queries, newQuery)
			}
		}
	}

	return queries, nil
}

// Name 返回名称
func (r *SynonymRewriter) Name() string {
	return "synonym"
}

// HyDERewriter 假设性文档嵌入改写器
type HyDERewriter struct {
	llm    llm.LLM
	prompt string
}

// NewHyDERewriter 创建HyDE改写器
func NewHyDERewriter(l llm.LLM) *HyDERewriter {
	return &HyDERewriter{
		llm:    l,
		prompt: "请生成一个假设性的文档，该文档能够回答以下问题。只输出文档内容，不要输出其他解释：\n\n问题：%s",
	}
}

// Rewrite 改写查询（生成假设文档）
func (r *HyDERewriter) Rewrite(ctx context.Context, query string) ([]string, error) {
	if r.llm == nil {
		return []string{query}, nil
	}

	prompt := fmt.Sprintf(r.prompt, query)
	hypotheticalDoc, err := r.llm.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("生成假设文档失败: %w", err)
	}

	// 返回原查询和假设文档
	return []string{query, hypotheticalDoc}, nil
}

// Name 返回名称
func (r *HyDERewriter) Name() string {
	return "hyde"
}

// MultiQueryRewriter 多查询生成改写器
type MultiQueryRewriter struct {
	llm        llm.LLM
	prompt     string
	numQueries int
}

// NewMultiQueryRewriter 创建多查询改写器
func NewMultiQueryRewriter(l llm.LLM, numQueries int) *MultiQueryRewriter {
	if numQueries <= 0 {
		numQueries = 3
	}
	return &MultiQueryRewriter{
		llm:        l,
		numQueries: numQueries,
		prompt:     "请生成 %d 个与以下问题语义相关但表述不同的查询，用于信息检索。每行一个查询，不要编号：\n\n问题：%s",
	}
}

// Rewrite 改写查询（生成多个相关查询）
func (r *MultiQueryRewriter) Rewrite(ctx context.Context, query string) ([]string, error) {
	queries := []string{query}

	if r.llm == nil {
		return queries, nil
	}

	prompt := fmt.Sprintf(r.prompt, r.numQueries, query)
	response, err := r.llm.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("生成多查询失败: %w", err)
	}

	// 解析生成的查询
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && line != query {
			queries = append(queries, line)
		}
	}

	return queries, nil
}

// Name 返回名称
func (r *MultiQueryRewriter) Name() string {
	return "multi_query"
}

// CompositeRewriter 组合改写器
type CompositeRewriter struct {
	rewriters []QueryRewriter
}

// NewCompositeRewriter 创建组合改写器
func NewCompositeRewriter(rewriters ...QueryRewriter) *CompositeRewriter {
	return &CompositeRewriter{rewriters: rewriters}
}

// AddRewriter 添加改写器
func (r *CompositeRewriter) AddRewriter(rw QueryRewriter) {
	r.rewriters = append(r.rewriters, rw)
}

// Rewrite 改写查询（依次应用所有改写器）
func (r *CompositeRewriter) Rewrite(ctx context.Context, query string) ([]string, error) {
	queries := []string{query}
	var errs []string

	for _, rw := range r.rewriters {
		var newQueries []string
		for _, q := range queries {
			rewritten, err := rw.Rewrite(ctx, q)
			if err != nil {
				errs = append(errs, fmt.Sprintf("改写器 %s 错误: %v", rw.Name(), err))
				newQueries = append(newQueries, q) // 保留原始查询
				continue
			}
			newQueries = append(newQueries, rewritten...)
		}
		if len(newQueries) > 0 {
			queries = newQueries
		}
	}

	if len(errs) > 0 {
		return queries, fmt.Errorf("部分改写器失败: %s", strings.Join(errs, "; "))
	}

	return queries, nil
}

// Name 返回名称
func (r *CompositeRewriter) Name() string {
	return "composite"
}

// SetLLM 设置LLM（用于 HyDE 和 MultiQuery）
func (h *HyDERewriter) SetLLM(l llm.LLM) {
	h.llm = l
}

// SetLLM 设置LLM
func (m *MultiQueryRewriter) SetLLM(l llm.LLM) {
	m.llm = l
}

// SetNumQueries 设置生成的查询数量
func (m *MultiQueryRewriter) SetNumQueries(n int) {
	m.numQueries = n
}
