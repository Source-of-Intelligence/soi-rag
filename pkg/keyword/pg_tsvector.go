package keyword

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
)

// PGTSVectorIndex 使用 PostgreSQL tsvector/tsquery 实现倒排索引
type PGTSVectorIndex struct {
	db        *sql.DB
	tableName string
	tokenizer Tokenizer
	mu        sync.RWMutex
}

// NewPGTSVectorIndex 创建 PostgreSQL tsvector 索引
func NewPGTSVectorIndex(db *sql.DB, tableName string, tokenizer Tokenizer) *PGTSVectorIndex {
	if tokenizer == nil {
		tokenizer = &ChineseTokenizer{}
	}
	if tableName == "" {
		tableName = "tsvector_index"
	}
	return &PGTSVectorIndex{
		db:        db,
		tableName: tableName,
		tokenizer: tokenizer,
	}
}

// InitSchema 初始化 tsvector 表结构
func (idx *PGTSVectorIndex) InitSchema(ctx context.Context) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 创建主表
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id SERIAL PRIMARY KEY,
			doc_id VARCHAR(255) UNIQUE NOT NULL,
			content TEXT NOT NULL,
			content_tsv tsvector,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`, idx.tableName)

	_, err := idx.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// 创建 GIN 索引以加速 tsvector 搜索
	createIndexSQL := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_tsv_idx ON %s USING GIN(content_tsv)`,
		idx.tableName, idx.tableName)

	_, err = idx.db.ExecContext(ctx, createIndexSQL)
	if err != nil {
		return fmt.Errorf("failed to create GIN index: %w", err)
	}

	// 创建 doc_id 索引
	createDocIDIndexSQL := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_doc_id_idx ON %s(doc_id)`,
		idx.tableName, idx.tableName)

	_, err = idx.db.ExecContext(ctx, createDocIDIndexSQL)
	if err != nil {
		return fmt.Errorf("failed to create doc_id index: %w", err)
	}

	// 创建更新 tsvector 的触发器函数
	triggerFuncSQL := fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION %s_tsv_trigger() RETURNS trigger AS $$
		BEGIN
			NEW.content_tsv := to_tsvector('simple', COALESCE(NEW.content, ''));
			RETURN NEW;
		END
		$$ LANGUAGE plpgsql`,
		idx.tableName)

	_, err = idx.db.ExecContext(ctx, triggerFuncSQL)
	if err != nil {
		return fmt.Errorf("failed to create trigger function: %w", err)
	}

	// 创建触发器（如果不存在）
	triggerSQL := fmt.Sprintf(`
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_trigger WHERE tgname = '%s_tsv_update'
			) THEN
				CREATE TRIGGER %s_tsv_update
				BEFORE INSERT OR UPDATE ON %s
				FOR EACH ROW EXECUTE FUNCTION %s_tsv_trigger();
			END IF;
		END $$`,
		idx.tableName, idx.tableName, idx.tableName, idx.tableName)

	_, err = idx.db.ExecContext(ctx, triggerSQL)
	if err != nil {
		return fmt.Errorf("failed to create trigger: %w", err)
	}

	return nil
}

// AddDocument 添加文档到索引
func (idx *PGTSVectorIndex) AddDocument(ctx context.Context, docID, content string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if docID == "" {
		return fmt.Errorf("docID cannot be empty")
	}

	// 使用 UPSERT (INSERT ... ON CONFLICT DO UPDATE)
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (doc_id, content, content_tsv)
		VALUES ($1, $2, to_tsvector('simple', $2))
		ON CONFLICT (doc_id) DO UPDATE SET
			content = EXCLUDED.content,
			content_tsv = to_tsvector('simple', EXCLUDED.content),
			created_at = CURRENT_TIMESTAMP`,
		idx.tableName)

	_, err := idx.db.ExecContext(ctx, insertSQL, docID, content)
	if err != nil {
		return fmt.Errorf("failed to add document: %w", err)
	}

	return nil
}

// RemoveDocument 从索引中删除文档
func (idx *PGTSVectorIndex) RemoveDocument(ctx context.Context, docID string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE doc_id = $1", idx.tableName)
	result, err := idx.db.ExecContext(ctx, deleteSQL, docID)
	if err != nil {
		return fmt.Errorf("failed to remove document: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("document not found: %s", docID)
	}

	return nil
}

// Search 执行全文搜索
func (idx *PGTSVectorIndex) Search(query string, topK int) []*models.RetrievalResult {
	ctx := context.Background()
	return idx.SearchWithContext(ctx, query, topK)
}

// SearchWithContext 带上下文的搜索方法
func (idx *PGTSVectorIndex) SearchWithContext(ctx context.Context, query string, topK int) []*models.RetrievalResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if query == "" {
		return nil
	}

	// 使用 ts_rank_cd 计算相关性分数
	// plainto_tsquery 将自然语言查询转换为 tsquery
	searchSQL := fmt.Sprintf(`
		SELECT doc_id, content, ts_rank_cd(content_tsv, plainto_tsquery('simple', $1)) as score
		FROM %s
		WHERE content_tsv @@ plainto_tsquery('simple', $1)
		ORDER BY score DESC
		LIMIT $2`,
		idx.tableName)

	rows, err := idx.db.QueryContext(ctx, searchSQL, query, topK)
	if err != nil {
		// 如果 tsquery 搜索失败，使用 ILIKE 作为后备
		return idx.fallbackSearch(ctx, query, topK)
	}
	defer rows.Close()

	var results []*models.RetrievalResult
	for rows.Next() {
		var docID, content string
		var score float64
		if err := rows.Scan(&docID, &content, &score); err != nil {
			continue
		}
		results = append(results, &models.RetrievalResult{
			ID:         docID,
			Content:    content,
			Score:      score,
			DocumentID: docID,
		})
	}

	return results
}

// fallbackSearch 后备搜索方法，使用 ILIKE
func (idx *PGTSVectorIndex) fallbackSearch(ctx context.Context, query string, topK int) []*models.RetrievalResult {
	searchSQL := fmt.Sprintf(`
		SELECT doc_id, content
		FROM %s
		WHERE content ILIKE $1
		LIMIT $2`,
		idx.tableName)

	likeQuery := "%" + query + "%"
	rows, err := idx.db.QueryContext(ctx, searchSQL, likeQuery, topK)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []*models.RetrievalResult
	for rows.Next() {
		var docID, content string
		if err := rows.Scan(&docID, &content); err != nil {
			continue
		}
		results = append(results, &models.RetrievalResult{
			ID:         docID,
			Content:    content,
			Score:      1.0,
			DocumentID: docID,
		})
	}

	return results
}

// Close 关闭索引（PostgreSQL 连接由调用方管理）
func (idx *PGTSVectorIndex) Close() error {
	// PostgreSQL 连接由调用方管理，这里不需要关闭
	return nil
}

// GetStats 获取索引统计信息
func (idx *PGTSVectorIndex) GetStats(ctx context.Context) (map[string]interface{}, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// 获取文档数量
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s", idx.tableName)
	var docCount int
	err := idx.db.QueryRowContext(ctx, countSQL).Scan(&docCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get document count: %w", err)
	}

	// 获取表大小
	sizeSQL := fmt.Sprintf("SELECT pg_total_relation_size('%s')", idx.tableName)
	var tableSize int64
	_ = idx.db.QueryRowContext(ctx, sizeSQL).Scan(&tableSize) // 忽略错误

	return map[string]interface{}{
		"document_count": docCount,
		"table_name":     idx.tableName,
		"table_size":     tableSize,
		"engine":         "pg_tsvector",
	}, nil
}

// BatchAddDocument 批量添加文档
func (idx *PGTSVectorIndex) BatchAddDocument(ctx context.Context, docs []struct {
	DocID   string
	Content string
}) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (doc_id, content, content_tsv)
		VALUES ($1, $2, to_tsvector('simple', $2))
		ON CONFLICT (doc_id) DO UPDATE SET
			content = EXCLUDED.content,
			content_tsv = to_tsvector('simple', EXCLUDED.content),
			created_at = CURRENT_TIMESTAMP`,
		idx.tableName)

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, doc := range docs {
		if doc.DocID == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, doc.DocID, doc.Content); err != nil {
			return fmt.Errorf("failed to insert document %s: %w", doc.DocID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// PhraseSearch 短语搜索
func (idx *PGTSVectorIndex) PhraseSearch(ctx context.Context, phrase string, topK int) []*models.RetrievalResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// phraseto_tsquery 用于短语搜索
	searchSQL := fmt.Sprintf(`
		SELECT doc_id, content, ts_rank_cd(content_tsv, phraseto_tsquery('simple', $1)) as score
		FROM %s
		WHERE content_tsv @@ phraseto_tsquery('simple', $1)
		ORDER BY score DESC
		LIMIT $2`,
		idx.tableName)

	rows, err := idx.db.QueryContext(ctx, searchSQL, phrase, topK)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []*models.RetrievalResult
	for rows.Next() {
		var docID, content string
		var score float64
		if err := rows.Scan(&docID, &content, &score); err != nil {
			continue
		}
		results = append(results, &models.RetrievalResult{
			ID:         docID,
			Content:    content,
			Score:      score,
			DocumentID: docID,
		})
	}

	return results
}

// BooleanSearch 布尔搜索
func (idx *PGTSVectorIndex) BooleanSearch(ctx context.Context, query BooleanQuery, topK int) []*models.RetrievalResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// 构建 tsquery 字符串
	var tsqueryParts []string

	// Must 条件 (AND)
	for _, term := range query.Must {
		tokens := idx.tokenizer.Tokenize(term)
		if len(tokens) > 0 {
			tsqueryParts = append(tsqueryParts, strings.Join(tokens, " & "))
		}
	}

	// Should 条件 (OR)
	if len(query.Should) > 0 {
		var orParts []string
		for _, term := range query.Should {
			tokens := idx.tokenizer.Tokenize(term)
			if len(tokens) > 0 {
				orParts = append(orParts, strings.Join(tokens, " | "))
			}
		}
		if len(orParts) > 0 {
			tsqueryParts = append(tsqueryParts, "("+strings.Join(orParts, " | ")+")")
		}
	}

	// MustNot 条件 (NOT)
	for _, term := range query.MustNot {
		tokens := idx.tokenizer.Tokenize(term)
		for _, token := range tokens {
			tsqueryParts = append(tsqueryParts, "!"+token)
		}
	}

	if len(tsqueryParts) == 0 {
		return nil
	}

	tsquery := strings.Join(tsqueryParts, " & ")

	searchSQL := fmt.Sprintf(`
		SELECT doc_id, content, ts_rank_cd(content_tsv, to_tsquery('simple', $1)) as score
		FROM %s
		WHERE content_tsv @@ to_tsquery('simple', $1)
		ORDER BY score DESC
		LIMIT $2`,
		idx.tableName)

	rows, err := idx.db.QueryContext(ctx, searchSQL, tsquery, topK)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []*models.RetrievalResult
	for rows.Next() {
		var docID, content string
		var score float64
		if err := rows.Scan(&docID, &content, &score); err != nil {
			continue
		}
		results = append(results, &models.RetrievalResult{
			ID:         docID,
			Content:    content,
			Score:      score,
			DocumentID: docID,
		})
	}

	return results
}

// PrefixSearch 前缀搜索
func (idx *PGTSVectorIndex) PrefixSearch(ctx context.Context, prefix string, topK int) []*models.RetrievalResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// 使用 ILIKE 进行前缀搜索
	searchSQL := fmt.Sprintf(`
		SELECT doc_id, content
		FROM %s
		WHERE content ILIKE $1
		ORDER BY doc_id
		LIMIT $2`,
		idx.tableName)

	likeQuery := prefix + "%"
	rows, err := idx.db.QueryContext(ctx, searchSQL, likeQuery, topK)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []*models.RetrievalResult
	for rows.Next() {
		var docID, content string
		if err := rows.Scan(&docID, &content); err != nil {
			continue
		}
		results = append(results, &models.RetrievalResult{
			ID:         docID,
			Content:    content,
			Score:      1.0,
			DocumentID: docID,
		})
	}

	return results
}

// TrigramSimilaritySearch 三元组相似度搜索（需要 pg_trgm 扩展）
func (idx *PGTSVectorIndex) TrigramSimilaritySearch(ctx context.Context, query string, threshold float64, topK int) []*models.RetrievalResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// 使用 pg_trgm 的相似度搜索
	searchSQL := fmt.Sprintf(`
		SELECT doc_id, content, similarity(content, $1) as score
		FROM %s
		WHERE content %% $1
		ORDER BY score DESC
		LIMIT $2`,
		idx.tableName)

	rows, err := idx.db.QueryContext(ctx, searchSQL, query, topK)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []*models.RetrievalResult
	for rows.Next() {
		var docID, content string
		var score float64
		if err := rows.Scan(&docID, &content, &score); err != nil {
			continue
		}
		if score >= threshold {
			results = append(results, &models.RetrievalResult{
				ID:         docID,
				Content:    content,
				Score:      score,
				DocumentID: docID,
			})
		}
	}

	return results
}

// PGTSVectorRetriever 基于 PostgreSQL tsvector 的检索器
type PGTSVectorRetriever struct {
	index *PGTSVectorIndex
}

// NewPGTSVectorRetriever 创建 PostgreSQL tsvector 检索器
func NewPGTSVectorRetriever(db *sql.DB, tableName string, tokenizer Tokenizer) *PGTSVectorRetriever {
	return &PGTSVectorRetriever{
		index: NewPGTSVectorIndex(db, tableName, tokenizer),
	}
}

// InitSchema 初始化索引结构
func (r *PGTSVectorRetriever) InitSchema(ctx context.Context) error {
	return r.index.InitSchema(ctx)
}

// IndexChunks 索引分块
func (r *PGTSVectorRetriever) IndexChunks(ctx context.Context, chunks []*models.Chunk) error {
	docs := make([]struct {
		DocID   string
		Content string
	}, len(chunks))
	for i, chunk := range chunks {
		docs[i].DocID = chunk.ID
		docs[i].Content = chunk.Content
	}
	return r.index.BatchAddDocument(ctx, docs)
}

// Search 搜索
func (r *PGTSVectorRetriever) Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	results := r.index.SearchWithContext(ctx, query, opts.TopK)
	return &models.SearchResult{
		Total:   len(results),
		Results: results,
	}, nil
}

// DeleteDocument 删除文档
func (r *PGTSVectorRetriever) DeleteDocument(ctx context.Context, docID string) error {
	return r.index.RemoveDocument(ctx, docID)
}

// AddDocument 添加文档
func (r *PGTSVectorRetriever) AddDocument(ctx context.Context, docID, content string) error {
	return r.index.AddDocument(ctx, docID, content)
}

// Close 关闭检索器
func (r *PGTSVectorRetriever) Close() error {
	return r.index.Close()
}

// GetStats 获取统计信息
func (r *PGTSVectorRetriever) GetStats(ctx context.Context) (map[string]interface{}, error) {
	return r.index.GetStats(ctx)
}

// ensure PGTSVectorIndex implements Searcher interface
var _ Searcher = (*PGTSVectorIndex)(nil)

// ensure PGTSVectorIndex implements FTSIndexer interface
var _ FTSIndexer = (*PGTSVectorIndex)(nil)

// PGSearchOptions PostgreSQL 搜索选项
type PGSearchOptions struct {
	TopK             int
	UseTrigram       bool    // 是否使用三元组搜索
	SimilarityThresh float64 // 相似度阈值
	QueryType        string  // 查询类型: "plain", "phrase", "websearch"
}

// AdvancedSearch 高级搜索方法
func (idx *PGTSVectorIndex) AdvancedSearch(ctx context.Context, query string, opts PGSearchOptions) []*models.RetrievalResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if query == "" {
		return nil
	}

	topK := opts.TopK
	if topK <= 0 {
		topK = 10
	}

	var searchSQL string
	var args []interface{}

	switch opts.QueryType {
	case "phrase":
		// 短语搜索
		searchSQL = fmt.Sprintf(`
			SELECT doc_id, content, ts_rank_cd(content_tsv, phraseto_tsquery('simple', $1)) as score
			FROM %s
			WHERE content_tsv @@ phraseto_tsquery('simple', $1)
			ORDER BY score DESC
			LIMIT $2`,
			idx.tableName)
		args = []interface{}{query, topK}

	case "websearch":
		// Web 风格搜索（支持 AND, OR, NOT, 引号等）
		searchSQL = fmt.Sprintf(`
			SELECT doc_id, content, ts_rank_cd(content_tsv, websearch_to_tsquery('simple', $1)) as score
			FROM %s
			WHERE content_tsv @@ websearch_to_tsquery('simple', $1)
			ORDER BY score DESC
			LIMIT $2`,
			idx.tableName)
		args = []interface{}{query, topK}

	default:
		// 普通搜索
		searchSQL = fmt.Sprintf(`
			SELECT doc_id, content, ts_rank_cd(content_tsv, plainto_tsquery('simple', $1)) as score
			FROM %s
			WHERE content_tsv @@ plainto_tsquery('simple', $1)
			ORDER BY score DESC
			LIMIT $2`,
			idx.tableName)
		args = []interface{}{query, topK}
	}

	rows, err := idx.db.QueryContext(ctx, searchSQL, args...)
	if err != nil {
		return idx.fallbackSearch(ctx, query, topK)
	}
	defer rows.Close()

	var results []*models.RetrievalResult
	for rows.Next() {
		var docID, content string
		var score float64
		if err := rows.Scan(&docID, &content, &score); err != nil {
			continue
		}
		results = append(results, &models.RetrievalResult{
			ID:         docID,
			Content:    content,
			Score:      score,
			DocumentID: docID,
		})
	}

	// 按分数排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// RebuildIndex 重建索引（更新所有 tsvector）
func (idx *PGTSVectorIndex) RebuildIndex(ctx context.Context) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	updateSQL := fmt.Sprintf(`
		UPDATE %s
		SET content_tsv = to_tsvector('simple', content)`,
		idx.tableName)

	_, err := idx.db.ExecContext(ctx, updateSQL)
	if err != nil {
		return fmt.Errorf("failed to rebuild index: %w", err)
	}

	return nil
}

// EnableTrigramExtension 启用 pg_trgm 扩展
func (idx *PGTSVectorIndex) EnableTrigramExtension(ctx context.Context) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 创建 pg_trgm 扩展
	_, err := idx.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm")
	if err != nil {
		return fmt.Errorf("failed to create pg_trgm extension: %w", err)
	}

	// 创建三元组索引
	createTrgmIdxSQL := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_content_trgm_idx ON %s USING GIN(content gin_trgm_ops)`,
		idx.tableName, idx.tableName)

	_, err = idx.db.ExecContext(ctx, createTrgmIdxSQL)
	if err != nil {
		return fmt.Errorf("failed to create trigram index: %w", err)
	}

	return nil
}
