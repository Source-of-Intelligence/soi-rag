package eval

import (
	"strings"
	"testing"

	"github.com/Source-of-Intelligence/soi-rag/pkg/config"
)

// TestEvalResultString 验证 String() 输出格式
func TestEvalResultString(t *testing.T) {
	result := &EvalResult{
		Recall:    map[int]float64{1: 0.5, 5: 0.8, 10: 0.9},
		Precision: map[int]float64{1: 0.5, 5: 0.6, 10: 0.4},
		MRR:       0.75,
		Map:       0.65,
		NDCG:      map[int]float64{1: 0.5, 10: 0.85},
		Total:     10,
		Errors:    nil,
	}

	s := result.String()

	// 验证输出包含关键内容
	if !strings.Contains(s, "=== 检索质量评估结果 ===") {
		t.Error("String() 输出应包含标题 '=== 检索质量评估结果 ==='")
	}
	if !strings.Contains(s, "Recall@K:") {
		t.Error("String() 输出应包含 'Recall@K:'")
	}
	if !strings.Contains(s, "Precision@K:") {
		t.Error("String() 输出应包含 'Precision@K:'")
	}
	if !strings.Contains(s, "MRR:") {
		t.Error("String() 输出应包含 'MRR:'")
	}
	if !strings.Contains(s, "MAP:") {
		t.Error("String() 输出应包含 'MAP:'")
	}
	if !strings.Contains(s, "NDCG@K:") {
		t.Error("String() 输出应包含 'NDCG@K:'")
	}
	if !strings.Contains(s, "总查询数: 10") {
		t.Error("String() 输出应包含 '总查询数: 10'")
	}

	// 验证具体数值
	if !strings.Contains(s, "Recall@1: 0.5000") {
		t.Error("String() 输出应包含 'Recall@1: 0.5000'")
	}
	if !strings.Contains(s, "MRR: 0.7500") {
		t.Error("String() 输出应包含 'MRR: 0.7500'")
	}

	// 测试带错误的输出
	resultWithErrors := &EvalResult{
		Recall:    map[int]float64{1: 0.0},
		Precision: map[int]float64{1: 0.0},
		MRR:       0.0,
		Map:       0.0,
		NDCG:      map[int]float64{1: 0.0},
		Total:     5,
		Errors: []EvalError{
			{Query: "test query", Error: "some error"},
			{Query: "another query", Error: "another error"},
		},
	}

	s = resultWithErrors.String()
	if !strings.Contains(s, "错误数: 2") {
		t.Error("String() 输出应包含 '错误数: 2'")
	}
}

// TestCheckQuality 验证质量检验通过/不通过
func TestCheckQuality(t *testing.T) {
	// 测试通过的情况：所有指标都达标
	passResult := &EvalResult{
		Recall:    map[int]float64{1: 0.8, 5: 0.9, 10: 1.0},
		Precision: map[int]float64{5: 0.8},
		MRR:       0.9,
		Map:       0.8,
		NDCG:      map[int]float64{10: 0.9},
		Total:     10,
	}

	check := passResult.CheckQuality(nil)
	if !check.Passed {
		t.Error("所有指标达标时 CheckQuality 应返回 Passed=true")
	}
	if check.Score <= 0 {
		t.Error("Score 应大于 0")
	}
	if len(check.Warnings) != 0 {
		t.Errorf("所有指标达标时不应有警告, 实际警告数=%d", len(check.Warnings))
	}
	if len(check.Details) == 0 {
		t.Error("Details 不应为空")
	}

	// 验证每个 Detail 都 Passed
	for _, d := range check.Details {
		if !d.Passed {
			t.Errorf("指标 %s 应通过检验", d.Metric)
		}
	}

	// 测试不通过的情况：部分指标不达标
	failResult := &EvalResult{
		Recall:    map[int]float64{1: 0.1, 5: 0.2, 10: 0.3},
		Precision: map[int]float64{5: 0.1},
		MRR:       0.1,
		Map:       0.1,
		NDCG:      map[int]float64{10: 0.1},
		Total:     10,
	}

	check = failResult.CheckQuality(nil)
	if check.Passed {
		t.Error("指标不达标时 CheckQuality 应返回 Passed=false")
	}
	if len(check.Warnings) == 0 {
		t.Error("指标不达标时应有警告信息")
	}

	// 验证存在未通过的 Detail
	hasFailed := false
	for _, d := range check.Details {
		if !d.Passed {
			hasFailed = true
			if d.Value >= d.Threshold {
				t.Errorf("指标 %s: value=%.4f 应小于 threshold=%.4f", d.Metric, d.Value, d.Threshold)
			}
		}
	}
	if !hasFailed {
		t.Error("应存在未通过的指标详情")
	}

	// 测试自定义阈值
	customThreshold := &config.QualityThreshold{
		MinRecallAt1:    0.0,
		MinRecallAt5:    0.0,
		MinRecallAt10:   0.0,
		MinPrecisionAt5: 0.0,
		MinMRR:          0.0,
		MinNDCGAt10:     0.0,
		MinMAP:          0.0,
	}

	check = failResult.CheckQuality(customThreshold)
	if !check.Passed {
		t.Error("阈值为 0 时所有指标都应通过")
	}
}

// TestCheckQualityWithConfig 使用配置文件阈值
func TestCheckQualityWithConfig(t *testing.T) {
	result := &EvalResult{
		Recall:    map[int]float64{1: 0.5, 5: 0.6, 10: 0.8},
		Precision: map[int]float64{5: 0.5},
		MRR:       0.5,
		Map:       0.5,
		NDCG:      map[int]float64{10: 0.6},
		Total:     10,
	}

	// 使用默认配置阈值
	cfg := config.DefaultFileConfig()
	check := result.CheckQualityWithConfig(cfg)

	// 验证使用了配置中的阈值
	if len(check.Details) == 0 {
		t.Fatal("Details 不应为空")
	}

	// Recall@1=0.5 >= 默认阈值 0.3, 应通过
	recall1Detail := findDetail(check.Details, "Recall@1")
	if recall1Detail == nil {
		t.Fatal("应包含 Recall@1 详情")
	}
	if !recall1Detail.Passed {
		t.Errorf("Recall@1=%.4f 应 >= 阈值 %.4f", recall1Detail.Value, recall1Detail.Threshold)
	}
	if recall1Detail.Threshold != cfg.Threshold.MinRecallAt1 {
		t.Errorf("Recall@1 阈值应来自配置, 期望=%.4f, 实际=%.4f",
			cfg.Threshold.MinRecallAt1, recall1Detail.Threshold)
	}

	// 使用自定义配置
	customCfg := &config.FileConfig{
		Threshold: config.QualityThreshold{
			MinRecallAt1:    0.9, // 设置高阈值，使 Recall@1=0.5 不通过
			MinRecallAt5:    0.0,
			MinRecallAt10:   0.0,
			MinPrecisionAt5: 0.0,
			MinMRR:          0.0,
			MinNDCGAt10:     0.0,
			MinMAP:          0.0,
		},
	}

	check = result.CheckQualityWithConfig(customCfg)
	if check.Passed {
		t.Error("使用高阈值配置时，Recall@1=0.5 < 0.9 应导致不通过")
	}

	recall1Detail = findDetail(check.Details, "Recall@1")
	if recall1Detail == nil {
		t.Fatal("应包含 Recall@1 详情")
	}
	if recall1Detail.Passed {
		t.Errorf("Recall@1=%.4f 应 < 阈值 %.4f, 不应通过", recall1Detail.Value, recall1Detail.Threshold)
	}
	if recall1Detail.Threshold != 0.9 {
		t.Errorf("Recall@1 阈值应为 0.9, 实际=%.4f", recall1Detail.Threshold)
	}
}

// findDetail 辅助函数：从详情列表中查找指定指标
func findDetail(details []QualityDetail, metric string) *QualityDetail {
	for i := range details {
		if details[i].Metric == metric {
			return &details[i]
		}
	}
	return nil
}

// TestGetErrors 验证错误获取
func TestGetErrors(t *testing.T) {
	// 测试无错误
	result := &EvalResult{
		Recall:    map[int]float64{1: 0.5},
		Precision: map[int]float64{1: 0.5},
		MRR:       0.5,
		Map:       0.5,
		NDCG:      map[int]float64{1: 0.5},
		Total:     10,
		Errors:    nil,
	}

	errors := result.GetErrors()
	if errors != nil {
		t.Errorf("无错误时 GetErrors 应返回 nil, 实际=%v", errors)
	}

	// 测试有错误
	result.Errors = []EvalError{
		{Query: "查询1", Error: "连接超时"},
		{Query: "查询2", Error: "索引为空"},
	}

	errors = result.GetErrors()
	if len(errors) != 2 {
		t.Fatalf("期望 2 个错误, 实际=%d", len(errors))
	}
	if errors[0].Query != "查询1" {
		t.Errorf("第一个错误 Query 应为 '查询1', 实际=%s", errors[0].Query)
	}
	if errors[0].Error != "连接超时" {
		t.Errorf("第一个错误 Error 应为 '连接超时', 实际=%s", errors[0].Error)
	}
	if errors[1].Query != "查询2" {
		t.Errorf("第二个错误 Query 应为 '查询2', 实际=%s", errors[1].Query)
	}

	// 测试空错误列表
	result.Errors = []EvalError{}
	errors = result.GetErrors()
	if len(errors) != 0 {
		t.Errorf("空错误列表应返回长度为 0, 实际=%d", len(errors))
	}
}

// TestCalculateRecall 测试 Recall 计算
func TestCalculateRecall(t *testing.T) {
	ev := &Evaluator{}

	relevant := map[string]bool{"a": true, "b": true, "c": true}
	retrieved := []string{"a", "x", "b", "y", "c"}

	// Recall@3: 前三个中有 a, b -> 2/3
	recall := ev.CalculateRecall(retrieved, relevant, 3)
	if recall != 2.0/3.0 {
		t.Errorf("Recall@3 应为 %.4f, 实际=%.4f", 2.0/3.0, recall)
	}

	// Recall@5: 前五个中有 a, b, c -> 3/3 = 1.0
	recall = ev.CalculateRecall(retrieved, relevant, 5)
	if recall != 1.0 {
		t.Errorf("Recall@5 应为 1.0, 实际=%.4f", recall)
	}

	// 空相关集
	recall = ev.CalculateRecall(retrieved, map[string]bool{}, 5)
	if recall != 0 {
		t.Errorf("空相关集 Recall 应为 0, 实际=%.4f", recall)
	}

	// k 大于检索结果长度
	recall = ev.CalculateRecall([]string{"a"}, relevant, 10)
	if recall != 1.0/3.0 {
		t.Errorf("k>len(retrieved) 时 Recall 应为 %.4f, 实际=%.4f", 1.0/3.0, recall)
	}
}

// TestCalculatePrecision 测试 Precision 计算
func TestCalculatePrecision(t *testing.T) {
	ev := &Evaluator{}

	relevant := map[string]bool{"a": true, "b": true}
	retrieved := []string{"a", "x", "b", "y"}

	// Precision@2: 前两个中 a 命中 -> 1/2
	prec := ev.CalculatePrecision(retrieved, relevant, 2)
	if prec != 0.5 {
		t.Errorf("Precision@2 应为 0.5, 实际=%.4f", prec)
	}

	// Precision@4: 前四个中 a, b 命中 -> 2/4
	prec = ev.CalculatePrecision(retrieved, relevant, 4)
	if prec != 0.5 {
		t.Errorf("Precision@4 应为 0.5, 实际=%.4f", prec)
	}

	// k=0
	prec = ev.CalculatePrecision(retrieved, relevant, 0)
	if prec != 0 {
		t.Errorf("k=0 时 Precision 应为 0, 实际=%.4f", prec)
	}
}

// TestCalculateMRR 测试 MRR 计算
func TestCalculateMRR(t *testing.T) {
	ev := &Evaluator{}

	relevant := map[string]bool{"b": true, "c": true}

	// 第一个相关结果在位置 2 -> MRR = 1/2
	mrr := ev.CalculateMRR([]string{"a", "b", "c"}, relevant)
	if mrr != 0.5 {
		t.Errorf("MRR 应为 0.5, 实际=%.4f", mrr)
	}

	// 第一个相关结果在位置 1 -> MRR = 1/1
	mrr = ev.CalculateMRR([]string{"b", "a", "c"}, relevant)
	if mrr != 1.0 {
		t.Errorf("MRR 应为 1.0, 实际=%.4f", mrr)
	}

	// 无相关结果 -> MRR = 0
	mrr = ev.CalculateMRR([]string{"a", "x", "y"}, relevant)
	if mrr != 0 {
		t.Errorf("无相关结果时 MRR 应为 0, 实际=%.4f", mrr)
	}
}

// TestNewEvalDataset 测试创建评估数据集
func TestNewEvalDataset(t *testing.T) {
	ds := NewEvalDataset()
	if ds == nil {
		t.Fatal("NewEvalDataset 返回 nil")
	}
	if len(ds.Queries) != 0 {
		t.Errorf("新建数据集 Queries 应为空, 实际=%d", len(ds.Queries))
	}

	ds.AddQuery("什么是RAG", []string{"doc1", "doc2"}, map[string]float64{"doc1": 1.0, "doc2": 0.8})
	if len(ds.Queries) != 1 {
		t.Errorf("添加查询后 Queries 应有 1 条, 实际=%d", len(ds.Queries))
	}
	if ds.Queries[0].Query != "什么是RAG" {
		t.Errorf("查询文本应为 '什么是RAG', 实际=%s", ds.Queries[0].Query)
	}

	ds.AddQuery("向量数据库", []string{"doc3"}, nil)
	if len(ds.Queries) != 2 {
		t.Errorf("再次添加后 Queries 应有 2 条, 实际=%d", len(ds.Queries))
	}
}

// TestDefaultEvalKs 测试默认 K 值
func TestDefaultEvalKs(t *testing.T) {
	ks := DefaultEvalKs()
	if len(ks) != 4 {
		t.Errorf("默认 K 值应有 4 个, 实际=%d", len(ks))
	}
	expected := []int{1, 5, 10, 20}
	for i, k := range ks {
		if k != expected[i] {
			t.Errorf("K[%d] 应为 %d, 实际=%d", i, expected[i], k)
		}
	}
}

// TestEvalResultToMap 测试结果转 map
func TestEvalResultToMap(t *testing.T) {
	result := &EvalResult{
		Recall:    map[int]float64{1: 0.5, 5: 0.8},
		Precision: map[int]float64{5: 0.6},
		MRR:       0.7,
		Map:       0.6,
		NDCG:      map[int]float64{10: 0.9},
		Total:     5,
	}

	m := result.ToMap()
	if m["total"] != 5 {
		t.Errorf("map['total'] 应为 5, 实际=%v", m["total"])
	}
	if m["mrr"] != 0.7 {
		t.Errorf("map['mrr'] 应为 0.7, 实际=%v", m["mrr"])
	}
	if _, ok := m["recall"]; !ok {
		t.Error("map 应包含 'recall'")
	}
	if _, ok := m["precision"]; !ok {
		t.Error("map 应包含 'precision'")
	}
}
