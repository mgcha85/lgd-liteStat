package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "github.com/marcboeker/go-duckdb"
	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	// Analytics is now a map of Facility -> *sql.DB
	// If no facility is specified or for backward compatibility, "default" key can be used
	Analytics map[string]*sql.DB
	App       *sql.DB // SQLite for job tracking/cache
}

// GetAnalyticsDB returns the DuckDB connection for a specific facility.
// If facility is empty, it returns the default connection if available.
func (db *DB) GetAnalyticsDB(facility string) (*sql.DB, error) {
	if facility == "" {
		// Fallback to first available or specific default?
		// For now, let's look for "default" or return error
		if conn, ok := db.Analytics["default"]; ok {
			return conn, nil
		}
		// Fallback: return any random one (not ideal) or error
		// Let's assume there's always a "default" based on Initialization
		return nil, fmt.Errorf("no facility specified and no default connection")
	}
	if conn, ok := db.Analytics[facility]; ok {
		return conn, nil
	}
	return nil, fmt.Errorf("connection for facility '%s' not found", facility)
}

func Initialize(baseDuckPath string, facilities []string, appPath string) (*DB, error) {
	analyticsDBs := make(map[string]*sql.DB)

	// Helper to open DuckDB
	openDuck := func(path string) (*sql.DB, error) {
		// Ensure directory exists
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		db, err := sql.Open("duckdb", path)
		if err != nil {
			return nil, err
		}
		if _, err := db.Exec("PRAGMA threads=4"); err != nil {
			log.Printf("Warning: Failed to set threads for %s: %v", path, err)
		}
		if err := db.Ping(); err != nil {
			return nil, err
		}
		return db, nil
	}

	// 1. Initialize Analytics DBs
	// Case A: No facilities configured -> Legacy/Default mode
	if len(facilities) == 0 {
		db, err := openDuck(baseDuckPath)
		if err != nil {
			return nil, err
		}
		analyticsDBs["default"] = db
	} else {
		// Case B: Facilities configured -> data/lake/{facility}.duckdb (Priority)
		// We use baseDir from baseDuckPath (e.g. /app/data)
		baseDir := filepath.Dir(baseDuckPath)

		for _, fac := range facilities {
			// Priority 1: data/lake/{fac}.duckdb (User specified)
			pathLake := filepath.Join(baseDir, "lake", fmt.Sprintf("%s.duckdb", fac))
			// Priority 2: data/{fac}/duck.db (Previous Folder structure)
			pathFolder := filepath.Join(baseDir, fac, "duck.db")

			var targetPath string

			if _, err := os.Stat(pathLake); err == nil {
				targetPath = pathLake
			} else if _, err := os.Stat(pathFolder); err == nil {
				targetPath = pathFolder
			} else {
				// Default to Lake structure
				targetPath = pathLake
			}

			log.Printf("Facility %s: using DB path %s", fac, targetPath)

			db, err := openDuck(targetPath)
			if err != nil {
				// Warn but continue? Or fail? Fail is safer.
				return nil, fmt.Errorf("failed to open DB for facility %s: %w", fac, err)
			}
			analyticsDBs[fac] = db
		}

		// If we want a default fall back to first facility for ease of use?
		if len(analyticsDBs) > 0 {
			analyticsDBs["default"] = analyticsDBs[facilities[0]]
		}
	}

	// 2. Initialize SQLite (App DB)
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
		Analytics: analyticsDBs,
		App:       appDB,
	}, nil
}

func (db *DB) Close() {
	for _, conn := range db.Analytics {
		conn.Close()
	}
	if db.App != nil {
		db.App.Close()
	}
}
