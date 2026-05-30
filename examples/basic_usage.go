package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/ragtool/rag/pkg/rag"
)

func main() {
	ctx := context.Background()

	// 创建RAG引擎
	config := rag.DefaultConfig()
	config.TopK = 5
	config.UseHybrid = true
	config.UseReranker = true

	engine, err := rag.NewEngine(config)
	if err != nil {
		log.Fatalf("Failed to create engine: %v", err)
	}

	// 添加一些示例文档
	docs := []struct {
		title   string
		content string
	}{
		{
			title: "人工智能简介",
			content: `人工智能（Artificial Intelligence，简称AI）是计算机科学的一个分支，致力于创造能够执行通常需要人类智能的任务的系统。
这些任务包括视觉感知、语音识别、决策制定和语言翻译等。AI技术包括机器学习、深度学习、自然语言处理等。`,
		},
		{
			title: "机器学习基础",
			content: `机器学习是人工智能的一个子领域，它使计算机系统能够从数据中自动学习和改进，而无需明确编程。
机器学习算法可以分为监督学习、无监督学习和强化学习三大类。监督学习使用标记数据训练模型，无监督学习发现数据中的隐藏模式，强化学习通过与环境交互来学习最优策略。`,
		},
		{
			title: "深度学习概述",
			content: `深度学习是机器学习的一个分支，基于人工神经网络，特别是深层神经网络。
深度学习在图像识别、语音识别、自然语言处理等领域取得了突破性进展。卷积神经网络（CNN）广泛用于图像处理，循环神经网络（RNN）和Transformer架构用于序列数据处理。`,
		},
		{
			title: "自然语言处理",
			content: `自然语言处理（NLP）是人工智能和语言学领域的交叉学科，关注计算机与人类语言之间的交互。
NLP技术包括文本分类、情感分析、机器翻译、问答系统等。近年来，基于Transformer的大型语言模型如GPT和BERT在NLP任务上取得了显著成果。`,
		},
		{
			title: "RAG技术介绍",
			content: `检索增强生成（Retrieval-Augmented Generation，RAG）是一种结合信息检索和文本生成的技术。
RAG系统首先从知识库中检索相关文档，然后使用生成模型基于检索到的信息生成回答。这种方法能够减少幻觉，提高回答的准确性和可解释性。`,
		},
	}

	fmt.Println("Adding documents...")
	for _, d := range docs {
		_, err := engine.AddDocumentFromText(ctx, d.title, d.content, "example")
		if err != nil {
			log.Printf("Failed to add document %s: %v", d.title, err)
		} else {
			fmt.Printf("✓ Added: %s\n", d.title)
		}
	}

	// 执行不同类型的搜索
	queries := []string{
		"什么是机器学习",
		"深度学习在图像识别中的应用",
		"RAG技术如何工作",
	}

	for _, query := range queries {
		fmt.Printf("\n%s\n", strings.Repeat("=", 80))
		fmt.Printf("Query: %s\n", query)
		fmt.Println(strings.Repeat("=", 80))

		// 混合搜索
		req := &rag.QueryRequest{
			Query:         query,
			TopK:          3,
			RetrievalType: "hybrid",
			UseRerank:     true,
		}

		resp, err := engine.Query(ctx, req)
		if err != nil {
			log.Printf("Search failed: %v", err)
			continue
		}

		fmt.Printf("Found %d results:\n", resp.Total)
		for i, result := range resp.Results {
			fmt.Printf("\n[%d] Score: %.4f\n", i+1, result.Score)
			fmt.Printf("Content: %s\n", truncate(result.Content, 200))
		}
	}

	// 列出所有文档
	fmt.Printf("\n%s\n", strings.Repeat("=", 80))
	fmt.Println("All Documents:")
	fmt.Println(strings.Repeat("=", 80))

	docList, err := engine.ListDocuments(ctx, 0, 100)
	if err != nil {
		log.Printf("Failed to list documents: %v", err)
	} else {
		for _, doc := range docList {
			fmt.Printf("- [%s] %s\n", doc.ID[:8], doc.Title)
		}
	}

	// 获取统计信息
	fmt.Printf("\n%s\n", strings.Repeat("=", 80))
	fmt.Println("Statistics:")
	fmt.Println(strings.Repeat("=", 80))
	stats := engine.GetStats()
	fmt.Printf("%v\n", stats)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
