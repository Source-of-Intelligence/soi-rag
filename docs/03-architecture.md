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
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │   │
│  │  │  REST API   │  │   gRPC      │  │   CLI       │  │   SDK       │    │   │
│  │  │   (HTTP)    │  │  (Protobuf) │  │ (Command)   │  │  (Go/Python)│    │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘    │   │
│  └─────────────────────────────────────────────────────────────────────────┘   │
│                                    │                                            │
│                                    ▼                                            │
│  ┌─────────────────────────────────────────────────────────────────────────┐   │
│  │                        Service Orchestration Layer                       │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │   │
│  │  │   Query     │  │   Result    │  │   Cache     │  │   Rate      │    │   │
│  │  │  Processor  │  │   Merger    │  │   Manager   │  │   Limiter   │    │   │
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
│  │  │ • Doc Parser    │ │ • NER/RE         │ │ • Embedding     │           │   │
│  │  │ • Chunker       │ │ • Graph Builder  │ │ • ANN Search    │           │   │
│  │  │ • Index Manager │ │ • Cypher Query   │ │ • Vector Store  │           │   │
│  │  └─────────────────┘ └─────────────────┘ └─────────────────┘           │   │
│  │  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐           │   │
│  │  │ Keyword Search  │ │ Hybrid Search   │ │    Reranker     │           │   │
│  │  │     Module      │ │     Module      │ │     Module      │           │   │
│  │  │                 │ │                 │ │                 │           │   │
│  │  │ • BM25/TF-IDF   │ │ • Multi-Recall  │ │ • Cross-Encoder │           │   │
│  │  │ • Inverted Index│ │ • RRF Fusion    │ │ • LLM Rerank    │           │   │
│  │  │ • Query Parser  │ │ • Query Router  │ │ • Score Fusion  │           │   │
│  │  └─────────────────┘ └─────────────────┘ └─────────────────┘           │   │
│  └─────────────────────────────────────────────────────────────────────────┘   │
│                                    │                                            │
│                                    ▼                                            │
│  ┌─────────────────────────────────────────────────────────────────────────┐   │
│  │                         Storage Layer                                    │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │   │
│  │  │ PostgreSQL  │  │    Redis    │  │ Elasticsearch│  │    Milvus   │    │   │
│  │  │  (Metadata) │  │   (Cache)   │  │  (Keyword)  │  │   (Vector)  │    │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘    │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                      │   │
│  │  │    Neo4j    │  │    MinIO    │  │    Etcd     │                      │   │
│  │  │   (Graph)   │  │  (Object)   │  │  (Config)   │                      │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘                      │   │
│  └─────────────────────────────────────────────────────────────────────────┘   │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 分层说明

| 层级 | 职责 | 核心组件 |
|------|------|----------|
| API Gateway | 统一入口，协议转换 | REST API, gRPC, CLI, SDK |
| Service Orchestration | 服务编排，流量控制 | Query Processor, Cache, Rate Limiter |
| Retrieval Engine | 检索能力实现 | 6大检索模块 |
| Storage | 数据持久化 | 多种存储系统 |

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
   - 向量存储 → Milvus/Chroma
   - 关键词索引 → Elasticsearch
   - 知识图谱 → Neo4j
   - 元数据 → PostgreSQL

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

### 3.2 事件驱动架构

```go
// 定义核心事件
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

// 事件总线
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
# config.yaml
server:
  http:
    host: "0.0.0.0"
    port: 8080
  grpc:
    host: "0.0.0.0"
    port: 9090

retrieval:
  # PageIndex配置
  page_index:
    chunk_size: 512
    chunk_overlap: 50
    chunk_strategy: "recursive"
    supported_formats: ["pdf", "docx", "md", "html", "txt"]
  
  # 向量检索配置
  vector:
    model: "BAAI/bge-m3"
    dimension: 1024
    normalize: true
    store_type: "milvus"
    store_config:
      host: "localhost"
      port: 19530
      collection: "rag_vectors"
    index_type: "HNSW"
    metric_type: "COSINE"
  
  # 关键词检索配置
  keyword:
    engine: "elasticsearch"
    hosts: ["http://localhost:9200"]
    index_name: "rag_keywords"
    analyzer: "ik_max_word"
    similarity: "BM25"
  
  # 知识图谱配置
  knowledge_graph:
    enabled: true
    store_type: "neo4j"
    uri: "bolt://localhost:7687"
    username: "neo4j"
    password: "password"
    extraction_model: "gpt-4"
  
  # 混合检索配置
  hybrid:
    strategies: ["vector", "keyword", "graph"]
    fusion_method: "rrf"
    rrf_k: 60
    weights:
      vector: 0.4
      keyword: 0.4
      graph: 0.2
  
  # 重排序配置
  rerank:
    enabled: true
    model: "cross-encoder/ms-marco-MiniLM-L-6-v2"
    top_k: 100

storage:
  postgresql:
    host: "localhost"
    port: 5432
    database: "rag"
    username: "postgres"
    password: "password"
  
  redis:
    host: "localhost"
    port: 6379
    db: 0
    password: ""

logging:
  level: "info"
  format: "json"
  output: "stdout"

metrics:
  enabled: true
  port: 9091
```

---

## 7. 部署架构

### 7.1 Docker Compose部署

```yaml
# docker-compose.yml
version: '3.8'

services:
  rag-server:
    build: .
    ports:
      - "8080:8080"
      - "9090:9090"
    environment:
      - CONFIG_PATH=/app/config.yaml
    volumes:
      - ./config.yaml:/app/config.yaml
    depends_on:
      - postgres
      - redis
      - elasticsearch
      - milvus
      - neo4j

  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: rag
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: password
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data

  elasticsearch:
    image: elasticsearch:8.11.0
    environment:
      - discovery.type=single-node
      - xpack.security.enabled=false
    volumes:
      - elasticsearch_data:/usr/share/elasticsearch/data

  milvus:
    image: milvusdb/milvus:v2.3.3
    command: ["milvus", "run", "standalone"]
    volumes:
      - milvus_data:/var/lib/milvus

  neo4j:
    image: neo4j:5.14
    environment:
      NEO4J_AUTH: neo4j/password
    volumes:
      - neo4j_data:/data

volumes:
  postgres_data:
  redis_data:
  elasticsearch_data:
  milvus_data:
  neo4j_data:
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

```go
type CacheStrategy struct {
    // 多级缓存
    L1Cache *LocalCache      // 本地缓存 (LRU)
    L2Cache *RedisCache      // 分布式缓存
    
    // 缓存配置
    TTL          time.Duration
    MaxSize      int
    EvictionPolicy string
}

// 查询缓存键生成
func GenerateCacheKey(query string, opts SearchOptions) string {
    hasher := sha256.New()
    hasher.Write([]byte(query))
    hasher.Write([]byte(fmt.Sprintf("%v", opts)))
    return hex.EncodeToString(hasher.Sum(nil))
}
```

### 10.2 性能指标目标

| 指标 | 目标值 | 优化策略 |
|------|--------|----------|
| 查询P99延迟 | < 200ms | 缓存、预计算、并行化 |
| 索引吞吐量 | > 1000 docs/s | 批量处理、异步化 |
| 并发QPS | > 1000 | 连接池、限流、水平扩展 |
| 内存使用 | < 8GB | 流式处理、分页加载 |
