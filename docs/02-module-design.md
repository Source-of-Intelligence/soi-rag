# RAG工具模块详细设计文档

## 1. PageIndex模块设计

### 1.1 概述
PageIndex模块负责文档的解析、分块、索引和检索，是RAG系统的基础数据层。

### 1.2 核心组件

```
┌─────────────────────────────────────────────────────────────┐
│                      PageIndex Module                        │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │   Parser     │  │   Chunker    │  │   Indexer    │       │
│  │  (文档解析)   │  │  (文档分块)   │  │  (索引构建)   │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
│         │                 │                 │               │
│         ▼                 ▼                 ▼               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Document Store (SQLite/PostgreSQL)     │   │
│  │  - documents: 文档元数据                              │   │
│  │  - chunks: 文档分块                                   │   │
│  │  - index_metadata: 索引元数据                         │   │
│  └──────────────────────────────────────────────────────┘   │
│                              │                              │
│                              ▼                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │                   Search Interface                    │   │
│  │  - 全文检索                                            │   │
│  │  - 元数据过滤                                          │   │
│  │  - 分页与排序                                          │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 1.3 数据模型

#### Document（文档）
```go
type Document struct {
    ID          string                 `json:"id" db:"id"`
    Title       string                 `json:"title" db:"title"`
    Content     string                 `json:"content" db:"content"`
    Source      string                 `json:"source" db:"source"`           // 来源URL/路径
    DocType     DocumentType           `json:"doc_type" db:"doc_type"`       // pdf, docx, md, etc.
    Metadata    map[string]interface{} `json:"metadata" db:"metadata"`       // 扩展元数据
    CreatedAt   time.Time              `json:"created_at" db:"created_at"`
    UpdatedAt   time.Time              `json:"updated_at" db:"updated_at"`
    Version     int                    `json:"version" db:"version"`
    Status      DocumentStatus         `json:"status" db:"status"`           // indexed, processing, failed
}
```

#### Chunk（文档分块）
```go
type Chunk struct {
    ID          string    `json:"id" db:"id"`
    DocumentID  string    `json:"document_id" db:"document_id"`
    Content     string    `json:"content" db:"content"`
    StartPos    int       `json:"start_pos" db:"start_pos"`       // 在原文中的起始位置
    EndPos      int       `json:"end_pos" db:"end_pos"`           // 在原文中的结束位置
    ChunkIndex  int       `json:"chunk_index" db:"chunk_index"`   // 分块序号
    TokenCount  int       `json:"token_count" db:"token_count"`   // Token数量
    PageNumber  int       `json:"page_number" db:"page_number"`   // 页码（PDF等）
    HeadingPath []string  `json:"heading_path" db:"heading_path"` // 标题层级路径
}
```

### 1.4 分块策略

#### 1.4.1 固定长度分块
```go
type FixedSizeChunker struct {
    ChunkSize    int  // 每个分块的目标token数
    ChunkOverlap int  // 分块之间的重叠token数
}
```

#### 1.4.2 递归分块
```go
type RecursiveChunker struct {
    Separators   []string // 分隔符优先级列表 ["\n\n", "\n", ".", " "]
    ChunkSize    int
    ChunkOverlap int
}
```

#### 1.4.3 语义分块
```go
type SemanticChunker struct {
    EmbeddingModel string  // 使用的嵌入模型
    SimilarityThreshold float64  // 句子相似度阈值
    MaxChunkSize   int
}
```

### 1.5 支持的文档格式

| 格式 | MIME类型 | 解析器 | 实现方式 |
|------|----------|--------|----------|
| PDF | application/pdf | PDFParser | `github.com/ledongthuc/pdf`（纯 Go） |
| Word | application/vnd.openxmlformats-officedocument.wordprocessingml.document | WordParser | `github.com/nguyenthenguyen/docx`（纯 Go） |
| Markdown | text/markdown | TextParser | 原生解析 |
| HTML | text/html | TextParser | 原生解析 |
| TXT | text/plain | TextParser | 原生解析 |
| CSV | text/csv | TextParser | 原生解析 |
| JSON | application/json | TextParser | 原生解析 |

### 1.6 API接口

```go
// 文档管理
type PageIndex interface {
    // 添加文档
    AddDocument(ctx context.Context, doc *Document, content io.Reader) error
    
    // 批量添加文档
    BatchAddDocuments(ctx context.Context, docs []*Document) error
    
    // 更新文档
    UpdateDocument(ctx context.Context, docID string, content io.Reader) error
    
    // 删除文档
    DeleteDocument(ctx context.Context, docID string) error
    
    // 获取文档
    GetDocument(ctx context.Context, docID string) (*Document, error)
    
    // 搜索文档
    Search(ctx context.Context, query string, opts SearchOptions) (*SearchResult, error)
    
    // 获取文档分块
    GetChunks(ctx context.Context, docID string) ([]*Chunk, error)
}
```

---

## 2. 知识图谱模块设计

### 2.1 概述
知识图谱模块负责从文档中抽取实体和关系，构建可推理的知识网络。

### 2.2 核心组件

```
┌─────────────────────────────────────────────────────────────┐
│                   Knowledge Graph Module                     │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │    NER       │  │      RE      │  │   Builder    │       │
│  │ 实体识别      │  │ 关系抽取      │  │ 图谱构建      │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
│         │                 │                 │               │
│         ▼                 ▼                 ▼               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Graph Store (SQLite/PostgreSQL)         │   │
│  │  - Node: 实体 (Entity)                                │   │
│  │  - Edge: 关系 (Relation)                              │   │
│  └──────────────────────────────────────────────────────┘   │
│                              │                              │
│                              ▼                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │                   Query Engine                        │   │
│  │  - Cypher查询                                          │   │
│  │  - 自然语言转Cypher                                     │   │
│  │  - 子图检索                                            │   │
│  │  - 路径推理                                            │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 2.3 数据模型

#### Entity（实体）
```go
type Entity struct {
    ID          string                 `json:"id"`
    Name        string                 `json:"name"`
    Type        EntityType             `json:"type"`           // PERSON, ORG, LOC, etc.
    Aliases     []string               `json:"aliases"`        // 别名
    Description string                 `json:"description"`
    Properties  map[string]interface{} `json:"properties"`
    SourceDocID string                 `json:"source_doc_id"`
    Confidence  float64                `json:"confidence"`
}
```

#### Relation（关系）
```go
type Relation struct {
    ID          string                 `json:"id"`
    SourceID    string                 `json:"source_id"`      // 源实体ID
    TargetID    string                 `json:"target_id"`      // 目标实体ID
    Type        RelationType           `json:"type"`           // WORKS_FOR, LOCATED_IN, etc.
    Properties  map[string]interface{} `json:"properties"`
    SourceDocID string                 `json:"source_doc_id"`
    Confidence  float64                `json:"confidence"`
}
```

### 2.4 实体类型定义

```go
type EntityType string

const (
    EntityPerson      EntityType = "PERSON"
    EntityOrganization EntityType = "ORGANIZATION"
    EntityLocation    EntityType = "LOCATION"
    EntityDate        EntityType = "DATE"
    EntityProduct     EntityType = "PRODUCT"
    EntityEvent       EntityType = "EVENT"
    EntityConcept     EntityType = "CONCEPT"
    EntityTechnology  EntityType = "TECHNOLOGY"
    EntityIndustry    EntityType = "INDUSTRY"
)
```

### 2.5 关系类型定义

```go
type RelationType string

const (
    RelWorksFor     RelationType = "WORKS_FOR"
    RelLocatedIn    RelationType = "LOCATED_IN"
    RelFoundedBy    RelationType = "FOUNDED_BY"
    RelPartOf       RelationType = "PART_OF"
    RelRelatedTo    RelationType = "RELATED_TO"
    RelUses         RelationType = "USES"
    RelProduces     RelationType = "PRODUCES"
    RelCompetesWith RelationType = "COMPETES_WITH"
)
```

### 2.6 抽取策略

#### 2.6.1 基于规则的抽取
- 使用正则表达式和词典匹配
- 适用于结构化程度高的文本
- 速度快，准确率高

#### 2.6.2 基于模型的抽取
- 使用预训练NER模型（spaCy、Stanza）
- 使用LLM进行开放域抽取
- 适用于复杂文本

#### 2.6.3 混合抽取
```go
type HybridExtractor struct {
    RuleExtractor  *RuleBasedExtractor
    ModelExtractor *ModelBasedExtractor
    LLMExtractor   *LLMBasedExtractor
}

func (h *HybridExtractor) Extract(ctx context.Context, text string) (*ExtractionResult, error) {
    // 1. 规则抽取（高置信度）
    ruleResult := h.RuleExtractor.Extract(text)
    
    // 2. 模型抽取（中等置信度）
    modelResult := h.ModelExtractor.Extract(text)
    
    // 3. LLM抽取（复杂关系）
    llmResult := h.LLMExtractor.Extract(text)
    
    // 4. 融合结果
    return h.MergeResults(ruleResult, modelResult, llmResult)
}
```

### 2.7 检索接口

```go
type KnowledgeGraph interface {
    // 添加实体
    AddEntity(ctx context.Context, entity *Entity) error
    
    // 添加关系
    AddRelation(ctx context.Context, relation *Relation) error
    
    // 从文档构建图谱
    BuildFromDocument(ctx context.Context, docID string, chunks []*Chunk) error
    
    // 实体搜索
    SearchEntities(ctx context.Context, query string, opts SearchOptions) ([]*Entity, error)
    
    // 关系查询
    QueryRelations(ctx context.Context, entityID string, relationType RelationType) ([]*Relation, error)
    
    // 子图检索
    GetSubgraph(ctx context.Context, entityIDs []string, depth int) (*Subgraph, error)
    
    // 路径推理
    FindPath(ctx context.Context, sourceID, targetID string, maxDepth int) ([]*Path, error)
    
    // 自然语言查询
    NaturalLanguageQuery(ctx context.Context, question string) (*QueryResult, error)
}
```

---

## 3. 向量检索模块设计

### 3.1 概述
向量检索模块提供基于语义相似度的文档检索能力。

### 3.2 核心组件

```
┌─────────────────────────────────────────────────────────────┐
│                   Vector Retrieval Module                    │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │  Embedding   │  │   Encoder    │  │    Store     │       │
│  │   Service    │  │  (向量化)     │  │  (向量存储)   │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
│         │                 │                 │               │
│         ▼                 ▼                 ▼               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Vector Store (HNSW/SQLite/PG)           │   │
│  │  - Collection: 向量集合                                │   │
│  │  - Index: HNSW 内存索引                                │   │
│  │  - Metadata: 关联文档信息                              │   │
│  └──────────────────────────────────────────────────────┘   │
│                              │                              │
│                              ▼                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │                   ANN Search                          │   │
│  │  - 相似度计算 (Cosine/IP/L2)                          │   │
│  │  - 近似最近邻搜索                                      │   │
│  │  - 过滤搜索                                            │   │
│  │  - 多向量查询 (ColBERT)                                │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 3.3 支持的嵌入模型

| 模型 | 维度 | 语言 | 特点 |
|------|------|------|------|
| text-embedding-3-small | 1536 | 多语言 | OpenAI，性价比高 |
| text-embedding-3-large | 3072 | 多语言 | OpenAI，高质量 |
| BAAI/bge-m3 | 1024 | 多语言 | 开源，支持100+语言 |
| BAAI/bge-large-zh | 1024 | 中文 | 中文优化 |
| intfloat/e5-large-v2 | 1024 | 英文 | 高质量英文嵌入 |
| Alibaba-NLP/gte-large-zh | 1024 | 中文 | 阿里开源 |

### 3.4 配置定义

```go
type VectorConfig struct {
    // 模型配置
    ModelName    string  `json:"model_name"`     // 模型名称
    ModelPath    string  `json:"model_path"`     // 本地模型路径
    APIKey       string  `json:"api_key"`        // API密钥（OpenAI等）
    APIBase      string  `json:"api_base"`       // API基础URL
    
    // 向量配置
    Dimension    int     `json:"dimension"`      // 向量维度
    Normalize    bool    `json:"normalize"`      // 是否归一化
    
    // 存储配置
    StoreType    string  `json:"store_type"`     // milvus, chroma, qdrant
    StoreConfig  map[string]interface{} `json:"store_config"`
    
    // 索引配置
    IndexType    string  `json:"index_type"`     // HNSW, IVF_FLAT, etc.
    MetricType   string  `json:"metric_type"`    // COSINE, IP, L2
}
```

### 3.5 向量存储接口

```go
type VectorStore interface {
    // 初始化
    Init(ctx context.Context) error
    
    // 创建集合
    CreateCollection(ctx context.Context, name string, dim int) error
    
    // 插入向量
    Insert(ctx context.Context, vectors []*VectorRecord) error
    
    // 批量插入
    BatchInsert(ctx context.Context, vectors []*VectorRecord) error
    
    // 相似度搜索
    Search(ctx context.Context, queryVector []float32, topK int, filters map[string]interface{}) ([]*SearchResult, error)
    
    // 删除
    Delete(ctx context.Context, ids []string) error
    
    // 更新
    Update(ctx context.Context, vectors []*VectorRecord) error
    
    // 关闭
    Close() error
}

type VectorRecord struct {
    ID       string                 `json:"id"`
    Vector   []float32              `json:"vector"`
    Metadata map[string]interface{} `json:"metadata"`  // chunk_id, doc_id, etc.
}
```

### 3.6 检索接口

```go
type VectorRetriever interface {
    // 索引分块
    IndexChunks(ctx context.Context, chunks []*Chunk) error
    
    // 语义检索
    Retrieve(ctx context.Context, query string, topK int) ([]*RetrievalResult, error)
    
    // 批量检索
    BatchRetrieve(ctx context.Context, queries []string, topK int) ([][]*RetrievalResult, error)
    
    // 相似度检索（使用已有向量）
    RetrieveByVector(ctx context.Context, vector []float32, topK int) ([]*RetrievalResult, error)
    
    // 多查询检索
    MultiQueryRetrieve(ctx context.Context, queries []string, topK int) ([]*RetrievalResult, error)
    
    // 删除文档向量
    DeleteDocument(ctx context.Context, docID string) error
}
```

---

## 4. 关键词检索模块设计

### 4.1 概述
关键词检索模块提供基于词项匹配的传统信息检索能力。

### 4.2 核心组件

```
┌─────────────────────────────────────────────────────────────┐
│                 Keyword Retrieval Module                     │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │  Tokenizer   │  │   Analyzer   │  │    Index     │       │
│  │  (分词器)     │  │  (分析器)     │  │  (倒排索引)   │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
│         │                 │                 │               │
│         ▼                 ▼                 ▼               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │           Search Engine (内存倒排/FTS5/tsvector)     │   │
│  │  - Inverted Index: 倒排索引                            │   │
│  │  - BM25: 评分算法                                     │   │
│  │  - Boolean Query: 布尔查询                            │   │
│  └──────────────────────────────────────────────────────┘   │
│                              │                              │
│                              ▼                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │                   Query Parser                        │   │
│  │  - 简单查询                                            │   │
│  │  - 布尔查询 (AND/OR/NOT)                              │   │
│  │  - 短语查询                                            │   │
│  │  - 通配符查询                                          │   │
│  │  - 模糊查询                                            │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 4.3 分词器支持

| 分词器 | 语言 | 特点 |
|--------|------|------|
| Standard | 通用 | 基于Unicode文本分割 |
| IK | 中文 | 中文分词，支持细粒度 |
| Jieba | 中文 | Python流行中文分词 |
| Whitespace | 通用 | 按空白分割 |
| Keyword | 通用 | 不分词 |

### 4.4 分析器配置

```go
type AnalyzerConfig struct {
    Tokenizer  string   `json:"tokenizer"`   // 分词器类型
    Filters    []string `json:"filters"`     // 过滤器链
    
    // 过滤器选项
    Lowercase      bool     `json:"lowercase"`       // 转小写
    Stopwords      []string `json:"stopwords"`       // 停用词
    Synonyms       map[string][]string `json:"synonyms"` // 同义词
    Stemming       bool     `json:"stemming"`        // 词干提取
}
```

### 4.5 检索接口

```go
type KeywordRetriever interface {
    // 索引分块
    IndexChunks(ctx context.Context, chunks []*Chunk) error
    
    // 简单检索
    Search(ctx context.Context, query string, opts SearchOptions) (*SearchResult, error)
    
    // 布尔检索
    BooleanSearch(ctx context.Context, query BooleanQuery, opts SearchOptions) (*SearchResult, error)
    
    // 短语检索
    PhraseSearch(ctx context.Context, phrase string, opts SearchOptions) (*SearchResult, error)
    
    // 模糊检索
    FuzzySearch(ctx context.Context, query string, fuzziness int, opts SearchOptions) (*SearchResult, error)
    
    // 前缀检索
    PrefixSearch(ctx context.Context, prefix string, opts SearchOptions) (*SearchResult, error)
    
    // 通配符检索
    WildcardSearch(ctx context.Context, pattern string, opts SearchOptions) (*SearchResult, error)
    
    // 删除文档
    DeleteDocument(ctx context.Context, docID string) error
}
```

### 4.6 查询类型

```go
// 布尔查询
type BooleanQuery struct {
    Must    []Query `json:"must"`     // 必须包含
    Should  []Query `json:"should"`   // 应该包含
    MustNot []Query `json:"must_not"` // 必须不包含
    Filter  []Query `json:"filter"`   // 过滤条件
}

type Query struct {
    Type     QueryType `json:"type"`     // match, term, range, etc.
    Field    string    `json:"field"`    // 查询字段
    Value    interface{} `json:"value"`  // 查询值
    Boost    float64   `json:"boost"`    // 权重提升
}
```

---

## 5. 混合检索模块设计

### 5.1 概述
混合检索模块整合多种检索方式，提供更全面的召回能力。

### 5.2 融合策略

#### 5.2.1 RRF (Reciprocal Rank Fusion)
```go
func RRFusion(results [][]*RetrievalResult, k int) []*RetrievalResult {
    scores := make(map[string]float64)
    
    for _, resultSet := range results {
        for rank, result := range resultSet {
            scores[result.ID] += 1.0 / (float64(k) + float64(rank) + 1.0)
        }
    }
    
    // 按分数排序返回
    return sortByScore(scores)
}
```

#### 5.2.2 加权融合
```go
func WeightedFusion(results [][]*RetrievalResult, weights []float64) []*RetrievalResult {
    scores := make(map[string]float64)
    
    for i, resultSet := range results {
        weight := weights[i]
        for _, result := range resultSet {
            scores[result.ID] += result.Score * weight
        }
    }
    
    return sortByScore(scores)
}
```

### 5.3 查询路由

```go
type QueryRouter struct {
    Classifier QueryClassifier
    Routes     map[string]RetrievalStrategy
}

func (r *QueryRouter) Route(ctx context.Context, query string) (RetrievalStrategy, error) {
    // 分析查询类型
    queryType := r.Classifier.Classify(query)
    
    // 选择策略
    switch queryType {
    case QueryTypeFactual:
        return r.Routes["keyword_graph"], nil
    case QueryTypeSemantic:
        return r.Routes["vector"], nil
    case QueryTypeComplex:
        return r.Routes["hybrid"], nil
    default:
        return r.Routes["hybrid"], nil
    }
}
```

### 5.4 混合检索接口

```go
type HybridRetriever interface {
    // 配置检索器
    Configure(config HybridConfig) error
    
    // 混合检索
    Retrieve(ctx context.Context, query string, opts HybridOptions) (*HybridResult, error)
    
    // 多路召回
    MultiRetrieve(ctx context.Context, query string, strategies []Strategy) (*HybridResult, error)
}

type HybridOptions struct {
    TopK           int                `json:"top_k"`
    Strategies     []RetrievalType    `json:"strategies"`     // vector, keyword, graph
    FusionMethod   FusionMethod       `json:"fusion_method"`  // rrf, weighted
    Weights        map[string]float64 `json:"weights"`
    RRFK           int                `json:"rrf_k"`
}
```

---

## 6. 重排序模块设计

### 6.1 概述
重排序模块对召回结果进行精细化排序，提升检索质量。

### 6.2 重排序策略

#### 6.2.1 交叉编码器（启发式简化版）

> **注意**: `CrossEncoderReranker` 为启发式简化实现，非真正的交叉编码器模型。
> 基于查询词匹配度的重排序，无需外部模型依赖。适用于轻量级场景。
> 如需更强的重排序效果，建议使用 `LLMReranker` 或集成专门的交叉编码器模型（如 cross-encoder/ms-marco）。

```go
type CrossEncoderReranker struct {
    ModelName string
    Model     *CrossEncoder
}
```

func (r *CrossEncoderReranker) Rerank(ctx context.Context, query string, candidates []*Candidate) ([]*Candidate, error) {
    pairs := make([][2]string, len(candidates))
    for i, c := range candidates {
        pairs[i] = [2]string{query, c.Content}
    }
    
    scores, err := r.Model.Predict(pairs)
    if err != nil {
        return nil, err
    }
    
    for i, c := range candidates {
        c.RerankScore = scores[i]
    }
    
    return sortByRerankScore(candidates), nil
}
```

#### 6.2.2 LLM重排序
```go
type LLMReranker struct {
    Client LLMClient
    Prompt string
}

func (r *LLMReranker) Rerank(ctx context.Context, query string, candidates []*Candidate) ([]*Candidate, error) {
    // 构建提示
    prompt := buildRerankPrompt(query, candidates)
    
    // 调用LLM
    response, err := r.Client.Complete(ctx, prompt)
    if err != nil {
        return nil, err
    }
    
    // 解析排序结果
    return parseRerankResponse(response, candidates)
}
```

### 6.3 重排序接口

```go
type Reranker interface {
    // 重排序
    Rerank(ctx context.Context, query string, candidates []*Candidate, topN int) ([]*Candidate, error)
    
    // 批量重排序
    BatchRerank(ctx context.Context, queries []string, candidates [][]*Candidate) ([][]*Candidate, error)
}
```
