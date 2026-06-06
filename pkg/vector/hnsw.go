package vector

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
)

// HNSW参数默认值
const (
	DefaultM              = 16
	DefaultEfConstruction = 200
	DefaultEfSearch       = 50
)

// HNSWConfig HNSW配置参数
type HNSWConfig struct {
	M              int     // 每个节点在每层的最大连接数（layer 0为2*M）
	EfConstruction int     // 构建时的候选列表大小
	EfSearch       int     // 搜索时的候选列表大小
	Ml             float64 // 层级乘数因子，通常为 1/ln(M)
	Dim            int     // 向量维度
}

// DefaultHNSWConfig 返回默认配置
func DefaultHNSWConfig(dim int) HNSWConfig {
	m := DefaultM
	return HNSWConfig{
		M:              m,
		EfConstruction: DefaultEfConstruction,
		EfSearch:       DefaultEfSearch,
		Ml:             1.0 / math.Log(float64(m)),
		Dim:            dim,
	}
}

// node HNSW节点
type node struct {
	id       string                 // 节点ID
	vector   []float32              // 向量
	metadata map[string]interface{} // 元数据
	level    int                    // 节点所在的最大层级
	friends  []map[string]struct{}  // friends[l] 是第l层的邻居节点ID集合
}

// newNode 创建新节点
func newNode(id string, vector []float32, metadata map[string]interface{}, maxLevel int) *node {
	friends := make([]map[string]struct{}, maxLevel+1)
	for i := range friends {
		friends[i] = make(map[string]struct{})
	}
	return &node{
		id:       id,
		vector:   vector,
		metadata: metadata,
		level:    maxLevel,
		friends:  friends,
	}
}

// candidate 搜索候选节点
type candidate struct {
	id   string
	dist float32
}

// candidateHeap 候选堆（最小堆，按距离）
type candidateHeap struct {
	items []candidate
}

func newCandidateHeap() *candidateHeap {
	return &candidateHeap{items: make([]candidate, 0)}
}

func (h *candidateHeap) Len() int { return len(h.items) }

func (h *candidateHeap) Less(i, j int) bool { return h.items[i].dist < h.items[j].dist }

func (h *candidateHeap) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }

func (h *candidateHeap) Push(c candidate) {
	h.items = append(h.items, c)
	h.up(len(h.items) - 1)
}

func (h *candidateHeap) Pop() candidate {
	if len(h.items) == 0 {
		return candidate{}
	}
	n := len(h.items) - 1
	h.Swap(0, n)
	h.down(0, n)
	item := h.items[n]
	h.items = h.items[:n]
	return item
}

func (h *candidateHeap) Peek() candidate {
	if len(h.items) == 0 {
		return candidate{}
	}
	return h.items[0]
}

func (h *candidateHeap) up(j int) {
	for {
		i := (j - 1) / 2
		if i == j || !h.Less(j, i) {
			break
		}
		h.Swap(i, j)
		j = i
	}
}

func (h *candidateHeap) down(i0, n int) {
	i := i0
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 {
			break
		}
		j := j1
		if j2 := j1 + 1; j2 < n && h.Less(j2, j1) {
			j = j2
		}
		if !h.Less(j, i) {
			break
		}
		h.Swap(i, j)
		i = j
	}
}

// maxCandidateHeap 最大堆（按距离）
type maxCandidateHeap struct {
	items []candidate
}

func newMaxCandidateHeap() *maxCandidateHeap {
	return &maxCandidateHeap{items: make([]candidate, 0)}
}

func (h *maxCandidateHeap) Len() int { return len(h.items) }

func (h *maxCandidateHeap) Push(c candidate) {
	h.items = append(h.items, c)
	h.upMax(len(h.items) - 1)
}

func (h *maxCandidateHeap) Pop() candidate {
	if len(h.items) == 0 {
		return candidate{}
	}
	n := len(h.items) - 1
	h.items[0], h.items[n] = h.items[n], h.items[0]
	h.downMax(0, n)
	item := h.items[n]
	h.items = h.items[:n]
	return item
}

func (h *maxCandidateHeap) Peek() candidate {
	if len(h.items) == 0 {
		return candidate{}
	}
	return h.items[0]
}

func (h *maxCandidateHeap) upMax(j int) {
	for {
		i := (j - 1) / 2
		if i == j || h.items[j].dist <= h.items[i].dist {
			break
		}
		h.items[i], h.items[j] = h.items[j], h.items[i]
		j = i
	}
}

func (h *maxCandidateHeap) downMax(i0, n int) {
	i := i0
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 {
			break
		}
		j := j1
		if j2 := j1 + 1; j2 < n && h.items[j2].dist > h.items[j1].dist {
			j = j2
		}
		if h.items[j].dist <= h.items[i].dist {
			break
		}
		h.items[i], h.items[j] = h.items[j], h.items[i]
		i = j
	}
}

// HNSWStore HNSW向量存储（实现VectorStore接口）
type HNSWStore struct {
	config     HNSWConfig
	nodes      map[string]*node // 节点ID -> 节点
	entryPoint string           // 入口点ID
	maxLevel   int              // 当前最大层级
	mu         sync.RWMutex
	rand       *rand.Rand
}

// NewHNSWStore 创建HNSW存储
func NewHNSWStore(config HNSWConfig) *HNSWStore {
	if config.M <= 0 {
		config.M = DefaultM
	}
	if config.EfConstruction <= 0 {
		config.EfConstruction = DefaultEfConstruction
	}
	if config.EfSearch <= 0 {
		config.EfSearch = DefaultEfSearch
	}
	if config.Ml <= 0 {
		config.Ml = 1.0 / math.Log(float64(config.M))
	}

	return &HNSWStore{
		config: config,
		nodes:  make(map[string]*node),
		rand:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Init 初始化
func (h *HNSWStore) Init(ctx context.Context) error {
	return nil
}

// randomLevel 随机生成节点层级
func (h *HNSWStore) randomLevel() int {
	// 使用指数分布生成层级
	// P(level >= l) = exp(-l * mL)
	r := h.rand.Float64()
	if r == 0 {
		r = 1e-10 // 避免log(0)
	}
	level := int(-math.Log(r) * h.config.Ml)
	return level
}

// distance 计算两个向量的距离（使用欧氏距离）
func (h *HNSWStore) distance(a, b []float32) float32 {
	return EuclideanDistance(a, b)
}

// searchLayer 在指定层搜索最近邻
// 返回ef个最近的候选节点
func (h *HNSWStore) searchLayer(query []float32, entryPoints []string, ef int, level int) []candidate {
	// visited记录已访问的节点
	visited := make(map[string]struct{})

	// candidates是最小堆，存储待访问的候选节点（按距离排序）
	candidates := newCandidateHeap()

	// results是最大堆，存储当前找到的最近邻（按距离排序）
	results := newMaxCandidateHeap()

	// 初始化
	for _, ep := range entryPoints {
		if node, exists := h.nodes[ep]; exists {
			dist := h.distance(query, node.vector)
			candidates.Push(candidate{id: ep, dist: dist})
			results.Push(candidate{id: ep, dist: dist})
			visited[ep] = struct{}{}
		}
	}

	for candidates.Len() > 0 {
		// 取出距离最近的候选
		curr := candidates.Pop()

		// 如果当前候选距离大于结果中最大距离，停止搜索
		if results.Len() >= ef && curr.dist > results.Peek().dist {
			break
		}

		// 遍历当前节点的邻居
		currNode, exists := h.nodes[curr.id]
		if !exists {
			continue
		}

		for neighborID := range currNode.friends[level] {
			if _, seen := visited[neighborID]; seen {
				continue
			}
			visited[neighborID] = struct{}{}

			neighborNode, exists := h.nodes[neighborID]
			if !exists {
				continue
			}

			dist := h.distance(query, neighborNode.vector)

			// 如果结果未满或距离小于结果中最大距离
			if results.Len() < ef || dist < results.Peek().dist {
				candidates.Push(candidate{id: neighborID, dist: dist})
				results.Push(candidate{id: neighborID, dist: dist})

				// 如果结果超过ef，移除最远的
				if results.Len() > ef {
					results.Pop()
				}
			}
		}
	}

	// 将结果转换为切片并排序
	resultSlice := make([]candidate, results.Len())
	for i := len(resultSlice) - 1; i >= 0; i-- {
		resultSlice[i] = results.Pop()
	}

	// 按距离排序（从小到大）
	sort.Slice(resultSlice, func(i, j int) bool {
		return resultSlice[i].dist < resultSlice[j].dist
	})

	return resultSlice
}

// selectNeighborsSimple 简单邻居选择：选择最近的M个
func (h *HNSWStore) selectNeighborsSimple(candidates []candidate, M int) []string {
	if len(candidates) <= M {
		result := make([]string, len(candidates))
		for i, c := range candidates {
			result[i] = c.id
		}
		return result
	}

	result := make([]string, M)
	for i := 0; i < M; i++ {
		result[i] = candidates[i].id
	}
	return result
}

// selectNeighborsHeuristic 启发式邻居选择
// 基于论文中的Algorithm 4，考虑候选之间的距离
func (h *HNSWStore) selectNeighborsHeuristic(query []float32, candidates []candidate, M int, level int, extendCandidates bool, keepPruned bool) []string {
	if len(candidates) == 0 {
		return nil
	}

	// 工作队列
	W := make([]candidate, len(candidates))
	copy(W, candidates)
	sort.Slice(W, func(i, j int) bool {
		return W[i].dist < W[j].dist
	})

	// 结果集合
	R := make([]string, 0, M)

	// 扩展候选（添加候选的邻居）
	if extendCandidates && level > 0 {
		visited := make(map[string]struct{})
		for _, c := range W {
			visited[c.id] = struct{}{}
		}

		extendedW := make([]candidate, 0, len(W)*2)
		extendedW = append(extendedW, W...)

		for _, c := range W {
			if node, exists := h.nodes[c.id]; exists {
				for neighborID := range node.friends[level] {
					if _, seen := visited[neighborID]; !seen {
						visited[neighborID] = struct{}{}
						if neighborNode, ok := h.nodes[neighborID]; ok {
							dist := h.distance(query, neighborNode.vector)
							extendedW = append(extendedW, candidate{id: neighborID, dist: dist})
						}
					}
				}
			}
		}
		W = extendedW
		sort.Slice(W, func(i, j int) bool {
			return W[i].dist < W[j].dist
		})
	}

	// 选择邻居
	for len(W) > 0 && len(R) < M {
		// 取最近的候选
		e := W[0]
		W = W[1:]

		// 检查是否应该添加e
		good := true
		queryNode := h.nodes[e.id]
		if queryNode != nil {
			for _, rID := range R {
				if rNode, exists := h.nodes[rID]; exists {
					distToR := h.distance(queryNode.vector, rNode.vector)
					// 如果e到某个已选邻居的距离小于e到查询点的距离，则不选e
					if distToR < e.dist {
						good = false
						break
					}
				}
			}
		}

		if good {
			R = append(R, e.id)
		}
	}

	// 如果keepPruned且结果不足M，从被剪枝的候选中补充
	if keepPruned && len(R) < M {
		// 这里简化处理，直接从剩余候选中补充
		for len(W) > 0 && len(R) < M {
			R = append(R, W[0].id)
			W = W[1:]
		}
	}

	return R
}

// connectNeighbors 连接节点与其邻居
func (h *HNSWStore) connectNeighbors(nodeID string, neighbors []string, level int) {
	node, exists := h.nodes[nodeID]
	if !exists {
		return
	}

	// 添加双向连接
	for _, neighborID := range neighbors {
		// 添加 node -> neighbor
		node.friends[level][neighborID] = struct{}{}

		// 添加 neighbor -> node
		if neighbor, exists := h.nodes[neighborID]; exists {
			neighbor.friends[level][nodeID] = struct{}{}

			// 检查邻居的连接数是否超过限制
			maxConn := h.config.M
			if level == 0 {
				maxConn = 2 * h.config.M
			}

			if len(neighbor.friends[level]) > maxConn {
				// 需要剪枝，保留最近的邻居
				h.pruneConnections(neighborID, level, maxConn)
			}
		}
	}
}

// pruneConnections 剪枝连接
func (h *HNSWStore) pruneConnections(nodeID string, level int, maxConn int) {
	node, exists := h.nodes[nodeID]
	if !exists {
		return
	}

	if len(node.friends[level]) <= maxConn {
		return
	}

	// 收集所有邻居及其距离
	candidates := make([]candidate, 0, len(node.friends[level]))
	for neighborID := range node.friends[level] {
		if neighbor, exists := h.nodes[neighborID]; exists {
			dist := h.distance(node.vector, neighbor.vector)
			candidates = append(candidates, candidate{id: neighborID, dist: dist})
		}
	}

	// 选择保留的邻居
	keep := h.selectNeighborsHeuristic(node.vector, candidates, maxConn, level, false, false)

	// 更新连接
	newFriends := make(map[string]struct{})
	for _, id := range keep {
		newFriends[id] = struct{}{}
	}

	// 移除被剪枝的连接
	for oldNeighbor := range node.friends[level] {
		if _, keep := newFriends[oldNeighbor]; !keep {
			// 从邻居节点移除反向连接
			if neighbor, exists := h.nodes[oldNeighbor]; exists {
				delete(neighbor.friends[level], nodeID)
			}
		}
	}

	node.friends[level] = newFriends
}

// insertNode 插入单个节点
func (h *HNSWStore) insertNode(id string, vector []float32, metadata map[string]interface{}) error {
	// 检查向量维度
	if len(vector) != h.config.Dim {
		return fmt.Errorf("向量维度不匹配: expected %d, got %d", h.config.Dim, len(vector))
	}

	// 创建新节点
	level := h.randomLevel()
	newNode := newNode(id, vector, metadata, level)
	h.nodes[id] = newNode

	// 如果是第一个节点，设为入口点
	if h.entryPoint == "" {
		h.entryPoint = id
		h.maxLevel = level
		return nil
	}

	// 获取入口点
	entryPoints := []string{h.entryPoint}

	// 从最高层向下搜索，找到每层的最近入口点
	for lc := h.maxLevel; lc > level; lc-- {
		results := h.searchLayer(vector, entryPoints, 1, lc)
		if len(results) > 0 {
			entryPoints = []string{results[0].id}
		}
	}

	// 从level层到0层，搜索并连接
	for lc := min(level, h.maxLevel); lc >= 0; lc-- {
		// 搜索efConstruction个最近邻
		results := h.searchLayer(vector, entryPoints, h.config.EfConstruction, lc)

		// 选择邻居
		maxConn := h.config.M
		if lc == 0 {
			maxConn = 2 * h.config.M
		}
		neighbors := h.selectNeighborsHeuristic(vector, results, maxConn, lc, true, true)

		// 连接
		h.connectNeighbors(id, neighbors, lc)

		// 更新入口点用于下一层
		if len(results) > 0 {
			entryPoints = make([]string, 0, len(results))
			for _, r := range results {
				entryPoints = append(entryPoints, r.id)
			}
		}
	}

	// 如果新节点层级高于当前最大层级，更新入口点
	if level > h.maxLevel {
		h.entryPoint = id
		h.maxLevel = level
	}

	return nil
}

// Insert 插入向量（实现VectorStore接口）
func (h *HNSWStore) Insert(ctx context.Context, records []*VectorRecord) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, record := range records {
		if record.ID == "" {
			record.ID = uuid.New().String()
		}
		if err := h.insertNode(record.ID, record.Vector, record.Metadata); err != nil {
			return err
		}
	}
	return nil
}

// Search 搜索相似向量（实现VectorStore接口）
func (h *HNSWStore) Search(ctx context.Context, queryVector []float32, topK int, filters map[string]interface{}) ([]*models.RetrievalResult, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(queryVector) != h.config.Dim {
		return nil, fmt.Errorf("查询向量维度不匹配: expected %d, got %d", h.config.Dim, len(queryVector))
	}

	if h.entryPoint == "" {
		return nil, nil
	}

	ef := h.config.EfSearch
	if ef < topK {
		ef = topK
	}

	// 从最高层开始搜索
	entryPoints := []string{h.entryPoint}

	// 在高层贪婪搜索，找到每层的最近入口点
	for lc := h.maxLevel; lc > 0; lc-- {
		results := h.searchLayer(queryVector, entryPoints, 1, lc)
		if len(results) > 0 {
			entryPoints = []string{results[0].id}
		}
	}

	// 在第0层搜索ef个候选
	results := h.searchLayer(queryVector, entryPoints, ef, 0)

	// 应用过滤器并限制结果数
	var filteredResults []*models.RetrievalResult
	for _, r := range results {
		node, exists := h.nodes[r.id]
		if !exists {
			continue
		}

		// 应用过滤器
		if !matchFilters(node.metadata, filters) {
			continue
		}

		// 将距离转换为相似度分数（距离越小，相似度越高）
		// 使用 1 / (1 + distance) 作为相似度
		similarity := 1.0 / (1.0 + float64(r.dist))

		filteredResults = append(filteredResults, &models.RetrievalResult{
			ID:       r.id,
			Score:    similarity,
			Metadata: node.metadata,
		})

		if len(filteredResults) >= topK {
			break
		}
	}

	return filteredResults, nil
}

// Delete 删除向量（实现VectorStore接口）
func (h *HNSWStore) Delete(ctx context.Context, ids []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, id := range ids {
		node, exists := h.nodes[id]
		if !exists {
			continue
		}

		// 从所有邻居中移除该节点的连接
		for level, friends := range node.friends {
			for friendID := range friends {
				if friend, exists := h.nodes[friendID]; exists {
					delete(friend.friends[level], id)
				}
			}
		}

		// 删除节点
		delete(h.nodes, id)

		// 如果删除的是入口点，选择新的入口点
		if id == h.entryPoint {
			h.entryPoint = ""
			h.maxLevel = 0
			for nodeID, node := range h.nodes {
				if h.entryPoint == "" || node.level > h.maxLevel {
					h.entryPoint = nodeID
					h.maxLevel = node.level
				}
			}
		}
	}
	return nil
}

// DeleteByFilter 根据过滤器删除（实现VectorStore接口）
func (h *HNSWStore) DeleteByFilter(ctx context.Context, filter map[string]interface{}) error {
	h.mu.RLock()
	var idsToDelete []string
	for id, node := range h.nodes {
		if matchFilters(node.metadata, filter) {
			idsToDelete = append(idsToDelete, id)
		}
	}
	h.mu.RUnlock()

	if len(idsToDelete) == 0 {
		return nil
	}

	return h.Delete(ctx, idsToDelete)
}

// Close 关闭存储（实现VectorStore接口）
func (h *HNSWStore) Close() error {
	return nil
}

// Stats 返回统计信息
func (h *HNSWStore) Stats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	totalConnections := 0
	levelDistribution := make(map[int]int)

	for _, node := range h.nodes {
		levelDistribution[node.level]++
		for _, friends := range node.friends {
			totalConnections += len(friends)
		}
	}

	return map[string]interface{}{
		"node_count":         len(h.nodes),
		"max_level":          h.maxLevel,
		"entry_point":        h.entryPoint,
		"total_connections":  totalConnections,
		"level_distribution": levelDistribution,
		"m":                  h.config.M,
		"ef_construction":    h.config.EfConstruction,
		"ef_search":          h.config.EfSearch,
		"ml":                 h.config.Ml,
	}
}

// SetEfSearch 设置搜索时的ef参数
func (h *HNSWStore) SetEfSearch(ef int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config.EfSearch = ef
}

// min 返回两个整数的最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
