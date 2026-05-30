package models

import (
	"time"

	"github.com/google/uuid"
)

// DocumentType 文档类型
type DocumentType string

const (
	DocTypePDF      DocumentType = "pdf"
	DocTypeWord     DocumentType = "docx"
	DocTypeMarkdown DocumentType = "md"
	DocTypeHTML     DocumentType = "html"
	DocTypeText     DocumentType = "txt"
	DocTypeCSV      DocumentType = "csv"
	DocTypeJSON     DocumentType = "json"
)

// DocumentStatus 文档状态
type DocumentStatus string

const (
	DocStatusPending    DocumentStatus = "pending"
	DocStatusProcessing DocumentStatus = "processing"
	DocStatusIndexed    DocumentStatus = "indexed"
	DocStatusFailed     DocumentStatus = "failed"
	DocStatusDeleted    DocumentStatus = "deleted"
)

// Document 文档模型
type Document struct {
	ID        string                 `json:"id" db:"id"`
	Title     string                 `json:"title" db:"title"`
	Content   string                 `json:"content" db:"content"`
	Source    string                 `json:"source" db:"source"`     // 来源URL/路径
	DocType   DocumentType           `json:"doc_type" db:"doc_type"` // 文档类型
	Metadata  map[string]interface{} `json:"metadata" db:"metadata"` // 扩展元数据
	CreatedAt time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt time.Time              `json:"updated_at" db:"updated_at"`
	Version   int                    `json:"version" db:"version"`
	Status    DocumentStatus         `json:"status" db:"status"`
}

// Chunk 文档分块模型
type Chunk struct {
	ID          string    `json:"id" db:"id"`
	DocumentID  string    `json:"document_id" db:"document_id"`
	Content     string    `json:"content" db:"content"`
	StartPos    int       `json:"start_pos" db:"start_pos"`       // 在原文中的起始位置
	EndPos      int       `json:"end_pos" db:"end_pos"`           // 在原文中的结束位置
	ChunkIndex  int       `json:"chunk_index" db:"chunk_index"`   // 分块序号
	TokenCount  int       `json:"token_count" db:"token_count"`   // Token数量
	PageNumber  int       `json:"page_number" db:"page_number"`   // 页码（PDF等）
	HeadingPath []string  `json:"heading_path" db:"heading_path"` // 标题层级路径
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// SearchOptions 搜索选项
type SearchOptions struct {
	TopK           int                    `json:"top_k"`
	Filters        map[string]interface{} `json:"filters"`
	ScoreThreshold float64                `json:"score_threshold"`
}

// SearchResult 搜索结果
type SearchResult struct {
	Total   int                `json:"total"`
	Results []*RetrievalResult `json:"results"`
}

// RetrievalResult 检索结果项
type RetrievalResult struct {
	ID          string                 `json:"id"`
	Content     string                 `json:"content"`
	Score       float64                `json:"score"`
	RerankScore float64                `json:"rerank_score,omitempty"`
	Source      string                 `json:"source"`
	DocumentID  string                 `json:"document_id"`
	ChunkID     string                 `json:"chunk_id"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// RetrievalType 检索类型
type RetrievalType string

const (
	RetrievalTypeVector  RetrievalType = "vector"
	RetrievalTypeKeyword RetrievalType = "keyword"
	RetrievalTypeGraph   RetrievalType = "graph"
	RetrievalTypeHybrid  RetrievalType = "hybrid"
)

// FusionMethod 融合方法
type FusionMethod string

const (
	FusionMethodRRF      FusionMethod = "rrf"
	FusionMethodWeighted FusionMethod = "weighted"
)

// HybridOptions 混合检索选项
type HybridOptions struct {
	TopK         int                `json:"top_k"`
	Strategies   []RetrievalType    `json:"strategies"`
	FusionMethod FusionMethod       `json:"fusion_method"`
	Weights      map[string]float64 `json:"weights"`
	RRFK         int                `json:"rrf_k"`
}

// HybridResult 混合检索结果
type HybridResult struct {
	Total     int                `json:"total"`
	Results   []*RetrievalResult `json:"results"`
	Sources   map[string]int     `json:"sources"` // 各来源结果数
	QueryTime int64              `json:"query_time_ms"`
}

// NewDocument 创建新文档（自动填充 ID、时间戳等）
func NewDocument(title, content, source string, docType DocumentType) *Document {
	now := time.Now()
	return &Document{
		ID:        uuid.New().String(),
		Title:     title,
		Content:   content,
		Source:    source,
		DocType:   docType,
		Status:    DocStatusPending,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  make(map[string]interface{}),
	}
}

// NewChunk 创建新分块
func NewChunk(documentID string, index int, content string) *Chunk {
	return &Chunk{
		ID:         uuid.New().String(),
		DocumentID: documentID,
		ChunkIndex: index,
		Content:    content,
		CreatedAt:  time.Now(),
	}
}

// DefaultSearchOptions 返回默认搜索选项
func DefaultSearchOptions() SearchOptions {
	return SearchOptions{
		TopK: 10,
	}
}

// DefaultHybridOptions 返回默认混合搜索选项
func DefaultHybridOptions() HybridOptions {
	return HybridOptions{
		TopK:         10,
		FusionMethod: FusionMethodRRF,
		RRFK:         60,
	}
}

// NewEntity 创建新实体
func NewEntity(name, entityType string) *Entity {
	return &Entity{
		ID:   uuid.New().String(),
		Name: name,
		Type: EntityType(entityType),
	}
}

// NewRelation 创建新关系
func NewRelation(sourceID, relationType, targetID string) *Relation {
	return &Relation{
		ID:       uuid.New().String(),
		SourceID: sourceID,
		Type:     RelationType(relationType),
		TargetID: targetID,
	}
}

// IsValid 验证文档是否有效
func (d *Document) IsValid() bool {
	return d.ID != "" && d.Content != ""
}

// GetMetadataString 获取字符串类型的元数据
func (d *Document) GetMetadataString(key string) string {
	if d.Metadata == nil {
		return ""
	}
	if v, ok := d.Metadata[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
