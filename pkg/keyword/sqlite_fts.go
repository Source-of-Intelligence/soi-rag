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

// SQLiteFTSIndex 使用 SQLite FTS5 全文搜索虚拟表实现倒排索引
type SQLiteFTSIndex struct {
	db        *sql.DB
	tableName string
	tokenizer Tokenizer
	mu        sync.RWMutex
}

// NewSQLiteFTSIndex 创建 SQLite FTS5 索引
func NewSQLiteFTSIndex(db *sql.DB, tableName string, tokenizer Tokenizer) *SQLiteFTSIndex {
	if tokenizer == nil {
		tokenizer = &ChineseTokenizer{}
	}
	if tableName == "" {
		tableName = "fts_index"
	}
	return &SQLiteFTSIndex{
		db:        db,
		tableName: tableName,
		tokenizer: tokenizer,
	}
}

// InitSchema 初始化 FTS5 虚拟表结构
func (idx *SQLiteFTSIndex) InitSchema(ctx context.Context) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 创建 FTS5 虚拟表
	// 使用 unicode61 分词器，支持中文
	createTableSQL := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(
			doc_id UNINDEXED,
			content,
			tokenize='unicode61'
		)`, idx.tableName)

	_, err := idx.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create FTS5 table: %w", err)
	}

	return nil
}

// AddDocument 添加文档到索引
func (idx *SQLiteFTSIndex) AddDocument(ctx context.Context, docID, content string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if docID == "" {
		return fmt.Errorf("docID cannot be empty")
	}

	// 先删除已存在的文档
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE doc_id = ?", idx.tableName)
	_, _ = idx.db.ExecContext(ctx, deleteSQL, docID)

	// 插入新文档
	insertSQL := fmt.Sprintf("INSERT INTO %s (doc_id, content) VALUES (?, ?)", idx.tableName)
	_, err := idx.db.ExecContext(ctx, insertSQL, docID, content)
	if err != nil {
		return fmt.Errorf("failed to add document: %w", err)
	}

	return nil
}

// RemoveDocument 从索引中删除文档
func (idx *SQLiteFTSIndex) RemoveDocument(ctx context.Context, docID string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE doc_id = ?", idx.tableName)
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
func (idx *SQLiteFTSIndex) Search(query string, topK int) []*models.RetrievalResult {
	ctx := context.Background()
	return idx.SearchWithContext(ctx, query, topK)
}

// SearchWithContext 带上下文的搜索方法
func (idx *SQLiteFTSIndex) SearchWithContext(ctx context.Context, query string, topK int) []*models.RetrievalResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if query == "" {
		return nil
	}

	// 使用 FTS5 的 MATCH 操作符进行搜索
	// bm25 函数用于计算相关性分数
	searchSQL := fmt.Sprintf(`
		SELECT doc_id, content, bm25(%s) as score
		FROM %s
		WHERE content MATCH ?
		ORDER BY score
		LIMIT ?
	`, idx.tableName, idx.tableName)

	// 处理查询字符串，移除可能导致 FTS5 语法错误的特殊字符
	cleanQuery := idx.cleanFTSQuery(query)

	rows, err := idx.db.QueryContext(ctx, searchSQL, cleanQuery, topK)
	if err != nil {
		// 如果 MATCH 查询失败，尝试使用 LIKE 作为后备方案
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
		// BM25 返回负数，取反得到正分数
		results = append(results, &models.RetrievalResult{
			ID:         docID,
			Content:    content,
			Score:      -score, // BM25 返回负数
			DocumentID: docID,
		})
	}

	return results
}

// cleanFTSQuery 清理 FTS5 查询字符串
func (idx *SQLiteFTSIndex) cleanFTSQuery(query string) string {
	// FTS5 特殊字符: * ^ " ( ) { } [ ] : ; ~
	replacer := strings.NewReplacer(
		"*", "",
		"^", "",
		"(", "",
		")", "",
		"{", "",
		"}", "",
		"[", "",
		"]", "",
		":", "",
		";", "",
		"~", "",
	)
	cleaned := replacer.Replace(query)

	// 分词并用 AND 连接（默认行为）
	tokens := idx.tokenizer.Tokenize(cleaned)
	if len(tokens) == 0 {
		return query
	}

	// 返回空格分隔的查询，FTS5 会隐式使用 AND
	return strings.Join(tokens, " ")
}

// fallbackSearch 后备搜索方法，使用 LIKE
func (idx *SQLiteFTSIndex) fallbackSearch(ctx context.Context, query string, topK int) []*models.RetrievalResult {
	searchSQL := fmt.Sprintf(`
		SELECT doc_id, content
		FROM %s
		WHERE content LIKE ?
		LIMIT ?
	`, idx.tableName)

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

// Close 关闭索引（SQLite 连接由调用方管理）
func (idx *SQLiteFTSIndex) Close() error {
	// SQLite 连接由调用方管理，这里不需要关闭
	return nil
}

// GetStats 获取索引统计信息
func (idx *SQLiteFTSIndex) GetStats(ctx context.Context) (map[string]interface{}, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// 获取文档数量
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s", idx.tableName)
	var docCount int
	err := idx.db.QueryRowContext(ctx, countSQL).Scan(&docCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get document count: %w", err)
	}

	return map[string]interface{}{
		"document_count": docCount,
		"table_name":     idx.tableName,
		"engine":         "sqlite_fts5",
	}, nil
}

// BatchAddDocument 批量添加文档
func (idx *SQLiteFTSIndex) BatchAddDocument(ctx context.Context, docs []struct {
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

	// 删除已存在的文档
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE doc_id = ?", idx.tableName)
	deleteStmt, err := tx.PrepareContext(ctx, deleteSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %w", err)
	}
	defer deleteStmt.Close()

	// 插入新文档
	insertSQL := fmt.Sprintf("INSERT INTO %s (doc_id, content) VALUES (?, ?)", idx.tableName)
	insertStmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer insertStmt.Close()

	for _, doc := range docs {
		if doc.DocID == "" {
			continue
		}
		// 删除已存在的文档
		_, _ = deleteStmt.ExecContext(ctx, doc.DocID)
		// 插入新文档
		if _, err := insertStmt.ExecContext(ctx, doc.DocID, doc.Content); err != nil {
			return fmt.Errorf("failed to insert document %s: %w", doc.DocID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// PhraseSearch 短语搜索
func (idx *SQLiteFTSIndex) PhraseSearch(ctx context.Context, phrase string, topK int) []*models.RetrievalResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// 使用引号进行短语搜索
	quotedPhrase := `"` + phrase + `"`

	searchSQL := fmt.Sprintf(`
		SELECT doc_id, content, bm25(%s) as score
		FROM %s
		WHERE content MATCH ?
		ORDER BY score
		LIMIT ?
	`, idx.tableName, idx.tableName)

	rows, err := idx.db.QueryContext(ctx, searchSQL, quotedPhrase, topK)
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
			Score:      -score,
			DocumentID: docID,
		})
	}

	return results
}

// BooleanSearch 布尔搜索
func (idx *SQLiteFTSIndex) BooleanSearch(ctx context.Context, query BooleanQuery, topK int) []*models.RetrievalResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// 构建 FTS5 查询字符串
	var parts []string

	// Must 条件 (AND)
	for _, term := range query.Must {
		tokens := idx.tokenizer.Tokenize(term)
		parts = append(parts, tokens...)
	}

	// Should 条件 (OR)
	if len(query.Should) > 0 {
		var orParts []string
		for _, term := range query.Should {
			tokens := idx.tokenizer.Tokenize(term)
			orParts = append(orParts, tokens...)
		}
		if len(orParts) > 0 {
			parts = append(parts, "("+strings.Join(orParts, " OR ")+")")
		}
	}

	// MustNot 条件 (NOT)
	for _, term := range query.MustNot {
		tokens := idx.tokenizer.Tokenize(term)
		for _, token := range tokens {
			parts = append(parts, "NOT "+token)
		}
	}

	if len(parts) == 0 {
		return nil
	}

	ftsQuery := strings.Join(parts, " ")

	searchSQL := fmt.Sprintf(`
		SELECT doc_id, content, bm25(%s) as score
		FROM %s
		WHERE content MATCH ?
		ORDER BY score
		LIMIT ?
	`, idx.tableName, idx.tableName)

	rows, err := idx.db.QueryContext(ctx, searchSQL, ftsQuery, topK)
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
			Score:      -score,
			DocumentID: docID,
		})
	}

	return results
}

// SQLiteFTSRetriever 基于 SQLite FTS5 的检索器
type SQLiteFTSRetriever struct {
	index *SQLiteFTSIndex
}

// NewSQLiteFTSRetriever 创建 SQLite FTS5 检索器
func NewSQLiteFTSRetriever(db *sql.DB, tableName string, tokenizer Tokenizer) *SQLiteFTSRetriever {
	return &SQLiteFTSRetriever{
		index: NewSQLiteFTSIndex(db, tableName, tokenizer),
	}
}

// InitSchema 初始化索引结构
func (r *SQLiteFTSRetriever) InitSchema(ctx context.Context) error {
	return r.index.InitSchema(ctx)
}

// IndexChunks 索引分块
func (r *SQLiteFTSRetriever) IndexChunks(ctx context.Context, chunks []*models.Chunk) error {
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
func (r *SQLiteFTSRetriever) Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	results := r.index.SearchWithContext(ctx, query, opts.TopK)
	return &models.SearchResult{
		Total:   len(results),
		Results: results,
	}, nil
}

// DeleteDocument 删除文档
func (r *SQLiteFTSRetriever) DeleteDocument(ctx context.Context, docID string) error {
	return r.index.RemoveDocument(ctx, docID)
}

// AddDocument 添加文档
func (r *SQLiteFTSRetriever) AddDocument(ctx context.Context, docID, content string) error {
	return r.index.AddDocument(ctx, docID, content)
}

// Close 关闭检索器
func (r *SQLiteFTSRetriever) Close() error {
	return r.index.Close()
}

// GetStats 获取统计信息
func (r *SQLiteFTSRetriever) GetStats(ctx context.Context) (map[string]interface{}, error) {
	return r.index.GetStats(ctx)
}

// ensure SQLiteFTSIndex implements Searcher interface
var _ Searcher = (*SQLiteFTSIndex)(nil)

// Searcher 接口定义（如果不存在则添加）
type Searcher interface {
	Search(query string, topK int) []*models.RetrievalResult
}

// FTSIndexer 扩展接口，包含文档管理方法
type FTSIndexer interface {
	Searcher
	AddDocument(ctx context.Context, docID, content string) error
	RemoveDocument(ctx context.Context, docID string) error
	InitSchema(ctx context.Context) error
	Close() error
}

// ensure SQLiteFTSIndex implements FTSIndexer interface
var _ FTSIndexer = (*SQLiteFTSIndex)(nil)

// RankedResult 用于排序的结果结构
type RankedResult struct {
	DocID   string
	Content string
	Score   float64
}

// RankResults 对结果进行排序
func RankResults(results []*models.RetrievalResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
}
