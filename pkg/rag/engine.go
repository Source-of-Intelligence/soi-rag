package rag

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Source-of-Intelligence/soi-rag/pkg/dedup"
	"github.com/Source-of-Intelligence/soi-rag/pkg/hybrid"
	"github.com/Source-of-Intelligence/soi-rag/pkg/keyword"
	"github.com/Source-of-Intelligence/soi-rag/pkg/knowledgegraph"
	"github.com/Source-of-Intelligence/soi-rag/pkg/llm"
	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/Source-of-Intelligence/soi-rag/pkg/pageindex"
	"github.com/Source-of-Intelligence/soi-rag/pkg/vector"
)

// Engine RAG引擎 - 核心结构
type Engine struct {
	config     *Config
	index      pageindex.PageIndex
	docStore   pageindex.Store
	keywordIdx *keyword.InvertedIndex
	dedup      *dedup.Service

	// 可选的扩展组件（通过 Set* 方法注入）
	llm             llm.LLM
	embedder        vector.Embedder
	vectorRetriever *vector.VectorRetriever
	graphStore      knowledgegraph.GraphStore
	hybridRetriever *hybrid.HybridRetriever
}

// AskOption 问答选项
type AskOption func(*askOptions)

type askOptions struct {
	topK          int
	useRerank     bool
	retrievalType string
}

func (o *askOptions) apply(opts ...AskOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithTopK 设置返回结果数量
func WithTopK(k int) AskOption {
	return func(o *askOptions) {
		if k > 0 {
			o.topK = k
		}
	}
}

// WithRerank 设置是否重排序
func WithRerank(r bool) AskOption {
	return func(o *askOptions) {
		o.useRerank = r
	}
}

// WithRetrievalType 设置检索策略（vector, keyword, graph, hybrid）
func WithRetrievalType(retrievalType string) AskOption {
	return func(o *askOptions) {
		o.retrievalType = retrievalType
	}
}

// AskResult 问答结果
type AskResult struct {
	Question  string                    `json:"question"`
	Answer    string                    `json:"answer"`
	Sources   []*models.RetrievalResult `json:"sources"`
	QueryTime int64                     `json:"query_time_ms"`
	TotalTime int64                     `json:"total_time_ms"`
}

// AddDocumentResult 添加文档结果
type AddDocumentResult struct {
	Document    *models.Document `json:"document"`
	Hash        string           `json:"hash,omitempty"`
	IsDuplicate bool             `json:"is_duplicate"`
	Skipped     bool             `json:"skipped"`
}

// --- 构造函数 ---

// NewEngine 创建新的RAG引擎
func NewEngine(cfg *Config) (*Engine, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	e := &Engine{
		config: cfg,
	}

	// 初始化文档存储和索引
	pageIdxConfig := &pageindex.Config{
		ChunkSize:     cfg.ChunkSize,
		ChunkOverlap:  cfg.ChunkOverlap,
		ChunkStrategy: cfg.ChunkStrategy,
	}

	switch cfg.StorageType {
	case StorageSQLite:
		store, err := pageindex.NewSQLiteStore(pageindex.SQLiteStoreConfig{DBPath: cfg.SQLitePath})
		if err != nil {
			return nil, fmt.Errorf("创建 SQLite 存储失败: %w", err)
		}
		if err := store.InitSchema(context.Background()); err != nil {
			return nil, fmt.Errorf("初始化 SQLite schema 失败: %w", err)
		}
		e.docStore = store
		e.index = pageindex.NewPageIndex(store, pageIdxConfig)
	default:
		// 默认使用内存存储
		e.docStore = pageindex.NewMemoryStore()
		e.index = pageindex.NewPageIndex(e.docStore, pageIdxConfig)
	}

	// 初始化关键词索引
	e.keywordIdx = keyword.NewInvertedIndex(&keyword.ChineseTokenizer{})

	// 初始化去重服务
	if cfg.UseDedup {
		e.dedup = dedup.NewService(dedup.NewInMemoryDedupStore(), true)
	}

	// 如果 config 中设置了 LLM，立即注入
	if cfg.LLM != nil {
		e.llm = cfg.LLM
	}

	return e, nil
}

// --- Setter / Getter 扩展组件 ---

// SetLLM 设置 LLM 用于生成答案或重排
func (e *Engine) SetLLM(llmClient llm.LLM) {
	e.llm = llmClient
}

// GetLLM 返回当前 LLM
func (e *Engine) GetLLM() llm.LLM {
	return e.llm
}

// SetEmbedder 设置文本向量器（供向量检索使用）
func (e *Engine) SetEmbedder(embedder vector.Embedder) {
	e.embedder = embedder
}

// SetVectorRetriever 设置向量检索器
func (e *Engine) SetVectorRetriever(vr *vector.VectorRetriever) {
	e.vectorRetriever = vr
}

// SetKeywordSearcher 设置关键词检索接口（这里与已有的 keywordIdx 兼容）
func (e *Engine) SetKeywordSearcher(kw keyword.Searcher) {
	// 目前内部使用 keyword.InvertedIndex，保留现有实现不变
}

// SetGraphStore 设置知识图谱存储
func (e *Engine) SetGraphStore(kg knowledgegraph.GraphStore) {
	e.graphStore = kg
}

// SetHybridRetriever 设置混合检索器
func (e *Engine) SetHybridRetriever(hr *hybrid.HybridRetriever) {
	e.hybridRetriever = hr
}

// SetDocStore 替换文档存储接口（用于测试/注入自定义实现）
func (e *Engine) SetDocStore(store pageindex.Store) {
	e.docStore = store
}

// Close 关闭引擎，释放资源
func (e *Engine) Close() error {
	if e.docStore != nil {
		if closer, ok := e.docStore.(interface{ Close() error }); ok {
			return closer.Close()
		}
	}
	return nil
}

// SetDedupStore 替换去重存储
func (e *Engine) SetDedupStore(store dedup.DedupStore) {
	if e.dedup == nil {
		e.dedup = dedup.NewService(store, true)
		return
	}
	// 保留接口简单：重写服务
	e.dedup = dedup.NewService(store, true)
}

// GetStats 获取引擎统计信息
func (e *Engine) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"storage_type":   string(e.config.StorageType),
		"chunk_size":     e.config.ChunkSize,
		"chunk_strategy": e.config.ChunkStrategy,
		"use_dedup":      e.config.UseDedup,
		"use_hybrid":     e.config.UseHybrid,
		"top_k":          e.config.TopK,
	}
	return stats
}

// --- 便捷方法 ---

// AddDocumentFromText 从文本添加文档
func (e *Engine) AddDocumentFromText(ctx context.Context, title, content, source string) (*models.Document, error) {
	doc := &models.Document{
		Title:   title,
		Content: content,
		Source:  source,
		Status:  models.DocStatusPending,
		DocType: models.DocTypeText,
	}
	result, err := e.AddDocumentWithDedup(ctx, doc)
	if err != nil {
		return nil, err
	}
	return result.Document, nil
}

// Query 执行查询（兼容旧API）
func (e *Engine) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	opts := models.SearchOptions{TopK: req.TopK}

	var result *models.SearchResult
	var err error

	retrievalType := strings.ToLower(req.RetrievalType)
	switch retrievalType {
	case "vector":
		var results []*models.RetrievalResult
		results, err = e.VectorSearch(ctx, req.Query, req.TopK)
		if err == nil {
			result = &models.SearchResult{Results: results, Total: len(results)}
		}
	case "keyword":
		var kwResult *models.SearchResult
		kwResult, err = e.KeywordSearch(ctx, req.Query, opts)
		if err == nil && kwResult != nil {
			result = kwResult
		}
	case "graph":
		var results []*models.RetrievalResult
		results, err = e.GraphSearch(ctx, req.Query, req.TopK)
		if err == nil {
			result = &models.SearchResult{Results: results, Total: len(results)}
		}
	default:
		result, err = e.Search(ctx, req.Query, opts)
	}

	if err != nil {
		return nil, err
	}

	var queryResults []*QueryResult
	for _, r := range result.Results {
		queryResults = append(queryResults, &QueryResult{
			ID:         r.ID,
			Content:    r.Content,
			Source:     r.Source,
			DocumentID: r.DocumentID,
			Score:      r.Score,
			Metadata:   r.Metadata,
		})
	}

	answer := ""
	if e.llm != nil {
		contextStr := buildContextFromResults(result.Results)
		prompt := buildPromptWithContext(req.Query, contextStr)
		generated, genErr := e.llm.Generate(ctx, prompt)
		if genErr == nil {
			answer = generated
		}
	}

	return &QueryResponse{
		Query:   req.Query,
		Answer:  answer,
		Results: queryResults,
		Total:   result.Total,
	}, nil
}

// --- 文档管理 ---

// AddDocumentWithDedup 添加文档（带去重）
func (e *Engine) AddDocumentWithDedup(ctx context.Context, doc *models.Document) (*AddDocumentResult, error) {
	// 去重检查
	if e.dedup != nil && doc.Content != "" {
		result, err := e.dedup.CheckAndDedup(ctx, []byte(doc.Content))
		if err == nil && result.IsDuplicate {
			return &AddDocumentResult{
				Document:    doc,
				Hash:        result.Hash,
				IsDuplicate: true,
				Skipped:     true,
			}, nil
		}
	}

	// 设置默认值
	if doc.Status == "" {
		doc.Status = models.DocStatusPending
	}
	if doc.DocType == "" {
		doc.DocType = models.DocTypeText
	}
	if doc.Metadata == nil {
		doc.Metadata = make(map[string]interface{})
	}

	// 添加到文档索引
	if err := e.index.AddDocument(ctx, doc, nil); err != nil {
		return nil, fmt.Errorf("添加文档失败: %w", err)
	}

	// 索引到关键词索引
	e.keywordIdx.AddDocument(doc.ID, doc.Title+" "+doc.Content)

	// 索引到向量检索器
	if e.vectorRetriever != nil && e.embedder != nil {
		chunk := &models.Chunk{
			ID:         doc.ID,
			DocumentID: doc.ID,
			Content:    doc.Content,
			ChunkIndex: 0,
		}
		_ = e.vectorRetriever.IndexChunks(ctx, []*models.Chunk{chunk})
	}

	// 记录到去重存储
	if e.dedup != nil {
		hashResult, _ := e.dedup.CheckAndDedup(ctx, []byte(doc.Content))
		if ds, ok := e.dedup.GetStore().(interface {
			CreateDocumentWithHash(context.Context, *models.Document, string) error
		}); ok {
			_ = ds.CreateDocumentWithHash(ctx, doc, hashResult.Hash)
		}
	}

	return &AddDocumentResult{
		Document:    doc,
		IsDuplicate: false,
		Skipped:     false,
	}, nil
}

// AddDocumentWithReader 添加文档（使用 io.Reader 作为内容源，支持结构化文件解析）
// 对于 .pdf、.docx、.html 等文件，pageindex 的解析器会根据 source 自动提取文本内容
func (e *Engine) AddDocumentWithReader(ctx context.Context, title, source string, content io.Reader) (*AddDocumentResult, error) {
	if content == nil {
		return nil, fmt.Errorf("content reader is nil")
	}

	doc := &models.Document{
		Title:  title,
		Source: source,
		// 不设置 DocType，让 pageIndex.AddDocument 通过 DetectDocType(source) 自动识别
		Status: models.DocStatusPending,
	}

	// 通过 io.Reader 让 pageindex 的解析器处理（PDF/DOCX/HTML 等）
	if err := e.index.AddDocument(ctx, doc, content); err != nil {
		return nil, fmt.Errorf("添加文档失败: %w", err)
	}

	// 索引到关键词索引
	if doc.Content != "" {
		e.keywordIdx.AddDocument(doc.ID, doc.Title+" "+doc.Content)
	}

	// 索引到向量检索器
	if e.vectorRetriever != nil && e.embedder != nil && doc.Content != "" {
		chunk := &models.Chunk{
			ID:         doc.ID,
			DocumentID: doc.ID,
			Content:    doc.Content,
			ChunkIndex: 0,
		}
		_ = e.vectorRetriever.IndexChunks(ctx, []*models.Chunk{chunk})
	}

	return &AddDocumentResult{
		Document:    doc,
		IsDuplicate: false,
		Skipped:     false,
	}, nil
}

// ListDocuments 列出文档
func (e *Engine) ListDocuments(ctx context.Context, offset, limit int) ([]*models.Document, error) {
	return e.index.ListDocuments(ctx, offset, limit)
}

// GetDocument 获取文档
func (e *Engine) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	return e.index.GetDocument(ctx, id)
}

// DeleteDocument 删除文档
func (e *Engine) DeleteDocument(ctx context.Context, id string) error {
	if err := e.index.DeleteDocument(ctx, id); err != nil {
		return err
	}
	// 从关键词索引中删除
	e.keywordIdx.RemoveDocument(id)
	// 从向量索引中删除（如存在）
	if e.vectorRetriever != nil {
		_ = e.vectorRetriever.DeleteDocument(ctx, id)
	}
	return nil
}

// --- 检索 API ---

// Search 混合检索（按优先级：hybrid → vector → keyword）
func (e *Engine) Search(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	if opts.TopK <= 0 {
		opts.TopK = e.config.TopK
	}

	// 优先级 1: 混合检索
	if e.hybridRetriever != nil {
		hybridOpts := models.HybridOptions{
			TopK: opts.TopK,
			Strategies: []models.RetrievalType{
				models.RetrievalTypeVector,
				models.RetrievalTypeKeyword,
			},
		}
		if hr, err := e.hybridRetriever.Retrieve(ctx, query, hybridOpts); err == nil && hr != nil {
			return &models.SearchResult{
				Results: hr.Results,
				Total:   len(hr.Results),
			}, nil
		}
	}

	// 优先级 2: 向量检索
	if e.vectorRetriever != nil {
		if results, err := e.vectorRetriever.Retrieve(ctx, query, opts.TopK); err == nil && len(results) > 0 {
			return &models.SearchResult{Results: results, Total: len(results)}, nil
		}
	}

	// 优先级 3: 关键词检索（回退）
	return e.KeywordSearch(ctx, query, opts)
}

// VectorSearch 向量检索
func (e *Engine) VectorSearch(ctx context.Context, query string, topK int) ([]*models.RetrievalResult, error) {
	if e.vectorRetriever == nil {
		return nil, fmt.Errorf("向量检索未配置")
	}
	if topK <= 0 {
		topK = e.config.TopK
	}
	return e.vectorRetriever.Retrieve(ctx, query, topK)
}

// KeywordSearch 关键词检索
func (e *Engine) KeywordSearch(ctx context.Context, query string, opts models.SearchOptions) (*models.SearchResult, error) {
	if opts.TopK <= 0 {
		opts.TopK = e.config.TopK
	}

	// 确保现有文档已经加入关键词索引
	docs, _ := e.index.ListDocuments(ctx, 0, 10000)
	for _, doc := range docs {
		if doc != nil {
			e.keywordIdx.AddDocument(doc.ID, doc.Title+" "+doc.Content)
		}
	}

	// 执行搜索
	results := e.keywordIdx.Search(query, opts.TopK)

	// 补充完整文档信息（内容和来源）
	docMap := make(map[string]*models.Document)
	for _, d := range docs {
		docMap[d.ID] = d
	}
	for _, r := range results {
		if doc, ok := docMap[r.ID]; ok {
			r.Source = doc.Source
			if len(doc.Content) > 500 {
				r.Content = doc.Content[:500] + "..."
			} else {
				r.Content = doc.Content
			}
		}
	}

	return &models.SearchResult{
		Results: results,
		Total:   len(results),
	}, nil
}

// GraphSearch 知识图谱检索
func (e *Engine) GraphSearch(ctx context.Context, query string, topK int) ([]*models.RetrievalResult, error) {
	if e.graphStore == nil {
		return nil, fmt.Errorf("知识图谱存储未配置")
	}
	if topK <= 0 {
		topK = e.config.TopK
	}

	entities, err := e.graphStore.SearchEntities(ctx, query, topK)
	if err != nil {
		return nil, err
	}

	var results []*models.RetrievalResult
	for _, entity := range entities {
		results = append(results, &models.RetrievalResult{
			ID:         entity.ID,
			Content:    entity.Description,
			Source:     entity.Name,
			DocumentID: entity.ID,
			Metadata: map[string]interface{}{
				"entity_type": entity.Type,
			},
		})
	}
	return results, nil
}

// --- 问答 ---

// Ask 问答 - 检索 + 生成（无LLM时只返回检索结果）
func (e *Engine) Ask(ctx context.Context, question string, opts ...AskOption) (*AskResult, error) {
	start := time.Now()

	// 处理选项
	o := &askOptions{
		topK:      e.config.TopK,
		useRerank: e.config.UseReranker,
	}
	o.apply(opts...)

	// 根据 retrievalType 选择检索方式
	var results []*models.RetrievalResult
	var err error

	retrievalType := strings.ToLower(strings.TrimSpace(o.retrievalType))
	switch retrievalType {
	case "vector":
		results, err = e.VectorSearch(ctx, question, o.topK)
	case "keyword":
		var kwResult *models.SearchResult
		kwResult, err = e.KeywordSearch(ctx, question, models.SearchOptions{TopK: o.topK})
		if err == nil && kwResult != nil {
			results = kwResult.Results
		}
	case "graph":
		results, err = e.GraphSearch(ctx, question, o.topK)
	default:
		// hybrid 或未指定：使用 Search() 的优先级逻辑
		var sr *models.SearchResult
		sr, err = e.Search(ctx, question, models.SearchOptions{TopK: o.topK})
		if err == nil && sr != nil {
			results = sr.Results
		}
	}

	if err != nil {
		return nil, err
	}

	queryTime := time.Since(start).Milliseconds()

	// 构建回答
	answer := ""
	if e.llm != nil {
		// 使用 LLM 生成回答
		contextStr := buildContextFromResults(results)
		prompt := buildPromptWithContext(question, contextStr)
		generated, genErr := e.llm.Generate(ctx, prompt)
		if genErr == nil {
			answer = generated
		} else {
			answer = fmt.Sprintf("（生成失败: %v）\n\n", genErr) + buildFallbackAnswer(results)
		}
	} else {
		answer = buildFallbackAnswer(results)
	}

	return &AskResult{
		Question:  question,
		Answer:    answer,
		Sources:   results,
		QueryTime: queryTime,
		TotalTime: time.Since(start).Milliseconds(),
	}, nil
}

// AskStream 流式问答
func (e *Engine) AskStream(ctx context.Context, question string, callback func(string), opts ...AskOption) (*AskResult, error) {
	start := time.Now()

	o := &askOptions{
		topK:      e.config.TopK,
		useRerank: e.config.UseReranker,
	}
	o.apply(opts...)

	var results []*models.RetrievalResult
	var err error
	retrievalType := strings.ToLower(strings.TrimSpace(o.retrievalType))
	switch retrievalType {
	case "vector":
		results, err = e.VectorSearch(ctx, question, o.topK)
	case "keyword":
		var kwResult *models.SearchResult
		kwResult, err = e.KeywordSearch(ctx, question, models.SearchOptions{TopK: o.topK})
		if err == nil && kwResult != nil {
			results = kwResult.Results
		}
	case "graph":
		results, err = e.GraphSearch(ctx, question, o.topK)
	default:
		var sr *models.SearchResult
		sr, err = e.Search(ctx, question, models.SearchOptions{TopK: o.topK})
		if err == nil && sr != nil {
			results = sr.Results
		}
	}
	if err != nil {
		return nil, err
	}

	var answer string
	if e.llm != nil {
		contextStr := buildContextFromResults(results)
		prompt := buildPromptWithContext(question, contextStr)
		genErr := e.llm.GenerateStream(ctx, prompt, func(chunk string) {
			answer += chunk
			callback(chunk)
		})
		if genErr != nil {
			fallback := buildFallbackAnswer(results)
			answer += "\n\n（注：LLM流式生成失败，使用检索摘要）"
			callback(fallback)
		}
	} else {
		answer = buildFallbackAnswer(results)
		callback(answer)
	}

	return &AskResult{
		Question:  question,
		Answer:    answer,
		Sources:   results,
		QueryTime: time.Since(start).Milliseconds(),
		TotalTime: time.Since(start).Milliseconds(),
	}, nil
}

// --- 内部辅助 ---

func buildContextFromResults(results []*models.RetrievalResult) string {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range results {
		b.WriteString(fmt.Sprintf("[%d] %s\n", i+1, r.Content))
		if r.Source != "" {
			b.WriteString(fmt.Sprintf("来源: %s\n", r.Source))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func buildPromptWithContext(question, context string) string {
	return fmt.Sprintf(`请根据以下参考资料回答问题。如果资料中没有相关信息，请如实说明。

参考资料：
%s

问题：%s

回答：`, context, question)
}

func buildFallbackAnswer(results []*models.RetrievalResult) string {
	if len(results) == 0 {
		return "知识库中没有找到与您问题相关的内容。请先使用 add_document 或 add_file 操作添加相关文档到知识库。"
	}
	answer := fmt.Sprintf("找到 %d 条相关内容：\n\n", len(results))
	for i, src := range results {
		srcName := src.Source
		if srcName == "" {
			srcName = fmt.Sprintf("来源%d", i+1)
		}
		score := ""
		if src.Score > 0 {
			score = fmt.Sprintf(" (相关度: %.2f)", src.Score)
		}
		answer += fmt.Sprintf("%d. %s%s\n   %s\n\n", i+1, srcName, score, truncateString(src.Content, 300))
	}
	answer += "（提示：配置 LLM provider 后可以生成基于检索内容的自然语言回答）"
	return answer
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
