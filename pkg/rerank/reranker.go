package rerank

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ragtool/rag/pkg/models"
)

// 重排序分数权重常量
const (
	// rerankOriginalWeight 原始检索分数在重排序中的权重
	rerankOriginalWeight = 0.3
	// rerankMatchWeight 查询匹配分数在重排序中的权重
	rerankMatchWeight = 0.7
)

// Reranker 重排序器接口
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []*models.RetrievalResult, topN int) ([]*models.RetrievalResult, error)
}

// CrossEncoderReranker 基于词匹配的启发式重排序器（简化版）
// 注意: 这不是真正的交叉编码器实现。真正的交叉编码器需要预训练模型（如 cross-encoder/ms-marco）。
// 本实现使用查询词匹配度作为重排序依据，适用于不需要额外模型依赖的场景。
// 如需更强的重排序效果，建议集成专门的交叉编码器模型。
type CrossEncoderReranker struct {
	// 预留：未来可集成真实的交叉编码器模型
}

// NewCrossEncoderReranker 创建启发式重排序器
func NewCrossEncoderReranker() *CrossEncoderReranker {
	return &CrossEncoderReranker{}
}

// Rerank 重排序
func (r *CrossEncoderReranker) Rerank(ctx context.Context, query string, candidates []*models.RetrievalResult, topN int) ([]*models.RetrievalResult, error) {
	if len(candidates) == 0 {
		return candidates, nil
	}

	// 简化版：基于查询词匹配度进行重排序
	queryTerms := tokenize(query)

	for _, candidate := range candidates {
		// 计算查询词在文档中的匹配程度
		contentTerms := tokenize(candidate.Content)
		matchScore := calculateMatchScore(queryTerms, contentTerms)

		// 结合原始分数和匹配分数
		candidate.RerankScore = candidate.Score*rerankOriginalWeight + matchScore*rerankMatchWeight
	}

	// 按重排序分数排序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].RerankScore > candidates[j].RerankScore
	})

	// 限制结果数
	if topN > 0 && len(candidates) > topN {
		candidates = candidates[:topN]
	}

	return candidates, nil
}

// DiversityReranker 多样性重排序器
type DiversityReranker struct {
	lambda float64 // 多样性与相关性的权衡参数
}

// NewDiversityReranker 创建多样性重排序器
func NewDiversityReranker(lambda float64) *DiversityReranker {
	if lambda <= 0 || lambda > 1 {
		lambda = 0.5
	}
	return &DiversityReranker{lambda: lambda}
}

// Rerank 重排序
func (r *DiversityReranker) Rerank(ctx context.Context, query string, candidates []*models.RetrievalResult, topN int) ([]*models.RetrievalResult, error) {
	if len(candidates) == 0 {
		return candidates, nil
	}

	var selected []*models.RetrievalResult
	remaining := make([]*models.RetrievalResult, len(candidates))
	copy(remaining, candidates)

	for len(selected) < topN && len(remaining) > 0 {
		var bestIdx int
		var bestScore float64

		for i, candidate := range remaining {
			// 计算MMR分数
			mmrScore := r.calculateMMR(candidate, selected, remaining)
			if mmrScore > bestScore {
				bestScore = mmrScore
				bestIdx = i
			}
		}

		selected = append(selected, remaining[bestIdx])
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}

	return selected, nil
}

// calculateMMR 计算MMR分数
func (r *DiversityReranker) calculateMMR(candidate *models.RetrievalResult, selected, remaining []*models.RetrievalResult) float64 {
	// 相关性分数
	relevance := candidate.Score

	// 多样性分数（与已选文档的最大相似度）
	maxSim := 0.0
	for _, s := range selected {
		sim := calculateSimilarity(candidate, s)
		if sim > maxSim {
			maxSim = sim
		}
	}

	// MMR = λ * Relevance - (1-λ) * maxSim
	return r.lambda*relevance - (1-r.lambda)*maxSim
}

// calculateSimilarity 计算两个文档的相似度
func calculateSimilarity(a, b *models.RetrievalResult) float64 {
	// 简化版：基于共同词汇的Jaccard相似度
	aTerms := make(map[string]bool)
	for _, term := range tokenize(a.Content) {
		aTerms[term] = true
	}

	var common int
	bTerms := tokenize(b.Content)
	for _, term := range bTerms {
		if aTerms[term] {
			common++
		}
	}

	if len(aTerms)+len(bTerms) == 0 {
		return 0
	}

	return float64(common) / float64(len(aTerms)+len(bTerms)-common)
}

// ReciprocalRankFusion 倒数排名融合
type ReciprocalRankFusion struct {
	k int
}

// NewReciprocalRankFusion 创建倒数排名融合器
func NewReciprocalRankFusion(k int) *ReciprocalRankFusion {
	if k <= 0 {
		k = 60
	}
	return &ReciprocalRankFusion{k: k}
}

// Fuse 融合多个排序列表
func (r *ReciprocalRankFusion) Fuse(lists [][]*models.RetrievalResult, topN int) []*models.RetrievalResult {
	scores := make(map[string]float64)
	docInfo := make(map[string]*models.RetrievalResult)

	for _, list := range lists {
		for rank, doc := range list {
			scores[doc.ID] += 1.0 / float64(r.k+rank+1)
			if _, ok := docInfo[doc.ID]; !ok {
				docInfo[doc.ID] = doc
			}
		}
	}

	// 排序
	type scoredDoc struct {
		id    string
		score float64
	}

	var scoredDocs []scoredDoc
	for id, score := range scores {
		scoredDocs = append(scoredDocs, scoredDoc{id: id, score: score})
	}

	sort.Slice(scoredDocs, func(i, j int) bool {
		return scoredDocs[i].score > scoredDocs[j].score
	})

	// 限制结果数
	if topN > 0 && len(scoredDocs) > topN {
		scoredDocs = scoredDocs[:topN]
	}

	// 构建结果
	var results []*models.RetrievalResult
	for _, sd := range scoredDocs {
		doc := docInfo[sd.id]
		doc.Score = sd.score
		results = append(results, doc)
	}

	return results
}

// Helper functions

func tokenize(text string) []string {
	// 简化版分词
	words := strings.Fields(strings.ToLower(text))
	var tokens []string
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:\"'()[]{}<>")
		if len(word) > 0 {
			tokens = append(tokens, word)
		}
	}
	return tokens
}

func calculateMatchScore(queryTerms, contentTerms []string) float64 {
	if len(queryTerms) == 0 {
		return 0
	}

	contentTermSet := make(map[string]int)
	for _, term := range contentTerms {
		contentTermSet[term]++
	}

	matchCount := 0
	for _, term := range queryTerms {
		if contentTermSet[term] > 0 {
			matchCount++
		}
	}

	return float64(matchCount) / float64(len(queryTerms))
}

// RerankPipeline 重排序管道
type RerankPipeline struct {
	rerankers []Reranker
}

// NewRerankPipeline 创建重排序管道
func NewRerankPipeline(rerankers ...Reranker) *RerankPipeline {
	return &RerankPipeline{rerankers: rerankers}
}

// AddReranker 添加重排序器
func (p *RerankPipeline) AddReranker(reranker Reranker) {
	p.rerankers = append(p.rerankers, reranker)
}

// Process 处理结果
func (p *RerankPipeline) Process(ctx context.Context, query string, candidates []*models.RetrievalResult, topN int) ([]*models.RetrievalResult, error) {
	var err error
	for _, reranker := range p.rerankers {
		candidates, err = reranker.Rerank(ctx, query, candidates, topN)
		if err != nil {
			return nil, fmt.Errorf("重排序失败: %w", err)
		}
	}
	return candidates, nil
}
