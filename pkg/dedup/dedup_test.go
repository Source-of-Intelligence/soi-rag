package dedup

import (
	"context"
	"testing"

	"github.com/ragtool/rag/pkg/models"
)

func TestCalculateStringHash(t *testing.T) {
	hash1 := CalculateStringHash("test content")
	hash2 := CalculateStringHash("test content")
	hash3 := CalculateStringHash("different content")

	if hash1 != hash2 {
		t.Error("相同内容应产生相同哈希")
	}
	if hash1 == hash3 {
		t.Error("不同内容应产生不同哈希")
	}

	// SM3 哈希应为 64 字符的十六进制字符串
	if len(hash1) != 64 {
		t.Errorf("SM3 哈希长度应为 64，实际为 %d", len(hash1))
	}
}

func TestVerifyHash(t *testing.T) {
	data := []byte("test data for verification")
	hash := CalculateStringHash(string(data))

	if !VerifyHash(data, hash) {
		t.Error("正确哈希应验证通过")
	}

	modified := []byte("modified data")
	if VerifyHash(modified, hash) {
		t.Error("错误哈希应验证失败")
	}
}

func TestInMemoryDedupStore_ExistsByHash(t *testing.T) {
	store := NewInMemoryDedupStore()
	ctx := context.Background()

	doc := models.NewDocument("Test", "test content for dedup", "source", models.DocTypeText)
	doc.ID = "doc-1"
	hash := CalculateStringHash("test content for dedup")

	// 创建文档
	err := store.CreateDocumentWithHash(ctx, doc, hash)
	if err != nil {
		t.Fatalf("CreateDocumentWithHash 失败: %v", err)
	}

	// 检查哈希是否存在
	found, foundDoc, err := store.ExistsByHash(ctx, hash)
	if err != nil {
		t.Fatalf("ExistsByHash 失败: %v", err)
	}
	if !found {
		t.Error("应能找到已创建的文档")
	}
	if foundDoc.ID != "doc-1" {
		t.Errorf("文档 ID 应为 doc-1，实际为 %s", foundDoc.ID)
	}
}

func TestInMemoryDedupStore_Duplicate(t *testing.T) {
	store := NewInMemoryDedupStore()
	ctx := context.Background()

	doc := models.NewDocument("Test", "duplicate content", "source", models.DocTypeText)
	doc.ID = "doc-1"
	hash := CalculateStringHash("duplicate content")

	// 第一次创建
	err := store.CreateDocumentWithHash(ctx, doc, hash)
	if err != nil {
		t.Fatalf("第一次创建失败: %v", err)
	}

	// 检查哈希已存在
	found, existingDoc, _ := store.ExistsByHash(ctx, hash)
	if !found {
		t.Error("应能找到已创建的文档")
	}
	if existingDoc.ID != "doc-1" {
		t.Errorf("应返回第一个文档 doc-1，实际为 %s", existingDoc.ID)
	}
}

func TestInMemoryDedupStore_GetByHash(t *testing.T) {
	store := NewInMemoryDedupStore()
	ctx := context.Background()

	doc := models.NewDocument("Test", "get by hash test", "source", models.DocTypeText)
	doc.ID = "doc-get"
	hash := CalculateStringHash("get by hash test")

	store.CreateDocumentWithHash(ctx, doc, hash)

	// 通过哈希获取
	foundDoc, err := store.GetByHash(ctx, hash)
	if err != nil {
		t.Fatalf("GetByHash 失败: %v", err)
	}
	if foundDoc.ID != "doc-get" {
		t.Errorf("文档 ID 应为 doc-get，实际为 %s", foundDoc.ID)
	}

	// 不存在的哈希
	_, err = store.GetByHash(ctx, "nonexistent-hash")
	if err == nil {
		t.Error("不存在的哈希应返回错误")
	}
}

func TestInMemoryDedupStore_DeleteDocument(t *testing.T) {
	store := NewInMemoryDedupStore()
	ctx := context.Background()

	doc := models.NewDocument("Test", "delete test content", "source", models.DocTypeText)
	doc.ID = "doc-delete"
	hash := CalculateStringHash("delete test content")

	store.CreateDocumentWithHash(ctx, doc, hash)
	store.DeleteDocument(ctx, "doc-delete")

	// 删除后不应存在
	found, _, _ := store.ExistsByHash(ctx, hash)
	if found {
		t.Error("删除后哈希不应存在")
	}
}

func TestDedupService_CheckAndDedup(t *testing.T) {
	store := NewInMemoryDedupStore()
	service := NewService(store, true)
	ctx := context.Background()

	content := []byte("unique content for service test")
	hash := CalculateHash(content)

	// 第一次检查（未存储，应返回非重复）
	result1, err := service.CheckAndDedup(ctx, content)
	if err != nil {
		t.Fatalf("CheckAndDedup 失败: %v", err)
	}
	if result1.IsDuplicate {
		t.Error("新内容检查时不应被标记为重复（需先存储）")
	}
	if result1.Hash == "" {
		t.Error("哈希不应为空")
	}

	// 先存储文档（模拟外部存储）
	doc := models.NewDocument("Test", string(content), "source", models.DocTypeText)
	store.CreateDocumentWithHash(ctx, doc, hash)

	// 第二次检查（已存储，应返回重复）
	result2, err := service.CheckAndDedup(ctx, content)
	if err != nil {
		t.Fatalf("CheckAndDedup 第二次失败: %v", err)
	}
	if !result2.IsDuplicate {
		t.Error("重复内容应被标记为重复")
	}
}

func TestDedupService_Disabled(t *testing.T) {
	store := NewInMemoryDedupStore()
	service := NewService(store, false) // 禁用去重
	ctx := context.Background()

	content := []byte("content with dedup disabled")

	result1, _ := service.CheckAndDedup(ctx, content)
	result2, _ := service.CheckAndDedup(ctx, content)

	// 禁用时两次检查都不应返回重复
	if result1.IsDuplicate || result2.IsDuplicate {
		t.Error("禁用去重时不应返回重复")
	}
}

func TestDedupService_DisableDedup(t *testing.T) {
	store := NewInMemoryDedupStore()
	service := NewService(store, false)
	ctx := context.Background()

	content := []byte("test content")
	result, err := service.CheckAndDedup(ctx, content)
	if err != nil {
		t.Fatalf("CheckAndDedup 失败: %v", err)
	}
	if result.Skipped {
		t.Error("禁用时不应跳过")
	}
}
