package pageindex

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ragtool/rag/pkg/models"
)

// BatchResult 批量索引结果
type BatchResult struct {
	Total      int           // 总文档数
	Success    int           // 成功数
	Failed     int           // 失败数
	Errors     []BatchError  // 错误列表
	Duration   time.Duration // 总耗时
	IndexedIDs []string      // 成功索引的文档ID
}

// BatchError 批量索引错误
type BatchError struct {
	Index  int    // 文档索引位置
	DocID  string // 文档ID（如果有）
	Source string // 文档来源
	Error  error  // 错误信息
}

// ProgressInfo 进度信息
type ProgressInfo struct {
	Total     int           // 总数
	Completed int           // 已完成数
	Success   int           // 成功数
	Failed    int           // 失败数
	Current   string        // 当前处理的文档来源
	Duration  time.Duration // 已耗时
}

// ProgressCallback 进度回调函数类型
type ProgressCallback func(info ProgressInfo)

// BatchConfig 批量索引配置
type BatchConfig struct {
	Workers     int  // 并发worker数
	BatchSize   int  // 每批次处理数量
	StopOnError bool // 遇到错误是否停止
}

// DefaultBatchConfig 默认批量配置
func DefaultBatchConfig() *BatchConfig {
	return &BatchConfig{
		Workers:     4,
		BatchSize:   100,
		StopOnError: false,
	}
}

// BatchIndexer 批量索引器
type BatchIndexer struct {
	engine *pageIndex
	config *BatchConfig
}

// NewBatchIndexer 创建批量索引器
func NewBatchIndexer(engine PageIndex, workers, batchSize int) *BatchIndexer {
	if workers <= 0 {
		workers = 4
	}
	if batchSize <= 0 {
		batchSize = 100
	}

	// 获取底层 pageIndex 实现
	var pi *pageIndex
	switch e := engine.(type) {
	case *pageIndex:
		pi = e
	default:
		// 如果不是 pageIndex 类型，创建一个适配器
		// 这里假设传入的就是 pageIndex
		panic("engine must be *pageIndex type")
	}

	return &BatchIndexer{
		engine: pi,
		config: &BatchConfig{
			Workers:     workers,
			BatchSize:   batchSize,
			StopOnError: false,
		},
	}
}

// NewBatchIndexerWithConfig 使用配置创建批量索引器
func NewBatchIndexerWithConfig(engine PageIndex, config *BatchConfig) *BatchIndexer {
	if config == nil {
		config = DefaultBatchConfig()
	}
	if config.Workers <= 0 {
		config.Workers = 4
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}

	var pi *pageIndex
	switch e := engine.(type) {
	case *pageIndex:
		pi = e
	default:
		panic("engine must be *pageIndex type")
	}

	return &BatchIndexer{
		engine: pi,
		config: config,
	}
}

// DocumentTask 文档索引任务
type DocumentTask struct {
	Index int              // 文档在原始切片中的索引
	Doc   *models.Document // 文档对象
}

// IndexDocuments 并发批量索引文档
func (b *BatchIndexer) IndexDocuments(ctx context.Context, docs []*models.Document, progressCallback ProgressCallback) (*BatchResult, error) {
	if len(docs) == 0 {
		return &BatchResult{}, nil
	}

	startTime := time.Now()
	total := len(docs)

	// 结果
	result := &BatchResult{
		Total:      total,
		IndexedIDs: make([]string, 0, total),
		Errors:     make([]BatchError, 0),
	}

	// 使用原子计数器
	var successCount int32
	var failedCount int32
	var completedCount int32

	// 错误收集（使用互斥锁保护）
	var errorsMu sync.Mutex
	errors := make([]BatchError, 0)

	// 成功ID收集
	var idsMu sync.Mutex
	indexedIDs := make([]string, 0, total)

	// 创建任务通道和结果通道
	taskChan := make(chan DocumentTask, total)
	resultChan := make(chan indexResult, total)

	// 填充任务通道
	for i, doc := range docs {
		taskChan <- DocumentTask{Index: i, Doc: doc}
	}
	close(taskChan)

	// 启动 worker pool
	var wg sync.WaitGroup
	workers := b.config.Workers
	if workers > total {
		workers = total
	}

	// 检查上下文取消
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 启动 workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go b.worker(ctx, &wg, taskChan, resultChan)
	}

	// 等待所有 workers 完成的 goroutine
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 处理结果
	for res := range resultChan {
		completed := int(atomic.AddInt32(&completedCount, 1))

		if res.Error != nil {
			atomic.AddInt32(&failedCount, 1)
			errorsMu.Lock()
			errors = append(errors, BatchError{
				Index:  res.Index,
				DocID:  res.DocID,
				Source: res.Source,
				Error:  res.Error,
			})
			errorsMu.Unlock()

			// 如果配置了遇错停止
			if b.config.StopOnError {
				cancel()
				break
			}
		} else {
			atomic.AddInt32(&successCount, 1)
			idsMu.Lock()
			indexedIDs = append(indexedIDs, res.DocID)
			idsMu.Unlock()
		}

		// 调用进度回调
		if progressCallback != nil {
			progressCallback(ProgressInfo{
				Total:     total,
				Completed: completed,
				Success:   int(atomic.LoadInt32(&successCount)),
				Failed:    int(atomic.LoadInt32(&failedCount)),
				Current:   res.Source,
				Duration:  time.Since(startTime),
			})
		}

		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			break
		default:
		}
	}

	// 汇总结果
	result.Success = int(successCount)
	result.Failed = int(failedCount)
	result.Errors = errors
	result.IndexedIDs = indexedIDs
	result.Duration = time.Since(startTime)

	return result, nil
}

// indexResult 索引结果
type indexResult struct {
	Index  int    // 文档索引
	DocID  string // 文档ID
	Source string // 文档来源
	Error  error  // 错误（如果有）
}

// worker 工作协程
func (b *BatchIndexer) worker(ctx context.Context, wg *sync.WaitGroup, taskChan <-chan DocumentTask, resultChan chan<- indexResult) {
	defer wg.Done()

	for task := range taskChan {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return
		default:
		}

		doc := task.Doc
		source := doc.Source
		if source == "" {
			source = fmt.Sprintf("doc-%d", task.Index)
		}

		// 执行索引
		err := b.indexDocument(ctx, doc)

		// 发送结果
		select {
		case <-ctx.Done():
			return
		case resultChan <- indexResult{
			Index:  task.Index,
			DocID:  doc.ID,
			Source: source,
			Error:  err,
		}:
		}
	}
}

// indexDocument 索引单个文档
func (b *BatchIndexer) indexDocument(ctx context.Context, doc *models.Document) error {
	// 直接调用 engine 的 AddDocument 方法
	return b.engine.AddDocument(ctx, doc, nil)
}

// IndexDocumentsBatch 按批次索引文档（分批处理，每批等待完成后再处理下一批）
func (b *BatchIndexer) IndexDocumentsBatch(ctx context.Context, docs []*models.Document, progressCallback ProgressCallback) (*BatchResult, error) {
	if len(docs) == 0 {
		return &BatchResult{}, nil
	}

	startTime := time.Now()
	total := len(docs)
	batchSize := b.config.BatchSize

	// 总结果
	result := &BatchResult{
		Total:      total,
		IndexedIDs: make([]string, 0, total),
		Errors:     make([]BatchError, 0),
	}

	var allErrors []BatchError
	var allIndexedIDs []string
	var totalSuccess, totalFailed int

	// 按批次处理
	for i := 0; i < total; i += batchSize {
		// 检查上下文
		select {
		case <-ctx.Done():
			result.Duration = time.Since(startTime)
			return result, ctx.Err()
		default:
		}

		end := i + batchSize
		if end > total {
			end = total
		}

		batch := docs[i:end]

		// 处理当前批次
		batchResult, err := b.IndexDocuments(ctx, batch, func(info ProgressInfo) {
			if progressCallback != nil {
				// 调整进度信息以反映整体进度
				progressCallback(ProgressInfo{
					Total:     total,
					Completed: i + info.Completed,
					Success:   totalSuccess + info.Success,
					Failed:    totalFailed + info.Failed,
					Current:   info.Current,
					Duration:  time.Since(startTime),
				})
			}
		})

		if err != nil {
			result.Duration = time.Since(startTime)
			return result, err
		}

		// 累加结果
		totalSuccess += batchResult.Success
		totalFailed += batchResult.Failed
		allIndexedIDs = append(allIndexedIDs, batchResult.IndexedIDs...)
		allErrors = append(allErrors, batchResult.Errors...)

		// 如果配置了遇错停止且有错误
		if b.config.StopOnError && batchResult.Failed > 0 {
			break
		}
	}

	// 汇总最终结果
	result.Success = totalSuccess
	result.Failed = totalFailed
	result.IndexedIDs = allIndexedIDs
	result.Errors = allErrors
	result.Duration = time.Since(startTime)

	return result, nil
}

// SetStopOnError 设置遇错停止标志
func (b *BatchIndexer) SetStopOnError(stop bool) {
	b.config.StopOnError = stop
}

// GetConfig 获取当前配置
func (b *BatchIndexer) GetConfig() *BatchConfig {
	return &BatchConfig{
		Workers:     b.config.Workers,
		BatchSize:   b.config.BatchSize,
		StopOnError: b.config.StopOnError,
	}
}

// Stats 统计信息
type Stats struct {
	Workers       int
	BatchSize     int
	ActiveJobs    int
	CompletedJobs int
}

// GetStats 获取统计信息（当前实现返回配置信息）
func (b *BatchIndexer) GetStats() Stats {
	return Stats{
		Workers:   b.config.Workers,
		BatchSize: b.config.BatchSize,
	}
}
