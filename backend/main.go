package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lgd-litestat/analysis"
	"lgd-litestat/api"
	"lgd-litestat/config"
	"lgd-litestat/database"
	"lgd-litestat/jobs"
	"lgd-litestat/mart"
)

func main() {
	fmt.Println("=== LGD liteStat - Display Manufacturing Data Analysis System ===")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Configuration loaded")

	// Initialize databases
	db, err := database.Initialize(cfg.DBPath, "./data/app.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	repo := database.NewRepository(db)
	if err := repo.CreateSchema(); err != nil {
		log.Fatalf("Failed to create schema: %v", err)
	}
	log.Println("✓ Database schema created")

	// Initialize worker pool
	workerPool := jobs.NewWorkerPool(cfg.WorkerPoolSize)
	defer workerPool.Stop()
	fmt.Printf("✓ Worker pool started with %d workers\n", cfg.WorkerPoolSize)

	// Initialize mart builder
	martBuilder := mart.NewMartBuilder(db)

	// Initialize analyzer
	analyzer := analysis.NewAnalyzer(db, repo, cfg, workerPool)

	// Initialize API handler
	handler := api.NewHandler(db, repo, cfg, martBuilder, analyzer)

	// Setup router
	router := api.SetupRouter(handler)
	router.Use(api.CORSMiddleware())
	router.Use(api.LoggingMiddleware())

	// Create HTTP server
	addr := fmt.Sprintf("%s:%s", cfg.APIHost, cfg.APIPort)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		fmt.Printf("✓ API server listening on %s\n", addr)
		fmt.Println("\nAPI Endpoints:")
		fmt.Println("  GET  /health")
		fmt.Println("  POST /api/ingest")
		fmt.Println("  POST /api/mart/refresh")
		fmt.Println("  POST /api/cleanup")
		fmt.Println("  POST /api/analyze")
		fmt.Println("  GET  /api/analyze/{jobId}/status")
		fmt.Println("  GET  /api/analyze/{jobId}/results")
		fmt.Println("  GET  /api/equipment/rankings")
		fmt.Println("\nPress Ctrl+C to shutdown")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		fmt.Printf("Server forced to shutdown: %v\n", err)
	}

	fmt.Println("Server exited")
}
