package vector

import (
	"context"
	"sync"
	"testing"
)

// TestNewHNSWStore 测试创建 HNSW 存储
func TestNewHNSWStore(t *testing.T) {
	cfg := DefaultHNSWConfig(4)
	store := NewHNSWStore(cfg)

	if store == nil {
		t.Fatal("NewHNSWStore 返回 nil")
	}

	// 验证默认配置被正确应用
	if store.config.Dim != 4 {
		t.Errorf("期望 Dim=4, 实际=%d", store.config.Dim)
	}
	if store.config.M != DefaultM {
		t.Errorf("期望 M=%d, 实际=%d", DefaultM, store.config.M)
	}
	if store.config.EfConstruction != DefaultEfConstruction {
		t.Errorf("期望 EfConstruction=%d, 实际=%d", DefaultEfConstruction, store.config.EfConstruction)
	}
	if store.config.EfSearch != DefaultEfSearch {
		t.Errorf("期望 EfSearch=%d, 实际=%d", DefaultEfSearch, store.config.EfSearch)
	}

	// 验证初始状态
	if store.entryPoint != "" {
		t.Errorf("新建 store 的 entryPoint 应为空, 实际=%s", store.entryPoint)
	}
	if store.maxLevel != 0 {
		t.Errorf("新建 store 的 maxLevel 应为 0, 实际=%d", store.maxLevel)
	}
	if len(store.nodes) != 0 {
		t.Errorf("新建 store 的 nodes 应为空, 实际数量=%d", len(store.nodes))
	}

	// 验证零值参数被修正
	zeroCfg := HNSWConfig{Dim: 4}
	zeroStore := NewHNSWStore(zeroCfg)
	if zeroStore.config.M != DefaultM {
		t.Errorf("零值 M 应被修正为 DefaultM=%d, 实际=%d", DefaultM, zeroStore.config.M)
	}
	if zeroStore.config.EfConstruction != DefaultEfConstruction {
		t.Errorf("零值 EfConstruction 应被修正为 %d, 实际=%d", DefaultEfConstruction, zeroStore.config.EfConstruction)
	}
	if zeroStore.config.EfSearch != DefaultEfSearch {
		t.Errorf("零值 EfSearch 应被修正为 %d, 实际=%d", DefaultEfSearch, zeroStore.config.EfSearch)
	}
	if zeroStore.config.Ml <= 0 {
		t.Errorf("零值 Ml 应被修正为正值, 实际=%f", zeroStore.config.Ml)
	}
}

// TestHNSWInsertAndSearch 测试插入向量后搜索，验证结果正确
func TestHNSWInsertAndSearch(t *testing.T) {
	cfg := HNSWConfig{
		M:              8,
		EfConstruction: 50,
		EfSearch:       50,
		Dim:            4,
		Ml:             1.0 / 2.0, // 固定 Ml 以便控制层级
	}
	store := NewHNSWStore(cfg)
	ctx := context.Background()

	// 插入一组 4 维向量
	vectors := []struct {
		id     string
		vector []float32
		label  string
	}{
		{"v1", []float32{1.0, 0.0, 0.0, 0.0}, "x轴正方向"},
		{"v2", []float32{0.0, 1.0, 0.0, 0.0}, "y轴正方向"},
		{"v3", []float32{0.0, 0.0, 1.0, 0.0}, "z轴正方向"},
		{"v4", []float32{0.0, 0.0, 0.0, 1.0}, "w轴正方向"},
		{"v5", []float32{0.9, 0.1, 0.0, 0.0}, "接近x轴"},
		{"v6", []float32{0.0, 0.9, 0.1, 0.0}, "接近y轴"},
	}

	var records []*VectorRecord
	for _, v := range vectors {
		records = append(records, &VectorRecord{
			ID:     v.id,
			Vector: v.vector,
			Metadata: map[string]interface{}{
				"label": v.label,
			},
		})
	}

	err := store.Insert(ctx, records)
	if err != nil {
		t.Fatalf("插入向量失败: %v", err)
	}

	// 验证节点数量
	stats := store.Stats()
	if stats["node_count"] != len(vectors) {
		t.Errorf("期望节点数=%d, 实际=%v", len(vectors), stats["node_count"])
	}

	// 搜索与 v1 (1,0,0,0) 最相似的向量，应返回 v1 本身和 v5 (0.9,0.1,0,0)
	query := []float32{1.0, 0.0, 0.0, 0.0}
	results, err := store.Search(ctx, query, 3, nil)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("搜索结果为空")
	}

	// 第一个结果应该是 v1（自身）
	if results[0].ID != "v1" {
		t.Errorf("期望最近邻为 v1, 实际=%s", results[0].ID)
	}

	// 搜索结果应包含 v1 和 v5
	found := make(map[string]bool)
	for _, r := range results {
		found[r.ID] = true
	}
	if !found["v1"] {
		t.Error("搜索结果应包含 v1")
	}
	if !found["v5"] {
		t.Error("搜索结果应包含 v5（接近x轴的向量）")
	}

	// 验证元数据正确传递
	if results[0].Metadata["label"] != "x轴正方向" {
		t.Errorf("期望元数据 label='x轴正方向', 实际=%v", results[0].Metadata["label"])
	}

	// 测试维度不匹配
	err = store.Insert(ctx, []*VectorRecord{
		{ID: "bad", Vector: []float32{1.0, 2.0}},
	})
	if err == nil {
		t.Error("插入维度不匹配的向量应返回错误")
	}

	// 测试查询维度不匹配
	_, err = store.Search(ctx, []float32{1.0, 2.0}, 3, nil)
	if err == nil {
		t.Error("查询维度不匹配应返回错误")
	}
}

// TestHNSWDelete 测试删除后搜索不再返回
func TestHNSWDelete(t *testing.T) {
	cfg := HNSWConfig{
		M:              8,
		EfConstruction: 50,
		EfSearch:       50,
		Dim:            4,
		Ml:             1.0 / 2.0,
	}
	store := NewHNSWStore(cfg)
	ctx := context.Background()

	// 插入向量
	vectors := []*VectorRecord{
		{ID: "v1", Vector: []float32{1.0, 0.0, 0.0, 0.0}, Metadata: map[string]interface{}{"label": "a"}},
		{ID: "v2", Vector: []float32{0.0, 1.0, 0.0, 0.0}, Metadata: map[string]interface{}{"label": "b"}},
		{ID: "v3", Vector: []float32{0.0, 0.0, 1.0, 0.0}, Metadata: map[string]interface{}{"label": "c"}},
	}

	err := store.Insert(ctx, vectors)
	if err != nil {
		t.Fatalf("插入向量失败: %v", err)
	}

	// 删除 v2
	err = store.Delete(ctx, []string{"v2"})
	if err != nil {
		t.Fatalf("删除向量失败: %v", err)
	}

	// 验证节点数量减少
	stats := store.Stats()
	if stats["node_count"] != 2 {
		t.Errorf("删除后期望节点数=2, 实际=%v", stats["node_count"])
	}

	// 搜索所有向量，确认 v2 不再出现
	query := []float32{0.0, 1.0, 0.0, 0.0}
	results, err := store.Search(ctx, query, 10, nil)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}

	for _, r := range results {
		if r.ID == "v2" {
			t.Error("删除后的 v2 不应出现在搜索结果中")
		}
	}

	// 删除不存在的节点不应报错
	err = store.Delete(ctx, []string{"nonexistent"})
	if err != nil {
		t.Errorf("删除不存在的节点不应返回错误, 实际=%v", err)
	}

	// 删除入口点后应能正常工作
	err = store.Delete(ctx, []string{"v1"})
	if err != nil {
		t.Fatalf("删除入口点失败: %v", err)
	}

	stats = store.Stats()
	if stats["node_count"] != 1 {
		t.Errorf("删除入口点后期望节点数=1, 实际=%v", stats["node_count"])
	}

	// 剩余节点仍可搜索
	results, err = store.Search(ctx, []float32{0.0, 0.0, 1.0, 0.0}, 1, nil)
	if err != nil {
		t.Fatalf("删除入口点后搜索失败: %v", err)
	}
	if len(results) == 0 {
		t.Error("删除入口点后仍应能搜索到剩余节点")
	}
	if results[0].ID != "v3" {
		t.Errorf("期望搜索到 v3, 实际=%s", results[0].ID)
	}
}

// TestHNSWConcurrentSearch 测试并发搜索不 panic
func TestHNSWConcurrentSearch(t *testing.T) {
	cfg := HNSWConfig{
		M:              8,
		EfConstruction: 50,
		EfSearch:       50,
		Dim:            4,
		Ml:             1.0 / 2.0,
	}
	store := NewHNSWStore(cfg)
	ctx := context.Background()

	// 插入足够多的向量
	for i := 0; i < 100; i++ {
		v := []float32{
			float32(i%10) / 10.0,
			float32((i/10)%10) / 10.0,
			float32((i/100)%10) / 10.0,
			float32(i%3) / 3.0,
		}
		err := store.Insert(ctx, []*VectorRecord{
			{
				ID:     string(rune('a' + i)),
				Vector: v,
				Metadata: map[string]interface{}{
					"index": i,
				},
			},
		})
		if err != nil {
			t.Fatalf("插入向量 %d 失败: %v", i, err)
		}
	}

	// 启动多个并发搜索
	const numGoroutines = 20
	var wg sync.WaitGroup
	panicCh := make(chan interface{}, numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCh <- r
				}
			}()

			for i := 0; i < 10; i++ {
				query := []float32{
					float32(goroutineID%10) / 10.0,
					float32(i%10) / 10.0,
					0.5,
					0.5,
				}
				results, err := store.Search(ctx, query, 5, nil)
				if err != nil {
					t.Errorf("并发搜索 goroutine=%d iter=%d 出错: %v", goroutineID, i, err)
					return
				}
				if len(results) > 5 {
					t.Errorf("并发搜索结果数不应超过 topK=5, 实际=%d", len(results))
				}
			}
		}(g)
	}

	wg.Wait()
	close(panicCh)

	// 检查是否有 panic
	for p := range panicCh {
		t.Fatalf("并发搜索发生 panic: %v", p)
	}
}

// TestHNSWEmptySearch 测试空索引搜索
func TestHNSWEmptySearch(t *testing.T) {
	cfg := DefaultHNSWConfig(4)
	store := NewHNSWStore(cfg)
	ctx := context.Background()

	// 空索引搜索应返回 nil, nil
	results, err := store.Search(ctx, []float32{1.0, 0.0, 0.0, 0.0}, 5, nil)
	if err != nil {
		t.Fatalf("空索引搜索不应返回错误: %v", err)
	}
	if results != nil {
		t.Errorf("空索引搜索应返回 nil, 实际=%v", results)
	}
}

// TestHNSWSearchWithFilter 测试带过滤器的搜索
func TestHNSWSearchWithFilter(t *testing.T) {
	cfg := HNSWConfig{
		M:              8,
		EfConstruction: 50,
		EfSearch:       50,
		Dim:            4,
		Ml:             1.0 / 2.0,
	}
	store := NewHNSWStore(cfg)
	ctx := context.Background()

	// 插入带不同元数据的向量
	vectors := []*VectorRecord{
		{ID: "v1", Vector: []float32{1.0, 0.0, 0.0, 0.0}, Metadata: map[string]interface{}{"category": "A"}},
		{ID: "v2", Vector: []float32{0.9, 0.1, 0.0, 0.0}, Metadata: map[string]interface{}{"category": "B"}},
		{ID: "v3", Vector: []float32{0.8, 0.2, 0.0, 0.0}, Metadata: map[string]interface{}{"category": "A"}},
	}

	err := store.Insert(ctx, vectors)
	if err != nil {
		t.Fatalf("插入向量失败: %v", err)
	}

	// 带过滤器搜索：只返回 category=A
	results, err := store.Search(ctx, []float32{1.0, 0.0, 0.0, 0.0}, 10, map[string]interface{}{"category": "A"})
	if err != nil {
		t.Fatalf("带过滤器搜索失败: %v", err)
	}

	for _, r := range results {
		if r.Metadata["category"] != "A" {
			t.Errorf("过滤器搜索结果中不应包含 category=B, 实际=%v", r.Metadata["category"])
		}
	}
}

// TestHNSWStats 测试统计信息
func TestHNSWStats(t *testing.T) {
	cfg := DefaultHNSWConfig(4)
	store := NewHNSWStore(cfg)
	ctx := context.Background()

	// 空存储的统计
	stats := store.Stats()
	if stats["node_count"] != 0 {
		t.Errorf("空存储 node_count 应为 0, 实际=%v", stats["node_count"])
	}

	// 插入后统计
	_ = store.Insert(ctx, []*VectorRecord{
		{ID: "v1", Vector: []float32{1.0, 0.0, 0.0, 0.0}},
		{ID: "v2", Vector: []float32{0.0, 1.0, 0.0, 0.0}},
	})

	stats = store.Stats()
	if stats["node_count"] != 2 {
		t.Errorf("插入后 node_count 应为 2, 实际=%v", stats["node_count"])
	}
	if stats["entry_point"] == "" {
		t.Error("插入后 entry_point 不应为空")
	}
}
