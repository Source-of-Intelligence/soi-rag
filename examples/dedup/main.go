package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ragtool/rag/pkg/dedup"
	"github.com/ragtool/rag/pkg/rag"
)

func main() {
	ctx := context.Background()

	// 创建RAG引擎（启用去重）
	config := rag.DefaultConfig()
	config.UseDedup = true // 启用SM3去重

	engine, err := rag.NewEngine(config)
	if err != nil {
		log.Fatalf("创建引擎失败: %v", err)
	}

	fmt.Println("=== SM3去重功能演示 ===")
	fmt.Println()

	// 示例1：添加文档并检查去重
	content1 := `人工智能（Artificial Intelligence，简称AI）是计算机科学的一个分支，
致力于创造能够执行通常需要人类智能的任务的系统。`

	fmt.Println("1. 添加第一个文档")
	doc1, err := engine.AddDocumentFromText(ctx, "AI简介", content1, "example")
	if err != nil {
		log.Printf("添加失败: %v", err)
	} else {
		fmt.Printf("   ✓ 文档已添加: %s\n", doc1.ID[:8])
	}

	// 计算并显示哈希
	hash1 := dedup.CalculateStringHash(content1)
	fmt.Printf("   SM3哈希: %s\n", hash1[:32]+"...")
	fmt.Println()

	// 示例2：尝试添加相同内容的文档
	fmt.Println("2. 尝试添加相同内容的文档（应该被去重跳过）")
	_, err = engine.AddDocumentFromText(ctx, "AI简介（副本）", content1, "example-copy")
	if err != nil {
		log.Printf("添加失败: %v", err)
	} else {
		fmt.Printf("   返回文档: %s (与原文档相同)\n", doc1.ID[:8])
	}
	fmt.Println()

	// 示例3：检查内容是否重复
	fmt.Println("3. 检查内容是否重复")
	dedupResult, err := engine.CheckDuplicate(ctx, content1)
	if err != nil {
		log.Printf("检查失败: %v", err)
	} else {
		fmt.Printf("   是否重复: %v\n", dedupResult.IsDuplicate)
		fmt.Printf("   SM3哈希: %s\n", dedupResult.Hash[:32]+"...")
		if dedupResult.ExistingDoc != nil {
			fmt.Printf("   已存在文档: %s\n", dedupResult.ExistingDoc.Title)
		}
	}
	fmt.Println()

	// 示例4：添加不同内容的文档
	content2 := `机器学习是人工智能的一个子领域，它使计算机系统能够从数据中自动学习和改进。`

	fmt.Println("4. 添加不同内容的文档")
	dedupResult2, err := engine.CheckDuplicate(ctx, content2)
	if err != nil {
		log.Printf("检查失败: %v", err)
	} else {
		fmt.Printf("   是否重复: %v\n", dedupResult2.IsDuplicate)
		fmt.Printf("   SM3哈希: %s\n", dedupResult2.Hash[:32]+"...")
	}

	doc3, err := engine.AddDocumentFromText(ctx, "机器学习", content2, "example")
	if err != nil {
		log.Printf("添加失败: %v", err)
	} else {
		fmt.Printf("   ✓ 文档已添加: %s\n", doc3.ID[:8])
	}
	fmt.Println()

	// 示例5：显示统计信息
	fmt.Println("5. 统计信息")
	stats := engine.GetStats()
	fmt.Printf("   去重启用: %v\n", stats["dedup_enabled"])
	if dedupStats, ok := stats["dedup_stats"].(map[string]interface{}); ok {
		fmt.Printf("   去重统计: %v\n", dedupStats)
	}
	fmt.Println()

	// 示例6：通过哈希获取文档
	fmt.Println("6. 通过SM3哈希获取文档")
	docByHash, err := engine.GetDocumentByHash(ctx, hash1)
	if err != nil {
		log.Printf("获取失败: %v", err)
	} else {
		fmt.Printf("   找到文档: %s\n", docByHash.Title)
	}
	fmt.Println()

	// 示例7：文件哈希计算演示
	fmt.Println("7. SM3哈希计算示例")
	testData := []byte("Hello, SM3 国密算法!")
	hash := dedup.CalculateHash(testData)
	fmt.Printf("   数据: %s\n", string(testData))
	fmt.Printf("   SM3哈希: %s\n", hash)
	fmt.Printf("   哈希长度: %d 字节 (256位)\n", len(hash)/2)
	fmt.Println()

	// 示例8：验证哈希
	fmt.Println("8. 哈希验证")
	isValid := dedup.VerifyHash(testData, hash)
	fmt.Printf("   验证结果: %v\n", isValid)

	// 篡改数据后验证
	tamperedData := []byte("Hello, SM3 国密算法!")
	tamperedData[len(tamperedData)-1] = 'X'
	isValidTampered := dedup.VerifyHash(tamperedData, hash)
	fmt.Printf("   篡改后验证: %v\n", isValidTampered)
	fmt.Println()

	// 示例9：动态启用/禁用去重
	fmt.Println("9. 动态控制去重")
	fmt.Printf("   当前去重状态: %v\n", engine.IsDedupEnabled())
	engine.SetDedupEnabled(false)
	fmt.Printf("   禁用去重后: %v\n", engine.IsDedupEnabled())
	engine.SetDedupEnabled(true)
	fmt.Printf("   重新启用后: %v\n", engine.IsDedupEnabled())
}
