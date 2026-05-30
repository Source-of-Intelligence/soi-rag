package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embedder 嵌入模型接口
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
	Dimension() int
}

// OpenAIEmbedder OpenAI嵌入模型
type OpenAIEmbedder struct {
	apiKey     string
	apiBase    string
	model      string
	dimension  int
	httpClient *http.Client
}

// OpenAIEmbedderConfig OpenAI嵌入模型配置
type OpenAIEmbedderConfig struct {
	APIKey    string
	APIBase   string
	Model     string
	Dimension int
}

// NewOpenAIEmbedder 创建OpenAI嵌入模型
func NewOpenAIEmbedder(config OpenAIEmbedderConfig) *OpenAIEmbedder {
	if config.APIBase == "" {
		config.APIBase = "https://api.openai.com/v1"
	}
	if config.Model == "" {
		config.Model = "text-embedding-3-small"
	}
	if config.Dimension == 0 {
		config.Dimension = 1536
	}

	return &OpenAIEmbedder{
		apiKey:    config.APIKey,
		apiBase:   config.APIBase,
		model:     config.Model,
		dimension: config.Dimension,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Embed 批量嵌入文本
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// 清理文本
	cleanedTexts := make([]string, len(texts))
	for i, text := range texts {
		cleanedTexts[i] = strings.TrimSpace(text)
		if cleanedTexts[i] == "" {
			cleanedTexts[i] = " "
		}
	}

	reqBody := map[string]interface{}{
		"input": cleanedTexts,
		"model": e.model,
	}

	if e.dimension > 0 {
		reqBody["dimensions"] = e.dimension
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.apiBase+"/embeddings", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Model string `json:"model"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 按索引排序
	embeddings := make([][]float32, len(texts))
	for _, item := range result.Data {
		embeddings[item.Index] = item.Embedding
	}

	return embeddings, nil
}

// EmbedQuery 嵌入单个查询
func (e *OpenAIEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	embeddings, err := e.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("嵌入结果为空")
	}
	return embeddings[0], nil
}

// Dimension 返回向量维度
func (e *OpenAIEmbedder) Dimension() int {
	return e.dimension
}

// MockEmbedder 模拟嵌入模型（用于测试）
type MockEmbedder struct {
	dimension int
}

// NewMockEmbedder 创建模拟嵌入模型
func NewMockEmbedder(dimension int) *MockEmbedder {
	if dimension <= 0 {
		dimension = 768
	}
	return &MockEmbedder{dimension: dimension}
}

// Embed 模拟嵌入
func (e *MockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i := range texts {
		embeddings[i] = e.generateRandomVector()
	}
	return embeddings, nil
}

// EmbedQuery 模拟嵌入查询
func (e *MockEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return e.generateRandomVector(), nil
}

// Dimension 返回向量维度
func (e *MockEmbedder) Dimension() int {
	return e.dimension
}

// generateRandomVector 生成随机向量（模拟）
func (e *MockEmbedder) generateRandomVector() []float32 {
	vec := make([]float32, e.dimension)
	for i := range vec {
		// 使用简单的哈希生成确定性向量
		vec[i] = float32(i%100) / 100.0
	}
	return vec
}

// NormalizeVector 归一化向量
func NormalizeVector(vec []float32) []float32 {
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))

	if norm == 0 {
		return vec
	}

	normalized := make([]float32, len(vec))
	for i, v := range vec {
		normalized[i] = v / norm
	}
	return normalized
}

// CosineSimilarity 计算余弦相似度
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
