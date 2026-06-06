package vector

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// PGStore PostgreSQL向量存储（使用pgvector扩展）
type PGStore struct {
	db        *sql.DB
	dim       int
	connStr   string
	tableName string
	mu        sync.RWMutex
}

// NewPGStore 创建PostgreSQL向量存储
func NewPGStore(connStr string, dim int, tableName string) *PGStore {
	if tableName == "" {
		tableName = "vectors"
	}
	return &PGStore{
		connStr:   connStr,
		dim:       dim,
		tableName: tableName,
	}
}

// InitSchema 创建数据库表结构
func (s *PGStore) InitSchema(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 启用pgvector扩展
	_, err := s.db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS vector`)
	if err != nil {
		return fmt.Errorf("启用pgvector扩展失败: %w", err)
	}

	// 创建表
	schema := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		id TEXT PRIMARY KEY,
		embedding vector(%d) NOT NULL,
		metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_%s_embedding ON %s USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
	CREATE INDEX IF NOT EXISTS idx_%s_id ON %s(id);
	`, s.tableName, s.dim, s.tableName, s.tableName, s.tableName, s.tableName)

	_, err = s.db.ExecContext(ctx, schema)
	if err != nil {
		// 如果ivfflat索引创建失败，尝试创建基础索引
		fallbackSchema := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			embedding vector(%d) NOT NULL,
			metadata JSONB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_%s_id ON %s(id);
		`, s.tableName, s.dim, s.tableName, s.tableName)

		_, fallbackErr := s.db.ExecContext(ctx, fallbackSchema)
		if fallbackErr != nil {
			return fmt.Errorf("创建表失败: %w (fallback error: %w)", err, fallbackErr)
		}
	}

	return nil
}

// Init 初始化数据库连接
func (s *PGStore) Init(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var err error
	s.db, err = sql.Open("postgres", s.connStr)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	// 设置连接池参数
	s.db.SetMaxOpenConns(10)
	s.db.SetMaxIdleConns(5)

	// 测试连接
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	return nil
}

// Insert 插入向量
func (s *PGStore) Insert(ctx context.Context, records []*VectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}
	defer tx.Rollback()

	query := fmt.Sprintf(`
		INSERT INTO %s (id, embedding, metadata)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET embedding = $2, metadata = $3
	`, s.tableName)

	stmt, err := tx.PrepareContext(ctx, query)
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

		// 将向量转换为pgvector格式字符串
		vectorStr := vectorToPGVector(record.Vector)

		// 将metadata序列化为JSON
		var metadataBytes []byte
		if record.Metadata != nil {
			metadataBytes, err = json.Marshal(record.Metadata)
			if err != nil {
				return fmt.Errorf("序列化元数据失败: %w", err)
			}
		}

		_, err = stmt.ExecContext(ctx, record.ID, vectorStr, metadataBytes)
		if err != nil {
			return fmt.Errorf("插入向量失败: %w", err)
		}
	}

	return tx.Commit()
}

// Search 搜索相似向量（使用pgvector的余弦相似度搜索）
func (s *PGStore) Search(ctx context.Context, queryVector []float32, topK int, filters map[string]interface{}) ([]*models.RetrievalResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(queryVector) != s.dim {
		return nil, fmt.Errorf("查询向量维度不匹配: expected %d, got %d", s.dim, len(queryVector))
	}

	vectorStr := vectorToPGVector(queryVector)

	// 如果有过滤器，需要先获取所有匹配的向量，然后在内存中排序
	// 因为pgvector的索引搜索和JSONB过滤不能很好地结合使用
	if len(filters) > 0 {
		return s.searchWithFilters(ctx, queryVector, topK, filters)
	}

	// 使用pgvector进行向量搜索
	query := fmt.Sprintf(`
		SELECT id, embedding, metadata, 1 - (embedding <=> $1) as similarity
		FROM %s
		ORDER BY embedding <=> $1
		LIMIT $2
	`, s.tableName)

	rows, err := s.db.QueryContext(ctx, query, vectorStr, topK)
	if err != nil {
		return nil, fmt.Errorf("查询向量失败: %w", err)
	}
	defer rows.Close()

	var retrievalResults []*models.RetrievalResult

	for rows.Next() {
		var id string
		var embeddingStr string
		var metadataBytes []byte
		var similarity float64

		if err := rows.Scan(&id, &embeddingStr, &metadataBytes, &similarity); err != nil {
			return nil, fmt.Errorf("扫描行失败: %w", err)
		}

		// 反序列化元数据
		var metadata map[string]interface{}
		if len(metadataBytes) > 0 {
			if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
				metadata = make(map[string]interface{})
			}
		} else {
			metadata = make(map[string]interface{})
		}

		retrievalResults = append(retrievalResults, &models.RetrievalResult{
			ID:       id,
			Score:    similarity,
			Metadata: metadata,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历行失败: %w", err)
	}

	return retrievalResults, nil
}

// searchWithFilters 带过滤器的搜索（在内存中进行过滤和排序）
func (s *PGStore) searchWithFilters(ctx context.Context, queryVector []float32, topK int, filters map[string]interface{}) ([]*models.RetrievalResult, error) {
	_ = vectorToPGVector(queryVector) // 预留给将来可能的数据库端向量计算

	// 构建过滤条件
	whereClause, filterArgs := s.buildFilterClause(filters)

	query := fmt.Sprintf(`
		SELECT id, embedding, metadata
		FROM %s
		WHERE %s
	`, s.tableName, whereClause)

	rows, err := s.db.QueryContext(ctx, query, filterArgs...)
	if err != nil {
		return nil, fmt.Errorf("查询向量失败: %w", err)
	}
	defer rows.Close()

	type scoredResult struct {
		id       string
		score    float32
		metadata map[string]interface{}
	}

	var results []scoredResult

	for rows.Next() {
		var id string
		var embeddingStr string
		var metadataBytes []byte

		if err := rows.Scan(&id, &embeddingStr, &metadataBytes); err != nil {
			return nil, fmt.Errorf("扫描行失败: %w", err)
		}

		// 解析向量
		vector := pgVectorToSlice(embeddingStr)

		// 反序列化元数据
		var metadata map[string]interface{}
		if len(metadataBytes) > 0 {
			if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
				metadata = make(map[string]interface{})
			}
		} else {
			metadata = make(map[string]interface{})
		}

		// 计算余弦相似度
		similarity := CosineSimilarity(queryVector, vector)
		results = append(results, scoredResult{
			id:       id,
			score:    similarity,
			metadata: metadata,
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
			ID:       r.id,
			Score:    float64(r.score),
			Metadata: r.metadata,
		})
	}

	return retrievalResults, nil
}

// buildFilterClause 构建过滤条件子句
func (s *PGStore) buildFilterClause(filters map[string]interface{}) (string, []interface{}) {
	if len(filters) == 0 {
		return "1=1", nil
	}

	var conditions []string
	var args []interface{}
	i := 1

	for key, value := range filters {
		conditions = append(conditions, fmt.Sprintf("metadata->>$%d = $%d", i, i+1))
		args = append(args, key, value)
		i += 2
	}

	return strings.Join(conditions, " AND "), args
}

// Delete 删除向量
func (s *PGStore) Delete(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`DELETE FROM %s WHERE id IN (%s)`, s.tableName, strings.Join(placeholders, ", "))

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("删除向量失败: %w", err)
	}

	return nil
}

// DeleteByFilter 根据过滤器删除
func (s *PGStore) DeleteByFilter(ctx context.Context, filter map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(filter) == 0 {
		// 删除所有向量
		query := fmt.Sprintf(`DELETE FROM %s`, s.tableName)
		_, err := s.db.ExecContext(ctx, query)
		return err
	}

	whereClause, filterArgs := s.buildFilterClause(filter)
	query := fmt.Sprintf(`DELETE FROM %s WHERE %s`, s.tableName, whereClause)

	_, err := s.db.ExecContext(ctx, query, filterArgs...)
	if err != nil {
		return fmt.Errorf("删除向量失败: %w", err)
	}

	return nil
}

// Close 关闭数据库连接
func (s *PGStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// vectorToPGVector 将float32切片转换为pgvector格式字符串
func vectorToPGVector(v []float32) string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, f := range v {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%f", f))
	}
	sb.WriteString("]")
	return sb.String()
}

// pgVectorToSlice 将pgvector格式字符串转换为float32切片
func pgVectorToSlice(s string) []float32 {
	// 移除方括号
	s = strings.Trim(s, "[]")
	if s == "" {
		return nil
	}

	// 分割字符串
	parts := strings.Split(s, ",")
	result := make([]float32, len(parts))

	for i, part := range parts {
		var f float32
		fmt.Sscanf(strings.TrimSpace(part), "%f", &f)
		result[i] = f
	}

	return result
}
