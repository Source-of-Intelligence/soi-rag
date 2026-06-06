package hybrid

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
)

// FusionStrategy 融合策略接口
type FusionStrategy interface {
	Fuse(results [][]*models.RetrievalResult, topK int) []*models.RetrievalResult
}

// RRFFusion RRF融合策略
type RRFFusion struct {
	K int // RRF常数
}

// NewRRFFusion 创建RRF融合策略
func NewRRFFusion(k int) *RRFFusion {
	if k <= 0 {
		k = 60
	}
	return &RRFFusion{K: k}
}

// Fuse 融合结果
func (f *RRFFusion) Fuse(results [][]*models.RetrievalResult, topK int) []*models.RetrievalResult {
	// 收集所有文档的RRF分数
	rrfScores := make(map[string]float64)
	docInfo := make(map[string]*models.RetrievalResult)

	for _, resultList := range results {
		for rank, result := range resultList {
			// RRF公式: 1 / (k + rank)
			rrfScores[result.ID] += 1.0 / float64(f.K+rank+1)

			// 保存文档信息（使用最高分数的版本）
			if existing, ok := docInfo[result.ID]; !ok || result.Score > existing.Score {
				docInfo[result.ID] = result
			}
		}
	}

	// 排序
	type scoredDoc struct {
		id    string
		score float64
	}

	var scoredDocs []scoredDoc
	for id, score := range rrfScores {
		scoredDocs = append(scoredDocs, scoredDoc{id: id, score: score})
	}

	sort.Slice(scoredDocs, func(i, j int) bool {
		return scoredDocs[i].score > scoredDocs[j].score
	})

	// 限制结果数
	if topK > 0 && len(scoredDocs) > topK {
		scoredDocs = scoredDocs[:topK]
	}

	// 构建结果
	var fusedResults []*models.RetrievalResult
	for _, sd := range scoredDocs {
		result := docInfo[sd.id]
		result.Score = sd.score
		fusedResults = append(fusedResults, result)
	}

	return fusedResults
}

// WeightedFusion 加权融合策略
// Weights 的 key 应与检索策略名称对应: "vector", "keyword", "graph"
type WeightedFusion struct {
	Weights map[string]float64 // 各检索器的权重，key 为策略名称（vector/keyword/graph）
}

// NewWeightedFusion 创建加权融合策略
func NewWeightedFusion(weights map[string]float64) *WeightedFusion {
	return &WeightedFusion{Weights: weights}
}

// WeightedFuseResult 加权融合的输入，包含策略名称与对应结果
type WeightedFuseResult struct {
	Strategy string                    // 策略名称: "vector", "keyword", "graph"
	Results  []*models.RetrievalResult // 该策略的检索结果
}

// Fuse 融合结果（使用策略名称映射权重）
func (f *WeightedFusion) Fuse(results [][]*models.RetrievalResult, topK int) []*models.RetrievalResult {
	// 归一化权重
	totalWeight := 0.0
	for _, w := range f.Weights {
		totalWeight += w
	}
	if totalWeight == 0 {
		totalWeight = 1
	}

	// 收集所有文档的加权分数
	weightedScores := make(map[string]float64)
	docInfo := make(map[string]*models.RetrievalResult)

	// 默认权重：当未配置某策略时使用均等权重
	defaultWeight := 1.0 / float64(len(results))

	for i, resultList := range results {
		// 按索引查找权重（保持向后兼容）
		weight := defaultWeight
		if len(f.Weights) > 0 {
			// 尝试按策略名称查找: vector->0, keyword->1, graph->2
			strategyNames := []string{"vector", "keyword", "graph"}
			if i < len(strategyNames) {
				if w, ok := f.Weights[strategyNames[i]]; ok {
					weight = w / totalWeight
				}
			}
		}

		for _, result := range resultList {
			weightedScores[result.ID] += result.Score * weight

			if existing, ok := docInfo[result.ID]; !ok || result.Score > existing.Score {
				docInfo[result.ID] = result
			}
		}
	}

	// 排序
	type scoredDoc struct {
		id    string
		score float64
	}

	var scoredDocs []scoredDoc
	for id, score := range weightedScores {
		scoredDocs = append(scoredDocs, scoredDoc{id: id, score: score})
	}

	sort.Slice(scoredDocs, func(i, j int) bool {
		return scoredDocs[i].score > scoredDocs[j].score
	})

	// 限制结果数
	if topK > 0 && len(scoredDocs) > topK {
		scoredDocs = scoredDocs[:topK]
	}

	// 构建结果
	var fusedResults []*models.RetrievalResult
	for _, sd := range scoredDocs {
		result := docInfo[sd.id]
		result.Score = sd.score
		fusedResults = append(fusedResults, result)
	}

	return fusedResults
}

// HybridRetriever 混合检索器
type HybridRetriever struct {
	vectorRetriever  VectorRetriever
	keywordRetriever KeywordRetriever
	graphRetriever   GraphRetriever
	fusionStrategy   FusionStrategy
}

// VectorRetriever 向量检索器接口
type VectorRetriever interface {
	Retrieve(ctx context.Context, query string, topK int) ([]*models.RetrievalResult, error)
}

// KeywordRetriever 关键词检索器接口
type KeywordRetriever interface {
	Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error)
}

// GraphRetriever 图谱检索器接口
type GraphRetriever interface {
	GraphBasedRetrieve(ctx context.Context, query string, topK int) ([]*models.RetrievalResult, error)
}

// NewHybridRetriever 创建混合检索器
func NewHybridRetriever(
	vectorRetriever VectorRetriever,
	keywordRetriever KeywordRetriever,
	graphRetriever GraphRetriever,
	fusionStrategy FusionStrategy,
) *HybridRetriever {
	if fusionStrategy == nil {
		fusionStrategy = NewRRFFusion(60)
	}

	return &HybridRetriever{
		vectorRetriever:  vectorRetriever,
		keywordRetriever: keywordRetriever,
		graphRetriever:   graphRetriever,
		fusionStrategy:   fusionStrategy,
	}
}

// Retrieve 混合检索
func (h *HybridRetriever) Retrieve(ctx context.Context, query string, opts models.HybridOptions) (*models.HybridResult, error) {
	if opts.TopK <= 0 {
		opts.TopK = 10
	}

	// 确定检索策略
	strategies := opts.Strategies
	if len(strategies) == 0 {
		strategies = []models.RetrievalType{
			models.RetrievalTypeVector,
			models.RetrievalTypeKeyword,
		}
	}

	// 执行多路召回
	var allResults [][]*models.RetrievalResult
	sources := make(map[string]int)

	for _, strategy := range strategies {
		switch strategy {
		case models.RetrievalTypeVector:
			if h.vectorRetriever != nil {
				results, err := h.vectorRetriever.Retrieve(ctx, query, opts.TopK*2)
				if err == nil && len(results) > 0 {
					allResults = append(allResults, results)
					sources["vector"] = len(results)
				}
			}

		case models.RetrievalTypeKeyword:
			if h.keywordRetriever != nil {
				searchOpts := models.SearchOptions{TopK: opts.TopK * 2}
				result, err := h.keywordRetriever.Search(ctx, query, searchOpts)
				if err == nil && result != nil && len(result.Results) > 0 {
					allResults = append(allResults, result.Results)
					sources["keyword"] = len(result.Results)
				}
			}

		case models.RetrievalTypeGraph:
			if h.graphRetriever != nil {
				results, err := h.graphRetriever.GraphBasedRetrieve(ctx, query, opts.TopK*2)
				if err == nil && len(results) > 0 {
					allResults = append(allResults, results)
					sources["graph"] = len(results)
				}
			}
		}
	}

	if len(allResults) == 0 {
		return &models.HybridResult{
			Total:   0,
			Results: []*models.RetrievalResult{},
			Sources: sources,
		}, nil
	}

	// 融合结果
	fusedResults := h.fusionStrategy.Fuse(allResults, opts.TopK)

	return &models.HybridResult{
		Total:   len(fusedResults),
		Results: fusedResults,
		Sources: sources,
	}, nil
}

// QueryRouter 查询路由器
type QueryRouter struct {
	keywordRetriever KeywordRetriever
	vectorRetriever  VectorRetriever
	graphRetriever   GraphRetriever
}

// NewQueryRouter 创建查询路由器
func NewQueryRouter(keywordRetriever KeywordRetriever, vectorRetriever VectorRetriever, graphRetriever GraphRetriever) *QueryRouter {
	return &QueryRouter{
		keywordRetriever: keywordRetriever,
		vectorRetriever:  vectorRetriever,
		graphRetriever:   graphRetriever,
	}
}

// Route 路由查询
func (r *QueryRouter) Route(ctx context.Context, query string) ([]models.RetrievalType, error) {
	// 简单的查询分类逻辑
	query = fmt.Sprintf("%s", query) // 避免空引用

	// 检查是否是事实性查询（包含特定实体名、日期等）
	if isFactualQuery(query) {
		return []models.RetrievalType{
			models.RetrievalTypeKeyword,
			models.RetrievalTypeGraph,
		}, nil
	}

	// 检查是否是语义查询
	if isSemanticQuery(query) {
		return []models.RetrievalType{
			models.RetrievalTypeVector,
		}, nil
	}

	// 默认使用混合检索
	return []models.RetrievalType{
		models.RetrievalTypeVector,
		models.RetrievalTypeKeyword,
	}, nil
}

// isFactualQuery 检查是否是事实性查询
func isFactualQuery(query string) bool {
	// 检查是否包含特定模式
	factualPatterns := []string{
		"什么", "谁", "哪里", "什么时候", "多少",
		"what", "who", "where", "when", "how many",
	}

	lowerQuery := fmt.Sprintf("%s", query)
	for _, pattern := range factualPatterns {
		if contains(lowerQuery, pattern) {
			return true
		}
	}

	return false
}

// isSemanticQuery 检查是否是语义查询
func isSemanticQuery(query string) bool {
	// 检查是否包含概念性词汇
	semanticPatterns := []string{
		"为什么", "如何", "解释", "描述",
		"why", "how to", "explain", "describe",
	}

	lowerQuery := fmt.Sprintf("%s", query)
	for _, pattern := range semanticPatterns {
		if contains(lowerQuery, pattern) {
			return true
		}
	}

	return false
}

func contains(s, substr string) bool {
	if len(s) == 0 || len(substr) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// SetFusionStrategy 设置融合策略
func (hr *HybridRetriever) SetFusionStrategy(strategy FusionStrategy) {
	hr.fusionStrategy = strategy
}

// DefaultFusionStrategy 返回默认融合策略（RRF）
func DefaultFusionStrategy() FusionStrategy {
	return NewRRFFusion(60)
}
