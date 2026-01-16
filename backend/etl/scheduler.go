package etl

import (
	"log"
	"time"

	"lgd-litestat/config"
	"lgd-litestat/database" // Import database
	"lgd-litestat/mart"
)

// Scheduler handles periodic tasks
type Scheduler struct {
	cfg         *config.Config
	ingestor    *DataIngestor
	martBuilder *mart.MartBuilder
	repo        *database.Repository
	ticker      *time.Ticker
	quit        chan struct{}
	lastCleanup time.Time
}

// NewScheduler creates a new scheduler
func NewScheduler(cfg *config.Config, ingestor *DataIngestor, martBuilder *mart.MartBuilder, repo *database.Repository) *Scheduler {
	return &Scheduler{
		cfg:         cfg,
		ingestor:    ingestor,
		martBuilder: martBuilder,
		repo:        repo,
		quit:        make(chan struct{}),
	}
}

// Start begins the scheduling loop
func (s *Scheduler) Start() {
	if !s.cfg.Scheduler.Enabled {
		log.Println("Scheduler is disabled by config.")
		return
	}

	interval := time.Duration(s.cfg.Scheduler.IntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 60 * time.Minute
	}

	log.Printf("Starting Scheduler. Interval: %v (Cleanup at %s)\n", interval, s.cfg.Retention.CleanupTime)
	s.ticker = time.NewTicker(interval)

	go func() {
		// Run once immediately on start?
		// No, let ticker handle it.
		for {
			select {
			case <-s.ticker.C:
				s.RunJob()
			case <-s.quit:
				s.ticker.Stop()
				return
			}
		}
	}()
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	if s.ticker != nil {
		close(s.quit)
	}
}

// RunJob executes the scheduled ingestion and mart refresh
func (s *Scheduler) RunJob() {
	log.Println("[Scheduler] Starting Scheduled Ingestion...")

	// 1. Ingest Data (Incremental)
	counts, err := s.ingestor.IngestData(time.Time{}, time.Time{}, nil)
	if err != nil {
		log.Printf("[Scheduler] Ingestion Failed: %v\n", err)
	} else {
		log.Printf("[Scheduler] Ingestion Complete. Records: %v\n", counts)
	}

	// 2. Refresh Mart
	facilities := s.cfg.Settings.Facilities
	if len(facilities) == 0 {
		facilities = []string{"default"}
	}

	for _, fac := range facilities {
		if _, err := s.martBuilder.Refresh(fac); err != nil {
			log.Printf("[Scheduler] Mart Refresh Failed for %s: %v\n", fac, err)
		}
	}

	// 3. Daily Cleanup
	s.checkAndRunCleanup()

	log.Println("[Scheduler] Job Finished.")
}

func (s *Scheduler) checkAndRunCleanup() {
	cleanupTimeStr := s.cfg.Retention.CleanupTime
	if cleanupTimeStr == "" {
		cleanupTimeStr = "06:00"
	}

	now := time.Now()
	// Parse HH:MM
	target, err := time.Parse("15:04", cleanupTimeStr)
	if err != nil {
		log.Printf("[Scheduler] Invalid cleanup time format: %v", err)
		return
	}

	// Construct today's target time
	cleanupTarget := time.Date(now.Year(), now.Month(), now.Day(), target.Hour(), target.Minute(), 0, 0, now.Location())

	// Condition: Now passed Target AND (LastCleanup was before Target OR LastCleanup is zero)
	// Also ensure we don't run it multiple times the same day?
	// s.lastCleanup.Day() != now.Day() is a good check if we run it daily.
	// But if we restart at 08:00, lastCleanup is 0 again.
	// We accept duplicate runs on restart.

	shouldRun := false
	if now.After(cleanupTarget) {
		if s.lastCleanup.IsZero() {
			shouldRun = true
		} else {
			// If last cleanup was BEFORE today's target (i.e. yesterday), run it.
			if s.lastCleanup.Before(cleanupTarget) {
				shouldRun = true
			}
		}
	}

	if shouldRun {
		log.Println("[Scheduler] Starting Daily Cleanup...")
		if err := s.repo.CleanupOldData(s.cfg.Retention.DataDays, s.cfg.Settings.Facilities); err != nil {
			log.Printf("[Scheduler] Data Cleanup Failed: %v", err)
		}
		if err := s.repo.CleanupOldAnalysis(s.cfg.Retention.AnalysisDays); err != nil {
			log.Printf("[Scheduler] Analysis Cleanup Failed: %v", err)
		}
		s.lastCleanup = now
		log.Println("[Scheduler] Cleanup Completed.")
	}
}
