package pageindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
)

// SQLiteStore SQLite存储实现
// 适用于本地单机部署，无需外部数据库服务
type SQLiteStore struct {
	db     *sql.DB
	dbPath string
}

// SQLiteStoreConfig SQLite存储配置
type SQLiteStoreConfig struct {
	DBPath string // 数据库文件路径，如 "rag.db"、":memory:"（内存模式）
}

// NewSQLiteStore 创建SQLite存储
func NewSQLiteStore(config SQLiteStoreConfig) (*SQLiteStore, error) {
	if config.DBPath == "" {
		config.DBPath = "rag.db"
	}

	dsn := config.DBPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开SQLite数据库失败: %w", err)
	}

	// 设置连接池参数（SQLite为单文件锁，不宜过大）
	db.SetMaxOpenConns(1) // SQLite写操作需要单连接保证WAL一致性
	db.SetMaxIdleConns(1)

	// 验证连接
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接SQLite数据库失败: %w", err)
	}

	store := &SQLiteStore{
		db:     db,
		dbPath: config.DBPath,
	}

	return store, nil
}

// InitSchema 初始化数据库表结构
func (s *SQLiteStore) InitSchema(ctx context.Context) error {
	// SQLite使用TEXT存储JSON，datetime('now')替代NOW()
	// 外键约束通过DSN中的PRAGMA foreign_keys(ON)启用

	docTable := `
	CREATE TABLE IF NOT EXISTS documents (
		id         TEXT PRIMARY KEY,
		title      TEXT NOT NULL,
		content    TEXT NOT NULL,
		source     TEXT NOT NULL DEFAULT '',
		doc_type   TEXT NOT NULL DEFAULT 'txt',
		metadata   TEXT NOT NULL DEFAULT '{}',
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
		version    INTEGER NOT NULL DEFAULT 1,
		status     TEXT NOT NULL DEFAULT 'pending'
	);
	CREATE INDEX IF NOT EXISTS idx_documents_source ON documents(source);
	CREATE INDEX IF NOT EXISTS idx_documents_status ON documents(status);
	CREATE INDEX IF NOT EXISTS idx_documents_doc_type ON documents(doc_type);
	CREATE INDEX IF NOT EXISTS idx_documents_created_at ON documents(created_at);
	`

	if _, err := s.db.ExecContext(ctx, docTable); err != nil {
		return fmt.Errorf("创建文档表失败: %w", err)
	}

	chunkTable := `
	CREATE TABLE IF NOT EXISTS chunks (
		id           TEXT PRIMARY KEY,
		document_id  TEXT NOT NULL,
		content      TEXT NOT NULL,
		start_pos    INTEGER NOT NULL DEFAULT 0,
		end_pos      INTEGER NOT NULL DEFAULT 0,
		chunk_index  INTEGER NOT NULL DEFAULT 0,
		token_count  INTEGER NOT NULL DEFAULT 0,
		page_number  INTEGER NOT NULL DEFAULT 0,
		heading_path TEXT NOT NULL DEFAULT '[]',
		created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
		FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_chunks_document_id ON chunks(document_id);
	CREATE INDEX IF NOT EXISTS idx_chunks_chunk_index ON chunks(document_id, chunk_index);
	`

	if _, err := s.db.ExecContext(ctx, chunkTable); err != nil {
		return fmt.Errorf("创建分块表失败: %w", err)
	}

	return nil
}

// Close 关闭数据库连接
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB 返回底层*sql.DB（供其他模块复用同一连接）
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

// CreateDocument 创建文档
func (s *SQLiteStore) CreateDocument(ctx context.Context, doc *models.Document) error {
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

	// SQLite使用 ? 占位符
	query := `
		INSERT INTO documents (id, title, content, source, doc_type, metadata, created_at, updated_at, version, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, query,
		doc.ID, doc.Title, doc.Content, doc.Source, string(doc.DocType),
		string(metadataJSON), formatTime(now), formatTime(now), doc.Version, string(doc.Status),
	)

	if err != nil {
		return fmt.Errorf("插入文档失败: %w", err)
	}

	return nil
}

// GetDocument 获取文档
func (s *SQLiteStore) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	query := `
		SELECT id, title, content, source, doc_type, metadata, created_at, updated_at, version, status
		FROM documents
		WHERE id = ?
	`

	doc := &models.Document{}
	var metadataJSON string
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&doc.ID, &doc.Title, &doc.Content, &doc.Source, &doc.DocType,
		&metadataJSON, &createdAt, &updatedAt, &doc.Version, &doc.Status,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("文档不存在: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询文档失败: %w", err)
	}

	if err := json.Unmarshal([]byte(metadataJSON), &doc.Metadata); err != nil {
		return nil, fmt.Errorf("解析元数据失败: %w", err)
	}

	doc.CreatedAt, _ = parseTime(createdAt)
	doc.UpdatedAt, _ = parseTime(updatedAt)

	return doc, nil
}

// UpdateDocument 更新文档
func (s *SQLiteStore) UpdateDocument(ctx context.Context, doc *models.Document) error {
	doc.UpdatedAt = time.Now()
	doc.Version++

	metadataJSON, err := json.Marshal(doc.Metadata)
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}

	query := `
		UPDATE documents
		SET title = ?, content = ?, source = ?, doc_type = ?, metadata = ?,
		    updated_at = ?, version = ?, status = ?
		WHERE id = ?
	`

	result, err := s.db.ExecContext(ctx, query,
		doc.Title, doc.Content, doc.Source, string(doc.DocType),
		string(metadataJSON), formatTime(doc.UpdatedAt), doc.Version, string(doc.Status),
		doc.ID,
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
func (s *SQLiteStore) DeleteDocument(ctx context.Context, id string) error {
	query := `DELETE FROM documents WHERE id = ?`

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
func (s *SQLiteStore) ListDocuments(ctx context.Context, offset, limit int) ([]*models.Document, error) {
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
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("查询文档列表失败: %w", err)
	}
	defer rows.Close()

	return scanDocuments(rows)
}

// CreateChunks 创建分块
func (s *SQLiteStore) CreateChunks(ctx context.Context, chunks []*models.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO chunks (id, document_id, content, start_pos, end_pos, chunk_index, token_count, page_number, heading_path, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("准备语句失败: %w", err)
	}
	defer stmt.Close()

	now := formatTime(time.Now())
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
			chunk.ChunkIndex, chunk.TokenCount, chunk.PageNumber, string(headingPathJSON), now,
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
func (s *SQLiteStore) GetChunksByDocument(ctx context.Context, docID string) ([]*models.Chunk, error) {
	query := `
		SELECT id, document_id, content, start_pos, end_pos, chunk_index, token_count, page_number, heading_path
		FROM chunks
		WHERE document_id = ?
		ORDER BY chunk_index
	`

	rows, err := s.db.QueryContext(ctx, query, docID)
	if err != nil {
		return nil, fmt.Errorf("查询分块失败: %w", err)
	}
	defer rows.Close()

	return scanChunks(rows)
}

// DeleteChunksByDocument 删除文档的分块
func (s *SQLiteStore) DeleteChunksByDocument(ctx context.Context, docID string) error {
	query := `DELETE FROM chunks WHERE document_id = ?`

	_, err := s.db.ExecContext(ctx, query, docID)
	if err != nil {
		return fmt.Errorf("删除分块失败: %w", err)
	}

	return nil
}

// Search 搜索（SQLite的LIKE默认对ASCII不区分大小写）
func (s *SQLiteStore) Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	// SQLite LIKE对ASCII字母不区分大小写，对中文需要用COLLATE NOCASE
	sqlQuery := `
		SELECT d.id, d.title, d.content, d.source, d.doc_type, d.metadata, d.created_at, d.updated_at, d.version, d.status,
			c.id as chunk_id, c.content as chunk_content
		FROM documents d
		LEFT JOIN chunks c ON d.id = c.document_id
		WHERE d.content LIKE ? COLLATE NOCASE OR d.title LIKE ? COLLATE NOCASE
		ORDER BY d.created_at DESC
		LIMIT ?
	`

	searchPattern := "%" + query + "%"
	if opts.TopK <= 0 {
		opts.TopK = 10
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery, searchPattern, searchPattern, opts.TopK)
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
		var metadataJSON string
		var chunkID, chunkContent sql.NullString
		var createdAt, updatedAt string

		err := rows.Scan(
			&doc.ID, &doc.Title, &doc.Content, &doc.Source, &doc.DocType,
			&metadataJSON, &createdAt, &updatedAt, &doc.Version, &doc.Status,
			&chunkID, &chunkContent,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描搜索结果失败: %w", err)
		}

		if err := json.Unmarshal([]byte(metadataJSON), &doc.Metadata); err != nil {
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

// --- 辅助函数 ---

// scanDocuments 从rows扫描文档列表
func scanDocuments(rows *sql.Rows) ([]*models.Document, error) {
	var docs []*models.Document

	for rows.Next() {
		doc := &models.Document{}
		var metadataJSON string
		var createdAt, updatedAt string

		err := rows.Scan(
			&doc.ID, &doc.Title, &doc.Content, &doc.Source, &doc.DocType,
			&metadataJSON, &createdAt, &updatedAt, &doc.Version, &doc.Status,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描文档行失败: %w", err)
		}

		if err := json.Unmarshal([]byte(metadataJSON), &doc.Metadata); err != nil {
			return nil, fmt.Errorf("解析元数据失败: %w", err)
		}

		doc.CreatedAt, _ = parseTime(createdAt)
		doc.UpdatedAt, _ = parseTime(updatedAt)

		docs = append(docs, doc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历文档行失败: %w", err)
	}

	return docs, nil
}

// scanChunks 从rows扫描分块列表
func scanChunks(rows *sql.Rows) ([]*models.Chunk, error) {
	var chunks []*models.Chunk

	for rows.Next() {
		chunk := &models.Chunk{}
		var headingPathJSON string

		err := rows.Scan(
			&chunk.ID, &chunk.DocumentID, &chunk.Content, &chunk.StartPos, &chunk.EndPos,
			&chunk.ChunkIndex, &chunk.TokenCount, &chunk.PageNumber, &headingPathJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("扫描分块行失败: %w", err)
		}

		if err := json.Unmarshal([]byte(headingPathJSON), &chunk.HeadingPath); err != nil {
			return nil, fmt.Errorf("解析标题路径失败: %w", err)
		}

		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历分块行失败: %w", err)
	}

	return chunks, nil
}

// formatTime 格式化时间为SQLite兼容的字符串
func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// parseTime 解析SQLite时间字符串
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02 15:04:05", s)
}
