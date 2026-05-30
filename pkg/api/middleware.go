package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// responseWriter 包装http.ResponseWriter以捕获状态码
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	buf        *bytes.Buffer
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.buf != nil {
		rw.buf.Write(b)
	}
	return rw.ResponseWriter.Write(b)
}

// LoggingMiddleware 日志中间件
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 创建响应写入器包装
		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// 调用下一个处理器
		next.ServeHTTP(rw, r)

		// 记录日志
		duration := time.Since(start)
		log.Printf("[%s] %s %s %d %v",
			r.Method,
			r.URL.Path,
			r.RemoteAddr,
			rw.statusCode,
			duration,
		)
	})
}

// RecoveryMiddleware 恢复中间件（panic恢复）
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("PANIC: %v", err)

				// 返回500错误
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)

				response := ErrorResponse{
					Success:   false,
					Error:     "服务器内部错误",
					Code:      http.StatusInternalServerError,
					Timestamp: time.Now().Unix(),
				}
				json.NewEncoder(w).Encode(response)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware CORS跨域中间件
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 设置CORS头
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400") // 24小时

		// 处理预检请求
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// AuthMiddleware 认证中间件
func AuthMiddleware(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 健康检查不需要认证
		if r.URL.Path == "/api/v1/health" || r.URL.Path == "/" {
			next.ServeHTTP(w, r)
			return
		}

		// 获取Authorization头
		auth := r.Header.Get("Authorization")
		if auth == "" {
			writeAuthError(w, "缺少认证令牌")
			return
		}

		// 验证Bearer Token
		const prefix = "Bearer "
		if len(auth) < len(prefix) || auth[:len(prefix)] != prefix {
			writeAuthError(w, "无效的认证格式")
			return
		}

		providedToken := auth[len(prefix):]
		if providedToken != token {
			writeAuthError(w, "认证令牌无效")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeAuthError 写入认证错误
func writeAuthError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)

	response := ErrorResponse{
		Success:   false,
		Error:     message,
		Code:      http.StatusUnauthorized,
		Timestamp: time.Now().Unix(),
	}
	json.NewEncoder(w).Encode(response)
}

// RateLimitMiddleware 速率限制中间件（简单实现）
func RateLimitMiddleware(next http.Handler, requestsPerSecond int) http.Handler {
	// 简单的令牌桶实现
	// 注意：这是一个简单实现，生产环境应使用更健壮的方案如 golang.org/x/time/rate
	type client struct {
		lastSeen time.Time
		tokens   float64
	}

	clients := make(map[string]*client)
	tokensPerRequest := 1.0 / float64(requestsPerSecond)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr

		c, exists := clients[ip]
		if !exists {
			c = &client{
				lastSeen: time.Now(),
				tokens:   1.0,
			}
			clients[ip] = c
		}

		// 补充令牌
		elapsed := time.Since(c.lastSeen).Seconds()
		c.tokens += elapsed * float64(requestsPerSecond)
		if c.tokens > 1.0 {
			c.tokens = 1.0
		}
		c.lastSeen = time.Now()

		// 检查是否有足够的令牌
		if c.tokens < tokensPerRequest {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)

			response := ErrorResponse{
				Success:   false,
				Error:     "请求过于频繁，请稍后再试",
				Code:      http.StatusTooManyRequests,
				Timestamp: time.Now().Unix(),
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		c.tokens -= tokensPerRequest
		next.ServeHTTP(w, r)
	})
}

// RequestIDMiddleware 请求ID中间件
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查是否已有请求ID
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			// 生成简单的请求ID
			requestID = time.Now().Format("20060102150405") + "-" + randomString(8)
		}

		// 设置响应头
		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r)
	})
}

// randomString 生成随机字符串（简单实现）
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[(time.Now().UnixNano()+int64(i))%int64(len(letters))]
	}
	return string(b)
}

// CompressMiddleware 压缩中间件（可选）
// 注意：完整实现需要引入 gzip 相关逻辑
func CompressMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查客户端是否支持gzip
		if r.Header.Get("Accept-Encoding") == "" {
			next.ServeHTTP(w, r)
			return
		}

		// 简单实现：直接传递
		// 完整实现应使用 gzip.Writer 包装响应
		next.ServeHTTP(w, r)
	})
}
