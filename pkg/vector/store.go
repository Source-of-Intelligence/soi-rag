package vector

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
)

// VectorRecord 向量记录
type VectorRecord struct {
	ID       string                 `json:"id"`
	Vector   []float32              `json:"vector"`
	Metadata map[string]interface{} `json:"metadata"`
}

// VectorStore 向量存储接口
type VectorStore interface {
	Init(ctx context.Context) error
	Insert(ctx context.Context, records []*VectorRecord) error
	Search(ctx context.Context, queryVector []float32, topK int, filters map[string]interface{}) ([]*models.RetrievalResult, error)
	Delete(ctx context.Context, ids []string) error
	DeleteByFilter(ctx context.Context, filter map[string]interface{}) error
	Close() error
}

// InMemoryVectorStore 内存向量存储（用于测试）
type InMemoryVectorStore struct {
	vectors map[string]*VectorRecord
	dim     int
	mu      sync.RWMutex
}

// NewInMemoryVectorStore 创建内存向量存储
func NewInMemoryVectorStore(dim int) *InMemoryVectorStore {
	return &InMemoryVectorStore{
		vectors: make(map[string]*VectorRecord),
		dim:     dim,
	}
}

// Init 初始化
func (s *InMemoryVectorStore) Init(ctx context.Context) error {
	return nil
}

// Insert 插入向量
func (s *InMemoryVectorStore) Insert(ctx context.Context, records []*VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, record := range records {
		if record.ID == "" {
			record.ID = uuid.New().String()
		}
		if len(record.Vector) != s.dim {
			return fmt.Errorf("向量维度不匹配: expected %d, got %d", s.dim, len(record.Vector))
		}
		s.vectors[record.ID] = record
	}
	return nil
}

// Search 搜索相似向量（暴力搜索）
func (s *InMemoryVectorStore) Search(ctx context.Context, queryVector []float32, topK int, filters map[string]interface{}) ([]*models.RetrievalResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(queryVector) != s.dim {
		return nil, fmt.Errorf("查询向量维度不匹配: expected %d, got %d", s.dim, len(queryVector))
	}

	type scoredResult struct {
		record *VectorRecord
		score  float32
	}

	var results []scoredResult

	for _, record := range s.vectors {
		// 应用过滤器
		if !matchFilters(record.Metadata, filters) {
			continue
		}

		// 计算相似度
		similarity := CosineSimilarity(queryVector, record.Vector)
		results = append(results, scoredResult{
			record: record,
			score:  similarity,
		})
	}

	// 按相似度排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// 限制结果数
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}

	// 转换为RetrievalResult
	var retrievalResults []*models.RetrievalResult
	for _, r := range results {
		retrievalResults = append(retrievalResults, &models.RetrievalResult{
			ID:       r.record.ID,
			Score:    float64(r.score),
			Metadata: r.record.Metadata,
		})
	}

	return retrievalResults, nil
}

// Delete 删除向量
func (s *InMemoryVectorStore) Delete(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range ids {
		delete(s.vectors, id)
	}
	return nil
}

// DeleteByFilter 根据过滤器删除
func (s *InMemoryVectorStore) DeleteByFilter(ctx context.Context, filter map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, record := range s.vectors {
		if matchFilters(record.Metadata, filter) {
			delete(s.vectors, id)
		}
	}
	return nil
}

// Close 关闭存储
func (s *InMemoryVectorStore) Close() error {
	return nil
}

// matchFilters 检查元数据是否匹配过滤器
func matchFilters(metadata, filters map[string]interface{}) bool {
	if len(filters) == 0 {
		return true
	}

	for key, filterValue := range filters {
		metaValue, exists := metadata[key]
		if !exists {
			return false
		}
		if metaValue != filterValue {
			return false
		}
	}
	return true
}

// VectorRetriever 向量检索器
type VectorRetriever struct {
	embedder Embedder
	store    VectorStore
	dim      int
}

// NewVectorRetriever 创建向量检索器
func NewVectorRetriever(embedder Embedder, store VectorStore) *VectorRetriever {
	return &VectorRetriever{
		embedder: embedder,
		store:    store,
		dim:      embedder.Dimension(),
	}
}

// IndexChunks 索引分块
func (r *VectorRetriever) IndexChunks(ctx context.Context, chunks []*models.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	// 提取文本
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Content
	}

	// 嵌入文本
	embeddings, err := r.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("嵌入文本失败: %w", err)
	}

	// 创建向量记录
	records := make([]*VectorRecord, len(chunks))
	for i, chunk := range chunks {
		records[i] = &VectorRecord{
			ID:     chunk.ID,
			Vector: embeddings[i],
			Metadata: map[string]interface{}{
				"chunk_id":    chunk.ID,
				"document_id": chunk.DocumentID,
				"content":     chunk.Content,
				"chunk_index": chunk.ChunkIndex,
			},
		}
	}

	// 插入向量
	if err := r.store.Insert(ctx, records); err != nil {
		return fmt.Errorf("插入向量失败: %w", err)
	}

	return nil
}

// Retrieve 语义检索
func (r *VectorRetriever) Retrieve(ctx context.Context, query string, topK int) ([]*models.RetrievalResult, error) {
	// 嵌入查询
	queryVector, err := r.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("嵌入查询失败: %w", err)
	}

	// 搜索
	results, err := r.store.Search(ctx, queryVector, topK, nil)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

	// 填充额外信息
	for _, result := range results {
		if content, ok := result.Metadata["content"].(string); ok {
			result.Content = content
		}
		if docID, ok := result.Metadata["document_id"].(string); ok {
			result.DocumentID = docID
		}
		if chunkID, ok := result.Metadata["chunk_id"].(string); ok {
			result.ChunkID = chunkID
		}
	}

	return results, nil
}

// RetrieveByVector 使用向量检索
func (r *VectorRetriever) RetrieveByVector(ctx context.Context, vector []float32, topK int) ([]*models.RetrievalResult, error) {
	results, err := r.store.Search(ctx, vector, topK, nil)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

	// 填充额外信息
	for _, result := range results {
		if content, ok := result.Metadata["content"].(string); ok {
			result.Content = content
		}
		if docID, ok := result.Metadata["document_id"].(string); ok {
			result.DocumentID = docID
		}
		if chunkID, ok := result.Metadata["chunk_id"].(string); ok {
			result.ChunkID = chunkID
		}
	}

	return results, nil
}

// DeleteDocument 删除文档的所有向量
func (r *VectorRetriever) DeleteDocument(ctx context.Context, docID string) error {
	filter := map[string]interface{}{
		"document_id": docID,
	}
	return r.store.DeleteByFilter(ctx, filter)
}

// MultiQueryRetrieve 多查询检索
func (r *VectorRetriever) MultiQueryRetrieve(ctx context.Context, queries []string, topK int) ([]*models.RetrievalResult, error) {
	// 去重集合
	seen := make(map[string]bool)
	var allResults []*models.RetrievalResult

	for _, query := range queries {
		results, err := r.Retrieve(ctx, query, topK)
		if err != nil {
			continue
		}

		for _, result := range results {
			if !seen[result.ID] {
				seen[result.ID] = true
				allResults = append(allResults, result)
			}
		}
	}

	// 按分数排序
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})

	// 限制结果数
	if topK > 0 && len(allResults) > topK {
		allResults = allResults[:topK]
	}

	return allResults, nil
}

// HNSWIndex HNSW索引（简化版）
type HNSWIndex struct {
	vectors  map[string][]float32
	metadata map[string]map[string]interface{}
	dim      int
	m        int // 每个节点的最大连接数
	ef       int // 搜索时考虑的候选数
	mu       sync.RWMutex
}

// NewHNSWIndex 创建HNSW索引
func NewHNSWIndex(dim, m, ef int) *HNSWIndex {
	if m <= 0 {
		m = 16
	}
	if ef <= 0 {
		ef = 200
	}
	return &HNSWIndex{
		vectors:  make(map[string][]float32),
		metadata: make(map[string]map[string]interface{}),
		dim:      dim,
		m:        m,
		ef:       ef,
	}
}

// Insert 插入向量
func (h *HNSWIndex) Insert(id string, vector []float32, metadata map[string]interface{}) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(vector) != h.dim {
		return fmt.Errorf("向量维度不匹配: expected %d, got %d", h.dim, len(vector))
	}

	h.vectors[id] = vector
	h.metadata[id] = metadata
	return nil
}

// Search 搜索（使用暴力搜索作为简化实现）
func (h *HNSWIndex) Search(query []float32, topK int, filters map[string]interface{}) ([]string, []float32, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(query) != h.dim {
		return nil, nil, fmt.Errorf("查询向量维度不匹配: expected %d, got %d", h.dim, len(query))
	}

	type scoredItem struct {
		id    string
		score float32
	}

	var items []scoredItem

	for id, vector := range h.vectors {
		// 应用过滤器
		if !matchFilters(h.metadata[id], filters) {
			continue
		}

		similarity := CosineSimilarity(query, vector)
		items = append(items, scoredItem{id: id, score: similarity})
	}

	// 按相似度排序
	sort.Slice(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})

	// 限制结果数
	if topK > 0 && len(items) > topK {
		items = items[:topK]
	}

	ids := make([]string, len(items))
	scores := make([]float32, len(items))
	for i, item := range items {
		ids[i] = item.id
		scores[i] = item.score
	}

	return ids, scores, nil
}

// Delete 删除向量
func (h *HNSWIndex) Delete(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.vectors, id)
	delete(h.metadata, id)
}

// EuclideanDistance 计算欧氏距离
func EuclideanDistance(a, b []float32) float32 {
	if len(a) != len(b) {
		return math.MaxFloat32
	}

	var sum float32
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return float32(math.Sqrt(float64(sum)))
}

// SetEmbedder 设置嵌入器
func (vr *VectorRetriever) SetEmbedder(embedder Embedder) {
	vr.embedder = embedder
}

// SetStore 设置向量存储
func (vr *VectorRetriever) SetStore(store VectorStore) {
	vr.store = store
}

// GetEmbedder 获取嵌入器
func (vr *VectorRetriever) GetEmbedder() Embedder {
	return vr.embedder
}

// GetStore 获取向量存储
func (vr *VectorRetriever) GetStore() VectorStore {
	return vr.store
}
