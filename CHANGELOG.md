# Changelog

本文件记录 RAG 项目的所有重要变更。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

---

## [Unreleased]

### 新增 (Added)

#### pkg/document/ - 通用文档数据模型（新模块）
- **[P0] 通用 Document 数据模型**: 定义 `document.Document` 作为所有文件解析器的统一输出格式，取代仅存储纯文本的方式
- **[P0] 内容元素体系**: 定义 `Element` 接口与 7 种具体元素类型：
  - `Paragraph`（段落）、`Heading`（标题 H1-H6）、`Table`（表格）
  - `List`（列表）、`Image`（图片）、`CodeBlock`（代码块）、`Separator`（分隔线）
- **[P1] 结构化组织**: 支持按页（`Page`，PDF/DOCX）、按章节（`Section`，HTML/Markdown）、按元素（顶层内容）三种组织方式
- **[P1] 便捷方法**: `doc.RawText()`（提取全文纯文本，用于向量/关键词检索）、`doc.PrettyPrint(indent)`（格式化打印文档结构，调试用）
- **[P1] 元数据与统计**: `PageCount`、`ParaCount`、`TableCount`、`ImageCount`、`Metadata` map 供各解析器填充额外信息

#### pkg/fileparser/ - 文件解析器集合（新模块）
- **[P0] Parser 接口与 ParserManager**: 定义统一 `Parser` 接口，`ParserManager` 按文件扩展名自动分发到对应解析器
- **[P0] PDFParser**: 使用 `ledongthuc/pdf` 库，按页提取文本，启发式识别标题段落
- **[P0] WordParser**: 解析 `.docx`（ZIP+XML），提取段落、标题、表格
- **[P0] HTMLParser**: 基于正则匹配 HTML 块级标签（h1-h6、p、ul/ol、table、pre），提取结构化内容
- **[P0] MarkdownParser**: 使用状态机解析 `#` 标题、代码块、列表项，自动构建章节嵌套树
- **[P1] TextParser**: 按空行分段，处理纯文本文件
- **[P1] CSVParser**: 使用 `encoding/csv`，首行为表头，其余为数据行，输出 `Table` 元素
- **[P1] JSONParser**: 美化 JSON 后作为 `CodeBlock`（带语言标识）输出

#### cmd/test/ - 文件解析诊断工具（新增）
- **[P1] CLI 诊断工具**: 支持单文件或目录扫描，输出每个文件的结构化解析结果
- 输出内容：基础信息（标题、类型、页数、段落数、字符数）、元数据、结构化内容（按页/章节/元素）、全文纯文本预览
- 可用于验证 PDF / DOCX / HTML / Markdown / TXT / CSV / JSON 的解析效果

### 修复 (Bug Fixes)

#### pkg/rag/engine.go - 核心引擎
- **[P0] 并发安全**: 为 Engine 添加 `sync.RWMutex`，所有 setter 方法（SetLLM、SetEmbedder、SetReranker、SetCache、SetDedupEnabled、SetPromptTemplate、SetKeywordTokenizer）和 getter 方法（GetCache、IsDedupEnabled）添加读写锁保护，防止多 goroutine 竞态
- **[P0] 错误处理一致性**: 统一非致命错误的处理方式，将 `fmt.Printf` 替换为 `log.Printf`，添加 `[WARN]` 级别标记和 docID 上下文信息，明确标注"非致命错误，不阻止主流程"
- **[P1] 缓存键生成**: 将 `buildCacheKey` 从不完整的 `fmt.Sprintf` 改为完整的 JSON 序列化 `SearchOptions`，避免不同字段组合产生相同的缓存键

#### pkg/config/config.go - 配置验证
- **[P0] 配置验证增强**: Validate 方法新增以下验证规则：
  - LLM Provider 合法性检查（仅允许 openai/ollama/mock）
  - 模型名称格式验证（正则校验，防止中文冒号等非法字符）
  - 引擎参数合理性检查（chunk_size/chunk_overlap/top_k 非负数，overlap < size）
  - 存储类型合法性检查
  - API 端口范围检查（0-65535）

#### config.yaml - 配置文件
- **[P0] 修复模型名称**: 将 `qwen3.5：9b`（中文冒号）修正为 `qwen2.5:9b`（英文冒号）

#### pkg/hybrid/fusion.go - 混合检索
- **[P1] 修复 WeightedFusion 权重映射**: 修复原实现中 map 遍历顺序不确定导致权重分配错误的问题。改为按固定策略名称（vector/keyword/graph）映射权重，未配置的策略使用均等默认权重，并添加权重归一化

#### pkg/rerank/reranker.go - 重排序器
- **[P1] 标注简化版实现**: 将 `CrossEncoderReranker` 的注释明确标注为"基于词匹配的启发式重排序器（简化版）"，说明与真正交叉编码器的区别，避免使用者误解
- **[P2] 提取魔法数字**: 将重排序分数权重 `0.3`/`0.7` 提取为命名常量 `rerankOriginalWeight`/`rerankMatchWeight`

#### pkg/pageindex/pageindex.go - 文档索引
- **[P1] 解析回退日志**: 当文档类型无匹配解析器回退到文本解析器时，添加 `log.Printf` 警告日志，记录文档类型和来源信息

#### pkg/api/handlers.go - API 处理器
- **[P2] 请求超时控制**: 为 Ask 接口添加 30 秒请求超时（`context.WithTimeout`），防止长时间 LLM 调用阻塞 HTTP 连接
- **[P2] 健康检查增强**: HealthCheck 接口新增返回存储类型、去重状态、LLM 可用性等依赖服务状态信息

### 变更文件清单

| 文件 | 变更类型 | 严重级别 |
|------|----------|----------|
| `pkg/rag/engine.go` | 修复 + 增强 | P0 / P1 |
| `pkg/config/config.go` | 增强 | P0 |
| `config.yaml` | 修复 | P0 |
| `pkg/hybrid/fusion.go` | 修复 | P1 |
| `pkg/rerank/reranker.go` | 标注 + 重构 | P1 / P2 |
| `pkg/pageindex/pageindex.go` | 增强 | P1 |
| `pkg/api/handlers.go` | 增强 | P2 |
