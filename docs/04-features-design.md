# RAG 工具扩展特性设计文档

## 概述

本文档覆盖 RAG 工具的 **19 个扩展特性**，按优先级分为三档：

- **高优先级（5 个）**：完成 RAG 闭环和持久化基础，是系统可用的前提
- **中优先级（8 个）**：提升易用性、性能和扩展能力
- **低优先级（6 个）**：面向生产环境的进阶能力

---

## 高优先级特性（5 个）

### 特性1：接入 LLM 生成回答

- **目标**：完成 RAG 闭环，将检索结果作为上下文通过 LLM 生成最终回答
- **接口设计**：
  ```go
  // pkg/llm/llm.go
  type LLM interface {
      Generate(ctx context.Context, prompt string, opts ...GenerateOption) (string, error)
      GenerateStream(ctx context.Context, prompt string, callback func(string), opts ...GenerateOption) error
  }

  type GenerateOption func(*GenerateConfig)
  type GenerateConfig struct {
      Temperature float64
      MaxTokens   int
      TopP        float64
      StopWords   []string
  }

  // 实现类
  type OpenAILLM struct { ... }      // OpenAI GPT 系列
  type OllamaLLM struct { ... }      // 本地 Ollama 模型
  type MockLLM struct { ... }        // 测试用模拟
  ```
- **Prompt 模板**：
  ```go
  // pkg/llm/prompt.go
  type PromptTemplate struct {
      SystemPrompt    string
      ContextFormat   string  // 检索结果的格式化模板
      QuestionFormat  string  // 用户问题的格式化模板
  }

  // DefaultRAGPrompt 默认RAG提示模板
  var DefaultRAGPrompt = &PromptTemplate{
      SystemPrompt: "你是一个知识助手，请根据以下参考资料回答用户问题。如果资料中没有相关信息，请如实说明。",
      ContextFormat: "参考资料{{.Index}}：\n{{.Content}}\n来源：{{.Source}}",
      QuestionFormat: "用户问题：{{.Question}}",
  }
  ```
- **Engine 集成**：
  ```go
  // 在 Engine 中添加
  type Engine struct {
      // ... 现有字段
      llm       llm.LLM           // LLM生成器
      promptTpl *llm.PromptTemplate // 提示模板
  }

  // 新增方法
  func (e *Engine) Ask(ctx context.Context, question string, opts ...AskOption) (*AskResponse, error)
  func (e *Engine) AskStream(ctx context.Context, question string, callback func(string), opts ...AskOption) error
  ```
- **配置扩展**：Config 中添加 LLM 相关配置
- **依赖**：无外部依赖（HTTP 调用）

### 特性2：向量存储持久化

- **目标**：向量数据持久化到 SQLite/PG，重启不丢失
- **接口设计**：
  ```go
  // pkg/vector/store.go 扩展
  type PersistentVectorStore interface {
      VectorStore  // 继承现有接口
      InitSchema(ctx context.Context) error
      Close() error
  }

  // pkg/vector/sqlite_store.go
  type SQLiteVectorStore struct {
      db *sql.DB
  }
  // 使用 SQLite 的 math functions 计算余弦相似度
  // CREATE TABLE vectors (id TEXT, doc_id TEXT, chunk_id TEXT, vector BLOB, dimension INT, metadata TEXT)

  // pkg/vector/pg_store.go
  type PGVectorStore struct {
      db *sql.DB
  }
  // 使用 pgvector 扩展
  // CREATE TABLE vectors (id TEXT, doc_id TEXT, chunk_id TEXT, embedding vector(1536), metadata JSONB)
  ```
- **性能考虑**：SQLite 方案使用暴力搜索（适合中小规模），PG 方案支持 IVFFlat/HNSW 索引
- **依赖**：pgvector 扩展（PG 方案）

### 特性3：关键词索引持久化

- **目标**：倒排索引持久化到 SQLite FTS5 / PG tsvector
- **接口设计**：
  ```go
  // pkg/keyword/persistent_index.go
  type PersistentIndex interface {
      keyword.Searcher  // 继承搜索接口
      AddDocument(ctx context.Context, docID string, tokens []string) error
      RemoveDocument(ctx context.Context, docID string) error
      InitSchema(ctx context.Context) error
      Close() error
  }

  // pkg/keyword/sqlite_fts.go
  type SQLiteFTSIndex struct {
      db *sql.DB
  }
  // 使用 FTS5 全文搜索
  // CREATE VIRTUAL TABLE fts_index USING fts5(doc_id, content, tokenize='unicode61')

  // pkg/keyword/pg_tsvector.go
  type PGTSVectorIndex struct {
      db *sql.DB
  }
  // 使用 tsvector/tsquery
  // ALTER TABLE documents ADD COLUMN tsv tsvector
  // CREATE INDEX idx_tsv ON documents USING GIN(tsv)
  ```

### 特性4：知识图谱持久化

- **目标**：实体和关系持久化到 SQLite/PG
- **接口设计**：
  ```go
  // pkg/knowledgegraph/persistent_store.go
  type PersistentGraphStore interface {
      knowledgegraph.GraphStore  // 继承现有接口
      InitSchema(ctx context.Context) error
      Close() error
  }

  // pkg/knowledgegraph/sqlite_store.go
  type SQLiteGraphStore struct {
      db *sql.DB
  }
  // 实体表：entities(id, name, type, description, confidence, properties TEXT)
  // 关系表：relations(id, source_id, target_id, type, confidence)

  // pkg/knowledgegraph/pg_store.go
  type PGGraphStore struct {
      db *sql.DB
  }
  // 同上结构，使用 PG 特性
  ```

### 特性5：实体抽取接入 LLM

- **目标**：通过 LLM API 进行结构化实体和关系抽取
- **接口设计**：
  ```go
  // pkg/knowledgegraph/llm_extractor.go
  type LLMEntityExtractor struct {
      llm     llm.LLM
      prompt  string
  }

  func NewLLMEntityExtractor(llm llm.LLM) *LLMEntityExtractor

  func (e *LLMEntityExtractor) Extract(ctx context.Context, text string) (*models.ExtractionResult, error)
  // 使用 JSON mode 让 LLM 返回结构化实体和关系
  ```
- **依赖**：特性1（LLM 接口）

---

## 中优先级特性（8 个）

### 特性6：HTTP API 服务

- **目标**：提供 RESTful HTTP API，支持远程调用
- **接口设计**：
  ```go
  // pkg/api/server.go
  type Server struct {
      engine *rag.Engine
      addr   string
  }

  func NewServer(engine *rag.Engine, addr string) *Server
  func (s *Server) Start(ctx context.Context) error
  func (s *Server) Stop() error
  ```
- **API 端点**：
  - POST /api/v1/documents - 添加文档
  - GET /api/v1/documents - 列出文档
  - GET /api/v1/documents/:id - 获取文档
  - DELETE /api/v1/documents/:id - 删除文档
  - POST /api/v1/search - 搜索
  - POST /api/v1/ask - RAG 问答（依赖特性1）
  - GET /api/v1/stats - 统计信息
  - GET /api/v1/health - 健康检查
- **使用标准库 net/http**，可选集成 gin/echo
- **依赖**：无

### 特性7：配置文件支持

- **目标**：支持 YAML/TOML 配置文件加载
- **接口设计**：
  ```go
  // pkg/config/config.go
  type FileConfig struct {
      Engine    rag.Config       `yaml:"engine"`
      LLM       llm.Config      `yaml:"llm"`
      API       api.Config       `yaml:"api"`
      Storage   StorageConfig    `yaml:"storage"`
  }

  type StorageConfig struct {
      Type     string         `yaml:"type"`     // memory, sqlite, postgres
      SQLite   SQLiteConfig   `yaml:"sqlite"`
      Postgres PostgresConfig `yaml:"postgres"`
  }

  func LoadFromFile(path string) (*FileConfig, error)
  func LoadFromFileWithOverride(path string, overrides map[string]string) (*FileConfig, error)
  ```
- **配置文件示例**：提供 rag.yaml 示例
- **依赖**：gopkg.in/yaml.v3

### 特性8：PDF/Word 文档解析

- **目标**：实现 PDF 和 Word 文档的解析，支持可扩展的解析器注册
- **技术选型**：
  - PDF：`github.com/ledongthuc/pdf`（纯 Go，MIT 协议，文本提取）
  - Word：`github.com/nguyenthenguyen/docx`（纯 Go，读取 .docx 内容）
  - 可扩展：通过 ParserRegistry 注册自定义解析器
- **接口设计**：
  ```go
  // pkg/pageindex/parser.go 扩展
  // PDF 解析器
  type PDFParser struct{}
  func (p *PDFParser) Parse(data []byte) (*ParseResult, error)
  func (p *PDFParser) SupportedExtensions() []string  // [".pdf"]

  // Word 解析器
  type WordParser struct{}
  func (p *WordParser) Parse(data []byte) (*ParseResult, error)
  func (p *WordParser) SupportedExtensions() []string  // [".docx", ".doc"]

  // 解析器注册中心（可扩展）
  type ParserRegistry struct {
      parsers map[string]Parser  // extension -> Parser
  }
  func NewParserRegistry() *ParserRegistry
  func (r *ParserRegistry) Register(parser Parser)
  func (r *ParserRegistry) GetParser(ext string) (Parser, bool)
  func (r *ParserRegistry) SupportedExtensions() []string
  ```
- **PDF 解析流程**：
  1. 使用 ledongthuc/pdf 打开文件
  2. 逐页提取文本内容
  3. 保留段落结构（标题/正文/列表）
  4. 提取元数据（标题、作者、页数）
- **Word 解析流程**：
  1. 使用 nguyenthenguyen/docx 解析 .docx
  2. 提取段落文本，保留标题层级
  3. 提取表格内容
  4. 提取元数据
- **扩展机制**：用户可自定义 Parser 实现并注册到 Registry

### 特性9：中文分词优化

- **目标**：替换逐字切分为真正的中文分词
- **技术选型**：`github.com/yanyiwu/gojieba`（Go 原生 jieba 分词）
  - 注意：gojieba 使用 CGO，需要 gcc 编译环境
  - 备选方案：`github.com/go-ego/gse`（纯 Go 实现，无 CGO 依赖）
- **接口设计**：
  ```go
  // pkg/keyword/tokenizer.go 扩展
  type JiebaTokenizer struct {
      jieba *gojieba.Jieba
  }
  func NewJiebaTokenizer(dictPath string) *JiebaTokenizer
  func (t *JiebaTokenizer) Tokenize(text string) []string

  // 或纯 Go 方案
  type GSETokenizer struct {
      seg gse.Segmenter
  }
  func NewGSETokenizer() *GSETokenizer
  func (t *GSETokenizer) Tokenize(text string) []string
  ```
- **分词模式**：精确模式（搜索用）、全模式（索引用）

### 特性10：真正的 HNSW 索引

- **目标**：实现基于多层图的近似最近邻搜索
- **接口设计**：
  ```go
  // pkg/vector/hnsw.go
  type HNSWIndex struct {
      nodes     []*hnswNode
      nodeMap   map[string]int  // id -> index
      M         int             // 每层最大连接数
      ef        int             // 搜索时的候选集大小
      mL        float64         // 层级因子
      maxLevel  int
      mu        sync.RWMutex
  }

  type hnswNode struct {
      id       string
      vector   []float32
      neighbors [][]int  // 每层的邻居列表
      level    int
  }

  func NewHNSWIndex(dimension int, M, ef int) *HNSWIndex
  func (h *HNSWIndex) Insert(id string, vector []float32) error
  func (h *HNSWIndex) Search(query []float32, k int) []*SearchResult
  func (h *HNSWIndex) Delete(id string) error
  ```
- **算法参数**：M=16, efConstruction=200, mL=1/ln(M)
- **性能目标**：10万向量 < 1ms 查询

### 特性11：流式文档处理

- **目标**：大文件流式读取，避免 OOM
- **接口设计**：
  ```go
  // pkg/pageindex/stream.go
  type StreamReader interface {
      ReadChunk() (*models.Chunk, error) // 逐块读取
      Close() error
  }

  type StreamChunker struct {
      reader    io.Reader
      chunkSize int
      overlap   int
  }

  func NewStreamChunker(reader io.Reader, strategy string, chunkSize, overlap int) *StreamChunker
  func (s *StreamChunker) NextChunk() (*models.Chunk, error)
  ```

### 特性12：并发批量索引

- **目标**：Worker Pool 并发索引，提升大批量文档处理速度
- **接口设计**：
  ```go
  // pkg/pageindex/batch.go
  type BatchIndexer struct {
      engine    *rag.Engine
      workers   int           // 并发worker数
      batchSize int           // 每批处理数量
  }

  func NewBatchIndexer(engine *rag.Engine, workers, batchSize int) *BatchIndexer
  func (b *BatchIndexer) IndexDocuments(ctx context.Context, docs []*DocumentInput) (*BatchResult, error)

  type BatchResult struct {
      Total     int
      Success   int
      Failed    int
      Errors    []BatchError
      Duration  time.Duration
  }
  ```

### 特性13：查询缓存层

- **目标**：LRU 缓存高频查询，降低延迟
- **接口设计**：
  ```go
  // pkg/cache/cache.go
  type Cache interface {
      Get(key string) (interface{}, bool)
      Set(key string, value interface{}, ttl time.Duration)
      Delete(key string)
      Clear()
      Stats() CacheStats
  }

  type LRUCache struct { ... }
  func NewLRUCache(maxSize int, defaultTTL time.Duration) *LRUCache

  // pkg/rag/engine.go 集成
  // 查询时先检查缓存，命中则直接返回
  ```

---

## 低优先级特性（6 个）

### 特性14：文档变更监控

- **目标**：监控目录变化，自动重新索引
- **技术选型**：`github.com/fsnotify/fsnotify`
- **接口设计**：
  ```go
  // pkg/watcher/watcher.go
  type DocumentWatcher struct {
      watcher *fsnotify.Watcher
      engine  *rag.Engine
      dirs    []string
  }

  func NewDocumentWatcher(engine *rag.Engine, dirs ...string) (*DocumentWatcher, error)
  func (w *DocumentWatcher) Start(ctx context.Context) error
  func (w *DocumentWatcher) Stop() error
  ```

### 特性15：多语言 Embedding 支持

- **目标**：支持多语言和跨语言 Embedding 模型
- **接口设计**：
  ```go
  // pkg/vector/embedder.go 扩展
  type MultilingualEmbedder struct {
      client    *http.Client
      model     string  // 如 "multilingual-e5-large"
      apiUrl    string
  }
  func NewMultilingualEmbedder(apiUrl, model, apiKey string) *MultilingualEmbedder
  ```

### 特性16：查询改写与扩展

- **目标**：Query Rewriting、HyDE、Multi-Query 等查询优化策略
- **接口设计**：
  ```go
  // pkg/query/rewrite.go
  type QueryRewriter interface {
      Rewrite(ctx context.Context, query string) ([]string, error)
  }

  type SynonymRewriter struct { ... }    // 同义词扩展
  type HyDERewriter struct { ... }       // 假设性文档嵌入
  type MultiQueryRewriter struct { ... }  // 多查询生成
  ```

### 特性17：检索质量评估框架

- **目标**：Recall@K、MRR、NDCG 等指标
- **接口设计**：
  ```go
  // pkg/eval/evaluator.go
  type Evaluator struct {
      engine *rag.Engine
  }

  type EvalResult struct {
      RecallAtK  map[int]float64
      MRR        float64
      NDCG       float64
      Precision  map[int]float64
  }

  func (e *Evaluator) Evaluate(ctx context.Context, dataset *EvalDataset) (*EvalResult, error)
  ```

### 特性18：可观测性集成

- **目标**：Prometheus metrics + OpenTelemetry tracing
- **接口设计**：
  ```go
  // pkg/metrics/metrics.go
  type Metrics struct {
      queryCount    prometheus.Counter
      queryDuration prometheus.Histogram
      indexCount    prometheus.Counter
      cacheHitRate  prometheus.Gauge
  }

  // pkg/tracing/tracing.go
  func InitTracing(serviceName string) (func(), error)
  ```

### 特性19：文档级权限控制

- **目标**：文档级别的访问控制，支持多租户
- **接口设计**：
  ```go
  // pkg/auth/auth.go
  type AccessControl struct {
      store auth.Store
  }

  type DocumentACL struct {
      DocID    string
      Owner    string
      Readers  []string
      Writers  []string
      Public   bool
  }

  func (a *AccessControl) CanRead(ctx context.Context, userID, docID string) (bool, error)
  func (a *AccessControl) CanWrite(ctx context.Context, userID, docID string) (bool, error)
  ```

---

## 实现路线图

| 阶段 | 特性 | 预计工作量 |
|------|------|-----------|
| 第一阶段 | #8 PDF/Word解析, #1 LLM生成, #7 配置文件 | 3-5天 |
| 第二阶段 | #2 向量持久化, #3 关键词持久化, #4 图谱持久化 | 3-4天 |
| 第三阶段 | #5 LLM实体抽取, #9 中文分词, #10 HNSW | 2-3天 |
| 第四阶段 | #6 HTTP API, #12 并发索引, #13 查询缓存 | 2-3天 |
| 第五阶段 | #11 流式处理, #14 变更监控, #15 多语言 | 2-3天 |
| 第六阶段 | #16 查询改写, #17 评估框架, #18 可观测性, #19 权限控制 | 3-4天 |

---

## 技术依赖汇总

| 特性 | 外部依赖 | 说明 |
|------|---------|------|
| #1 LLM | 无 | HTTP 调用 OpenAI/Ollama API |
| #2 向量持久化 | pgvector（PG方案） | SQLite 方案无额外依赖 |
| #3 关键词持久化 | 无 | FTS5/tsvector 均为内置 |
| #4 图谱持久化 | 无 | 标准 SQL |
| #5 LLM实体抽取 | 无 | 依赖 #1 |
| #6 HTTP API | 无（或 gin） | 标准库 net/http |
| #7 配置文件 | gopkg.in/yaml.v3 | YAML 解析 |
| #8 PDF/Word | ledongthuc/pdf, nguyenthenguyen/docx | 纯 Go 库 |
| #9 中文分词 | go-ego/gse（推荐）或 gojieba | gse 纯 Go，gojieba 需要 CGO |
| #10 HNSW | 无 | 纯 Go 实现 |
| #11 流式处理 | 无 | 标准库 io |
| #12 并发索引 | 无 | 标准库 goroutine |
| #13 查询缓存 | 无 | 纯 Go LRU |
| #14 变更监控 | fsnotify | 文件系统监控 |
| #15 多语言 | 无 | HTTP 调用 Embedding API |
| #16 查询改写 | 无 | 依赖 #1 |
| #17 评估框架 | 无 | 纯 Go 计算 |
| #18 可观测性 | prometheus/client_golang, otel | metrics + tracing |
| #19 权限控制 | 无 | 纯 Go 实现 |

---

## 实现状态（截至 2026-05-31）

所有 19 个扩展特性均已实现，以下是各特性的实现状态和所在包：

| # | 特性 | 优先级 | 状态 | 实现位置 |
|---|------|--------|------|---------|
| 1 | LLM 生成回答 | P0 | ✅ 已实现 | `pkg/llm/` (OpenAI, Ollama, Mock) |
| 2 | 向量持久化 | P0 | ✅ 已实现 | `pkg/vector/sqlite_store.go`, `pkg/vector/pg_store.go` |
| 3 | 关键词持久化 | P0 | ✅ 已实现 | `pkg/keyword/sqlite_fts.go`, `pkg/keyword/pg_tsvector.go` |
| 4 | 图谱持久化 | P0 | ✅ 已实现 | `pkg/knowledgegraph/sqlite_store.go`, `pkg/knowledgegraph/pg_store.go` |
| 5 | LLM 实体抽取 | P0 | ✅ 已实现 | `pkg/knowledgegraph/llm_extractor.go` |
| 6 | HTTP API | P1 | ✅ 已实现 | `pkg/api/` (RESTful + SSE 流式) |
| 7 | YAML 配置文件 | P1 | ✅ 已实现 | `pkg/config/config.go` |
| 8 | PDF/Word 解析 | P1 | ✅ 已实现 | `pkg/pageindex/pdf_parser.go`, `pkg/pageindex/word_parser.go` |
| 9 | 中文分词 | P1 | ✅ 已实现 | `pkg/keyword/gse_tokenizer.go` (GSE) |
| 10 | HNSW 索引 | P1 | ✅ 已实现 | `pkg/vector/hnsw.go` |
| 11 | 流式处理 | P1 | ✅ 已实现 | `pkg/pageindex/stream.go` |
| 12 | 并发批量索引 | P1 | ✅ 已实现 | `pkg/pageindex/batch.go` (Worker Pool) |
| 13 | 查询缓存 | P1 | ✅ 已实现 | `pkg/cache/cache.go` (LRU + TTL) |
| 14 | 文件监控 | P2 | ✅ 已实现 | `pkg/watcher/watcher.go` (fsnotify) |
| 15 | 多语言 Embedding | P2 | ✅ 已实现 | `pkg/vector/multilingual.go` (E5, BGE-M3) |
| 16 | 查询改写 | P2 | ✅ 已实现 | `pkg/query/rewrite.go` (同义词, HyDE, 多查询) |
| 17 | 评估框架 | P2 | ✅ 已实现 | `pkg/eval/evaluator.go` (Recall, MRR, NDCG, MAP) |
| 18 | 可观测性 | P2 | ✅ 已实现 | `pkg/metrics/metrics.go`, `pkg/tracing/tracing.go` |
| 19 | 权限控制 | P2 | ✅ 已实现 | `pkg/auth/auth.go` (ACL) |

### 额外实现的优化

- **便捷接口**: 为所有包补充了 New/Default/Set/Get 系列便捷方法，总计 271 个导出符号
- **Bug 修复**: 修复了 LLM GenerateConfig 并发安全、Query err 变量遮蔽、MultilingualEmbedder normalizing 逻辑 3 个 P0 Bug
- **HTTP 服务入口**: 新增 `cmd/rag-server/main.go`，支持 config.yaml 驱动启动
- **流式 SSE 端点**: 新增 `POST /api/v1/ask/stream` 流式问答端点
- **SM3 去重**: 新增 SQLite 去重存储支持
