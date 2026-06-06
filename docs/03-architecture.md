# RAG工具整体架构设计文档

## 1. 系统架构概览

### 1.1 整体架构图

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              RAG System Architecture                             │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                 │
│  ┌─────────────────────────────────────────────────────────────────────────┐   │
│  │                         API Gateway Layer                                │   │
│  │  ┌─────────────┐  ┌─────────────────────────────────────────────┐     │   │
│  │  │  REST API   │  │   SSE Streaming (流式问答)                   │     │   │
│  │  │   (HTTP)    │  │                                             │     │   │
│  │  └─────────────┘  └─────────────────────────────────────────────┘     │   │
│  └─────────────────────────────────────────────────────────────────────────┘   │
│                                    │                                            │
│                                    ▼                                            │
│  ┌─────────────────────────────────────────────────────────────────────────┐   │
│  │                        Service Orchestration Layer                       │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │   │
│  │  │   Query     │  │   Result    │  │   Cache     │  │   Dedup     │    │   │
│  │  │  Processor  │  │   Merger    │  │   Manager   │  │   Service   │    │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘    │   │
│  └─────────────────────────────────────────────────────────────────────────┘   │
│                                    │                                            │
│                                    ▼                                            │
│  ┌─────────────────────────────────────────────────────────────────────────┐   │
│  │                        Retrieval Engine Layer                            │   │
│  │  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐           │   │
│  │  │   PageIndex     │ │  Knowledge Graph │ │  Vector Search  │           │   │
│  │  │   Module        │ │     Module       │ │     Module      │           │   │
│  │  │                 │ │                  │ │                 │           │   │
│  │  │ • Doc Parser    │ │ • LLM Extractor  │ │ • HNSW Index    │           │   │
│  │  │ • Chunker       │ │ • Graph Builder  │ │ • ANN Search    │           │   │
│  │  │ • Index Manager │ │ • Graph Query    │ │ • Embedder      │           │   │
│  │  └─────────────────┘ └─────────────────┘ └─────────────────┘           │   │
│  │  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐           │   │
│  │  │ Keyword Search  │ │ Hybrid Search   │ │    Reranker     │           │   │
│  │  │     Module      │ │     Module      │ │     Module      │           │   │
│  │  │                 │ │                 │ │                 │           │   │
│  │  │ • BM25 Scoring  │ │ • Multi-Recall  │ │ • Heuristic     │           │   │
│  │  │ • Inverted Index│ │ • RRF Fusion    │ │ • Diversity     │           │   │
│  │  │ • Query Parser  │ │ • Query Router  │ │ • Rerank Pipeline│          │   │
│  │  └─────────────────┘ └─────────────────┘ └─────────────────┘           │   │
│  └─────────────────────────────────────────────────────────────────────────┘   │
│                                    │                                            │
│                                    ▼                                            │
│  ┌─────────────────────────────────────────────────────────────────────────┐   │
│  │                         Storage Layer                                    │   │
│  │  ┌───────────────────────┐  ┌───────────────────────┐                    │   │
│  │  │   SQLite (默认)       │  │   PostgreSQL (可选)    │                    │   │
│  │  │  • 文档和分块存储      │  │  • 文档和分块存储      │                    │   │
│  │  │  • FTS5 全文检索       │  │  • tsvector 全文检索    │                    │   │
│  │  │  • 向量 BLOB 存储      │  │  • pgvector 向量存储    │                    │   │
│  │  │  • 知识图谱关系表      │  │  • 知识图谱关系表       │                    │   │
│  │  │  • 去重索引            │  │  • 去重索引             │                    │   │
│  │  └───────────────────────┘  └───────────────────────┘                    │   │
│  │  ┌───────────────────────┐  ┌───────────────────────┐                    │   │
│  │  │   Memory (开发/测试)   │  │   LRU Cache (内存)    │                    │   │
│  │  │  • 内存向量存储(HNSW)  │  │  • 查询结果缓存         │                    │   │
│  │  │  • 内存倒排索引        │  │  • TTL 过期策略        │                    │   │
│  │  │  • 内存图谱存储        │  │                       │                    │   │
│  │  └───────────────────────┘  └───────────────────────┘                    │   │
│  └─────────────────────────────────────────────────────────────────────────┘   │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 分层说明

| 层级 | 职责 | 核心组件 |
|------|------|----------|
| API Gateway | 统一入口，协议支持 | REST API (net/http), SSE 流式问答 |
| Service Orchestration | 服务编排，查询处理 | Query Processor, Cache, Dedup Service |
| Retrieval Engine | 检索能力实现 | 6大检索模块 |
| Storage | 数据持久化 | SQLite（默认）/ PostgreSQL（可选）/ Memory（开发测试） |

---

## 2. 核心流程设计

### 2.1 文档索引流程

```
┌─────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐
│ Document│───▶│  Parser  │───▶│ Chunker  │───▶│ Embedder │───▶│  Store   │
│  Input  │    │          │    │          │    │          │    │          │
└─────────┘    └──────────┘    └──────────┘    └──────────┘    └──────────┘
     │              │              │              │              │
     │              │              │              │              │
     ▼              ▼              ▼              ▼              ▼
  File/URL    Raw Text      Chunks[]      Vectors[]      Multi-Store
  (PDF/DOC)   + Metadata    + Position    + Metadata     (Vector/Graph/Keyword)
```

**详细步骤：**

1. **文档解析 (Parser)**
   - 识别文档类型
   - 提取文本内容
   - 提取元数据（标题、作者、时间等）

2. **文档分块 (Chunker)**
   - 应用分块策略
   - 保持上下文连贯性
   - 记录位置信息

3. **向量化 (Embedder)**
   - 调用嵌入模型
   - 生成向量表示
   - 归一化处理

4. **多路存储 (Store)**
   - 向量存储 → HNSW 内存索引 / SQLite BLOB / PostgreSQL pgvector
   - 关键词索引 → 内存倒排索引 / SQLite FTS5 / PostgreSQL tsvector
   - 知识图谱 → 内存图谱 / SQLite 关系表 / PostgreSQL 关系表
   - 元数据 → Memory Store / SQLite / PostgreSQL

### 2.2 检索流程

```
┌─────────┐    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│  Query  │───▶│ Query Analyze│───▶│ Multi-Recall │───▶│   Rerank    │
│  Input  │    │              │    │              │    │             │
└─────────┘    └──────────────┘    └──────────────┘    └──────────────┘
     │               │                   │                   │
     │               │                   │                   │
     ▼               ▼                   ▼                   ▼
  User Query    Query Type        Results[]           Final Results
                + Expansion       (Vector/Keyword/    (Sorted by
                + Rewrite         Graph)               Relevance)
```

**详细步骤：**

1. **查询分析 (Query Analyzer)**
   - 意图识别
   - 查询分类（事实型/语义型/复杂型）
   - 查询扩展（同义词、相关词）
   - 查询重写

2. **多路召回 (Multi-Recall)**
   - 向量检索（语义相似）
   - 关键词检索（精确匹配）
   - 图谱检索（实体关系）
   - 结果融合（RRF/加权）

3. **重排序 (Rerank)**
   - 交叉编码器打分
   - 多样性去重
   - 最终排序

---

## 3. 模块交互设计

### 3.1 模块依赖关系

```
                    ┌─────────────┐
                    │   API Layer │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
              ▼            ▼            ▼
       ┌──────────┐ ┌──────────┐ ┌──────────┐
       │  Hybrid  │ │  Rerank  │ │  Query   │
       │  Search  │ │          │ │ Processor│
       └────┬─────┘ └────┬─────┘ └────┬─────┘
            │            │            │
    ┌───────┼───────┐    │            │
    │       │       │    │            │
    ▼       ▼       ▼    │            │
┌──────┐ ┌──────┐ ┌──────┐│           │
│Page  │ │Vector│ │Keyword││           │
│Index │ │Search│ │Search ││           │
└──┬───┘ └──┬───┘ └──┬───┘│           │
   │        │        │    │           │
   │        │        │    │           │
   ▼        ▼        ▼    ▼           ▼
┌─────────────────────────────────────────┐
│           Knowledge Graph               │
└─────────────────────────────────────────┘
```

### 3.2 事件驱动架构（规划中）

> **注意**: 以下事件总线设计为未来规划，当前版本尚未实现。当前版本使用直接的函数调用模式。

```go
// 定义核心事件（规划中）
type EventType string

const (
    EventDocumentAdded     EventType = "document.added"
    EventDocumentUpdated   EventType = "document.updated"
    EventDocumentDeleted   EventType = "document.deleted"
    EventChunkIndexed      EventType = "chunk.indexed"
    EventEntityExtracted   EventType = "entity.extracted"
    EventRelationExtracted EventType = "relation.extracted"
    EventQueryReceived     EventType = "query.received"
    EventSearchCompleted   EventType = "search.completed"
)

type Event struct {
    Type      EventType              `json:"type"`
    Timestamp time.Time              `json:"timestamp"`
    Payload   map[string]interface{} `json:"payload"`
    Source    string                 `json:"source"`
}

// 事件总线（规划中）
type EventBus interface {
    Publish(ctx context.Context, event *Event) error
    Subscribe(eventType EventType, handler EventHandler) error
    Unsubscribe(eventType EventType, handler EventHandler) error
}
```

---

## 4. 数据流设计

### 4.1 数据模型关系

```
┌──────────────┐       ┌──────────────┐       ┌──────────────┐
│  Document    │◄─────►│    Chunk     │◄─────►│   Vector     │
│  ─────────   │  1:N  │  ─────────   │  1:1  │  ─────────   │
│  id          │       │  id          │       │  id          │
│  title       │       │  document_id │       │  chunk_id    │
│  content     │       │  content     │       │  vector      │
│  source      │       │  position    │       │  metadata    │
│  metadata    │       │  token_count │       │              │
└──────────────┘       └──────────────┘       └──────────────┘
        │
        │ 1:N
        ▼
┌──────────────┐       ┌──────────────┐
│   Entity     │◄─────►│  Relation    │
│  ─────────   │  N:M  │  ─────────   │
│  id          │       │  id          │
│  name        │       │  source_id   │
│  type        │       │  target_id   │
│  doc_id      │       │  type        │
└──────────────┘       └──────────────┘
```

### 4.2 数据一致性策略

| 场景 | 策略 | 实现方式 |
|------|------|----------|
| 文档新增 | 最终一致性 | 异步索引，消息队列 |
| 文档更新 | 版本控制 | 乐观锁，版本号 |
| 文档删除 | 软删除 | 标记删除，定期清理 |
| 跨存储同步 | 事务消息 | Outbox模式 |

---

## 5. 扩展性设计

### 5.1 插件架构

```go
// 插件接口定义
type Plugin interface {
    Name() string
    Version() string
    Initialize(config map[string]interface{}) error
    Shutdown() error
}

// 检索插件
type RetrieverPlugin interface {
    Plugin
    Retrieve(ctx context.Context, query string, opts Options) (*Result, error)
}

// 嵌入模型插件
type EmbeddingPlugin interface {
    Plugin
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimension() int
}

// 重排序插件
type RerankerPlugin interface {
    Plugin
    Rerank(ctx context.Context, query string, candidates []*Candidate) ([]*Candidate, error)
}

// 插件管理器
type PluginManager struct {
    plugins map[string]Plugin
    loaders []PluginLoader
}
```

### 5.2 水平扩展

```
                    ┌─────────────┐
                    │   Load      │
                    │  Balancer   │
                    └──────┬──────┘
                           │
           ┌───────────────┼───────────────┐
           │               │               │
           ▼               ▼               ▼
    ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
    │   RAG       │ │   RAG       │ │   RAG       │
    │  Server 1   │ │  Server 2   │ │  Server N   │
    └──────┬──────┘ └──────┬──────┘ └──────┬──────┘
           │               │               │
           └───────────────┼───────────────┘
                           │
           ┌───────────────┼───────────────┐
           │               │               │
           ▼               ▼               ▼
    ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
    │   Shared    │ │   Shared    │ │   Shared    │
    │   Cache     │ │   Queue     │ │   Config    │
    │   (Redis)   │ │   (Redis)   │ │   (Etcd)    │
    └─────────────┘ └─────────────┘ └─────────────┘
```

---

## 6. 配置管理

### 6.1 配置结构

```yaml
# config.yaml - 实际配置格式
engine:
  chunk_size: 512
  chunk_overlap: 50
  chunk_strategy: "recursive"  # fixed, recursive, semantic
  top_k: 10
  use_reranker: false
  use_hybrid: true
  use_dedup: true

# LLM 配置
# provider 支持: openai, ollama, mock
# api_key 支持环境变量引用: $OPENAI_API_KEY 或 ${OPENAI_API_KEY}
llm:
  provider: ollama
  model: qwen2.5:9b
  api_key: ""
  api_url: "http://localhost:11434"
  temperature: 0.7
  max_tokens: 4096

# 存储配置
# type 支持: memory, sqlite, postgres
storage:
  type: sqlite
  sqlite:
    path: rag.db
  postgres:
    host: localhost
    port: 5432
    dbname: rag
    user: postgres
    password: ""
    ssl_mode: disable
    max_open: 10
    max_idle: 5

# HTTP API 配置
api:
  host: 0.0.0.0
  port: 8080

# 检索质量评估阈值（可选）
threshold:
  min_recall_at_1: 0.3
  min_recall_at_5: 0.5
  min_recall_at_10: 0.7
  min_precision_at_5: 0.4
  min_mrr: 0.4
  min_ndcg_at_10: 0.5
  min_map: 0.4
```

---

## 7. 部署架构

### 7.1 单机部署（推荐）

当前版本设计为轻量级单机部署，无需外部依赖：

```bash
# 使用 SQLite 存储（零依赖）
./rag-server -config config.yaml

# 使用 PostgreSQL 存储
./rag-server -config config.yaml  # config.yaml 中 storage.type: postgres
```

**依赖说明：**

| 存储类型 | 外部依赖 | 适用场景 |
|----------|----------|----------|
| memory | 无 | 开发、测试 |
| sqlite | 无 | 单机生产、嵌入式 |
| postgres | PostgreSQL | 多实例共享、大数据量 |

### 7.2 Docker 部署（SQLite 模式）

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o rag-server ./cmd/rag-server

FROM alpine:latest
COPY --from=builder /app/rag-server /usr/local/bin/
COPY config.yaml /app/config.yaml
EXPOSE 8080
CMD ["rag-server", "-config", "/app/config.yaml"]
```

### 7.3 Docker Compose 部署（PostgreSQL 模式）

```yaml
# docker-compose.yml
version: '3.8'

services:
  rag-server:
    build: .
    ports:
      - "8080:8080"
    environment:
      - CONFIG_PATH=/app/config.yaml
    volumes:
      - ./config.yaml:/app/config.yaml
      - rag-data:/app/data
    depends_on:
      - postgres

  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: rag
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: ${PG_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data

volumes:
  rag-data:
  postgres_data:
```

### 7.2 Kubernetes部署

```yaml
# k8s-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rag-server
spec:
  replicas: 3
  selector:
    matchLabels:
      app: rag-server
  template:
    metadata:
      labels:
        app: rag-server
    spec:
      containers:
      - name: rag-server
        image: rag-server:latest
        ports:
        - containerPort: 8080
        - containerPort: 9090
        env:
        - name: CONFIG_PATH
          value: "/app/config.yaml"
        volumeMounts:
        - name: config
          mountPath: /app/config.yaml
          subPath: config.yaml
      volumes:
      - name: config
        configMap:
          name: rag-config
---
apiVersion: v1
kind: Service
metadata:
  name: rag-server
spec:
  selector:
    app: rag-server
  ports:
  - name: http
    port: 8080
    targetPort: 8080
  - name: grpc
    port: 9090
    targetPort: 9090
  type: LoadBalancer
```

---

## 8. 监控与运维

### 8.1 监控指标

| 指标类别 | 指标名称 | 说明 |
|----------|----------|------|
| 系统指标 | cpu_usage | CPU使用率 |
| | memory_usage | 内存使用率 |
| | goroutine_count | Goroutine数量 |
| 业务指标 | query_qps | 查询QPS |
| | query_latency | 查询延迟 |
| | index_rate | 索引速率 |
| | retrieval_recall | 检索召回率 |
| | retrieval_precision | 检索精确率 |
| 存储指标 | storage_size | 存储大小 |
| | index_size | 索引大小 |
| | cache_hit_rate | 缓存命中率 |

### 8.2 健康检查

```go
// 健康检查接口
type HealthChecker interface {
    Check(ctx context.Context) (*HealthStatus, error)
}

type HealthStatus struct {
    Status    HealthState              `json:"status"`
    Timestamp time.Time                `json:"timestamp"`
    Checks    map[string]ComponentCheck `json:"checks"`
}

type ComponentCheck struct {
    Status  HealthState `json:"status"`
    Message string      `json:"message"`
    Latency int64       `json:"latency_ms"`
}
```

---

## 9. 安全设计

### 9.1 认证授权

```go
// API认证中间件
type AuthMiddleware struct {
    TokenValidator TokenValidator
    RateLimiter    RateLimiter
}

func (m *AuthMiddleware) Handle(next Handler) Handler {
    return func(ctx context.Context, req *Request) (*Response, error) {
        // 验证Token
        token := extractToken(req)
        claims, err := m.TokenValidator.Validate(token)
        if err != nil {
            return nil, ErrUnauthorized
        }
        
        // 检查限流
        if !m.RateLimiter.Allow(claims.UserID) {
            return nil, ErrRateLimited
        }
        
        // 设置上下文
        ctx = WithUserID(ctx, claims.UserID)
        ctx = WithPermissions(ctx, claims.Permissions)
        
        return next(ctx, req)
    }
}
```

### 9.2 数据安全

- 传输加密：TLS 1.3
- 存储加密：AES-256
- 敏感数据脱敏
- 访问日志审计

---

## 10. 性能优化

### 10.1 缓存策略

当前版本使用内存 LRU 缓存，支持 TTL 过期：

```go
// pkg/cache/cache.go
type Cache interface {
    Get(key string) (interface{}, bool)
    Set(key string, value interface{})
    SetWithTTL(key string, value interface{}, ttl time.Duration)
    Delete(key string)
    Clear()
    Stats() Stats
}

type LRUCache struct { ... }
func NewLRUCache(maxSize int, defaultTTL time.Duration) *LRUCache

// 查询缓存键生成（使用 JSON 序列化确保唯一性）
func (e *Engine) buildCacheKey(query string, opts SearchOptions) string {
    h := sha256.New()
    h.Write([]byte(query))
    optsData, _ := json.Marshal(opts)
    h.Write(optsData)
    return hex.EncodeToString(h.Sum(nil))
}
```

### 10.2 性能指标目标

| 指标 | 目标值 | 优化策略 |
|------|--------|----------|
| 查询P99延迟 | < 200ms | 缓存、预计算、并行化 |
| 索引吞吐量 | > 1000 docs/s | 批量处理、异步化 |
| 并发QPS | > 1000 | 连接池、限流、水平扩展 |
| 内存使用 | < 8GB | 流式处理、分页加载 |
