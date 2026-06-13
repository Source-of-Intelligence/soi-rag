# RAG Tool - 检索增强生成工具

一个功能完善的 RAG（Retrieval-Augmented Generation）工具，支持多种检索方式和存储后端，为 LLM 提供高质量的上下文信息。

## 功能特性

### 核心检索

| 检索方式 | 说明 |
|---------|------|
| **向量检索** | 语义相似度检索，支持 OpenAI / Ollama / 多语言 E5 / BGE-M3 嵌入模型，HNSW 近似最近邻索引 |
| **关键词检索** | BM25 评分 + 倒排索引，支持布尔/短语/前缀/模糊查询，GSE 中文分词 |
| **知识图谱** | 实体关系图谱构建与推理，规则抽取 + LLM 抽取双策略 |
| **混合检索** | RRF / 加权融合 + 查询路由，多路召回 |
| **重排序** | 交叉编码器 + 多样性(MMR) + RRF 管道 |

### 文档处理

- **多格式解析**: PDF、Word(.docx)、Markdown、HTML、TXT、CSV、JSON（统一的 `fileparser` 层）
- **结构化数据模型**: `document.Document` 支持页、章节、段落、标题、表格、列表等语义元素
- **分块策略**: 固定长度、递归分块、语义分块
- **批量索引**: Worker Pool 并发批处理
- **流式处理**: 大文件流式分块
- **文件监控**: fsnotify 自动检测文件变更并重新索引
- **SM3 去重**: 基于国密 SM3 算法的内容级去重

### 存储后端

| 存储类型 | 说明 |
|---------|------|
| **Memory** | 内存存储，适合测试和轻量场景 |
| **SQLite** | 本地文件存储，纯 Go 实现（modernc.org/sqlite），零依赖部署 |
| **PostgreSQL** | 生产级存储，支持 pgvector 向量扩展 |

三种存储可通过 `config.yaml` 一键切换，无需修改代码。

### LLM 集成

- **OpenAI** 兼容 API（支持自定义 API URL）
- **Ollama** 本地模型
- **MockLLM** 测试用
- RAG 问答 + 流式 SSE 输出
- 查询改写: 同义词扩展、HyDE、多查询生成

### 运维与可观测性

- **HTTP API**: RESTful 接口 + SSE 流式问答
- **查询缓存**: LRU + TTL
- **评估框架**: Recall@K、MRR、NDCG、MAP、Precision@K
- **Prometheus** 指标采集
- **OpenTelemetry** 链路追踪
- **ACL 权限控制**: 文档级读写权限

## 快速开始

### 安装

```bash
go get github.com/Source-of-Intelligence/soi-rag
```

### 基本使用

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/Source-of-Intelligence/soi-rag/pkg/rag"
)

func main() {
    ctx := context.Background()

    // 创建 RAG 引擎
    engine, err := rag.NewEngineWithMemory()
    if err != nil {
        log.Fatal(err)
    }
    defer engine.Close()

    // 添加文档
    doc, err := engine.AddDocumentFromText(ctx,
        "人工智能简介",
        "人工智能是计算机科学的一个分支...",
        "source",
    )
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("文档已添加: %s\n", doc.ID)

    // 搜索
    resp, err := engine.Query(ctx, &rag.QueryRequest{
        Query:         "什么是人工智能",
        TopK:          5,
        RetrievalType: "hybrid",
    })
    if err != nil {
        log.Fatal(err)
    }

    for _, r := range resp.Results {
        fmt.Printf("Score: %.4f, Content: %s\n", r.Score, r.Content)
    }
}
```

### 从文件添加文档

```go
// 自动检测文件类型（PDF/Word/Markdown 等）
doc, err := engine.AddDocumentFromFile(ctx, "/path/to/document.pdf")
```

### 直接使用文件解析器

你也可以直接使用底层的 `fileparser` 包获取结构化文档数据：

```go
import (
    "github.com/Source-of-Intelligence/soi-rag/pkg/document"
    "github.com/Source-of-Intelligence/soi-rag/pkg/fileparser"
)

// 方式一：使用管理器自动检测文件类型
pm := fileparser.NewManager()
doc, err := pm.ParseFromPath("report.pdf")
if err != nil {
    log.Fatal(err)
}

// 查看结构化数据
fmt.Printf("标题: %s\n", doc.Title)
fmt.Printf("页数: %d, 段落数: %d\n", doc.PageCount, doc.ParaCount)
fmt.Printf("总字符数: %d\n", len(doc.RawText()))

// 按页查看（PDF/DOCX）
for _, page := range doc.Pages {
    fmt.Printf("--- Page %d (%d 个元素) ---\n",
        page.PageNumber, len(page.Elements))
    for _, el := range page.Elements {
        fmt.Printf("  [%s] %s\n", el.Type(), truncate(el.Text(), 60))
    }
}

// 按章节查看（HTML/Markdown）
for _, sec := range doc.Sections {
    fmt.Printf("[H%d] %s\n", sec.Level, sec.Title)
}

// 漂亮打印整个文档结构（调试用）
fmt.Println(doc.PrettyPrint(""))
```

### 支持的文件格式与解析策略

| 文件类型 | 扩展名 | 解析器 | 输出结构 | 备注 |
|---------|--------|--------|---------|-----|
| **PDF** | `.pdf` | `pdf_parser.go` | Pages + Paragraphs/Headings | 对文字型 PDF 效果好；扫描件需 OCR |
| **Word** | `.docx` | `word_parser.go` | Elements + Heading Hierarchy | 通过 ZIP + XML 解析 |
| **HTML** | `.html` `.htm` | `html_parser.go` | Elements (Heading/Paragraph/List/Table) | 基于正则的块级元素抽取 |
| **Markdown** | `.md` `.markdown` | `markdown_parser.go` | Sections + Elements | 支持标题层级、代码块、列表 |
| **TXT** | `.txt` | `text_parser.go` | Elements (Paragraph) | 空行分段 |
| **CSV** | `.csv` | `csv_parser.go` | Elements (Table) | 第一行视为表头 |
| **JSON** | `.json` | `json_parser.go` | Elements (CodeBlock) | 格式化后展示 |

### 结构化数据模型

```go
document.Document
├── Title, Source, DocType          // 基础信息
├── PageCount, ParaCount, TableCount // 统计信息
├── Pages []*Page                   // 按页组织（PDF/DOCX）
│   └── Elements []Element          //   Paragraph / Heading / Table / ...
├── Sections []*Section             // 按章节组织（HTML/Markdown）
│   ├── Level, Title                //   H1-H6
│   └── Elements []Element
└── Elements []Element              // 顶层元素（无层级结构的文档）

// Element 类型
[paragraph] 普通段落
[heading]    标题（H1-H6）
[table]      表格（Headers + Rows）
[list]       列表（有序/无序）
[image]      图片（alt + src）
[code_block] 代码块（带语言）
[separator]  分隔线
```

### 使用文件解析诊断工具

```bash
cd cmd/test
go run . /path/to/document.pdf
go run . /path/to/folder/          # 自动扫描目录下的所有支持格式
```

也可以先编译后使用：

```bash
cd cmd/test
go build -o fileparser-test.exe
./fileparser-test.exe document.pdf
```

工具会打印：
- 文档基础信息（标题、页数、字符数）
- 按页/章节/元素的完整结构
- 元数据
- 全文纯文本预览

### 批量添加文档

```go
docs := []*models.Document{
    models.NewDocument("标题1", "内容1", "source1", models.DocTypeText),
    models.NewDocument("标题2", "内容2", "source2", models.DocTypeText),
}
result, err := engine.BatchAddDocuments(ctx, docs)
fmt.Printf("成功: %d, 失败: %d\n", result.Success, result.Failed)
```

### RAG 问答

```go
// 设置 LLM（通过配置文件或代码）
engine.SetLLM(llmInstance)

// 问答
answer, err := engine.Ask(ctx, "什么是人工智能？", rag.WithTopK(5))
fmt.Printf("回答: %s\n", answer.Answer)
fmt.Printf("引用来源: %d 个\n", len(answer.Sources))
```

### 配置文件驱动

创建 `config.yaml`：

```yaml
engine:
  chunk_size: 512
  chunk_overlap: 50
  chunk_strategy: recursive
  top_k: 10
  use_hybrid: true
  use_dedup: true

llm:
  provider: openai
  model: gpt-4o
  api_key: $OPENAI_API_KEY
  temperature: 0.7
  max_tokens: 2048

storage:
  type: sqlite
  sqlite:
    path: rag.db

api:
  host: 0.0.0.0
  port: 8080

# 检索质量评估配置（可选）
eval:
  enabled: true
  ks: [1, 5, 10, 20]
  min_recall: 0.0

# 质量检验阈值（可选）
threshold:
  min_recall_at_1: 0.3
  min_recall_at_5: 0.5
  min_recall_at_10: 0.7
  min_precision_at_5: 0.4
  min_mrr: 0.4
  min_ndcg_at_10: 0.5
  min_map: 0.4
```

通过代码加载配置：

```go
cfg, err := config.LoadFromFile("config.yaml")
ragConfig := cfg.ToRagConfig()
engine, err := rag.NewEngine(ragConfig)

llmInstance, err := cfg.ToLLM()
engine.SetLLM(llmInstance)
```

### 启动 HTTP 服务

```bash
# 使用默认 config.yaml
rag-server

# 指定配置文件
rag-server -c /path/to/config.yaml
```

API 端点：

```
POST   /api/v1/documents          添加文档
GET    /api/v1/documents          列出文档
GET    /api/v1/documents/:id      获取文档
DELETE /api/v1/documents/:id      删除文档
POST   /api/v1/search             搜索
POST   /api/v1/ask                RAG 问答
POST   /api/v1/ask/stream         RAG 流式问答 (SSE)
GET    /api/v1/stats              统计信息
GET    /api/v1/health             健康检查
```

### CLI 工具

```bash
rag -cmd=add -title="标题" -content="内容"
rag -cmd=search -query="查询" -topk=5 -type=hybrid
rag -cmd=list
rag -cmd=delete -id="文档ID"
rag -cmd=interactive
rag -cmd=stats

# 存储切换
rag -cmd=add ... -storage=sqlite -dbpath=rag.db
rag -cmd=add ... -storage=postgres -pgdb=rag -pguser=postgres
```

## 项目结构

```
rag/
├── cmd/
│   ├── rag/                  # CLI 工具
│   ├── rag-server/             # HTTP API 服务
│   └── test/                  # 文件解析器诊断工具（新增）
├── pkg/
│   ├── models/               # 数据模型 (Document, Chunk, Entity, Relation 等)
│   ├── document/             # 通用文档模型（新增）
│   │   └── document.go       # Document / Page / Section / Element
│   ├── fileparser/           # 文件解析器（新增）
│   │   ├── parser.go         # Parser 接口与管理器
│   │   ├── pdf_parser.go     # PDF 解析
│   │   ├── word_parser.go    # DOCX 解析
│   │   ├── html_parser.go    # HTML 解析
│   │   ├── markdown_parser.go # Markdown 解析
│   │   ├── text_parser.go   # 纯文本解析
│   │   ├── csv_parser.go    # CSV 解析
│   │   └── json_parser.go   # JSON 解析
│   ├── config/               # 配置管理 (YAML + 环境变量)
│   ├── llm/                  # LLM 接口 (OpenAI, Ollama, Mock)
│   ├── pageindex/            # 文档解析 + 分块 + 存储
│   ├── vector/               # 向量嵌入 + 检索 (HNSW, 多语言)
│   ├── keyword/             # 关键词检索 (BM25, GSE 中文分词)
│   ├── knowledgegraph/       # 知识图谱 (规则 + LLM 抽取)
│   ├── hybrid/             # 混合检索 (RRF, 加权融合)
│   ├── rerank/             # 重排序 (交叉编码器, MMR)
│   ├── dedup/              # SM3 去重
│   ├── resource/             # SM3 国密算法 (纯 Go)
│   ├── cache/              # LRU 缓存
│   ├── query/              # 查询改写 (同义词, HyDE, 多查询)
│   ├── eval/               # 评估框架 (Recall, MRR, NDCG, MAP)
│   ├── api/                # HTTP RESTful API
│   ├── watcher/            # 文件变更监控
│   ├── metrics/          # Prometheus 指标
│   ├── tracing/          # OpenTelemetry 追踪
│   ├── auth/             # ACL 权限控制
│   └── rag/              # RAG 核心引擎
├── docs/                     # 设计文档
├── examples/                 # 示例代码
├── rag_test/               # 集成测试
└── config.example.yaml      # 配置示例
```

## 架构概览

```
┌─────────────────────────────────────────────────────────────┐
│                         API Layer                            │
│              HTTP REST API / CLI / 代码调用                    │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                      RAG Engine                              │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │  Vector  │ │ Keyword  │ │  Graph   │ │  Hybrid  │       │
│  │  Search  │ │  Search  │ │  Search  │ │  Search  │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │ Reranker │ │  Dedup   │ │   LLM    │ │  Cache   │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                    File Parser Layer（新增）                  │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │ PDF Parser│ │ Word Parser│ │ HTML Parser│ │ MD Parser │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐                    │
│  │ TXT Parser│ │ CSV Parser│ │ JSON Parser│                    │
│  └──────────┘ └──────────┘ └──────────┘                    │
│        └──► 统一输出 document.Document 结构化数据               │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                      Storage Layer                           │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐                    │
│  │ Memory   │ │  SQLite  │ │PostgreSQL│                    │
│  │ (默认)   │ │ (本地)   │ │ (生产)   │                    │
│  └──────────┘ └──────────┘ └──────────┘                    │
└─────────────────────────────────────────────────────────────┘
```

## 设计文档

- [01-requirements.md](docs/01-requirements.md) - 需求规格说明书
- [02-module-design.md](docs/02-module-design.md) - 模块详细设计
- [03-architecture.md](docs/03-architecture.md) - 整体架构设计
- [04-features-design.md](docs/04-features-design.md) - 扩展特性设计

### 质量检验

```go
// 执行评估
result := evaluator.Evaluate(ctx, engine, testQueries)

// 质量检验（对比阈值）
check := result.CheckQuality(&config.QualityThreshold{
    MinRecallAt5: 0.5,
    MinMRR: 0.4,
})

if check.Passed {
    fmt.Println("质量检验通过")
} else {
    for _, w := range check.Warnings {
        fmt.Println("警告:", w)
    }
}
```

### 使用配置文件的阈值

```go
cfg := config.DefaultFileConfig()
result := evaluator.Evaluate(ctx, engine, testQueries)
check := result.CheckQualityWithConfig(cfg)
fmt.Printf("综合得分: %.4f, 通过: %v\n", check.Score, check.Passed)
```

## 已知问题与修复

| 问题 | 状态 | 说明 |
|------|------|------|
| HNSW 并发安全 | ✅ 已修复 | `Search` 中 `visited` map 加锁保护，使用线程安全随机数 |
| BatchIndexer panic | ✅ 已修复 | `NewBatchIndexer` 改为返回 error |
| 模板重复解析 | ✅ 已修复 | `PromptTemplate` 使用 `sync.Once` 缓存解析结果 |
| 流式响应错误处理 | ✅ 已修复 | `AskStream` 中 `json.Marshal` 错误不再忽略 |
| Evaluator String() | ✅ 已修复 | 使用 `strings.Builder` 替代字符串拼接 |
| **PDF 扫描件识别** | ⚠️ 计划中 | `ledongthuc/pdf` 对图片型 PDF 效果有限，计划集成 OCR |

## 技术栈

- **语言**: Go 1.25+ (纯 Go 实现，无 CGO 依赖)
- **存储**: SQLite (modernc.org/sqlite) / PostgreSQL (lib/pq + pgvector)
- **中文分词**: go-ego/gse (纯 Go)
- **PDF 解析**: ledongthuc/pdf (纯 Go)
- **配置**: YAML (gopkg.in/yaml.v3)
- **可观测性**: Prometheus + OpenTelemetry
- **文件监控**: fsnotify

## 许可证

MIT License
