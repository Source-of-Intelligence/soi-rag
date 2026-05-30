package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ragtool/rag/pkg/api"
	"github.com/ragtool/rag/pkg/config"
	"github.com/ragtool/rag/pkg/rag"
)

func main() {
	var (
		configFile  string
		showVersion bool
	)

	flag.StringVar(&configFile, "config", "config.yaml", "配置文件路径")
	flag.StringVar(&configFile, "c", "config.yaml", "配置文件路径（简写）")
	flag.BoolVar(&showVersion, "version", false, "显示版本信息")
	flag.BoolVar(&showVersion, "v", false, "显示版本信息（简写）")
	flag.Parse()

	if showVersion {
		fmt.Println("RAG Server v1.0.0")
		os.Exit(0)
	}

	// 加载配置
	cfg, err := config.LoadFromFileWithDefaults(configFile)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	log.Printf("配置加载成功: %s", configFile)

	// 验证配置
	if err := cfg.Validate(); err != nil {
		log.Fatalf("配置验证失败: %v", err)
	}

	// 创建 RAG 引擎
	ragConfig := cfg.ToRagConfig()
	engine, err := rag.NewEngine(ragConfig)
	if err != nil {
		log.Fatalf("创建 RAG 引擎失败: %v", err)
	}
	defer engine.Close()

	// 设置 LLM（如果配置了非 mock provider）
	if cfg.LLM.Provider != "" && cfg.LLM.Provider != "mock" {
		llmInstance, err := cfg.ToLLM()
		if err != nil {
			log.Printf("警告: LLM 初始化失败，问答功能将不可用: %v", err)
		} else {
			engine.SetLLM(llmInstance)
			log.Printf("LLM 已加载: provider=%s, model=%s", cfg.LLM.Provider, cfg.LLM.Model)
		}
	}

	// 创建 HTTP 服务器
	apiConfig := &api.Config{
		Host:       cfg.API.Host,
		Port:       cfg.API.Port,
		EnableCORS: true,
	}
	if apiConfig.Host == "" {
		apiConfig.Host = "0.0.0.0"
	}
	if apiConfig.Port == 0 {
		apiConfig.Port = 8080
	}

	server := api.NewServer(engine, apiConfig)

	// 打印启动信息
	printBanner(cfg)

	// 优雅启动（支持 Ctrl+C 优雅关闭）
	if err := server.StartGraceful(); err != nil {
		log.Fatalf("服务器异常退出: %v", err)
	}
}

func printBanner(cfg *config.FileConfig) {
	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║           RAG API Server v1.0.0                    ║")
	fmt.Println("╠══════════════════════════════════════════════════════╣")
	fmt.Printf("║  监听地址: %-40s ║\n", fmt.Sprintf("%s:%d", cfg.API.Host, cfg.API.Port))
	fmt.Printf("║  存储类型: %-40s ║\n", cfg.Storage.Type)
	fmt.Printf("║  LLM:      %-40s ║\n", fmt.Sprintf("%s/%s", cfg.LLM.Provider, cfg.LLM.Model))
	fmt.Println("╠══════════════════════════════════════════════════════╣")
	fmt.Println("║  API 端点:                                         ║")
	fmt.Println("║    POST   /api/v1/documents     添加文档           ║")
	fmt.Println("║    GET    /api/v1/documents     列出文档           ║")
	fmt.Println("║    GET    /api/v1/documents/:id 获取文档           ║")
	fmt.Println("║    DELETE /api/v1/documents/:id 删除文档           ║")
	fmt.Println("║    POST   /api/v1/search        搜索               ║")
	fmt.Println("║    POST   /api/v1/ask           RAG 问答            ║")
	fmt.Println("║    GET    /api/v1/stats         统计信息           ║")
	fmt.Println("║    GET    /api/v1/health        健康检查           ║")
	fmt.Println("╠══════════════════════════════════════════════════════╣")
	fmt.Println("║  按 Ctrl+C 优雅关闭服务                              ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
}
