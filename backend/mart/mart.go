package mart

import (
	"database/sql"
	"fmt"
	"lgd-litestat/database"
	"log"
	"time"
)

// MartBuilder handles glass_stats mart creation and refresh
type MartBuilder struct {
	db *database.DB
}

// MartStats holds statistics about the refreshed mart
type MartStats struct {
	TotalRows          int64
	MinDate            string
	MaxDate            string
	AvgDefectsPerGlass float64
	TotalDefects       int64
	UniqueLots         int64
}

// NewMartBuilder creates a new mart builder
func NewMartBuilder(db *database.DB) *MartBuilder {
	return &MartBuilder{db: db}
}

// Refresh rebuilds the glass_stats materialized view
func (m *MartBuilder) Refresh(facility string) (MartStats, error) {
	start := time.Now()
	stats := MartStats{}

	conn, err := m.db.GetAnalyticsDB(facility)
	if err != nil {
		return MartStats{}, err
	}

	// Begin transaction
	tx, err := conn.Begin()
	if err != nil {
		return MartStats{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r) // re-throw panic
		} else if err != nil {
			tx.Rollback() // err is already set, so we know it failed
		} else {
			err = tx.Commit() // Only commit if no error occurred
		}
	}()

	// Drop and recreate the glass_stats table with fresh data
	// ... (Query unchanged but reformatted/trimmed for tool) ...
	query := `
		CREATE OR REPLACE TABLE glass_stats AS
		WITH glass_defects AS (
			SELECT 
				product_id,
				list_distinct(list(
					(ascii(SUBSTR(panel_addr, 1, 1)) - 65) * 10 + 
					CAST(SUBSTR(panel_addr, 2, 1) AS INTEGER) + 1
				)) as defect_indices
			FROM inspection
			WHERE panel_addr IS NOT NULL AND LENGTH(panel_addr) >= 2
			GROUP BY product_id
		)
		SELECT 
			h.product_id,
			h.lot_id,
			CAST(h.timekey_ymdhms AS DATE) AS work_date,
			COALESCE(SUM(i.defect_count), 0) AS total_defects,
			list_transform(range(1, 261), idx -> 
				CASE WHEN list_contains(COALESCE(d.defect_indices, []), idx) THEN 1 
				ELSE 0 END
			) as panel_map,
			CURRENT_TIMESTAMP AS created_at
		FROM (
			SELECT * FROM (
				SELECT *, 
					ROW_NUMBER() OVER (PARTITION BY product_id ORDER BY timekey_ymdhms DESC) as rn
				FROM history
			) WHERE rn = 1
		) h
		LEFT JOIN inspection i ON h.product_id = i.product_id
		LEFT JOIN glass_defects d ON h.product_id = d.product_id
		GROUP BY h.product_id, h.lot_id, CAST(h.timekey_ymdhms AS DATE), d.defect_indices;
	`

	if _, err := conn.Exec(query); err != nil {
		return MartStats{}, fmt.Errorf("failed to refresh glass_stats: %w", err)
	}

	// Recreate indexes
	indexQueries := []string{
		`CREATE INDEX IF NOT EXISTS idx_glass_stats_lot ON glass_stats(lot_id)`,
		`CREATE INDEX IF NOT EXISTS idx_glass_stats_date ON glass_stats(work_date)`,
		`CREATE INDEX IF NOT EXISTS idx_glass_stats_product ON glass_stats(product_id)`,
	}

	for _, indexQuery := range indexQueries {
		if _, err := conn.Exec(indexQuery); err != nil {
			return MartStats{}, fmt.Errorf("failed to create index: %w", err)
		}
	}

	// Calculate stats for return
	err = conn.QueryRow("SELECT COUNT(*) FROM glass_stats").Scan(&stats.TotalRows)
	if err != nil {
		log.Printf("Warning: Failed to get row count: %v", err)
	}

	var minDate, maxDate sql.NullString
	err = conn.QueryRow("SELECT MIN(work_date), MAX(work_date) FROM glass_stats").Scan(&minDate, &maxDate)
	if err == nil {
		stats.MinDate = minDate.String
		stats.MaxDate = maxDate.String
	}

	err = conn.QueryRow("SELECT AVG(total_defects) FROM glass_stats").Scan(&stats.AvgDefectsPerGlass)
	if err != nil {
		log.Printf("Warning: Failed to get average defects: %v", err)
	}

	err = conn.QueryRow("SELECT SUM(total_defects) FROM glass_stats").Scan(&stats.TotalDefects)
	if err != nil {
		log.Printf("Warning: Failed to get total defects: %v", err)
	}

	// Get unique lots count via join with history
	err = conn.QueryRow(`
		SELECT COUNT(DISTINCT h.lot_id) 
		FROM glass_stats g
		JOIN history h ON g.product_id = h.product_id
	`).Scan(&stats.UniqueLots)
	if err != nil {
		log.Printf("Warning: Failed to get unique lots: %v", err)
	}

	// Commit transaction
	// Transaction commit is handled by defer logic

	duration := time.Since(start)
	log.Printf("✓ Mart refresh completed for %s in %v. Rows: %d", facility, duration, stats.TotalRows)

	return stats, nil
}

// GetMartStats returns statistics about the glass_stats mart
func (m *MartBuilder) GetMartStats(facility string) (map[string]interface{}, error) {
	conn, err := m.db.GetAnalyticsDB(facility)
	if err != nil {
		return nil, err
	}
	stats := make(map[string]interface{})

	// Total rows
	var totalRows int64
	if err := conn.QueryRow(`SELECT COUNT(*) FROM glass_stats`).Scan(&totalRows); err != nil {
		return nil, err
	}
	stats["total_rows"] = totalRows

	// Date range
	var minDate, maxDate sql.NullTime
	err = conn.QueryRow(`
		SELECT MIN(work_date), MAX(work_date) FROM glass_stats
	`).Scan(&minDate, &maxDate)
	if err != nil {
		return nil, err
	}

	if minDate.Valid {
		stats["min_date"] = minDate.Time.Format("2006-01-02")
	}
	if maxDate.Valid {
		stats["max_date"] = maxDate.Time.Format("2006-01-02")
	}

	// Average defects per glass
	var avgDefects float64
	if err := conn.QueryRow(`SELECT AVG(total_defects) FROM glass_stats`).Scan(&avgDefects); err != nil {
		return nil, err
	}
	stats["avg_defects_per_glass"] = avgDefects

	// Total defects
	var totalDefects int64
	if err := conn.QueryRow(`SELECT SUM(total_defects) FROM glass_stats`).Scan(&totalDefects); err != nil {
		return nil, err
	}
	stats["total_defects"] = totalDefects

	// Unique lots
	var uniqueLots int64
	if err := conn.QueryRow(`SELECT COUNT(DISTINCT lot_id) FROM glass_stats`).Scan(&uniqueLots); err != nil {
		return nil, err
	}
	stats["unique_lots"] = uniqueLots

	return stats, nil
}
