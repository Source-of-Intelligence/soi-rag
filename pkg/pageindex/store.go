package pageindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
)

// Store 文档存储接口
type Store interface {
	// 文档操作
	CreateDocument(ctx context.Context, doc *models.Document) error
	GetDocument(ctx context.Context, id string) (*models.Document, error)
	UpdateDocument(ctx context.Context, doc *models.Document) error
	DeleteDocument(ctx context.Context, id string) error
	ListDocuments(ctx context.Context, offset, limit int) ([]*models.Document, error)

	// 分块操作
	CreateChunks(ctx context.Context, chunks []*models.Chunk) error
	GetChunksByDocument(ctx context.Context, docID string) ([]*models.Chunk, error)
	DeleteChunksByDocument(ctx context.Context, docID string) error

	// 搜索
	Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error)
}

// PostgreSQLStore PostgreSQL存储实现
type PostgreSQLStore struct {
	db *sql.DB
}

// NewPostgreSQLStore 创建PostgreSQL存储
func NewPostgreSQLStore(db *sql.DB) *PostgreSQLStore {
	return &PostgreSQLStore{db: db}
}

// InitSchema 初始化数据库表结构
func (s *PostgreSQLStore) InitSchema(ctx context.Context) error {
	// 创建文档表
	docTable := `
	CREATE TABLE IF NOT EXISTS documents (
		id VARCHAR(36) PRIMARY KEY,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		source VARCHAR(512) NOT NULL,
		doc_type VARCHAR(20) NOT NULL,
		metadata JSONB DEFAULT '{}',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
		version INTEGER NOT NULL DEFAULT 1,
		status VARCHAR(20) NOT NULL DEFAULT 'pending'
	);
	CREATE INDEX IF NOT EXISTS idx_documents_source ON documents(source);
	CREATE INDEX IF NOT EXISTS idx_documents_status ON documents(status);
	CREATE INDEX IF NOT EXISTS idx_documents_doc_type ON documents(doc_type);
	`

	if _, err := s.db.ExecContext(ctx, docTable); err != nil {
		return fmt.Errorf("创建文档表失败: %w", err)
	}

	// 创建分块表
	chunkTable := `
	CREATE TABLE IF NOT EXISTS chunks (
		id VARCHAR(36) PRIMARY KEY,
		document_id VARCHAR(36) NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		start_pos INTEGER NOT NULL,
		end_pos INTEGER NOT NULL,
		chunk_index INTEGER NOT NULL,
		token_count INTEGER NOT NULL DEFAULT 0,
		page_number INTEGER NOT NULL DEFAULT 0,
		heading_path JSONB DEFAULT '[]',
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_chunks_document_id ON chunks(document_id);
	CREATE INDEX IF NOT EXISTS idx_chunks_chunk_index ON chunks(document_id, chunk_index);
	`

	if _, err := s.db.ExecContext(ctx, chunkTable); err != nil {
		return fmt.Errorf("创建分块表失败: %w", err)
	}

	return nil
}

// CreateDocument 创建文档
func (s *PostgreSQLStore) CreateDocument(ctx context.Context, doc *models.Document) error {
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

	query := `
		INSERT INTO documents (id, title, content, source, doc_type, metadata, created_at, updated_at, version, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err = s.db.ExecContext(ctx, query,
		doc.ID, doc.Title, doc.Content, doc.Source, doc.DocType,
		metadataJSON, doc.CreatedAt, doc.UpdatedAt, doc.Version, doc.Status,
	)

	if err != nil {
		return fmt.Errorf("插入文档失败: %w", err)
	}

	return nil
}

// GetDocument 获取文档
func (s *PostgreSQLStore) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	query := `
		SELECT id, title, content, source, doc_type, metadata, created_at, updated_at, version, status
		FROM documents
		WHERE id = $1
	`

	doc := &models.Document{}
	var metadataJSON []byte

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&doc.ID, &doc.Title, &doc.Content, &doc.Source, &doc.DocType,
		&metadataJSON, &doc.CreatedAt, &doc.UpdatedAt, &doc.Version, &doc.Status,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("文档不存在: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询文档失败: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &doc.Metadata); err != nil {
		return nil, fmt.Errorf("解析元数据失败: %w", err)
	}

	return doc, nil
}

// UpdateDocument 更新文档
func (s *PostgreSQLStore) UpdateDocument(ctx context.Context, doc *models.Document) error {
	doc.UpdatedAt = time.Now()
	doc.Version++

	metadataJSON, err := json.Marshal(doc.Metadata)
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}

	query := `
		UPDATE documents
		SET title = $2, content = $3, source = $4, doc_type = $5, metadata = $6,
		    updated_at = $7, version = $8, status = $9
		WHERE id = $1
	`

	result, err := s.db.ExecContext(ctx, query,
		doc.ID, doc.Title, doc.Content, doc.Source, doc.DocType,
		metadataJSON, doc.UpdatedAt, doc.Version, doc.Status,
	)

	if err != nil {
		return fmt.Errorf("更新文档失败: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("获取影响行数失败: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("文档不存在: %s", doc.ID)
	}

	return nil
}

// DeleteDocument 删除文档
func (s *PostgreSQLStore) DeleteDocument(ctx context.Context, id string) error {
	query := `DELETE FROM documents WHERE id = $1`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("删除文档失败: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("获取影响行数失败: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("文档不存在: %s", id)
	}

	return nil
}

// ListDocuments 列出文档
func (s *PostgreSQLStore) ListDocuments(ctx context.Context, offset, limit int) ([]*models.Document, error) {
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, title, content, source, doc_type, metadata, created_at, updated_at, version, status
		FROM documents
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("查询文档列表失败: %w", err)
	}
	defer rows.Close()

	var docs []*models.Document

	for rows.Next() {
		doc := &models.Document{}
		var metadataJSON []byte

		err := rows.Scan(
			&doc.ID, &doc.Title, &doc.Content, &doc.Source, &doc.DocType,
			&metadataJSON, &doc.CreatedAt, &doc.UpdatedAt, &doc.Version, &doc.Status,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描文档行失败: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &doc.Metadata); err != nil {
			return nil, fmt.Errorf("解析元数据失败: %w", err)
		}

		docs = append(docs, doc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历文档行失败: %w", err)
	}

	return docs, nil
}

// CreateChunks 创建分块
func (s *PostgreSQLStore) CreateChunks(ctx context.Context, chunks []*models.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO chunks (id, document_id, content, start_pos, end_pos, chunk_index, token_count, page_number, heading_path)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("准备语句失败: %w", err)
	}
	defer stmt.Close()

	for _, chunk := range chunks {
		if chunk.ID == "" {
			chunk.ID = uuid.New().String()
		}

		headingPathJSON, err := json.Marshal(chunk.HeadingPath)
		if err != nil {
			return fmt.Errorf("序列化标题路径失败: %w", err)
		}

		_, err = stmt.ExecContext(ctx,
			chunk.ID, chunk.DocumentID, chunk.Content, chunk.StartPos, chunk.EndPos,
			chunk.ChunkIndex, chunk.TokenCount, chunk.PageNumber, headingPathJSON,
		)
		if err != nil {
			return fmt.Errorf("插入分块失败: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// GetChunksByDocument 获取文档的分块
func (s *PostgreSQLStore) GetChunksByDocument(ctx context.Context, docID string) ([]*models.Chunk, error) {
	query := `
		SELECT id, document_id, content, start_pos, end_pos, chunk_index, token_count, page_number, heading_path
		FROM chunks
		WHERE document_id = $1
		ORDER BY chunk_index
	`

	rows, err := s.db.QueryContext(ctx, query, docID)
	if err != nil {
		return nil, fmt.Errorf("查询分块失败: %w", err)
	}
	defer rows.Close()

	var chunks []*models.Chunk

	for rows.Next() {
		chunk := &models.Chunk{}
		var headingPathJSON []byte

		err := rows.Scan(
			&chunk.ID, &chunk.DocumentID, &chunk.Content, &chunk.StartPos, &chunk.EndPos,
			&chunk.ChunkIndex, &chunk.TokenCount, &chunk.PageNumber, &headingPathJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描分块行失败: %w", err)
		}

		if err := json.Unmarshal(headingPathJSON, &chunk.HeadingPath); err != nil {
			return nil, fmt.Errorf("解析标题路径失败: %w", err)
		}

		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历分块行失败: %w", err)
	}

	return chunks, nil
}

// DeleteChunksByDocument 删除文档的分块
func (s *PostgreSQLStore) DeleteChunksByDocument(ctx context.Context, docID string) error {
	query := `DELETE FROM chunks WHERE document_id = $1`

	_, err := s.db.ExecContext(ctx, query, docID)
	if err != nil {
		return fmt.Errorf("删除分块失败: %w", err)
	}

	return nil
}

// Search 简单搜索（基于LIKE）
func (s *PostgreSQLStore) Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	sqlQuery := `
		SELECT d.id, d.title, d.content, d.source, d.doc_type, d.metadata, d.created_at, d.updated_at, d.version, d.status,
			c.id as chunk_id, c.content as chunk_content
		FROM documents d
		LEFT JOIN chunks c ON d.id = c.document_id
		WHERE d.content ILIKE $1 OR c.content ILIKE $1
		ORDER BY d.created_at DESC
		LIMIT $2
	`

	searchPattern := "%" + query + "%"
	if opts.TopK <= 0 {
		opts.TopK = 10
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery, searchPattern, opts.TopK)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}
	defer rows.Close()

	result := &models.SearchResult{
		Results: []*models.RetrievalResult{},
	}

	docMap := make(map[string]*models.RetrievalResult)

	for rows.Next() {
		var doc models.Document
		var metadataJSON []byte
		var chunkID, chunkContent sql.NullString

		err := rows.Scan(
			&doc.ID, &doc.Title, &doc.Content, &doc.Source, &doc.DocType,
			&metadataJSON, &doc.CreatedAt, &doc.UpdatedAt, &doc.Version, &doc.Status,
			&chunkID, &chunkContent,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描搜索结果失败: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &doc.Metadata); err != nil {
			return nil, fmt.Errorf("解析元数据失败: %w", err)
		}

		// 去重
		if _, exists := docMap[doc.ID]; !exists {
			item := &models.RetrievalResult{
				ID:         doc.ID,
				Content:    doc.Content,
				Source:     doc.Source,
				DocumentID: doc.ID,
				Metadata:   doc.Metadata,
			}
			if chunkID.Valid && chunkContent.Valid {
				item.ChunkID = chunkID.String
				item.Content = chunkContent.String
			}
			docMap[doc.ID] = item
			result.Results = append(result.Results, item)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历搜索结果失败: %w", err)
	}

	result.Total = len(result.Results)
	return result, nil
}

// MemoryStore 内存存储实现（用于测试）
type MemoryStore struct {
	documents map[string]*models.Document
	chunks    map[string][]*models.Chunk
}

// NewMemoryStore 创建内存存储
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		documents: make(map[string]*models.Document),
		chunks:    make(map[string][]*models.Chunk),
	}
}

// CreateDocument 创建文档
func (s *MemoryStore) CreateDocument(ctx context.Context, doc *models.Document) error {
	if doc.ID == "" {
		doc.ID = uuid.New().String()
	}
	doc.CreatedAt = time.Now()
	doc.UpdatedAt = time.Now()
	s.documents[doc.ID] = doc
	return nil
}

// GetDocument 获取文档
func (s *MemoryStore) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	doc, ok := s.documents[id]
	if !ok {
		return nil, fmt.Errorf("文档不存在: %s", id)
	}
	return doc, nil
}

// UpdateDocument 更新文档
func (s *MemoryStore) UpdateDocument(ctx context.Context, doc *models.Document) error {
	if _, ok := s.documents[doc.ID]; !ok {
		return fmt.Errorf("文档不存在: %s", doc.ID)
	}
	doc.UpdatedAt = time.Now()
	doc.Version++
	s.documents[doc.ID] = doc
	return nil
}

// DeleteDocument 删除文档
func (s *MemoryStore) DeleteDocument(ctx context.Context, id string) error {
	if _, ok := s.documents[id]; !ok {
		return fmt.Errorf("文档不存在: %s", id)
	}
	delete(s.documents, id)
	delete(s.chunks, id)
	return nil
}

// ListDocuments 列出文档
func (s *MemoryStore) ListDocuments(ctx context.Context, offset, limit int) ([]*models.Document, error) {
	var docs []*models.Document
	for _, doc := range s.documents {
		docs = append(docs, doc)
	}

	// 按创建时间倒序排列，保证分页结果稳定
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].CreatedAt.After(docs[j].CreatedAt)
	})

	// 简单的分页
	if offset >= len(docs) {
		return []*models.Document{}, nil
	}

	end := offset + limit
	if end > len(docs) {
		end = len(docs)
	}

	return docs[offset:end], nil
}

// CreateChunks 创建分块
func (s *MemoryStore) CreateChunks(ctx context.Context, chunks []*models.Chunk) error {
	for _, chunk := range chunks {
		if chunk.ID == "" {
			chunk.ID = uuid.New().String()
		}
		s.chunks[chunk.DocumentID] = append(s.chunks[chunk.DocumentID], chunk)
	}
	return nil
}

// GetChunksByDocument 获取文档的分块
func (s *MemoryStore) GetChunksByDocument(ctx context.Context, docID string) ([]*models.Chunk, error) {
	return s.chunks[docID], nil
}

// DeleteChunksByDocument 删除文档的分块
func (s *MemoryStore) DeleteChunksByDocument(ctx context.Context, docID string) error {
	delete(s.chunks, docID)
	return nil
}

// Search 搜索
func (s *MemoryStore) Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	var results []*models.RetrievalResult

	for _, doc := range s.documents {
		if contains(doc.Content, query) || contains(doc.Title, query) {
			results = append(results, &models.RetrievalResult{
				ID:         doc.ID,
				Content:    doc.Content,
				Source:     doc.Source,
				DocumentID: doc.ID,
				Metadata:   doc.Metadata,
			})
		}
	}

	if opts.TopK > 0 && len(results) > opts.TopK {
		results = results[:opts.TopK]
	}

	return &models.SearchResult{
		Total:   len(results),
		Results: results,
	}, nil
}

// contains 检查字符串是否包含子串（不区分大小写）
func contains(s, substr string) bool {
	if len(s) == 0 || len(substr) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
