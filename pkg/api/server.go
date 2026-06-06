package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Source-of-Intelligence/soi-rag/pkg/rag"
)

// Server HTTP API服务器
type Server struct {
	engine *rag.Engine
	server *http.Server
	config *Config
}

// Config 服务器配置
type Config struct {
	Host         string        // 监听地址
	Port         int           // 监听端口
	ReadTimeout  time.Duration // 读超时
	WriteTimeout time.Duration // 写超时
	IdleTimeout  time.Duration // 空闲超时
	EnableCORS   bool          // 启用CORS
	EnableAuth   bool          // 启用认证
	AuthToken    string        // 认证Token
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		Host:         "0.0.0.0",
		Port:         8080,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
		EnableCORS:   true,
		EnableAuth:   false,
	}
}

// NewServer 创建API服务器
func NewServer(engine *rag.Engine, config *Config) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	return &Server{
		engine: engine,
		config: config,
	}
}

// Start 启动服务器
func (s *Server) Start() error {
	// 创建路由器
	mux := http.NewServeMux()

	// 注册路由
	s.registerRoutes(mux)

	// 应用中间件
	handler := s.applyMiddleware(mux)

	// 创建HTTP服务器
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	log.Printf("API服务器启动，监听地址: %s", addr)

	return s.server.ListenAndServe()
}

// StartGraceful 优雅启动服务器（支持优雅关闭）
func (s *Server) StartGraceful() error {
	// 创建路由器
	mux := http.NewServeMux()

	// 注册路由
	s.registerRoutes(mux)

	// 应用中间件
	handler := s.applyMiddleware(mux)

	// 创建HTTP服务器
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	// 启动服务器（在goroutine中）
	errChan := make(chan error, 1)
	go func() {
		log.Printf("API服务器启动，监听地址: %s", addr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return err
	case sig := <-quit:
		log.Printf("收到信号 %v，开始优雅关闭...", sig)
	}

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("服务器关闭失败: %w", err)
	}

	log.Println("服务器已优雅关闭")
	return nil
}

// Stop 停止服务器
func (s *Server) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

// registerRoutes 注册路由
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// 创建处理器
	h := NewHandlers(s.engine)

	// API v1 路由
	// 文档操作
	mux.HandleFunc("POST /api/v1/documents", h.AddDocument)
	mux.HandleFunc("GET /api/v1/documents", h.ListDocuments)
	mux.HandleFunc("GET /api/v1/documents/", h.GetDocument)       // :id 通过路径提取
	mux.HandleFunc("DELETE /api/v1/documents/", h.DeleteDocument) // :id 通过路径提取

	// 搜索和问答
	mux.HandleFunc("POST /api/v1/search", h.Search)
	mux.HandleFunc("POST /api/v1/ask", h.Ask)
	mux.HandleFunc("POST /api/v1/ask/stream", h.AskStream)

	// 系统信息
	mux.HandleFunc("GET /api/v1/stats", h.GetStats)
	mux.HandleFunc("GET /api/v1/health", h.HealthCheck)

	// 根路径
	mux.HandleFunc("GET /", h.Index)
}

// applyMiddleware 应用中间件
func (s *Server) applyMiddleware(handler http.Handler) http.Handler {
	// 恢复中间件（最外层）
	handler = RecoveryMiddleware(handler)

	// 日志中间件
	handler = LoggingMiddleware(handler)

	// CORS中间件
	if s.config.EnableCORS {
		handler = CORSMiddleware(handler)
	}

	// 认证中间件
	if s.config.EnableAuth {
		handler = AuthMiddleware(handler, s.config.AuthToken)
	}

	return handler
}

// Handler 返回服务器的 http.Handler（用于集成到其他路由框架）
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return s.applyMiddleware(mux)
}

// Addr 返回监听地址
func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
}

// extractID 从路径中提取ID
// 路径格式: /api/v1/documents/{id}
func extractID(path string) string {
	// 移除前缀
	prefix := "/api/v1/documents/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}

	// 提取ID部分
	id := strings.TrimPrefix(path, prefix)
	// 移除查询参数
	if idx := strings.Index(id, "?"); idx != -1 {
		id = id[:idx]
	}

	return id
}
