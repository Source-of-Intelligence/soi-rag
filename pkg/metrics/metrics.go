package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics Prometheus指标集合
type Metrics struct {
	// 查询相关
	QueryCount    *prometheus.CounterVec
	QueryDuration *prometheus.HistogramVec
	QueryErrors   *prometheus.CounterVec

	// 索引相关
	IndexCount    *prometheus.CounterVec
	IndexDuration *prometheus.HistogramVec
	DocumentCount prometheus.Gauge

	// 缓存相关
	CacheHits   prometheus.Counter
	CacheMisses prometheus.Counter
	CacheSize   prometheus.Gauge

	// HTTP相关
	RequestCount    *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec

	registry *prometheus.Registry
}

// NewMetrics 创建指标集合
func NewMetrics(namespace string) *Metrics {
	if namespace == "" {
		namespace = "rag"
	}

	m := &Metrics{
		registry: prometheus.NewRegistry(),
	}

	// 查询指标
	m.QueryCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "query",
			Name:      "total",
			Help:      "Total number of queries",
		},
		[]string{"type", "status"},
	)

	m.QueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "query",
			Name:      "duration_seconds",
			Help:      "Query duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"type"},
	)

	m.QueryErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "query",
			Name:      "errors_total",
			Help:      "Total number of query errors",
		},
		[]string{"type"},
	)

	// 索引指标
	m.IndexCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "index",
			Name:      "total",
			Help:      "Total number of documents indexed",
		},
		[]string{"status"},
	)

	m.IndexDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "index",
			Name:      "duration_seconds",
			Help:      "Index duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{},
	)

	m.DocumentCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "index",
			Name:      "documents",
			Help:      "Current number of indexed documents",
		},
	)

	// 缓存指标
	m.CacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "hits_total",
			Help:      "Total number of cache hits",
		},
	)

	m.CacheMisses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "misses_total",
			Help:      "Total number of cache misses",
		},
	)

	m.CacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "size",
			Help:      "Current cache size",
		},
	)

	// HTTP指标
	m.RequestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	m.RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// 注册所有指标
	m.registry.MustRegister(
		m.QueryCount, m.QueryDuration, m.QueryErrors,
		m.IndexCount, m.IndexDuration, m.DocumentCount,
		m.CacheHits, m.CacheMisses, m.CacheSize,
		m.RequestCount, m.RequestDuration,
	)

	return m
}

// Registry 返回Prometheus注册表
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// Handler 返回metrics HTTP处理器
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Middleware HTTP中间件
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 包装ResponseWriter以捕获状态码
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()
		path := r.URL.Path
		method := r.Method
		status := strconv.Itoa(wrapped.status)

		m.RequestCount.WithLabelValues(method, path, status).Inc()
		m.RequestDuration.WithLabelValues(method, path).Observe(duration)
	})
}

// RecordQuery 记录查询
func (m *Metrics) RecordQuery(queryType string, duration time.Duration, err error) {
	status := "success"
	if err != nil {
		status = "error"
		m.QueryErrors.WithLabelValues(queryType).Inc()
	}

	m.QueryCount.WithLabelValues(queryType, status).Inc()
	m.QueryDuration.WithLabelValues(queryType).Observe(duration.Seconds())
}

// RecordIndex 记录索引
func (m *Metrics) RecordIndex(count int, duration time.Duration, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}

	m.IndexCount.WithLabelValues(status).Add(float64(count))
	m.IndexDuration.WithLabelValues().Observe(duration.Seconds())
}

// RecordCacheHit 记录缓存命中
func (m *Metrics) RecordCacheHit() {
	m.CacheHits.Inc()
}

// RecordCacheMiss 记录缓存未命中
func (m *Metrics) RecordCacheMiss() {
	m.CacheMisses.Inc()
}

// SetCacheSize 设置缓存大小
func (m *Metrics) SetCacheSize(size int) {
	m.CacheSize.Set(float64(size))
}

// SetDocumentCount 设置文档数量
func (m *Metrics) SetDocumentCount(count int) {
	m.DocumentCount.Set(float64(count))
}

// responseWriter 包装http.ResponseWriter以捕获状态码
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
