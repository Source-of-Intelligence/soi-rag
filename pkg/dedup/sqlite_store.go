package dedup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
)

// SQLiteDedupStore SQLite去重存储实现
// 复用PageIndex的SQLite连接，共享同一个数据库文件
type SQLiteDedupStore struct {
	db *sql.DB
}

// NewSQLiteDedupStore 创建SQLite去重存储
// 接受一个已有的*sql.DB（SQLite连接），共享连接避免锁冲突
func NewSQLiteDedupStore(db *sql.DB) *SQLiteDedupStore {
	return &SQLiteDedupStore{db: db}
}

// InitSchema 初始化去重相关的表结构
func (s *SQLiteDedupStore) InitSchema(ctx context.Context) error {
	// 创建哈希索引表
	hashTable := `
	CREATE TABLE IF NOT EXISTS hash_index (
		hash         TEXT PRIMARY KEY,
		document_id  TEXT NOT NULL,
		content_size INTEGER NOT NULL DEFAULT 0,
		source       TEXT NOT NULL DEFAULT '',
		created_at   DATETIME NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_hash_index_document_id ON hash_index(document_id);
	`

	if _, err := s.db.ExecContext(ctx, hashTable); err != nil {
		return fmt.Errorf("创建哈希索引表失败: %w", err)
	}

	return nil
}

// ExistsByHash 检查哈希是否存在
func (s *SQLiteDedupStore) ExistsByHash(ctx context.Context, hash string) (bool, *models.Document, error) {
	// 从PageIndex的documents表中查询关联文档
	query := `
		SELECT d.id, d.title, d.content, d.source, d.doc_type, d.metadata, d.created_at, d.updated_at, d.version, d.status
		FROM documents d
		INNER JOIN hash_index h ON d.id = h.document_id
		WHERE h.hash = ?
	`

	doc := &models.Document{}
	var metadataJSON string
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx, query, hash).Scan(
		&doc.ID, &doc.Title, &doc.Content, &doc.Source, &doc.DocType,
		&metadataJSON, &createdAt, &updatedAt, &doc.Version, &doc.Status,
	)

	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("查询失败: %w", err)
	}

	if err := json.Unmarshal([]byte(metadataJSON), &doc.Metadata); err != nil {
		return false, nil, fmt.Errorf("解析元数据失败: %w", err)
	}

	doc.CreatedAt, _ = parseTime(createdAt)
	doc.UpdatedAt, _ = parseTime(updatedAt)

	return true, doc, nil
}

// GetByHash 通过哈希获取文档
func (s *SQLiteDedupStore) GetByHash(ctx context.Context, hash string) (*models.Document, error) {
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
func (s *SQLiteDedupStore) CreateDocumentWithHash(ctx context.Context, doc *models.Document, hash string) error {
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

	// 插入文档到PageIndex的documents表
	docQuery := `
		INSERT OR IGNORE INTO documents (id, title, content, source, doc_type, metadata, created_at, updated_at, version, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err = tx.ExecContext(ctx, docQuery,
		doc.ID, doc.Title, doc.Content, doc.Source, string(doc.DocType),
		string(metadataJSON), formatTime(now), formatTime(now), doc.Version, string(doc.Status),
	)
	if err != nil {
		return fmt.Errorf("插入文档失败: %w", err)
	}

	// 插入哈希索引
	hashQuery := `
		INSERT OR REPLACE INTO hash_index (hash, document_id, content_size, source, created_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err = tx.ExecContext(ctx, hashQuery, hash, doc.ID, int64(len(doc.Content)), doc.Source, formatTime(now))
	if err != nil {
		return fmt.Errorf("插入哈希索引失败: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// UpdateHashIndex 更新哈希索引
func (s *SQLiteDedupStore) UpdateHashIndex(ctx context.Context, docID, hash string) error {
	query := `
		INSERT OR REPLACE INTO hash_index (hash, document_id, content_size, source, created_at)
		VALUES (?, ?, (SELECT LENGTH(content) FROM documents WHERE id = ?), (SELECT source FROM documents WHERE id = ?), datetime('now'))
	`
	_, err := s.db.ExecContext(ctx, query, hash, docID, docID)
	if err != nil {
		return fmt.Errorf("更新哈希索引失败: %w", err)
	}
	return nil
}

// DeleteDocument 删除文档（同时删除哈希索引）
func (s *SQLiteDedupStore) DeleteDocument(ctx context.Context, docID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 删除哈希索引
	if _, err := tx.ExecContext(ctx, `DELETE FROM hash_index WHERE document_id = ?`, docID); err != nil {
		return fmt.Errorf("删除哈希索引失败: %w", err)
	}

	// 删除文档
	result, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, docID)
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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}

	return nil
}

// GetStats 获取统计信息
func (s *SQLiteDedupStore) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var docCount, hashCount int

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&docCount); err != nil {
		return nil, fmt.Errorf("查询文档数失败: %w", err)
	}

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM hash_index`).Scan(&hashCount); err != nil {
		return nil, fmt.Errorf("查询哈希数失败: %w", err)
	}

	stats["document_count"] = docCount
	stats["hash_count"] = hashCount

	return stats, nil
}

// --- 辅助函数 ---

func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02 15:04:05", s)
}
