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

// MultilingualModelType 多语言模型类型
type MultilingualModelType string

const (
	// ModelMultilingualE5 multilingual-e5 系列模型
	ModelMultilingualE5 MultilingualModelType = "multilingual-e5"
	// ModelBGE_M3 BGE-M3 模型
	ModelBGE_M3 MultilingualModelType = "bge-m3"
	// ModelMultilingualE5Large multilingual-e5-large 模型
	ModelMultilingualE5Large MultilingualModelType = "multilingual-e5-large"
	// ModelMultilingualE5Small multilingual-e5-small 模型
	ModelMultilingualE5Small MultilingualModelType = "multilingual-e5-small"
	// ModelBGE_M3Retrieval BGE-M3 检索优化模型
	ModelBGE_M3Retrieval MultilingualModelType = "bge-m3-retrieval"
	// ModelCustom 自定义模型
	ModelCustom MultilingualModelType = "custom"
)

// MultilingualModelConfig 多语言模型配置
type MultilingualModelConfig struct {
	ModelType    MultilingualModelType
	ModelName    string // 实际模型名称，如 "intfloat/multilingual-e5-large"
	Dimension    int    // 向量维度
	APIURL       string // API 地址
	APIKey       string // API 密钥（可选）
	MaxBatchSize int    // 最大批次大小
}

// DefaultModelConfigs 默认模型配置
var DefaultModelConfigs = map[MultilingualModelType]MultilingualModelConfig{
	ModelMultilingualE5: {
		ModelType:    ModelMultilingualE5,
		ModelName:    "intfloat/multilingual-e5-base",
		Dimension:    768,
		MaxBatchSize: 32,
	},
	ModelMultilingualE5Large: {
		ModelType:    ModelMultilingualE5Large,
		ModelName:    "intfloat/multilingual-e5-large",
		Dimension:    1024,
		MaxBatchSize: 32,
	},
	ModelMultilingualE5Small: {
		ModelType:    ModelMultilingualE5Small,
		ModelName:    "intfloat/multilingual-e5-small",
		Dimension:    384,
		MaxBatchSize: 32,
	},
	ModelBGE_M3: {
		ModelType:    ModelBGE_M3,
		ModelName:    "BAAI/bge-m3",
		Dimension:    1024,
		MaxBatchSize: 32,
	},
	ModelBGE_M3Retrieval: {
		ModelType:    ModelBGE_M3Retrieval,
		ModelName:    "BAAI/bge-m3-retromae",
		Dimension:    1024,
		MaxBatchSize: 32,
	},
}

// MultilingualEmbedder 多语言嵌入模型
type MultilingualEmbedder struct {
	config      MultilingualModelConfig
	httpClient  *http.Client
	normalizing bool // 是否对结果进行归一化
}

// MultilingualEmbedderConfig 多语言嵌入模型配置
type MultilingualEmbedderConfig struct {
	ModelType    MultilingualModelType // 模型类型
	ModelName    string                // 自定义模型名称（可选）
	Dimension    int                   // 向量维度（可选，使用默认值）
	APIURL       string                // API 地址
	APIKey       string                // API 密钥（可选）
	MaxBatchSize int                   // 最大批次大小
	Normalizing  bool                  // 是否归一化向量（默认 true）
	Timeout      time.Duration         // HTTP 超时时间
}

// NewMultilingualEmbedder 创建多语言嵌入模型
func NewMultilingualEmbedder(config MultilingualEmbedderConfig) (*MultilingualEmbedder, error) {
	// 获取默认配置或创建自定义配置
	modelConfig := MultilingualModelConfig{
		APIKey: config.APIKey,
	}

	// 设置模型类型相关配置
	if config.ModelType != "" && config.ModelType != ModelCustom {
		defaultConfig, exists := DefaultModelConfigs[config.ModelType]
		if !exists {
			return nil, fmt.Errorf("不支持的多语言模型类型: %s", config.ModelType)
		}
		modelConfig.ModelType = defaultConfig.ModelType
		modelConfig.ModelName = defaultConfig.ModelName
		modelConfig.Dimension = defaultConfig.Dimension
		modelConfig.MaxBatchSize = defaultConfig.MaxBatchSize
	} else {
		modelConfig.ModelType = ModelCustom
	}

	// 覆盖自定义配置
	if config.ModelName != "" {
		modelConfig.ModelName = config.ModelName
	}
	if config.Dimension > 0 {
		modelConfig.Dimension = config.Dimension
	}
	if config.MaxBatchSize > 0 {
		modelConfig.MaxBatchSize = config.MaxBatchSize
	}

	// 设置 API URL
	if config.APIURL == "" {
		return nil, fmt.Errorf("API URL 不能为空")
	}
	modelConfig.APIURL = strings.TrimSuffix(config.APIURL, "/")

	// 验证必要配置
	if modelConfig.ModelName == "" {
		return nil, fmt.Errorf("模型名称不能为空")
	}
	if modelConfig.Dimension <= 0 {
		return nil, fmt.Errorf("向量维度必须大于 0")
	}

	// 设置超时
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	// 设置归一化（默认开启归一化）
	normalizing := true

	return &MultilingualEmbedder{
		config:      modelConfig,
		normalizing: normalizing,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Embed 批量嵌入文本
func (e *MultilingualEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
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

	// 分批处理
	batchSize := e.config.MaxBatchSize
	if batchSize <= 0 {
		batchSize = 32
	}

	var allEmbeddings [][]float32

	for i := 0; i < len(cleanedTexts); i += batchSize {
		end := i + batchSize
		if end > len(cleanedTexts) {
			end = len(cleanedTexts)
		}

		batch := cleanedTexts[i:end]
		embeddings, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("批次 %d-%d 嵌入失败: %w", i, end, err)
		}

		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings, nil
}

// embedBatch 处理单个批次的嵌入请求
func (e *MultilingualEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	// 构建请求体
	reqBody := map[string]interface{}{
		"input": texts,
		"model": e.config.ModelName,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", e.config.APIURL+"/embeddings", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if e.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.config.APIKey)
	}

	// 发送请求
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API 返回错误 [%d]: %s", resp.StatusCode, string(body))
	}

	// 解析响应
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

	// 按索引排序并归一化
	embeddings := make([][]float32, len(texts))
	for _, item := range result.Data {
		emb := item.Embedding
		if e.normalizing {
			emb = NormalizeVector(emb)
		}
		embeddings[item.Index] = emb
	}

	return embeddings, nil
}

// EmbedQuery 嵌入单个查询
// 对于 E5 系列模型，查询需要添加 "query: " 前缀
func (e *MultilingualEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	// 根据模型类型处理查询前缀
	processedQuery := e.processQuery(query)

	embeddings, err := e.Embed(ctx, []string{processedQuery})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("嵌入结果为空")
	}
	return embeddings[0], nil
}

// processQuery 根据模型类型处理查询文本
func (e *MultilingualEmbedder) processQuery(query string) string {
	query = strings.TrimSpace(query)

	// E5 系列模型需要添加 "query: " 前缀
	switch e.config.ModelType {
	case ModelMultilingualE5, ModelMultilingualE5Large, ModelMultilingualE5Small:
		if !strings.HasPrefix(query, "query: ") {
			return "query: " + query
		}
	case ModelBGE_M3, ModelBGE_M3Retrieval:
		// BGE-M3 模型通常不需要特殊前缀，但某些部署可能需要
		// 这里保持原样
	}

	return query
}

// processDocument 根据模型类型处理文档文本
func (e *MultilingualEmbedder) processDocument(doc string) string {
	doc = strings.TrimSpace(doc)

	// E5 系列模型需要添加 "passage: " 前缀
	switch e.config.ModelType {
	case ModelMultilingualE5, ModelMultilingualE5Large, ModelMultilingualE5Small:
		if !strings.HasPrefix(doc, "passage: ") {
			return "passage: " + doc
		}
	}

	return doc
}

// EmbedDocuments 嵌入文档（自动添加必要的前缀）
func (e *MultilingualEmbedder) EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error) {
	if len(documents) == 0 {
		return [][]float32{}, nil
	}

	// 处理文档前缀
	processedDocs := make([]string, len(documents))
	for i, doc := range documents {
		processedDocs[i] = e.processDocument(doc)
	}

	return e.Embed(ctx, processedDocs)
}

// Dimension 返回向量维度
func (e *MultilingualEmbedder) Dimension() int {
	return e.config.Dimension
}

// ModelName 返回模型名称
func (e *MultilingualEmbedder) ModelName() string {
	return e.config.ModelName
}

// ModelType 返回模型类型
func (e *MultilingualEmbedder) ModelType() MultilingualModelType {
	return e.config.ModelType
}

// SetNormalizing 设置是否归一化向量
func (e *MultilingualEmbedder) SetNormalizing(normalizing bool) {
	e.normalizing = normalizing
}

// NewMultilingualE5Embedder 创建 multilingual-e5 模型的便捷方法
func NewMultilingualE5Embedder(apiURL, apiKey string, dimension int) (*MultilingualEmbedder, error) {
	modelType := ModelMultilingualE5
	if dimension == 1024 {
		modelType = ModelMultilingualE5Large
	} else if dimension == 384 {
		modelType = ModelMultilingualE5Small
	}

	return NewMultilingualEmbedder(MultilingualEmbedderConfig{
		ModelType: modelType,
		APIURL:    apiURL,
		APIKey:    apiKey,
		Dimension: dimension,
	})
}

// NewBGEM3Embedder 创建 BGE-M3 模型的便捷方法
func NewBGEM3Embedder(apiURL, apiKey string) (*MultilingualEmbedder, error) {
	return NewMultilingualEmbedder(MultilingualEmbedderConfig{
		ModelType: ModelBGE_M3,
		APIURL:    apiURL,
		APIKey:    apiKey,
	})
}

// NormalizeVector 归一化向量（复用 embedder.go 中的实现）
func normalizeVector(vec []float32) []float32 {
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
