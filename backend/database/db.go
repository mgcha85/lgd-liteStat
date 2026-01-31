package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/marcboeker/go-duckdb"
	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	// Analytics is now a map of Facility -> *sql.DB
	Analytics map[string]*sql.DB
	mu        sync.RWMutex // Protects Analytics map
	BaseDir   string       // Base directory for data (e.g. /app/data)
	App       *sql.DB      // SQLite for job tracking/cache
}

// GetAnalyticsDB returns the DuckDB connection for a specific facility.
// It uses lazy loading: if connection doesn't exist, it attempts to open it.
func (db *DB) GetAnalyticsDB(facility string) (*sql.DB, error) {
	if facility == "" {
		return nil, fmt.Errorf("no facility specified")
	}

	// 1. Fast Path: Check with Read Lock
	db.mu.RLock()
	if conn, ok := db.Analytics[facility]; ok {
		db.mu.RUnlock()
		return conn, nil
	}
	db.mu.RUnlock()

	// 2. Slow Path: Connect with Write Lock
	db.mu.Lock()
	defer db.mu.Unlock()

	// Double check in case another goroutine connected while we waited for lock
	if conn, ok := db.Analytics[facility]; ok {
		return conn, nil
	}

	// Construct Path: /app/data/lake/{facility}.duckdb
	// derived from BaseDir
	targetPath := filepath.Join(db.BaseDir, "lake", fmt.Sprintf("%s.duckdb", facility))

	// Ensure directory exists (parent of target)
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	log.Printf("Facility %s: lazy connecting to DB path %s", facility, targetPath)

	conn, err := sql.Open("duckdb", targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open newly requested DB for %s: %w", facility, err)
	}

	// Set connection settings
	if _, err := conn.Exec("PRAGMA threads=4"); err != nil {
		log.Printf("Warning: Failed to set threads for %s: %v", targetPath, err)
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping newly requested DB for %s: %w", facility, err)
	}

	// EXECUTE SCHEMA (CRITICAL Fix for Lazy Loading)
	// Since we don't pre-load DBs, we must apply schema here.
	schemaContent, err := os.ReadFile("database/schema_duckdb.sql")
	if err != nil {
		// Try fallback relative path?
		// Assuming working directory is app root
		log.Printf("Error: Failed to read schema_duckdb.sql: %v", err)
	} else {
		statements := strings.Split(string(schemaContent), ";")
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := conn.Exec(stmt); err != nil {
				// Don't fail connection, but log error. Schema might be duplicate or invalid.
				// However, missing views is fatal.
				log.Printf("Warning: Schema execution error for %s: %v", facility, err)
			}
		}
		log.Printf("Applied schema to %s", facility)
	}

	// Store in map
	db.Analytics[facility] = conn

	return conn, nil
}

func Initialize(baseDuckPath string, facilities []string, appPath string) (*DB, error) {
	// Flexible Path Handling:
	// If baseDuckPath has an extension (e.g. /app/data/analytics.duckdb), assume it's a file and use its parent dir.
	// If it has no extension (e.g. /app/data), assume it's the base directory.
	baseDir := baseDuckPath
	if filepath.Ext(baseDuckPath) != "" {
		baseDir = filepath.Dir(baseDuckPath)
	}

	// Initialize SQLite (App DB)
	appDB, err := sql.Open("sqlite3", appPath)
	if err != nil {
		return nil, err
	}
	if _, err := appDB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		log.Printf("Warning: Failed to set WAL mode: %v", err)
	}
	if err := appDB.Ping(); err != nil {
		return nil, err
	}

	return &DB{
		Analytics: make(map[string]*sql.DB),
		BaseDir:   baseDir,
		App:       appDB,
	}, nil
}

func (db *DB) Close() {
	db.mu.Lock()
	defer db.mu.Unlock()

	for _, conn := range db.Analytics {
		conn.Close()
	}
	if db.App != nil {
		db.App.Close()
	}
}
