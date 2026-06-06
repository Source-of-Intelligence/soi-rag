package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/ragtool/rag/pkg/rag"
)

// FileEvent 文件事件类型
type FileEvent string

const (
	EventCreate FileEvent = "create"
	EventModify FileEvent = "modify"
	EventDelete FileEvent = "delete"
)

// FileInfo 文件信息
type FileInfo struct {
	Path      string    // 文件路径
	Name      string    // 文件名
	Ext       string    // 文件扩展名
	Event     FileEvent // 事件类型
	IsDir     bool      // 是否是目录
	Timestamp int64     // 事件时间戳（纳秒）
}

// Callback 文件变更回调函数类型
type Callback func(ctx context.Context, info FileInfo) error

// DocumentWatcher 文档监控器
type DocumentWatcher struct {
	engine  *rag.Engine        // RAG引擎
	watcher *fsnotify.Watcher  // fsnotify watcher
	dirs    []string           // 监控的目录列表
	exts    map[string]bool    // 允许的文件扩展名过滤
	running bool               // 是否正在运行
	mu      sync.RWMutex       // 读写锁
	ctx     context.Context    // 上下文
	cancel  context.CancelFunc // 取消函数
	wg      sync.WaitGroup     // 等待组

	// 回调函数
	onCreate Callback
	onModify Callback
	onDelete Callback
}

// WatcherOption 监控器选项
type WatcherOption func(*DocumentWatcher)

// WithExtensions 设置允许的文件扩展名
func WithExtensions(exts ...string) WatcherOption {
	return func(w *DocumentWatcher) {
		for _, ext := range exts {
			// 确保扩展名以 . 开头
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			w.exts[strings.ToLower(ext)] = true
		}
	}
}

// WithOnCreate 设置创建事件回调
func WithOnCreate(cb Callback) WatcherOption {
	return func(w *DocumentWatcher) {
		w.onCreate = cb
	}
}

// WithOnModify 设置修改事件回调
func WithOnModify(cb Callback) WatcherOption {
	return func(w *DocumentWatcher) {
		w.onModify = cb
	}
}

// WithOnDelete 设置删除事件回调
func WithOnDelete(cb Callback) WatcherOption {
	return func(w *DocumentWatcher) {
		w.onDelete = cb
	}
}

// NewDocumentWatcher 创建文档监控器
func NewDocumentWatcher(engine *rag.Engine, dirs []string, opts ...WatcherOption) (*DocumentWatcher, error) {
	if engine == nil {
		return nil, fmt.Errorf("engine不能为nil")
	}

	if len(dirs) == 0 {
		return nil, fmt.Errorf("至少需要指定一个监控目录")
	}

	// 验证目录是否存在
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("目录 %s 不存在或无法访问: %w", dir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s 不是目录", dir)
		}
	}

	// 创建 fsnotify watcher
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("创建fsnotify watcher失败: %w", err)
	}

	w := &DocumentWatcher{
		engine:  engine,
		watcher: fsWatcher,
		dirs:    dirs,
		exts:    make(map[string]bool),
	}

	// 应用选项
	for _, opt := range opts {
		opt(w)
	}

	// 如果没有设置扩展名过滤，默认支持常见文档类型
	if len(w.exts) == 0 {
		w.exts = map[string]bool{
			".txt":  true,
			".md":   true,
			".pdf":  true,
			".docx": true,
			".doc":  true,
			".html": true,
			".htm":  true,
			".json": true,
			".csv":  true,
		}
	}

	return w, nil
}

// Start 开始监控
func (w *DocumentWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return fmt.Errorf("监控器已在运行")
	}

	// 添加监控目录
	for _, dir := range w.dirs {
		// 递归添加子目录
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if err := w.watcher.Add(path); err != nil {
					return fmt.Errorf("添加监控目录 %s 失败: %w", path, err)
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("遍历目录 %s 失败: %w", dir, err)
		}
	}

	// 创建可取消的上下文
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.running = true

	// 启动事件处理协程
	w.wg.Add(1)
	go w.eventLoop()

	return nil
}

// Stop 停止监控
func (w *DocumentWatcher) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return nil
	}

	// 取消上下文
	if w.cancel != nil {
		w.cancel()
	}

	// 关闭 watcher
	if err := w.watcher.Close(); err != nil {
		return fmt.Errorf("关闭watcher失败: %w", err)
	}

	// 等待事件循环结束
	w.wg.Wait()

	w.running = false
	return nil
}

// IsRunning 返回监控器是否正在运行
func (w *DocumentWatcher) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

// GetDirs 获取监控的目录列表
func (w *DocumentWatcher) GetDirs() []string {
	return w.dirs
}

// GetExtensions 获取允许的文件扩展名列表
func (w *DocumentWatcher) GetExtensions() []string {
	exts := make([]string, 0, len(w.exts))
	for ext := range w.exts {
		exts = append(exts, ext)
	}
	return exts
}

// OnCreate 设置创建事件回调
func (w *DocumentWatcher) OnCreate(cb Callback) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onCreate = cb
}

// OnModify 设置修改事件回调
func (w *DocumentWatcher) OnModify(cb Callback) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onModify = cb
}

// OnDelete 设置删除事件回调
func (w *DocumentWatcher) OnDelete(cb Callback) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onDelete = cb
}

// eventLoop 事件循环
func (w *DocumentWatcher) eventLoop() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			// 记录错误但不退出
			fmt.Printf("watcher error: %v\n", err)
		}
	}
}

// handleEvent 处理文件事件
func (w *DocumentWatcher) handleEvent(event fsnotify.Event) {
	// 获取文件信息
	info, err := os.Stat(event.Name)
	isDir := err == nil && info.IsDir()

	// 获取文件扩展名
	ext := strings.ToLower(filepath.Ext(event.Name))

	// 检查扩展名是否允许（目录事件不过滤）
	if !isDir && !w.isAllowedExt(ext) {
		return
	}

	// 构建文件信息
	fileInfo := FileInfo{
		Path:      event.Name,
		Name:      filepath.Base(event.Name),
		Ext:       ext,
		IsDir:     isDir,
		Timestamp: 0, // 将在具体处理中设置
	}

	// 根据事件类型处理
	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		fileInfo.Event = EventCreate
		w.handleCreate(event, fileInfo, isDir)

	case event.Op&fsnotify.Write == fsnotify.Write:
		fileInfo.Event = EventModify
		w.handleModify(event, fileInfo)

	case event.Op&fsnotify.Remove == fsnotify.Remove:
		fileInfo.Event = EventDelete
		w.handleDelete(event, fileInfo)

	case event.Op&fsnotify.Rename == fsnotify.Rename:
		// 重命名视为删除+创建
		fileInfo.Event = EventDelete
		w.handleDelete(event, fileInfo)
	}
}

// handleCreate 处理创建事件
func (w *DocumentWatcher) handleCreate(event fsnotify.Event, fileInfo FileInfo, isDir bool) {
	fileInfo.Timestamp = nanoTime()

	// 如果是目录，添加到监控
	if isDir {
		if err := w.watcher.Add(event.Name); err != nil {
			fmt.Printf("添加监控目录 %s 失败: %v\n", event.Name, err)
		}
	}

	// 调用回调
	if w.onCreate != nil {
		if err := w.onCreate(w.ctx, fileInfo); err != nil {
			fmt.Printf("处理创建事件失败 %s: %v\n", event.Name, err)
		}
	}
}

// handleModify 处理修改事件
func (w *DocumentWatcher) handleModify(event fsnotify.Event, fileInfo FileInfo) {
	fileInfo.Timestamp = nanoTime()

	// 调用回调
	if w.onModify != nil {
		if err := w.onModify(w.ctx, fileInfo); err != nil {
			fmt.Printf("处理修改事件失败 %s: %v\n", event.Name, err)
		}
	}
}

// handleDelete 处理删除事件
func (w *DocumentWatcher) handleDelete(event fsnotify.Event, fileInfo FileInfo) {
	fileInfo.Timestamp = nanoTime()

	// 调用回调
	if w.onDelete != nil {
		if err := w.onDelete(w.ctx, fileInfo); err != nil {
			fmt.Printf("处理删除事件失败 %s: %v\n", event.Name, err)
		}
	}
}

// isAllowedExt 检查扩展名是否允许
func (w *DocumentWatcher) isAllowedExt(ext string) bool {
	// 如果没有设置过滤，允许所有
	if len(w.exts) == 0 {
		return true
	}
	return w.exts[strings.ToLower(ext)]
}

// nanoTime 获取当前纳秒时间戳
func nanoTime() int64 {
	return time.Now().UnixNano()
}

// AddDirectory 添加监控目录
func (w *DocumentWatcher) AddDirectory(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 验证目录
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("目录 %s 不存在或无法访问: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s 不是目录", dir)
	}

	// 检查是否已在监控列表
	for _, d := range w.dirs {
		if d == dir {
			return nil // 已存在
		}
	}

	// 添加到列表
	w.dirs = append(w.dirs, dir)

	// 如果正在运行，添加到 watcher
	if w.running {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if err := w.watcher.Add(path); err != nil {
					return fmt.Errorf("添加监控目录 %s 失败: %w", path, err)
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("遍历目录 %s 失败: %w", dir, err)
		}
	}

	return nil
}

// RemoveDirectory 移除监控目录
func (w *DocumentWatcher) RemoveDirectory(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 从列表中移除
	found := false
	newDirs := make([]string, 0, len(w.dirs))
	for _, d := range w.dirs {
		if d == dir {
			found = true
			continue
		}
		newDirs = append(newDirs, d)
	}

	if !found {
		return fmt.Errorf("目录 %s 不在监控列表中", dir)
	}

	w.dirs = newDirs

	// 如果正在运行，从 watcher 中移除
	if w.running {
		if err := w.watcher.Remove(dir); err != nil {
			return fmt.Errorf("移除监控目录 %s 失败: %w", dir, err)
		}
	}

	return nil
}

// AddExtension 添加允许的扩展名
func (w *DocumentWatcher) AddExtension(ext string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	w.exts[strings.ToLower(ext)] = true
}

// RemoveExtension 移除扩展名
func (w *DocumentWatcher) RemoveExtension(ext string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	delete(w.exts, strings.ToLower(ext))
}
