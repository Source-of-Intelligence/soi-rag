package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Source-of-Intelligence/soi-rag/pkg/rag"
)

func main() {
	var (
		command       = flag.String("cmd", "", "Command: add, search, list, delete, interactive, stats")
		title         = flag.String("title", "", "Document title")
		content       = flag.String("content", "", "Document content")
		source        = flag.String("source", "", "Document source")
		docID         = flag.String("id", "", "Document ID")
		query         = flag.String("query", "", "Search query")
		topK          = flag.Int("topk", 5, "Number of results")
		retrievalType = flag.String("type", "hybrid", "Retrieval type: vector, keyword, hybrid, graph")
		storageType   = flag.String("storage", "memory", "Storage type: memory, sqlite, postgres")
		sqlitePath    = flag.String("dbpath", "rag.db", "SQLite database path (for storage=sqlite)")
		pgHost        = flag.String("pghost", "localhost", "PostgreSQL host")
		pgPort        = flag.Int("pgport", 5432, "PostgreSQL port")
		pgDBName      = flag.String("pgdb", "", "PostgreSQL database name (required for storage=postgres)")
		pgUser        = flag.String("pguser", "", "PostgreSQL user (required for storage=postgres)")
		pgPassword    = flag.String("pgpass", "", "PostgreSQL password (required for storage=postgres)")
	)
	flag.Parse()

	// 创建RAG引擎
	config := rag.DefaultConfig()
	config.TopK = *topK
	config.UseHybrid = *retrievalType == "hybrid"

	// 根据参数选择存储类型
	switch *storageType {
	case "sqlite":
		config.StorageType = rag.StorageSQLite
		config.SQLitePath = *sqlitePath
	case "postgres":
		if *pgDBName == "" || *pgUser == "" {
			fmt.Fprintf(os.Stderr, "Error: -pgdb and -pguser are required for PostgreSQL storage\n")
			os.Exit(1)
		}
		config.StorageType = rag.StoragePostgres
		config.PostgresConfig = &rag.PostgresConfig{
			Host:     *pgHost,
			Port:     *pgPort,
			DBName:   *pgDBName,
			User:     *pgUser,
			Password: *pgPassword,
		}
	default:
		config.StorageType = rag.StorageMemory
	}

	engine, err := rag.NewEngine(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating engine: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	switch *command {
	case "add":
		if *content == "" {
			fmt.Fprintf(os.Stderr, "Error: content is required\n")
			os.Exit(1)
		}
		if *title == "" {
			*title = "Untitled"
		}
		if *source == "" {
			*source = "cli"
		}

		doc, err := engine.AddDocumentFromText(ctx, *title, *content, *source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding document: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Document added successfully!\n")
		fmt.Printf("ID: %s\n", doc.ID)
		fmt.Printf("Title: %s\n", doc.Title)
		fmt.Printf("Status: %s\n", doc.Status)

	case "search":
		if *query == "" {
			fmt.Fprintf(os.Stderr, "Error: query is required\n")
			os.Exit(1)
		}

		req := &rag.QueryRequest{
			Query:         *query,
			TopK:          *topK,
			RetrievalType: *retrievalType,
			UseRerank:     true,
		}

		start := time.Now()
		resp, err := engine.Query(ctx, req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error searching: %v\n", err)
			os.Exit(1)
		}
		elapsed := time.Since(start)

		fmt.Printf("Query: %s\n", resp.Query)
		fmt.Printf("Results: %d\n", resp.Total)
		fmt.Printf("Time: %v\n", elapsed)
		fmt.Println(strings.Repeat("-", 80))

		for i, result := range resp.Results {
			fmt.Printf("\n[%d] Score: %.4f\n", i+1, result.Score)
			fmt.Printf("Content: %s\n", truncate(result.Content, 200))
			fmt.Printf("Source: %s\n", result.Source)
			fmt.Println(strings.Repeat("-", 80))
		}

	case "list":
		docs, err := engine.ListDocuments(ctx, 0, 100)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing documents: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Documents (%d):\n", len(docs))
		fmt.Println(strings.Repeat("-", 80))
		for _, doc := range docs {
			fmt.Printf("ID: %s\n", doc.ID)
			fmt.Printf("Title: %s\n", doc.Title)
			fmt.Printf("Type: %s\n", doc.DocType)
			fmt.Printf("Status: %s\n", doc.Status)
			fmt.Printf("Created: %s\n", doc.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Println(strings.Repeat("-", 80))
		}

	case "delete":
		if *docID == "" {
			fmt.Fprintf(os.Stderr, "Error: id is required\n")
			os.Exit(1)
		}

		if err := engine.DeleteDocument(ctx, *docID); err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting document: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Document %s deleted successfully!\n", *docID)

	case "interactive", "i":
		runInteractive(engine)

	case "stats":
		stats := engine.GetStats()
		data, _ := json.MarshalIndent(stats, "", "  ")
		fmt.Printf("Statistics:\n%s\n", string(data))

	default:
		fmt.Println("RAG Tool - Retrieval Augmented Generation")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  rag -cmd=add -title=\"Title\" -content=\"Content\" [-source=\"Source\"]")
		fmt.Println("  rag -cmd=search -query=\"Query\" [-topk=5] [-type=hybrid]")
		fmt.Println("  rag -cmd=list")
		fmt.Println("  rag -cmd=delete -id=\"DocumentID\"")
		fmt.Println("  rag -cmd=interactive")
		fmt.Println("  rag -cmd=stats")
		fmt.Println()
		fmt.Println("Storage options:")
		fmt.Println("  -storage=memory              Use in-memory storage (default)")
		fmt.Println("  -storage=sqlite -dbpath=path Use SQLite file storage")
		fmt.Println("  -storage=postgres -pgdb=name -pguser=user [-pgpass=pass] [-pghost=host] [-pgport=port]")
		fmt.Println()
		fmt.Println("Retrieval types:")
		fmt.Println("  vector  - Vector similarity search")
		fmt.Println("  keyword - Keyword/BM25 search")
		fmt.Println("  hybrid  - Combined vector + keyword search (default)")
		fmt.Println("  graph   - Knowledge graph search")
	}
}

func runInteractive(engine *rag.Engine) {
	ctx := context.Background()
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("RAG Interactive Mode")
	fmt.Println("Commands: add, search, list, delete, stats, quit")
	fmt.Println(strings.Repeat("=", 80))

	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := strings.ToLower(parts[0])

		switch cmd {
		case "quit", "exit", "q":
			fmt.Println("Goodbye!")
			return

		case "add":
			fmt.Print("Title: ")
			scanner.Scan()
			title := strings.TrimSpace(scanner.Text())

			fmt.Print("Content: ")
			scanner.Scan()
			content := strings.TrimSpace(scanner.Text())

			if content == "" {
				fmt.Println("Error: content cannot be empty")
				continue
			}

			doc, err := engine.AddDocumentFromText(ctx, title, content, "interactive")
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			fmt.Printf("Document added! ID: %s\n", doc.ID)

		case "search":
			if len(parts) < 2 {
				fmt.Print("Query: ")
				scanner.Scan()
				parts = append(parts, scanner.Text())
			}

			query := strings.Join(parts[1:], " ")
			req := &rag.QueryRequest{
				Query:         query,
				TopK:          5,
				RetrievalType: "hybrid",
				UseRerank:     true,
			}

			start := time.Now()
			resp, err := engine.Query(ctx, req)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			elapsed := time.Since(start)

			fmt.Printf("\nFound %d results in %v\n", resp.Total, elapsed)
			fmt.Println(strings.Repeat("-", 80))

			for i, result := range resp.Results {
				fmt.Printf("\n[%d] Score: %.4f\n", i+1, result.Score)
				fmt.Printf("%s\n", truncate(result.Content, 300))
				fmt.Println(strings.Repeat("-", 80))
			}

		case "list":
			docs, err := engine.ListDocuments(ctx, 0, 20)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			fmt.Printf("\nDocuments (%d):\n", len(docs))
			fmt.Println(strings.Repeat("-", 80))
			for _, doc := range docs {
				fmt.Printf("[%s] %s (%s)\n", doc.ID[:8], doc.Title, doc.Status)
			}

		case "delete":
			if len(parts) < 2 {
				fmt.Print("Document ID: ")
				scanner.Scan()
				parts = append(parts, scanner.Text())
			}

			docID := parts[1]
			if err := engine.DeleteDocument(ctx, docID); err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			fmt.Printf("Document %s deleted\n", docID)

		case "stats":
			stats := engine.GetStats()
			data, _ := json.MarshalIndent(stats, "", "  ")
			fmt.Printf("Statistics:\n%s\n", string(data))

		case "help", "h":
			fmt.Println("Commands:")
			fmt.Println("  add              - Add a new document")
			fmt.Println("  search <query>   - Search documents")
			fmt.Println("  list             - List all documents")
			fmt.Println("  delete <id>      - Delete a document")
			fmt.Println("  stats            - Show statistics")
			fmt.Println("  quit             - Exit interactive mode")

		default:
			fmt.Printf("Unknown command: %s\n", cmd)
			fmt.Println("Type 'help' for available commands")
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
