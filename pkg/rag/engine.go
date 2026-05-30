package rag

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"

	"github.com/ragtool/rag/pkg/cache"
	"github.com/ragtool/rag/pkg/dedup"
	"github.com/ragtool/rag/pkg/hybrid"
	"github.com/ragtool/rag/pkg/keyword"
	"github.com/ragtool/rag/pkg/knowledgegraph"
	"github.com/ragtool/rag/pkg/llm"
	"github.com/ragtool/rag/pkg/models"
	"github.com/ragtool/rag/pkg/pageindex"
	"github.com/ragtool/rag/pkg/rerank"
	"github.com/ragtool/rag/pkg/vector"
)

// StorageType 存储类型
type StorageType string

const (
	StorageMemory   StorageType = "memory"   // 内存存储（默认，用于测试）
	StorageSQLite   StorageType = "sqlite"   // SQLite本地文件存储
	StoragePostgres StorageType = "postgres" // PostgreSQL存储
)

// Engine RAG引擎
type Engine struct {
	pageIndex        pageindex.PageIndex
	vectorRetriever  *vector.VectorRetriever
	keywordRetriever *keyword.KeywordRetriever
	knowledgeGraph   *knowledgegraph.KnowledgeGraph
	hybridRetriever  *hybrid.HybridRetriever
	reranker         rerank.Reranker
	dedupService     *dedup.Service
	llm              llm.LLM             // LLM生成器
	promptTpl        *llm.PromptTemplate // 提示模板
	config           *Config
	closers          []func() error // 需要关闭的资源
	queryCache       cache.Cache    // 查询缓存
}

// Config RAG引擎配置
type Config struct {
	ChunkSize        int
	ChunkOverlap     int
	ChunkStrategy    string
	TopK             int
	UseReranker      bool
	UseHybrid        bool
	UseDedup         bool
	StorageType      StorageType     // 存储类型: memory, sqlite, postgres
	SQLitePath       string          // SQLite数据库文件路径，默认 "rag.db"
	PostgresConfig   *PostgresConfig // PostgreSQL连接配置（StorageType=postgres时使用）
	HybridStrategies []models.RetrievalType
}

// PostgresConfig PostgreSQL连接配置
type PostgresConfig struct {
	Host     string // 数据库主机地址，默认 "localhost"
	Port     int    // 数据库端口，默认 5432
	DBName   string // 数据库名称（必填）
	User     string // 用户名（必填）
	Password string // 密码（必填）
	SSLMode  string // SSL模式，默认 "disable"
	MaxOpen  int    // 最大连接数，默认 10
	MaxIdle  int    // 最大空闲连接数，默认 5
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		ChunkSize:     512,
		ChunkOverlap:  50,
		ChunkStrategy: "recursive",
		TopK:          10,
		UseReranker:   true,
		UseHybrid:     true,
		UseDedup:      true,
		StorageType:   StorageMemory,
		SQLitePath:    "rag.db",
		HybridStrategies: []models.RetrievalType{
			models.RetrievalTypeVector,
			models.RetrievalTypeKeyword,
		},
	}
}

// NewEngine 创建RAG引擎
func NewEngine(config *Config) (*Engine, error) {
	if config == nil {
		config = DefaultConfig()
	}

	var store pageindex.Store
	var dedupStore dedup.DedupStore
	var closers []func() error

	ctx := context.Background()

	// 根据存储类型创建对应的存储后端
	switch config.StorageType {
	case StorageSQLite:
		sqliteStore, err := pageindex.NewSQLiteStore(pageindex.SQLiteStoreConfig{
			DBPath: config.SQLitePath,
		})
		if err != nil {
			return nil, fmt.Errorf("创建SQLite存储失败: %w", err)
		}

		// 初始化表结构
		if err := sqliteStore.InitSchema(ctx); err != nil {
			sqliteStore.Close()
			return nil, fmt.Errorf("初始化SQLite表结构失败: %w", err)
		}

		store = sqliteStore
		closers = append(closers, sqliteStore.Close)

		// 去重存储复用同一个SQLite连接
		sqliteDedup := dedup.NewSQLiteDedupStore(sqliteStore.DB())
		if err := sqliteDedup.InitSchema(ctx); err != nil {
			sqliteStore.Close()
			return nil, fmt.Errorf("初始化SQLite去重表结构失败: %w", err)
		}
		dedupStore = sqliteDedup

	case StoragePostgres:
		if config.PostgresConfig == nil {
			return nil, fmt.Errorf("PostgreSQL存储需要配置 PostgresConfig")
		}
		pgCfg := config.PostgresConfig

		// 设置默认值
		if pgCfg.Host == "" {
			pgCfg.Host = "localhost"
		}
		if pgCfg.Port <= 0 {
			pgCfg.Port = 5432
		}
		if pgCfg.SSLMode == "" {
			pgCfg.SSLMode = "disable"
		}
		if pgCfg.MaxOpen <= 0 {
			pgCfg.MaxOpen = 10
		}
		if pgCfg.MaxIdle <= 0 {
			pgCfg.MaxIdle = 5
		}

		// 构建DSN
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			pgCfg.Host, pgCfg.Port, pgCfg.User, pgCfg.Password, pgCfg.DBName, pgCfg.SSLMode)

		// 连接数据库
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, fmt.Errorf("连接PostgreSQL失败: %w", err)
		}
		db.SetMaxOpenConns(pgCfg.MaxOpen)
		db.SetMaxIdleConns(pgCfg.MaxIdle)

		// 验证连接
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("PostgreSQL连接测试失败: %w", err)
		}

		// 初始化PageIndex存储
		pgStore := pageindex.NewPostgreSQLStore(db)
		if err := pgStore.InitSchema(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("初始化PostgreSQL文档表结构失败: %w", err)
		}
		store = pgStore

		// 初始化去重存储（复用同一个PG连接）
		pgDedup := dedup.NewPostgreSQLDedupStore(db)
		if err := pgDedup.InitSchema(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("初始化PostgreSQL去重表结构失败: %w", err)
		}
		dedupStore = pgDedup

		closers = append(closers, db.Close)

	default: // memory
		store = pageindex.NewMemoryStore()
		dedupStore = dedup.NewInMemoryDedupStore()
	}

	// 创建PageIndex
	piConfig := &pageindex.Config{
		ChunkSize:     config.ChunkSize,
		ChunkOverlap:  config.ChunkOverlap,
		ChunkStrategy: config.ChunkStrategy,
	}
	pi := pageindex.NewPageIndex(store, piConfig)

	// 创建向量检索器
	embedder := vector.NewMockEmbedder(768)
	vectorStore := vector.NewInMemoryVectorStore(768)
	if err := vectorStore.Init(ctx); err != nil {
		closeAll(closers)
		return nil, fmt.Errorf("初始化向量存储失败: %w", err)
	}
	vectorRetriever := vector.NewVectorRetriever(embedder, vectorStore)

	// 创建关键词检索器
	keywordRetriever := keyword.NewKeywordRetriever(nil)

	// 创建知识图谱
	graphStore := knowledgegraph.NewInMemoryGraphStore()
	kg := knowledgegraph.NewKnowledgeGraph(graphStore, nil)

	// 创建混合检索器
	fusionStrategy := hybrid.NewRRFFusion(60)
	hybridRetriever := hybrid.NewHybridRetriever(
		vectorRetriever,
		keywordRetriever,
		kg,
		fusionStrategy,
	)

	// 创建重排序器
	var reranker rerank.Reranker
	if config.UseReranker {
		reranker = rerank.NewCrossEncoderReranker()
	}

	// 创建去重服务
	dedupService := dedup.NewService(dedupStore, config.UseDedup)

	return &Engine{
		pageIndex:        pi,
		vectorRetriever:  vectorRetriever,
		keywordRetriever: keywordRetriever,
		knowledgeGraph:   kg,
		hybridRetriever:  hybridRetriever,
		reranker:         reranker,
		dedupService:     dedupService,
		config:           config,
		closers:          closers,
	}, nil
}

// NewEngineWithSQLite 便捷方法：创建基于SQLite的RAG引擎
func NewEngineWithSQLite(dbPath string) (*Engine, error) {
	config := DefaultConfig()
	config.StorageType = StorageSQLite
	config.SQLitePath = dbPath
	return NewEngine(config)
}

// NewEngineWithPostgres 便捷方法：创建基于PostgreSQL的RAG引擎
func NewEngineWithPostgres(pgCfg *PostgresConfig) (*Engine, error) {
	config := DefaultConfig()
	config.StorageType = StoragePostgres
	config.PostgresConfig = pgCfg
	return NewEngine(config)
}

// NewEngineWithMemory 便捷方法：创建基于内存的RAG引擎
func NewEngineWithMemory() (*Engine, error) {
	config := DefaultConfig()
	config.StorageType = StorageMemory
	return NewEngine(config)
}

// SetEmbedder 设置向量嵌入器（替换默认的 MockEmbedder）
func (e *Engine) SetEmbedder(embedder vector.Embedder) {
	e.vectorRetriever.SetEmbedder(embedder)
}

// SetVectorStore 设置向量存储
func (e *Engine) SetVectorStore(store vector.VectorStore) {
	e.vectorRetriever.SetStore(store)
}

// SetReranker 设置重排序器
func (e *Engine) SetReranker(r rerank.Reranker) {
	e.reranker = r
}

// SetKeywordTokenizer 设置关键词分词器
func (e *Engine) SetKeywordTokenizer(tokenizer keyword.Tokenizer) {
	e.keywordRetriever.SetTokenizer(tokenizer)
}

// AddDocumentFromFile 从文件添加文档（自动检测类型）
func (e *Engine) AddDocumentFromFile(ctx context.Context, filePath string) (*models.Document, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	pm := pageindex.NewParserManager()
	doc, err := pm.ParseByExtension(file, filePath)
	if err != nil {
		return nil, fmt.Errorf("解析文件失败: %w", err)
	}

	result, err := e.AddDocumentWithDedup(ctx, doc)
	if err != nil {
		return nil, err
	}

	if result.IsDuplicate && result.ExistingDoc != nil {
		return result.ExistingDoc, nil
	}
	return doc, nil
}

// BatchAddDocuments 批量添加文档
func (e *Engine) BatchAddDocuments(ctx context.Context, docs []*models.Document) (*BatchResult, error) {
	result := &BatchResult{
		Total:   len(docs),
		Success: 0,
		Failed:  0,
		Errors:  make([]BatchError, 0),
	}

	for i, doc := range docs {
		_, err := e.AddDocumentWithDedup(ctx, doc)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, BatchError{
				Index: i,
				Error: err.Error(),
			})
		} else {
			result.Success++
		}
	}

	return result, nil
}

// BatchResult 批量操作结果
type BatchResult struct {
	Total   int          `json:"total"`
	Success int          `json:"success"`
	Failed  int          `json:"failed"`
	Errors  []BatchError `json:"errors,omitempty"`
}

// BatchError 批量操作错误
type BatchError struct {
	Index int    `json:"index"`
	Error string `json:"error"`
}

// GetDocumentCount 获取文档总数
func (e *Engine) GetDocumentCount(ctx context.Context) (int, error) {
	docs, err := e.pageIndex.ListDocuments(ctx, 0, 1)
	if err != nil {
		return 0, err
	}
	// ListDocuments 返回的是分页结果，需要通过 stats 获取总数
	stats := e.GetStats()
	if ks, ok := stats["keyword_stats"].(map[string]interface{}); ok {
		if count, ok := ks["document_count"].(int); ok {
			return count, nil
		}
	}
	return len(docs), nil
}

// GetLLM 获取当前LLM实例
func (e *Engine) GetLLM() llm.LLM {
	return e.llm
}

// GetConfig 获取引擎配置
func (e *Engine) GetConfig() *Config {
	return e.config
}

// Close 关闭引擎，释放所有资源
func (e *Engine) Close() error {
	return closeAll(e.closers)
}

// closeAll 依次关闭所有资源
func closeAll(closers []func() error) error {
	var firstErr error
	for _, closer := range closers {
		if err := closer(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// AddDocumentResult 添加文档结果
type AddDocumentResult struct {
	Document    *models.Document `json:"document"`
	Hash        string           `json:"hash"`
	IsDuplicate bool             `json:"is_duplicate"`
	Skipped     bool             `json:"skipped"`
	ExistingDoc *models.Document `json:"existing_doc,omitempty"`
}

// AddDocument 添加文档
func (e *Engine) AddDocument(ctx context.Context, doc *models.Document) error {
	result, err := e.AddDocumentWithDedup(ctx, doc)
	if err != nil {
		return err
	}
	if result.Skipped {
		return nil
	}
	return nil
}

// AddDocumentWithDedup 添加文档（带去重检查）
func (e *Engine) AddDocumentWithDedup(ctx context.Context, doc *models.Document) (*AddDocumentResult, error) {
	result := &AddDocumentResult{}

	// 检查去重
	dedupResult, err := e.dedupService.CheckAndDedup(ctx, []byte(doc.Content))
	if err != nil {
		return nil, fmt.Errorf("去重检查失败: %w", err)
	}

	result.Hash = dedupResult.Hash
	result.IsDuplicate = dedupResult.IsDuplicate
	result.Skipped = dedupResult.Skipped
	result.ExistingDoc = dedupResult.ExistingDoc

	// 如果重复，跳过索引
	if dedupResult.IsDuplicate {
		return result, nil
	}

	// 添加到PageIndex
	if err = e.pageIndex.AddDocument(ctx, doc, nil); err != nil {
		return nil, fmt.Errorf("添加文档到PageIndex失败: %w", err)
	}

	result.Document = doc

	// 获取分块
	chunks, err := e.pageIndex.GetChunks(ctx, doc.ID)
	if err != nil {
		return nil, fmt.Errorf("获取文档分块失败: %w", err)
	}

	// 索引到向量存储
	if err = e.vectorRetriever.IndexChunks(ctx, chunks); err != nil {
		return nil, fmt.Errorf("索引向量失败: %w", err)
	}

	// 索引到关键词存储
	if err = e.keywordRetriever.IndexChunks(ctx, chunks); err != nil {
		return nil, fmt.Errorf("索引关键词失败: %w", err)
	}

	// 构建知识图谱
	if err = e.knowledgeGraph.BuildFromDocument(ctx, doc.ID, chunks); err != nil {
		fmt.Printf("构建知识图谱失败: %v\n", err)
	}

	// 添加到去重存储
	if e.config.UseDedup {
		if _, err = e.dedupService.AddDocumentWithDedup(ctx, doc); err != nil {
			// 去重索引失败不阻止主流程（文档已索引到PageIndex）
			fmt.Printf("更新去重索引失败: %v\n", err)
		}
	}

	return result, nil
}

// AddDocumentFromText 从文本添加文档
func (e *Engine) AddDocumentFromText(ctx context.Context, title, content, source string) (*models.Document, error) {
	doc := &models.Document{
		Title:   title,
		Content: content,
		Source:  source,
		DocType: models.DocTypeText,
	}

	result, err := e.AddDocumentWithDedup(ctx, doc)
	if err != nil {
		return nil, err
	}

	if result.IsDuplicate && result.ExistingDoc != nil {
		return result.ExistingDoc, nil
	}

	return doc, nil
}

// CheckDuplicate 检查内容是否重复
func (e *Engine) CheckDuplicate(ctx context.Context, content string) (*dedup.DedupResult, error) {
	return e.dedupService.CheckAndDedup(ctx, []byte(content))
}

// GetDocumentByHash 通过哈希获取文档
func (e *Engine) GetDocumentByHash(ctx context.Context, hash string) (*models.Document, error) {
	return e.dedupService.GetByHash(ctx, hash)
}

// Search 搜索
func (e *Engine) Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	if opts.TopK <= 0 {
		opts.TopK = e.config.TopK
	}

	// 检查缓存
	if e.queryCache != nil {
		cacheKey := e.buildCacheKey(query, opts)
		if cached, hit := e.queryCache.Get(cacheKey); hit {
			if result, ok := cached.(*models.SearchResult); ok {
				return result, nil
			}
		}
	}

	var result *models.SearchResult
	var err error

	if e.config.UseHybrid {
		result, err = e.hybridSearch(ctx, query, opts)
	} else {
		result, err = e.pageIndex.Search(ctx, query, opts)
	}

	if err != nil {
		return nil, err
	}

	// 写入缓存
	if e.queryCache != nil && result != nil {
		cacheKey := e.buildCacheKey(query, opts)
		e.queryCache.Set(cacheKey, result)
	}

	return result, nil
}

// buildCacheKey 构建缓存键
func (e *Engine) buildCacheKey(query string, opts models.SearchOptions) string {
	// 使用SHA256生成唯一键
	h := sha256.New()
	h.Write([]byte(query))
	h.Write([]byte(fmt.Sprintf("%d", opts.TopK)))
	if opts.ScoreThreshold > 0 {
		h.Write([]byte(fmt.Sprintf("%.4f", opts.ScoreThreshold)))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SetCache 设置查询缓存
func (e *Engine) SetCache(c cache.Cache) {
	e.queryCache = c
}

// GetCache 获取查询缓存
func (e *Engine) GetCache() cache.Cache {
	return e.queryCache
}

// GetCacheStats 获取缓存统计信息
func (e *Engine) GetCacheStats() *cache.Stats {
	if e.queryCache == nil {
		return nil
	}
	stats := e.queryCache.Stats()
	return &stats
}

// hybridSearch 混合搜索
func (e *Engine) hybridSearch(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	hybridOpts := models.HybridOptions{
		TopK:         opts.TopK * 2,
		Strategies:   e.config.HybridStrategies,
		FusionMethod: models.FusionMethodRRF,
		RRFK:         60,
	}

	hybridResult, err := e.hybridRetriever.Retrieve(ctx, query, hybridOpts)
	if err != nil {
		return nil, fmt.Errorf("混合检索失败: %w", err)
	}

	if e.config.UseReranker && e.reranker != nil {
		hybridResult.Results, err = e.reranker.Rerank(ctx, query, hybridResult.Results, opts.TopK)
		if err != nil {
			return nil, fmt.Errorf("重排序失败: %w", err)
		}
	}

	return &models.SearchResult{
		Total:   hybridResult.Total,
		Results: hybridResult.Results,
	}, nil
}

// VectorSearch 向量搜索
func (e *Engine) VectorSearch(ctx context.Context, query string, topK int) ([]*models.RetrievalResult, error) {
	return e.vectorRetriever.Retrieve(ctx, query, topK)
}

// KeywordSearch 关键词搜索
func (e *Engine) KeywordSearch(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	return e.keywordRetriever.Search(ctx, query, opts)
}

// GraphSearch 图谱搜索
func (e *Engine) GraphSearch(ctx context.Context, query string, topK int) ([]*models.RetrievalResult, error) {
	return e.knowledgeGraph.GraphBasedRetrieve(ctx, query, topK)
}

// DeleteDocument 删除文档
func (e *Engine) DeleteDocument(ctx context.Context, docID string) error {
	if err := e.vectorRetriever.DeleteDocument(ctx, docID); err != nil {
		return fmt.Errorf("删除向量失败: %w", err)
	}

	if err := e.keywordRetriever.DeleteDocument(ctx, docID); err != nil {
		return fmt.Errorf("删除关键词索引失败: %w", err)
	}

	if err := e.knowledgeGraph.BuildFromDocument(ctx, docID, nil); err != nil {
		return fmt.Errorf("删除知识图谱数据失败: %w", err)
	}

	if err := e.pageIndex.DeleteDocument(ctx, docID); err != nil {
		return fmt.Errorf("删除文档失败: %w", err)
	}

	if e.config.UseDedup {
		if err := e.dedupService.DeleteDocument(ctx, docID); err != nil {
			fmt.Printf("删除去重索引失败: %v\n", err)
		}
	}

	return nil
}

// GetDocument 获取文档
func (e *Engine) GetDocument(ctx context.Context, docID string) (*models.Document, error) {
	return e.pageIndex.GetDocument(ctx, docID)
}

// ListDocuments 列出文档
func (e *Engine) ListDocuments(ctx context.Context, offset, limit int) ([]*models.Document, error) {
	return e.pageIndex.ListDocuments(ctx, offset, limit)
}

// GetStats 获取统计信息
func (e *Engine) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"storage_type":  string(e.config.StorageType),
		"keyword_stats": e.keywordRetriever.GetStats(),
		"dedup_enabled": e.config.UseDedup,
	}

	if e.config.UseDedup {
		stats["dedup_stats"] = e.dedupService.GetStats()
	}

	if e.queryCache != nil {
		stats["cache_stats"] = e.queryCache.Stats()
	}

	return stats
}

// QueryRequest 查询请求
type QueryRequest struct {
	Query         string                 `json:"query"`
	TopK          int                    `json:"top_k"`
	Filters       map[string]interface{} `json:"filters,omitempty"`
	UseRerank     bool                   `json:"use_rerank,omitempty"`
	RetrievalType string                 `json:"retrieval_type,omitempty"`
}

// QueryResponse 查询响应
type QueryResponse struct {
	Query     string                    `json:"query"`
	Results   []*models.RetrievalResult `json:"results"`
	Total     int                       `json:"total"`
	QueryTime int64                     `json:"query_time_ms"`
	Sources   map[string]int            `json:"sources,omitempty"`
}

// Query 执行查询
func (e *Engine) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	if req.TopK <= 0 {
		req.TopK = e.config.TopK
	}

	start := time.Now()

	var results []*models.RetrievalResult
	var err error

	switch req.RetrievalType {
	case "vector":
		results, err = e.VectorSearch(ctx, req.Query, req.TopK)
	case "keyword":
		var searchResult *models.SearchResult
		searchResult, err = e.KeywordSearch(ctx, req.Query, models.SearchOptions{TopK: req.TopK})
		if err == nil {
			results = searchResult.Results
		}
	case "graph":
		results, err = e.GraphSearch(ctx, req.Query, req.TopK)
	default:
		var searchResult *models.SearchResult
		searchResult, err = e.Search(ctx, req.Query, models.SearchOptions{TopK: req.TopK})
		if err == nil {
			results = searchResult.Results
		}
	}

	if err != nil {
		return nil, err
	}

	if req.UseRerank && e.reranker != nil {
		results, err = e.reranker.Rerank(ctx, req.Query, results, req.TopK)
		if err != nil {
			return nil, fmt.Errorf("重排序失败: %w", err)
		}
	}

	queryTime := time.Since(start).Milliseconds()

	return &QueryResponse{
		Query:     req.Query,
		Results:   results,
		Total:     len(results),
		QueryTime: queryTime,
	}, nil
}

// SetDedupEnabled 设置是否启用去重
func (e *Engine) SetDedupEnabled(enabled bool) {
	e.config.UseDedup = enabled
	e.dedupService.SetEnabled(enabled)
}

// IsDedupEnabled 是否启用去重
func (e *Engine) IsDedupEnabled() bool {
	return e.config.UseDedup
}

// SetLLM 设置LLM生成器
func (e *Engine) SetLLM(l llm.LLM) {
	e.llm = l
}

// SetPromptTemplate 设置提示模板
func (e *Engine) SetPromptTemplate(tpl *llm.PromptTemplate) {
	e.promptTpl = tpl
}

// AskRequest 问答请求
type AskRequest struct {
	Question      string               // 用户问题
	TopK          int                  // 检索文档数量
	UseRerank     bool                 // 是否重排序
	RetrievalType string               // 检索类型: vector, keyword, hybrid, graph
	Opts          []llm.GenerateOption // LLM生成选项
}

// AskResponse 问答响应
type AskResponse struct {
	Question   string                    // 用户问题
	Answer     string                    // 生成的回答
	Sources    []*models.RetrievalResult // 参考文档
	QueryTime  int64                     // 检索耗时(ms)
	TotalTime  int64                     // 总耗时(ms)
	TokenUsage *TokenUsage               // Token使用情况（如果可用）
}

// TokenUsage Token使用情况
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Ask RAG问答（检索+生成）
func (e *Engine) Ask(ctx context.Context, question string, opts ...AskOption) (*AskResponse, error) {
	req := &AskRequest{
		Question:      question,
		TopK:          e.config.TopK,
		UseRerank:     e.config.UseReranker,
		RetrievalType: "hybrid",
	}
	for _, opt := range opts {
		opt(req)
	}

	start := time.Now()

	// 1. 检索相关文档
	searchOpts := models.SearchOptions{TopK: req.TopK}
	var sources []*models.RetrievalResult
	var err error

	switch req.RetrievalType {
	case "vector":
		sources, err = e.VectorSearch(ctx, question, req.TopK)
	case "keyword":
		var searchResult *models.SearchResult
		searchResult, err = e.KeywordSearch(ctx, question, searchOpts)
		if searchResult != nil {
			sources = searchResult.Results
		}
	case "graph":
		sources, err = e.GraphSearch(ctx, question, req.TopK)
	default:
		var searchResult *models.SearchResult
		searchResult, err = e.Search(ctx, question, searchOpts)
		if searchResult != nil {
			sources = searchResult.Results
		}
	}

	if err != nil {
		return nil, fmt.Errorf("检索失败: %w", err)
	}

	// 2. 重排序（可选）
	if req.UseRerank && e.reranker != nil {
		sources, err = e.reranker.Rerank(ctx, question, sources, req.TopK)
		if err != nil {
			return nil, fmt.Errorf("重排序失败: %w", err)
		}
	}

	queryTime := time.Since(start).Milliseconds()

	// 3. 检查LLM是否配置
	if e.llm == nil {
		// 未配置LLM，返回检索结果作为回答
		return &AskResponse{
			Question:  question,
			Answer:    e.buildAnswerFromSources(sources),
			Sources:   sources,
			QueryTime: queryTime,
			TotalTime: time.Since(start).Milliseconds(),
		}, nil
	}

	// 4. 构建提示
	tpl := e.promptTpl
	if tpl == nil {
		tpl = llm.DefaultRAGPrompt
	}

	prompt := tpl.BuildPrompt(question, sources)

	// 5. 调用LLM生成回答
	answer, err := e.llm.Generate(ctx, prompt, req.Opts...)
	if err != nil {
		return nil, fmt.Errorf("生成回答失败: %w", err)
	}

	return &AskResponse{
		Question:  question,
		Answer:    answer,
		Sources:   sources,
		QueryTime: queryTime,
		TotalTime: time.Since(start).Milliseconds(),
	}, nil
}

// AskStream RAG流式问答
func (e *Engine) AskStream(ctx context.Context, question string, callback func(string), opts ...AskOption) (*AskResponse, error) {
	req := &AskRequest{
		Question:      question,
		TopK:          e.config.TopK,
		UseRerank:     e.config.UseReranker,
		RetrievalType: "hybrid",
	}
	for _, opt := range opts {
		opt(req)
	}

	start := time.Now()

	// 1. 检索相关文档
	searchOpts := models.SearchOptions{TopK: req.TopK}
	var sources []*models.RetrievalResult
	var err error

	var searchResult *models.SearchResult
	searchResult, err = e.Search(ctx, question, searchOpts)
	if searchResult != nil {
		sources = searchResult.Results
	}

	if err != nil {
		return nil, fmt.Errorf("检索失败: %w", err)
	}

	// 2. 重排序
	if req.UseRerank && e.reranker != nil {
		sources, err = e.reranker.Rerank(ctx, question, sources, req.TopK)
		if err != nil {
			return nil, fmt.Errorf("重排序失败: %w", err)
		}
	}

	queryTime := time.Since(start).Milliseconds()

	// 3. 检查LLM
	if e.llm == nil {
		return &AskResponse{
			Question:  question,
			Answer:    e.buildAnswerFromSources(sources),
			Sources:   sources,
			QueryTime: queryTime,
			TotalTime: time.Since(start).Milliseconds(),
		}, nil
	}

	// 4. 构建提示
	tpl := e.promptTpl
	if tpl == nil {
		tpl = llm.DefaultRAGPrompt
	}

	prompt := tpl.BuildPrompt(question, sources)

	// 5. 流式生成
	var answer string
	err = e.llm.GenerateStream(ctx, prompt, func(chunk string) {
		answer += chunk
		callback(chunk)
	}, req.Opts...)

	if err != nil {
		return nil, fmt.Errorf("生成回答失败: %w", err)
	}

	return &AskResponse{
		Question:  question,
		Answer:    answer,
		Sources:   sources,
		QueryTime: queryTime,
		TotalTime: time.Since(start).Milliseconds(),
	}, nil
}

// AskOption 问答选项
type AskOption func(*AskRequest)

// WithTopK 设置检索数量
func WithTopK(topK int) AskOption {
	return func(r *AskRequest) {
		r.TopK = topK
	}
}

// WithRerank 设置是否重排序
func WithRerank(useRerank bool) AskOption {
	return func(r *AskRequest) {
		r.UseRerank = useRerank
	}
}

// WithRetrievalType 设置检索类型
func WithRetrievalType(retType string) AskOption {
	return func(r *AskRequest) {
		r.RetrievalType = retType
	}
}

// WithLLMOptions 设置LLM选项
func WithLLMOptions(opts ...llm.GenerateOption) AskOption {
	return func(r *AskRequest) {
		r.Opts = opts
	}
}

// buildAnswerFromSources 从检索结果构建回答（无LLM时使用）
func (e *Engine) buildAnswerFromSources(sources []*models.RetrievalResult) string {
	if len(sources) == 0 {
		tpl := e.promptTpl
		if tpl == nil {
			tpl = llm.DefaultRAGPrompt
		}
		return tpl.GetNoContextMessage()
	}

	var answer string
	for i, src := range sources {
		if i > 0 {
			answer += "\n\n"
		}
		answer += fmt.Sprintf("【参考%d】%s", i+1, src.Content)
		if len(answer) > 2000 {
			answer += "\n...(内容过长，已截断)"
			break
		}
	}
	return answer
}
