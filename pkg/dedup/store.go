package dedup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
)

// DedupStore 去重存储接口
type DedupStore interface {
	// 检查哈希是否存在
	ExistsByHash(ctx context.Context, hash string) (bool, *models.Document, error)

	// 通过哈希获取文档
	GetByHash(ctx context.Context, hash string) (*models.Document, error)

	// 创建带哈希的文档
	CreateDocumentWithHash(ctx context.Context, doc *models.Document, hash string) error

	// 更新哈希索引
	UpdateHashIndex(ctx context.Context, docID, hash string) error

	// 删除文档（同时删除哈希索引）
	DeleteDocument(ctx context.Context, docID string) error
}

// HashIndex 哈希索引记录
type HashIndex struct {
	Hash        string    `json:"hash"`
	DocumentID  string    `json:"document_id"`
	ContentSize int64     `json:"content_size"`
	CreatedAt   time.Time `json:"created_at"`
	Source      string    `json:"source"`
}

// InMemoryDedupStore 内存去重存储
type InMemoryDedupStore struct {
	documents map[string]*models.Document // docID -> document
	hashIndex map[string]*HashIndex       // hash -> hashIndex
	docToHash map[string]string           // docID -> hash
	mu        sync.RWMutex
}

// NewInMemoryDedupStore 创建内存去重存储
func NewInMemoryDedupStore() *InMemoryDedupStore {
	return &InMemoryDedupStore{
		documents: make(map[string]*models.Document),
		hashIndex: make(map[string]*HashIndex),
		docToHash: make(map[string]string),
	}
}

// ExistsByHash 检查哈希是否存在
func (s *InMemoryDedupStore) ExistsByHash(ctx context.Context, hash string) (bool, *models.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, exists := s.hashIndex[hash]
	if !exists {
		return false, nil, nil
	}

	doc, ok := s.documents[idx.DocumentID]
	if !ok {
		return false, nil, nil
	}

	return true, doc, nil
}

// GetByHash 通过哈希获取文档
func (s *InMemoryDedupStore) GetByHash(ctx context.Context, hash string) (*models.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, exists := s.hashIndex[hash]
	if !exists {
		return nil, fmt.Errorf("哈希不存在: %s", hash)
	}

	doc, ok := s.documents[idx.DocumentID]
	if !ok {
		return nil, fmt.Errorf("文档不存在: %s", idx.DocumentID)
	}

	return doc, nil
}

// CreateDocumentWithHash 创建带哈希的文档
func (s *InMemoryDedupStore) CreateDocumentWithHash(ctx context.Context, doc *models.Document, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if doc.ID == "" {
		doc.ID = uuid.New().String()
	}

	now := time.Now()
	doc.CreatedAt = now
	doc.UpdatedAt = now

	// 检查哈希是否已存在
	if _, exists := s.hashIndex[hash]; exists {
		return fmt.Errorf("文档已存在，哈希: %s", hash)
	}

	// 保存文档
	s.documents[doc.ID] = doc

	// 创建哈希索引
	s.hashIndex[hash] = &HashIndex{
		Hash:        hash,
		DocumentID:  doc.ID,
		ContentSize: int64(len(doc.Content)),
		CreatedAt:   now,
		Source:      doc.Source,
	}
	s.docToHash[doc.ID] = hash

	return nil
}

// UpdateHashIndex 更新哈希索引
func (s *InMemoryDedupStore) UpdateHashIndex(ctx context.Context, docID, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, ok := s.documents[docID]
	if !ok {
		return fmt.Errorf("文档不存在: %s", docID)
	}

	// 删除旧哈希索引
	if oldHash, exists := s.docToHash[docID]; exists {
		delete(s.hashIndex, oldHash)
	}

	// 创建新哈希索引
	s.hashIndex[hash] = &HashIndex{
		Hash:        hash,
		DocumentID:  docID,
		ContentSize: int64(len(doc.Content)),
		CreatedAt:   time.Now(),
		Source:      doc.Source,
	}
	s.docToHash[docID] = hash

	return nil
}

// DeleteDocument 删除文档
func (s *InMemoryDedupStore) DeleteDocument(ctx context.Context, docID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 删除哈希索引
	if hash, exists := s.docToHash[docID]; exists {
		delete(s.hashIndex, hash)
		delete(s.docToHash, docID)
	}

	// 删除文档
	delete(s.documents, docID)

	return nil
}

// GetDocument 获取文档
func (s *InMemoryDedupStore) GetDocument(ctx context.Context, docID string) (*models.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	doc, ok := s.documents[docID]
	if !ok {
		return nil, fmt.Errorf("文档不存在: %s", docID)
	}
	return doc, nil
}

// ListDocuments 列出文档
func (s *InMemoryDedupStore) ListDocuments(ctx context.Context, offset, limit int) ([]*models.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var docs []*models.Document
	for _, doc := range s.documents {
		docs = append(docs, doc)
	}

	if offset >= len(docs) {
		return []*models.Document{}, nil
	}

	end := offset + limit
	if end > len(docs) {
		end = len(docs)
	}

	return docs[offset:end], nil
}

// GetStats 获取统计信息
func (s *InMemoryDedupStore) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"document_count": len(s.documents),
		"hash_count":     len(s.hashIndex),
	}
}

// PostgreSQLDedupStore PostgreSQL去重存储
type PostgreSQLDedupStore struct {
	db *sql.DB
}

// NewPostgreSQLDedupStore 创建PostgreSQL去重存储
func NewPostgreSQLDedupStore(db *sql.DB) *PostgreSQLDedupStore {
	return &PostgreSQLDedupStore{db: db}
}

// InitSchema 初始化数据库表结构
func (s *PostgreSQLDedupStore) InitSchema(ctx context.Context) error {
	// 创建文档表（增加content_hash字段）
	docTable := `
	CREATE TABLE IF NOT EXISTS documents (
		id VARCHAR(36) PRIMARY KEY,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		content_hash VARCHAR(64) NOT NULL,
		source VARCHAR(512) NOT NULL,
		doc_type VARCHAR(20) NOT NULL,
		metadata JSONB DEFAULT '{}',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
		version INTEGER NOT NULL DEFAULT 1,
		status VARCHAR(20) NOT NULL DEFAULT 'pending'
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_content_hash ON documents(content_hash);
	CREATE INDEX IF NOT EXISTS idx_documents_source ON documents(source);
	CREATE INDEX IF NOT EXISTS idx_documents_status ON documents(status);
	`

	if _, err := s.db.ExecContext(ctx, docTable); err != nil {
		return fmt.Errorf("创建文档表失败: %w", err)
	}

	// 创建哈希索引表
	hashTable := `
	CREATE TABLE IF NOT EXISTS hash_index (
		hash VARCHAR(64) PRIMARY KEY,
		document_id VARCHAR(36) NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
		content_size BIGINT NOT NULL,
		source VARCHAR(512),
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_hash_index_document_id ON hash_index(document_id);
	`

	if _, err := s.db.ExecContext(ctx, hashTable); err != nil {
		return fmt.Errorf("创建哈希索引表失败: %w", err)
	}

	return nil
}

// ExistsByHash 检查哈希是否存在
func (s *PostgreSQLDedupStore) ExistsByHash(ctx context.Context, hash string) (bool, *models.Document, error) {
	query := `
		SELECT d.id, d.title, d.content, d.source, d.doc_type, d.metadata, d.created_at, d.updated_at, d.version, d.status
		FROM documents d
		INNER JOIN hash_index h ON d.id = h.document_id
		WHERE h.hash = $1
	`

	doc := &models.Document{}
	var metadataJSON []byte

	err := s.db.QueryRowContext(ctx, query, hash).Scan(
		&doc.ID, &doc.Title, &doc.Content, &doc.Source, &doc.DocType,
		&metadataJSON, &doc.CreatedAt, &doc.UpdatedAt, &doc.Version, &doc.Status,
	)

	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("查询失败: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &doc.Metadata); err != nil {
		return false, nil, fmt.Errorf("解析元数据失败: %w", err)
	}

	return true, doc, nil
}

// GetByHash 通过哈希获取文档
func (s *PostgreSQLDedupStore) GetByHash(ctx context.Context, hash string) (*models.Document, error) {
	exists, doc, err := s.ExistsByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("哈希不存在: %s", hash)
	}
	return doc, nil
}

// CreateDocumentWithHash 创建带哈希的文档
func (s *PostgreSQLDedupStore) CreateDocumentWithHash(ctx context.Context, doc *models.Document, hash string) error {
	if doc.ID == "" {
		doc.ID = uuid.New().String()
	}

	now := time.Now()
	doc.CreatedAt = now
	doc.UpdatedAt = now

	metadataJSON, err := json.Marshal(doc.Metadata)
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 插入文档
	docQuery := `
		INSERT INTO documents (id, title, content, content_hash, source, doc_type, metadata, created_at, updated_at, version, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err = tx.ExecContext(ctx, docQuery,
		doc.ID, doc.Title, doc.Content, hash, doc.Source, doc.DocType,
		metadataJSON, doc.CreatedAt, doc.UpdatedAt, doc.Version, doc.Status,
	)
	if err != nil {
		return fmt.Errorf("插入文档失败: %w", err)
	}

	// 插入哈希索引
	hashQuery := `
		INSERT INTO hash_index (hash, document_id, content_size, source, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err = tx.ExecContext(ctx, hashQuery, hash, doc.ID, len(doc.Content), doc.Source, now)
	if err != nil {
		return fmt.Errorf("插入哈希索引失败: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// UpdateHashIndex 更新哈希索引
func (s *PostgreSQLDedupStore) UpdateHashIndex(ctx context.Context, docID, hash string) error {
	query := `
		INSERT INTO hash_index (hash, document_id, content_size, source, created_at)
		VALUES ($1, $2, (SELECT LENGTH(content) FROM documents WHERE id = $2), (SELECT source FROM documents WHERE id = $2), NOW())
		ON CONFLICT (hash) DO UPDATE SET document_id = $2
	`
	_, err := s.db.ExecContext(ctx, query, hash, docID)
	if err != nil {
		return fmt.Errorf("更新哈希索引失败: %w", err)
	}
	return nil
}

// DeleteDocument 删除文档
func (s *PostgreSQLDedupStore) DeleteDocument(ctx context.Context, docID string) error {
	// 哈希索引会通过CASCADE自动删除
	query := `DELETE FROM documents WHERE id = $1`
	result, err := s.db.ExecContext(ctx, query, docID)
	if err != nil {
		return fmt.Errorf("删除文档失败: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("获取影响行数失败: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("文档不存在: %s", docID)
	}

	return nil
}
