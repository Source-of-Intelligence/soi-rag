package knowledgegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Source-of-Intelligence/soi-rag/pkg/llm"
	"github.com/Source-of-Intelligence/soi-rag/pkg/models"
	"github.com/google/uuid"
)

// LLMExtractor 基于LLM的实体和关系抽取器
type LLMExtractor struct {
	llm        llm.LLM
	maxRetries int
}

// NewLLMExtractor 创建基于LLM的抽取器
func NewLLMExtractor(llm llm.LLM) *LLMExtractor {
	return &LLMExtractor{
		llm:        llm,
		maxRetries: 3,
	}
}

// extractionResponse LLM返回的抽取结果结构
type extractionResponse struct {
	Entities  []extractedEntity   `json:"entities"`
	Relations []extractedRelation `json:"relations"`
}

// extractedEntity LLM返回的实体结构
type extractedEntity struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Aliases     []string `json:"aliases,omitempty"`
	Description string   `json:"description,omitempty"`
}

// extractedRelation LLM返回的关系结构
type extractedRelation struct {
	SourceName string `json:"source"`
	TargetName string `json:"target"`
	Type       string `json:"type"`
}

// Extract 从文本中抽取实体和关系
func (e *LLMExtractor) Extract(ctx context.Context, text string) (*models.ExtractionResult, error) {
	prompt := e.buildPrompt(text)

	var response *extractionResponse
	var err error

	// 尝试多次解析
	for i := 0; i < e.maxRetries; i++ {
		response, err = e.callLLM(ctx, prompt)
		if err == nil {
			break
		}
	}

	if err != nil {
		return nil, fmt.Errorf("LLM抽取失败: %w", err)
	}

	return e.convertResult(response), nil
}

// buildPrompt 构建抽取提示词
func (e *LLMExtractor) buildPrompt(text string) string {
	return fmt.Sprintf(`你是一个知识图谱构建专家。请从以下文本中识别实体和关系。

## 实体类型
- PERSON: 人名
- ORGANIZATION: 组织、公司、机构
- LOCATION: 地名、城市、国家
- DATE: 日期、时间
- PRODUCT: 产品、软件、工具
- EVENT: 事件、活动
- CONCEPT: 概念、理论、方法
- TECHNOLOGY: 技术、编程语言、框架
- INDUSTRY: 行业、领域

## 关系类型
- WORKS_FOR: 工作于
- LOCATED_IN: 位于
- FOUNDED_BY: 由...创立
- PART_OF: 是...的一部分
- RELATED_TO: 相关
- USES: 使用
- PRODUCES: 生产/产出
- COMPETES_WITH: 与...竞争
- INVESTED_IN: 投资于
- ACQUIRED: 收购
- PARTNERED_WITH: 与...合作

## 要求
1. 识别文本中所有重要的命名实体（人名、地名、组织、产品、技术等）
2. 识别实体之间的关系
3. 只识别文本中明确提及的实体和关系，不要推断
4. 返回严格的JSON格式

## 待分析文本
%s

## 输出格式（JSON）
{
  "entities": [
    {
      "name": "实体名称",
      "type": "实体类型",
      "aliases": ["别名1", "别名2"],
      "description": "简要描述"
    }
  ],
  "relations": [
    {
      "source": "源实体名称",
      "target": "目标实体名称",
      "type": "关系类型"
    }
  ]
}

请只输出JSON，不要包含其他内容。`, text)
}

// callLLM 调用LLM并解析结果
func (e *LLMExtractor) callLLM(ctx context.Context, prompt string) (*extractionResponse, error) {
	output, err := e.llm.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM生成失败: %w", err)
	}

	// 清理输出，提取JSON部分
	jsonStr := e.extractJSON(output)

	var response extractionResponse
	if err := json.Unmarshal([]byte(jsonStr), &response); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w, 原始输出: %s", err, output)
	}

	return &response, nil
}

// extractJSON 从LLM输出中提取JSON部分
func (e *LLMExtractor) extractJSON(output string) string {
	output = strings.TrimSpace(output)

	// 尝试找到JSON对象的边界
	startIdx := strings.Index(output, "{")
	if startIdx == -1 {
		return output
	}

	// 找到匹配的闭合括号
	braceCount := 0
	endIdx := -1
	for i := startIdx; i < len(output); i++ {
		if output[i] == '{' {
			braceCount++
		} else if output[i] == '}' {
			braceCount--
			if braceCount == 0 {
				endIdx = i + 1
				break
			}
		}
	}

	if endIdx == -1 {
		return output[startIdx:]
	}

	return output[startIdx:endIdx]
}

// convertResult 将LLM响应转换为ExtractionResult
func (e *LLMExtractor) convertResult(resp *extractionResponse) *models.ExtractionResult {
	result := &models.ExtractionResult{
		Entities:  make([]*models.Entity, 0, len(resp.Entities)),
		Relations: make([]*models.Relation, 0, len(resp.Relations)),
	}

	// 创建实体名称到ID的映射
	entityNameToID := make(map[string]string)

	// 转换实体
	for _, ent := range resp.Entities {
		entityType := e.parseEntityType(ent.Type)
		entity := &models.Entity{
			ID:          uuid.New().String(),
			Name:        ent.Name,
			Type:        entityType,
			Aliases:     ent.Aliases,
			Description: ent.Description,
			Confidence:  0.85, // LLM抽取的置信度
		}
		result.Entities = append(result.Entities, entity)

		// 记录名称到ID的映射
		entityNameToID[strings.ToLower(ent.Name)] = entity.ID
		for _, alias := range ent.Aliases {
			entityNameToID[strings.ToLower(alias)] = entity.ID
		}
	}

	// 转换关系
	for _, rel := range resp.Relations {
		sourceID := entityNameToID[strings.ToLower(rel.SourceName)]
		targetID := entityNameToID[strings.ToLower(rel.TargetName)]

		// 如果实体ID不存在，跳过该关系
		if sourceID == "" || targetID == "" {
			continue
		}

		relationType := e.parseRelationType(rel.Type)
		relation := &models.Relation{
			ID:         uuid.New().String(),
			SourceID:   sourceID,
			TargetID:   targetID,
			Type:       relationType,
			Confidence: 0.80,
		}
		result.Relations = append(result.Relations, relation)
	}

	return result
}

// parseEntityType 解析实体类型
func (e *LLMExtractor) parseEntityType(typeStr string) models.EntityType {
	switch strings.ToUpper(typeStr) {
	case "PERSON":
		return models.EntityPerson
	case "ORGANIZATION", "ORG", "COMPANY":
		return models.EntityOrganization
	case "LOCATION", "PLACE", "CITY", "COUNTRY":
		return models.EntityLocation
	case "DATE", "TIME":
		return models.EntityDate
	case "PRODUCT", "SOFTWARE", "TOOL":
		return models.EntityProduct
	case "EVENT":
		return models.EntityEvent
	case "CONCEPT", "METHOD", "THEORY":
		return models.EntityConcept
	case "TECHNOLOGY", "TECH", "LANGUAGE", "FRAMEWORK":
		return models.EntityTechnology
	case "INDUSTRY", "FIELD", "DOMAIN":
		return models.EntityIndustry
	default:
		return models.EntityConcept
	}
}

// parseRelationType 解析关系类型
func (e *LLMExtractor) parseRelationType(typeStr string) models.RelationType {
	switch strings.ToUpper(typeStr) {
	case "WORKS_FOR", "WORK_FOR":
		return models.RelWorksFor
	case "LOCATED_IN", "LOCATE_IN":
		return models.RelLocatedIn
	case "FOUNDED_BY", "FOUND_BY":
		return models.RelFoundedBy
	case "PART_OF":
		return models.RelPartOf
	case "RELATED_TO", "RELATED":
		return models.RelRelatedTo
	case "USES", "USE":
		return models.RelUses
	case "PRODUCES", "PRODUCE":
		return models.RelProduces
	case "COMPETES_WITH", "COMPETE_WITH":
		return models.RelCompetesWith
	case "INVESTED_IN", "INVEST_IN":
		return models.RelInvestedIn
	case "ACQUIRED", "ACQUIRE":
		return models.RelAcquired
	case "PARTNERED_WITH", "PARTNER_WITH":
		return models.RelPartneredWith
	default:
		return models.RelRelatedTo
	}
}

// SetMaxRetries 设置最大重试次数
func (e *LLMExtractor) SetMaxRetries(maxRetries int) {
	if maxRetries > 0 {
		e.maxRetries = maxRetries
	}
}
