package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/ragtool/rag/pkg/models"
	"github.com/ragtool/rag/pkg/rag"
)

// API 请求超时常量
const (
	// defaultRequestTimeout 默认 API 请求超时时间
	defaultRequestTimeout = 30 * time.Second
)

// Handlers 请求处理器集合
type Handlers struct {
	engine *rag.Engine
}

// NewHandlers 创建处理器
func NewHandlers(engine *rag.Engine) *Handlers {
	return &Handlers{engine: engine}
}

// Response 通用响应结构
type Response struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Message   string      `json:"message,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Success   bool   `json:"success"`
	Error     string `json:"error"`
	Code      int    `json:"code"`
	Timestamp int64  `json:"timestamp"`
}

// writeJSON 写入JSON响应
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeSuccess 写入成功响应
func writeSuccess(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, Response{
		Success:   true,
		Data:      data,
		Timestamp: time.Now().Unix(),
	})
}

// writeError 写入错误响应
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{
		Success:   false,
		Error:     message,
		Code:      status,
		Timestamp: time.Now().Unix(),
	})
}

// Index 首页
func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"name":    "RAG API Server",
		"version": "1.0.0",
		"endpoints": []string{
			"POST   /api/v1/documents     - 添加文档",
			"GET    /api/v1/documents     - 列出文档",
			"GET    /api/v1/documents/:id - 获取文档",
			"DELETE /api/v1/documents/:id - 删除文档",
			"POST   /api/v1/search        - 搜索",
			"POST   /api/v1/ask           - RAG问答",
			"GET    /api/v1/stats         - 统计信息",
			"GET    /api/v1/health        - 健康检查",
		},
	}
	writeSuccess(w, info)
}

// HealthCheck 健康检查（包含依赖服务状态）
func (h *Handlers) HealthCheck(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
	}

	// 检查引擎状态
	if h.engine != nil {
		stats := h.engine.GetStats()
		health["storage_type"] = stats["storage_type"]
		health["dedup_enabled"] = stats["dedup_enabled"]

		// 检查 LLM 是否可用
		if h.engine.GetLLM() != nil {
			health["llm_status"] = "available"
		} else {
			health["llm_status"] = "not_configured"
		}
	}

	writeSuccess(w, health)
}

// AddDocumentRequest 添加文档请求
type AddDocumentRequest struct {
	Title    string                 `json:"title"`
	Content  string                 `json:"content"`
	Source   string                 `json:"source,omitempty"`
	DocType  string                 `json:"doc_type,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// AddDocument 添加文档
func (h *Handlers) AddDocument(w http.ResponseWriter, r *http.Request) {
	var req AddDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的JSON格式: "+err.Error())
		return
	}

	// 验证必填字段
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "标题不能为空")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "内容不能为空")
		return
	}

	// 创建文档
	docType := models.DocTypeText
	if req.DocType != "" {
		docType = models.DocumentType(req.DocType)
	}

	doc := &models.Document{
		ID:       uuid.New().String(),
		Title:    req.Title,
		Content:  req.Content,
		Source:   req.Source,
		DocType:  docType,
		Metadata: req.Metadata,
		Status:   models.DocStatusPending,
	}

	// 添加文档
	result, err := h.engine.AddDocumentWithDedup(r.Context(), doc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "添加文档失败: "+err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"document":     result.Document,
		"hash":         result.Hash,
		"is_duplicate": result.IsDuplicate,
		"skipped":      result.Skipped,
	})
}

// ListDocumentsRequest 列出文档请求
type ListDocumentsRequest struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// ListDocuments 列出文档
func (h *Handlers) ListDocuments(w http.ResponseWriter, r *http.Request) {
	// 解析查询参数
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	docs, err := h.engine.ListDocuments(r.Context(), offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "获取文档列表失败: "+err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"documents": docs,
		"offset":    offset,
		"limit":     limit,
		"count":     len(docs),
	})
}

// GetDocument 获取文档
func (h *Handlers) GetDocument(w http.ResponseWriter, r *http.Request) {
	// 从路径提取ID
	id := extractID(r.URL.Path)
	if id == "" {
		writeError(w, http.StatusBadRequest, "缺少文档ID")
		return
	}

	doc, err := h.engine.GetDocument(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "文档不存在: "+err.Error())
		return
	}

	writeSuccess(w, doc)
}

// DeleteDocument 删除文档
func (h *Handlers) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	// 从路径提取ID
	id := extractID(r.URL.Path)
	if id == "" {
		writeError(w, http.StatusBadRequest, "缺少文档ID")
		return
	}

	if err := h.engine.DeleteDocument(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "删除文档失败: "+err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"message": "文档已删除",
		"id":      id,
	})
}

// SearchRequest 搜索请求
type SearchRequest struct {
	Query          string                 `json:"query"`
	TopK           int                    `json:"top_k,omitempty"`
	Filters        map[string]interface{} `json:"filters,omitempty"`
	ScoreThreshold float64                `json:"score_threshold,omitempty"`
	UseRerank      bool                   `json:"use_rerank,omitempty"`
	RetrievalType  string                 `json:"retrieval_type,omitempty"`
}

// Search 搜索
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的JSON格式: "+err.Error())
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "查询内容不能为空")
		return
	}

	if req.TopK <= 0 {
		req.TopK = 10
	}

	// 构建搜索选项
	opts := models.SearchOptions{
		TopK:           req.TopK,
		Filters:        req.Filters,
		ScoreThreshold: req.ScoreThreshold,
	}

	// 执行搜索
	var results []*models.RetrievalResult
	var err error

	switch req.RetrievalType {
	case "vector":
		results, err = h.engine.VectorSearch(r.Context(), req.Query, req.TopK)
	case "keyword":
		var searchResult *models.SearchResult
		searchResult, err = h.engine.KeywordSearch(r.Context(), req.Query, opts)
		if searchResult != nil {
			results = searchResult.Results
		}
	case "graph":
		results, err = h.engine.GraphSearch(r.Context(), req.Query, req.TopK)
	default:
		var searchResult *models.SearchResult
		searchResult, err = h.engine.Search(r.Context(), req.Query, opts)
		if searchResult != nil {
			results = searchResult.Results
		}
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "搜索失败: "+err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"query":   req.Query,
		"results": results,
		"count":   len(results),
	})
}

// AskRequest RAG问答请求
type AskRequest struct {
	Question      string `json:"question"`
	TopK          int    `json:"top_k,omitempty"`
	UseRerank     bool   `json:"use_rerank,omitempty"`
	RetrievalType string `json:"retrieval_type,omitempty"`
}

// Ask RAG问答
func (h *Handlers) Ask(w http.ResponseWriter, r *http.Request) {
	var req AskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的JSON格式: "+err.Error())
		return
	}

	if req.Question == "" {
		writeError(w, http.StatusBadRequest, "问题不能为空")
		return
	}

	// 设置请求超时
	ctx, cancel := context.WithTimeout(r.Context(), defaultRequestTimeout)
	defer cancel()

	// 构建选项
	var opts []rag.AskOption
	if req.TopK > 0 {
		opts = append(opts, rag.WithTopK(req.TopK))
	}
	opts = append(opts, rag.WithRerank(req.UseRerank))
	if req.RetrievalType != "" {
		opts = append(opts, rag.WithRetrievalType(req.RetrievalType))
	}

	// 执行问答
	result, err := h.engine.Ask(ctx, req.Question, opts...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "问答失败: "+err.Error())
		return
	}

	writeSuccess(w, map[string]interface{}{
		"question":   result.Question,
		"answer":     result.Answer,
		"sources":    result.Sources,
		"query_time": result.QueryTime,
		"total_time": result.TotalTime,
	})
}

// GetStats 获取统计信息
func (h *Handlers) GetStats(w http.ResponseWriter, r *http.Request) {
	stats := h.engine.GetStats()
	writeSuccess(w, stats)
}

// AskStream RAG流式问答（SSE）
func (h *Handlers) AskStream(w http.ResponseWriter, r *http.Request) {
	var req AskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "无效的JSON格式: "+err.Error())
		return
	}

	if req.Question == "" {
		writeError(w, http.StatusBadRequest, "问题不能为空")
		return
	}

	// 设置SSE头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "不支持流式响应")
		return
	}

	// 构建选项
	var opts []rag.AskOption
	if req.TopK > 0 {
		opts = append(opts, rag.WithTopK(req.TopK))
	}
	opts = append(opts, rag.WithRerank(req.UseRerank))

	// 流式生成
	ctx := r.Context()
	_, err := h.engine.AskStream(ctx, req.Question, func(chunk string) {
		data, marshalErr := json.Marshal(map[string]string{"content": chunk})
		if marshalErr != nil {
			// 序列化失败时发送错误事件
			errData, _ := json.Marshal(map[string]string{"error": "marshal failed: " + marshalErr.Error()})
			fmt.Fprintf(w, "data: %s\n\n", errData)
		} else {
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		flusher.Flush()
	}, opts...)

	if err != nil {
		data, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}

	// 发送结束标记
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
