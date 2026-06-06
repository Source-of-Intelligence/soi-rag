package eval

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/ragtool/rag/pkg/config"
	"github.com/ragtool/rag/pkg/models"
	"github.com/ragtool/rag/pkg/rag"
)

// Evaluator 检索质量评估器
type Evaluator struct {
	engine *rag.Engine
}

// NewEvaluator 创建评估器
func NewEvaluator(engine *rag.Engine) *Evaluator {
	return &Evaluator{engine: engine}
}

// EvalDataset 评估数据集
type EvalDataset struct {
	Queries []EvalQuery `json:"queries"`
}

// EvalQuery 评估查询
type EvalQuery struct {
	Query          string             `json:"query"`                     // 查询文本
	RelevantIDs    []string           `json:"relevant_ids"`              // 相关文档ID列表
	RelevantScores map[string]float64 `json:"relevant_scores,omitempty"` // 文档相关性分数（用于NDCG）
}

// EvalResult 评估结果
type EvalResult struct {
	Recall    map[int]float64 `json:"recall"`           // Recall@K
	Precision map[int]float64 `json:"precision"`        // Precision@K
	MRR       float64         `json:"mrr"`              // Mean Reciprocal Rank
	NDCG      map[int]float64 `json:"ndcg"`             // NDCG@K
	Map       float64         `json:"map"`              // Mean Average Precision
	Total     int             `json:"total"`            // 总查询数
	Errors    []EvalError     `json:"errors,omitempty"` // 错误列表
}

// EvalError 评估错误
type EvalError struct {
	Query string `json:"query"`
	Error string `json:"error"`
}

// Evaluate 执行评估
func (e *Evaluator) Evaluate(ctx context.Context, dataset *EvalDataset, ks ...int) (*EvalResult, error) {
	if len(ks) == 0 {
		ks = []int{1, 5, 10, 20}
	}

	result := &EvalResult{
		Recall:    make(map[int]float64),
		Precision: make(map[int]float64),
		NDCG:      make(map[int]float64),
		Total:     len(dataset.Queries),
	}

	var mrrSum float64
	var mapSum float64

	for _, q := range dataset.Queries {
		// 计算最大K值
		maxK := 0
		for _, k := range ks {
			if k > maxK {
				maxK = k
			}
		}

		// 执行搜索
		searchResult, err := e.engine.Search(ctx, q.Query, models.SearchOptions{TopK: maxK})
		if err != nil {
			result.Errors = append(result.Errors, EvalError{
				Query: q.Query,
				Error: err.Error(),
			})
			continue
		}

		// 提取检索结果ID
		retrievedIDs := make([]string, len(searchResult.Results))
		for i, r := range searchResult.Results {
			retrievedIDs[i] = r.DocumentID
		}

		// 计算各指标
		relevantSet := make(map[string]bool)
		for _, id := range q.RelevantIDs {
			relevantSet[id] = true
		}

		// Recall@K 和 Precision@K
		for _, k := range ks {
			recall := e.CalculateRecall(retrievedIDs, relevantSet, k)
			precision := e.CalculatePrecision(retrievedIDs, relevantSet, k)
			result.Recall[k] += recall
			result.Precision[k] += precision
		}

		// MRR
		mrrSum += e.CalculateMRR(retrievedIDs, relevantSet)

		// NDCG@K
		for _, k := range ks {
			ndcg := e.CalculateNDCG(retrievedIDs, q.RelevantScores, k)
			result.NDCG[k] += ndcg
		}

		// MAP
		mapSum += e.CalculateMAP(retrievedIDs, relevantSet)
	}

	// 计算平均值
	validQueries := float64(result.Total - len(result.Errors))
	if validQueries > 0 {
		for k := range result.Recall {
			result.Recall[k] /= validQueries
			result.Precision[k] /= validQueries
			result.NDCG[k] /= validQueries
		}
		result.MRR = mrrSum / validQueries
		result.Map = mapSum / validQueries
	}

	return result, nil
}

// GetErrors 返回评估过程中收集的错误
func (r *EvalResult) GetErrors() []EvalError {
	return r.Errors
}

// CalculateRecall 计算 Recall@K
func (e *Evaluator) CalculateRecall(retrieved []string, relevant map[string]bool, k int) float64 {
	if len(relevant) == 0 {
		return 0
	}

	if k > len(retrieved) {
		k = len(retrieved)
	}

	hits := 0
	for i := 0; i < k; i++ {
		if relevant[retrieved[i]] {
			hits++
		}
	}

	return float64(hits) / float64(len(relevant))
}

// CalculatePrecision 计算 Precision@K
func (e *Evaluator) CalculatePrecision(retrieved []string, relevant map[string]bool, k int) float64 {
	if k == 0 {
		return 0
	}

	if k > len(retrieved) {
		k = len(retrieved)
	}

	hits := 0
	for i := 0; i < k; i++ {
		if relevant[retrieved[i]] {
			hits++
		}
	}

	return float64(hits) / float64(k)
}

// CalculateMRR 计算 Mean Reciprocal Rank
func (e *Evaluator) CalculateMRR(retrieved []string, relevant map[string]bool) float64 {
	for i, id := range retrieved {
		if relevant[id] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// CalculateNDCG 计算 NDCG@K
func (e *Evaluator) CalculateNDCG(retrieved []string, scores map[string]float64, k int) float64 {
	if k > len(retrieved) {
		k = len(retrieved)
	}

	// 计算 DCG
	dcg := 0.0
	for i := 0; i < k; i++ {
		score := scores[retrieved[i]]
		dcg += score / math.Log2(float64(i+2))
	}

	// 计算 IDCG（理想情况）
	idealScores := make([]float64, 0, len(scores))
	for _, s := range scores {
		idealScores = append(idealScores, s)
	}
	// 排序（降序）
	for i := 0; i < len(idealScores); i++ {
		for j := i + 1; j < len(idealScores); j++ {
			if idealScores[i] < idealScores[j] {
				idealScores[i], idealScores[j] = idealScores[j], idealScores[i]
			}
		}
	}

	idcg := 0.0
	for i := 0; i < k && i < len(idealScores); i++ {
		idcg += idealScores[i] / math.Log2(float64(i+2))
	}

	if idcg == 0 {
		return 0
	}

	return dcg / idcg
}

// CalculateMAP 计算 Mean Average Precision
func (e *Evaluator) CalculateMAP(retrieved []string, relevant map[string]bool) float64 {
	if len(relevant) == 0 {
		return 0
	}

	var apSum float64
	hits := 0

	for i, id := range retrieved {
		if relevant[id] {
			hits++
			apSum += float64(hits) / float64(i+1)
		}
	}

	return apSum / float64(len(relevant))
}

// EvaluateSingle 评估单个查询
func (ev *Evaluator) EvaluateSingle(ctx context.Context, query string, ks ...int) (*EvalResult, error) {
	dataset := &EvalDataset{
		Queries: []EvalQuery{
			{Query: query, RelevantIDs: nil},
		},
	}
	return ev.Evaluate(ctx, dataset, ks...)
}

// DefaultEvalKs 返回默认评估K值
func DefaultEvalKs() []int {
	return []int{1, 5, 10, 20}
}

// NewEvalDataset 创建评估数据集
func NewEvalDataset() *EvalDataset {
	return &EvalDataset{
		Queries: make([]EvalQuery, 0),
	}
}

// AddQuery 添加评估查询
func (d *EvalDataset) AddQuery(query string, relevantIDs []string, relevantScores map[string]float64) {
	d.Queries = append(d.Queries, EvalQuery{
		Query:          query,
		RelevantIDs:    relevantIDs,
		RelevantScores: relevantScores,
	})
}

// ToMap 将评估结果转换为map
func (r *EvalResult) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"recall":    r.Recall,
		"precision": r.Precision,
		"mrr":       r.MRR,
		"ndcg":      r.NDCG,
		"map":       r.Map,
		"total":     r.Total,
	}
}

// QualityCheck 质量检验结果
type QualityCheck struct {
	Passed   bool            `json:"passed"`   // 是否通过检验
	Score    float64         `json:"score"`    // 综合得分
	Details  []QualityDetail `json:"details"`  // 各指标详情
	Warnings []string        `json:"warnings"` // 警告信息
}

// QualityDetail 单个指标检验详情
type QualityDetail struct {
	Metric    string  `json:"metric"`    // 指标名称
	Value     float64 `json:"value"`     // 实际值
	Threshold float64 `json:"threshold"` // 阈值
	Passed    bool    `json:"passed"`    // 是否通过
}

// CheckQuality 执行质量检验，对比评估结果与阈值配置
func (r *EvalResult) CheckQuality(threshold *config.QualityThreshold) *QualityCheck {
	if threshold == nil {
		threshold = &config.QualityThreshold{
			MinRecallAt1:    0.3,
			MinRecallAt5:    0.5,
			MinRecallAt10:   0.7,
			MinPrecisionAt5: 0.4,
			MinMRR:          0.4,
			MinNDCGAt10:     0.5,
			MinMAP:          0.4,
		}
	}

	check := &QualityCheck{
		Passed:  true,
		Details: make([]QualityDetail, 0),
	}

	checks := []struct {
		name      string
		value     float64
		threshold float64
	}{
		{"Recall@1", r.Recall[1], threshold.MinRecallAt1},
		{"Recall@5", r.Recall[5], threshold.MinRecallAt5},
		{"Recall@10", r.Recall[10], threshold.MinRecallAt10},
		{"Precision@5", r.Precision[5], threshold.MinPrecisionAt5},
		{"MRR", r.MRR, threshold.MinMRR},
		{"NDCG@10", r.NDCG[10], threshold.MinNDCGAt10},
		{"MAP", r.Map, threshold.MinMAP},
	}

	var totalScore float64
	for _, c := range checks {
		passed := c.value >= c.threshold
		if !passed {
			check.Passed = false
			check.Warnings = append(check.Warnings,
				fmt.Sprintf("%s: %.4f < %.4f (未达标)", c.name, c.value, c.threshold))
		}
		check.Details = append(check.Details, QualityDetail{
			Metric:    c.name,
			Value:     c.value,
			Threshold: c.threshold,
			Passed:    passed,
		})
		totalScore += c.value
	}

	check.Score = totalScore / float64(len(checks))
	return check
}

// CheckQualityWithConfig 使用配置文件中的阈值进行质量检验
func (r *EvalResult) CheckQualityWithConfig(cfg *config.FileConfig) *QualityCheck {
	return r.CheckQuality(&cfg.Threshold)
}

// String 返回结果字符串
func (r *EvalResult) String() string {
	var sb strings.Builder
	sb.WriteString("=== 检索质量评估结果 ===\n\n")

	sb.WriteString("Recall@K:\n")
	for k, v := range r.Recall {
		sb.WriteString(fmt.Sprintf("  Recall@%d: %.4f\n", k, v))
	}

	sb.WriteString("\nPrecision@K:\n")
	for k, v := range r.Precision {
		sb.WriteString(fmt.Sprintf("  Precision@%d: %.4f\n", k, v))
	}

	sb.WriteString(fmt.Sprintf("\nMRR: %.4f\n", r.MRR))
	sb.WriteString(fmt.Sprintf("MAP: %.4f\n", r.Map))

	sb.WriteString("\nNDCG@K:\n")
	for k, v := range r.NDCG {
		sb.WriteString(fmt.Sprintf("  NDCG@%d: %.4f\n", k, v))
	}

	sb.WriteString(fmt.Sprintf("\n总查询数: %d\n", r.Total))
	if len(r.Errors) > 0 {
		sb.WriteString(fmt.Sprintf("错误数: %d\n", len(r.Errors)))
	}

	return sb.String()
}
