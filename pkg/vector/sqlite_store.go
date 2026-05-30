package vector

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/ragtool/rag/pkg/models"
	_ "modernc.org/sqlite"
)

// SQLiteStore SQLite向量存储
type SQLiteStore struct {
	db   *sql.DB
	dim  int
	path string
	mu   sync.RWMutex
}

// NewSQLiteStore 创建SQLite向量存储
func NewSQLiteStore(path string, dim int) *SQLiteStore {
	return &SQLiteStore{
		path: path,
		dim:  dim,
	}
}

// InitSchema 创建数据库表结构
func (s *SQLiteStore) InitSchema(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	schema := `
	CREATE TABLE IF NOT EXISTS vectors (
		id TEXT PRIMARY KEY,
		vector BLOB NOT NULL,
		metadata TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_vectors_id ON vectors(id);
	`

	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// Init 初始化数据库连接
func (s *SQLiteStore) Init(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var err error
	s.db, err = sql.Open("sqlite", s.path)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	// 设置连接池参数
	s.db.SetMaxOpenConns(1) // SQLite建议单连接
	s.db.SetMaxIdleConns(1)

	// 测试连接
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	return nil
}

// Insert 插入向量
func (s *SQLiteStore) Insert(ctx context.Context, records []*VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO vectors (id, vector, metadata)
		VALUES (?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("准备语句失败: %w", err)
	}
	defer stmt.Close()

	for _, record := range records {
		if record.ID == "" {
			record.ID = uuid.New().String()
		}
		if len(record.Vector) != s.dim {
			return fmt.Errorf("向量维度不匹配: expected %d, got %d", s.dim, len(record.Vector))
		}

		// 将向量序列化为BLOB
		vectorBytes := vectorToBytes(record.Vector)

		// 将metadata序列化为JSON
		var metadataBytes []byte
		if record.Metadata != nil {
			metadataBytes, err = json.Marshal(record.Metadata)
			if err != nil {
				return fmt.Errorf("序列化元数据失败: %w", err)
			}
		}

		_, err = stmt.ExecContext(ctx, record.ID, vectorBytes, metadataBytes)
		if err != nil {
			return fmt.Errorf("插入向量失败: %w", err)
		}
	}

	return tx.Commit()
}

// Search 搜索相似向量（暴力搜索）
func (s *SQLiteStore) Search(ctx context.Context, queryVector []float32, topK int, filters map[string]interface{}) ([]*models.RetrievalResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(queryVector) != s.dim {
		return nil, fmt.Errorf("查询向量维度不匹配: expected %d, got %d", s.dim, len(queryVector))
	}

	// 查询所有向量
	rows, err := s.db.QueryContext(ctx, `SELECT id, vector, metadata FROM vectors`)
	if err != nil {
		return nil, fmt.Errorf("查询向量失败: %w", err)
	}
	defer rows.Close()

	type scoredResult struct {
		record *VectorRecord
		score  float32
	}

	var results []scoredResult

	for rows.Next() {
		var id string
		var vectorBytes []byte
		var metadataBytes []byte

		if err := rows.Scan(&id, &vectorBytes, &metadataBytes); err != nil {
			return nil, fmt.Errorf("扫描行失败: %w", err)
		}

		// 反序列化向量
		vector := bytesToVector(vectorBytes)

		// 反序列化元数据
		var metadata map[string]interface{}
		if len(metadataBytes) > 0 {
			if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
				metadata = make(map[string]interface{})
			}
		} else {
			metadata = make(map[string]interface{})
		}

		record := &VectorRecord{
			ID:       id,
			Vector:   vector,
			Metadata: metadata,
		}

		// 应用过滤器
		if !matchFilters(record.Metadata, filters) {
			continue
		}

		// 计算余弦相似度
		similarity := CosineSimilarity(queryVector, record.Vector)
		results = append(results, scoredResult{
			record: record,
			score:  similarity,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历行失败: %w", err)
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
func (s *SQLiteStore) Delete(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(ids) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `DELETE FROM vectors WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("准备语句失败: %w", err)
	}
	defer stmt.Close()

	for _, id := range ids {
		_, err = stmt.ExecContext(ctx, id)
		if err != nil {
			return fmt.Errorf("删除向量失败: %w", err)
		}
	}

	return tx.Commit()
}

// DeleteByFilter 根据过滤器删除
func (s *SQLiteStore) DeleteByFilter(ctx context.Context, filter map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(filter) == 0 {
		// 删除所有向量
		_, err := s.db.ExecContext(ctx, `DELETE FROM vectors`)
		return err
	}

	// 查询所有向量并过滤删除
	rows, err := s.db.QueryContext(ctx, `SELECT id, metadata FROM vectors`)
	if err != nil {
		return fmt.Errorf("查询向量失败: %w", err)
	}
	defer rows.Close()

	var idsToDelete []string
	for rows.Next() {
		var id string
		var metadataBytes []byte

		if err := rows.Scan(&id, &metadataBytes); err != nil {
			return fmt.Errorf("扫描行失败: %w", err)
		}

		var metadata map[string]interface{}
		if len(metadataBytes) > 0 {
			if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
				metadata = make(map[string]interface{})
			}
		} else {
			metadata = make(map[string]interface{})
		}

		if matchFilters(metadata, filter) {
			idsToDelete = append(idsToDelete, id)
		}
	}

	if len(idsToDelete) == 0 {
		return nil
	}

	return s.Delete(ctx, idsToDelete)
}

// Close 关闭数据库连接
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// vectorToBytes 将float32切片转换为字节切片
func vectorToBytes(v []float32) []byte {
	// 每个float32占4字节
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		// 使用小端序
		bits := uint32(f)
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}
	return buf
}

// bytesToVector 将字节切片转换为float32切片
func bytesToVector(b []byte) []float32 {
	// 每个float32占4字节
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		bits := uint32(b[i*4]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16 | uint32(b[i*4+3])<<24
		v[i] = float32(bits)
	}
	return v
}
