package main

import (
	"context"
	"flag"
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
	"lgd-litestat/etl"
	"lgd-litestat/jobs"
	"lgd-litestat/mart"
)

func main() {
	mockFlag := flag.Bool("mock", false, "Generate mock data and exit")
	flag.Parse()

	fmt.Println("=== LGD liteStat - Display Manufacturing Data Analysis System ===")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Configuration loaded")

	// Initialize databases
	db, err := database.Initialize(cfg.DBPath, cfg.Settings.Facilities, "./data/app.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	repo := database.NewRepository(db)
	if err := repo.CreateSchema(); err != nil {
		log.Fatalf("Failed to create schema: %v", err)
	}
	log.Println("✓ Database schema created")

	// Create images directory
	if err := os.MkdirAll("/app/data/images", 0755); err != nil {
		log.Printf("Warning: Failed to create images directory: %v", err)
	}

	// Check mock flag
	if *mockFlag {
		if err := etl.RunMockGeneration(repo, cfg); err != nil {
			log.Fatalf("Mock generation failed: %v", err)
		}
		return
	}

	// Initialize worker pool
	workerPool := jobs.NewWorkerPool(cfg.WorkerPoolSize)
	defer workerPool.Stop()
	fmt.Printf("✓ Worker pool started with %d workers\n", cfg.WorkerPoolSize)

	// Initialize mart builder
	martBuilder := mart.NewMartBuilder(db)

	// Initialize analyzer
	analyzer := analysis.NewAnalyzer(db, repo, cfg, workerPool)

	// Initialize Ingestor
	ingestor := etl.NewDataIngestor(cfg, repo)

	// Initialize Scheduler
	scheduler := etl.NewScheduler(cfg, ingestor, martBuilder, repo)
	scheduler.Start()
	defer scheduler.Stop()
	fmt.Println("✓ Scheduler started")

	// Auto-Generate Mock Data on startup if enabled and empty
	if cfg.MockData.Enabled && !*mockFlag {
		log.Println("Checking for existing data...")
		hasData := false
		fac := "default"
		if len(cfg.Settings.Facilities) > 0 {
			fac = cfg.Settings.Facilities[0]
		}

		if count, err := repo.GetHistoryCount(fac); err == nil && count > 0 {
			hasData = true
		}

		if !hasData {
			log.Println("No data found. Generating Mock Data...")
			if err := etl.RunMockGeneration(repo, cfg); err != nil {
				log.Printf("Failed to generate mock data: %v", err)
			} else {
				log.Println("✓ Mock Data Generated")
			}
		}
	}

	// Refresh Data Mart (Equipment Stats)
	log.Println("Building Data Mart (Equipment Rankings)...")
	go func() {
		facilities := cfg.Settings.Facilities
		if len(facilities) == 0 {
			facilities = []string{"default"}
		}

		for _, fac := range facilities {
			if _, err := martBuilder.Refresh(fac); err != nil {
				log.Printf("Failed to refresh mart for %s: %v", fac, err)
			}
		}
		log.Println("✓ Data Mart Ready")
	}()

	// Initialize API handler
	handler := api.NewHandler(db, repo, cfg, martBuilder, analyzer, ingestor)

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
		fmt.Println("  POST /api/ingest (Support JSON body for backfill)")
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
