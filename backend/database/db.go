package database

import (
	"database/sql"
	"log"

	_ "github.com/marcboeker/go-duckdb"
	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	Analytics *sql.DB // DuckDB for heavy OLAP
	App       *sql.DB // SQLite for job tracking/cache
}

func Initialize(duckPath string, appPath string) (*DB, error) {
	// 1. Initialize DuckDB
	duckDB, err := sql.Open("duckdb", duckPath)
	if err != nil {
		return nil, err
	}

	// Set DuckDB pragma for concurrency
	if _, err := duckDB.Exec("PRAGMA threads=4"); err != nil {
		log.Printf("Warning: Failed to set threads: %v", err)
	}

	if err := duckDB.Ping(); err != nil {
		return nil, err
	}

	// 2. Initialize SQLite
	appDB, err := sql.Open("sqlite3", appPath)
	if err != nil {
		return nil, err
	}

	// Set SQLite WAL mode for better concurrency
	if _, err := appDB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		log.Printf("Warning: Failed to set WAL mode: %v", err)
	}

	if err := appDB.Ping(); err != nil {
		return nil, err
	}

	return &DB{
		Analytics: duckDB,
		App:       appDB,
	}, nil
}

func (db *DB) Close() {
	if db.Analytics != nil {
		db.Analytics.Close()
	}
	if db.App != nil {
		db.App.Close()
	}
}
