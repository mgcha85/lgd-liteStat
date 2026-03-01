package database

import (
	"fmt"
	"log"
	"path/filepath"
)

// UpdateMetadata executes the daily metadata update job
// Ports logic from python-scheduler/daily_metadata_job.py
func (r *Repository) UpdateMetadata(facility string, targetDate string) error {
	conn, err := r.db.GetAnalyticsDB(facility)
	if err != nil {
		return fmt.Errorf("failed to get analytics db for %s: %w", facility, err)
	}

	// Paths are relative to DataDir/lake usually
	// But DuckDB read_parquet connects relative to where process runs or absolute paths.
	// In Docker, /app/data/lake is proper.
	// Let's assume db.BaseDir is /app/data.
	// History: /app/data/lake/history
	// Inspection: /app/data/lake/inspection

	lakeDir := filepath.Join(r.db.BaseDir, "lake")
	historyPath := filepath.Join(lakeDir, "history")
	inspectionPath := filepath.Join(lakeDir, "inspection")

	log.Printf("[Metadata] Starting update for %s @ %s", facility, targetDate)

	// 1. Create Tables
	setupSQL := `
		CREATE TABLE IF NOT EXISTS model_master (
			model_code TEXT PRIMARY KEY,
			updated_at TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS defect_master (
			defect_name TEXT PRIMARY KEY,
			updated_at TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS model_layout_master (
			model_code TEXT PRIMARY KEY,
			ref_panels TEXT[],
			updated_at TIMESTAMP
		);
	`
	if _, err := conn.Exec(setupSQL); err != nil {
		return fmt.Errorf("failed to create master tables: %w", err)
	}

	// 2. Update Model Master
	// FROM read_parquet('{history_root}/**/*.parquet', hive_partitioning=true)
	// WHERE facility_code = '{facility}'
	// AND strftime(move_in_ymdhms, '%Y-%m-%d') = '{target_date_str}'
	modelQuery := fmt.Sprintf(`
		INSERT INTO model_master (model_code, updated_at)
		SELECT DISTINCT model_code, now()
		FROM read_parquet('%s/**/*.parquet', hive_partitioning=true)
		WHERE facility_code = ? 
		  AND strftime(move_in_ymdhms, '%%Y-%%m-%%d') = ?
		ON CONFLICT (model_code) DO UPDATE SET updated_at = now()
	`, historyPath)

	if _, err := conn.Exec(modelQuery, facility, targetDate); err != nil {
		log.Printf("[Metadata] Warning: Model Master update failed: %v", err)
		// Don't return error, proceed to next
	} else {
		log.Printf("[Metadata] Model Master updated.")
	}

	// 3. Update Defect Master
	// FROM read_parquet(['{inspection_root}/facility_code={facility}/*/*/inspection_data_*.parquet'], hive_partitioning=true)
	// WHERE strftime(inspection_end_ymdhms, '%Y-%m-%d') = '{target_date_str}'
	inspectionGlob := fmt.Sprintf("%s/facility_code=%s/*/*/inspection_data_*.parquet", inspectionPath, facility)
	defectQuery := fmt.Sprintf(`
		INSERT INTO defect_master (defect_name, updated_at)
		SELECT DISTINCT defect_name, now()
		FROM read_parquet(['%s'], hive_partitioning=true)
		WHERE strftime(inspection_end_ymdhms, '%%Y-%%m-%%d') = ?
		  AND defect_name IS NOT NULL
		ON CONFLICT (defect_name) DO UPDATE SET updated_at = now()
	`, inspectionGlob)

	if _, err := conn.Exec(defectQuery, targetDate); err != nil {
		log.Printf("[Metadata] Warning: Defect Master update failed: %v", err)
	} else {
		log.Printf("[Metadata] Defect Master updated.")
	}

	// 4. Update Model Layout Master
	// Joins 'part_no' and 'pnl_map'
	// Assumes these tables exist (they might be views or actual tables)
	// In Python script it just ran the query.
	layoutQuery := `
		INSERT INTO model_layout_master (model_code, ref_panels, updated_at)
		SELECT 
			pn.model_code,
			ANY_VALUE(pm.ref_panels) as ref_panels,
			now()
		FROM part_no pn
		JOIN pnl_map pm ON pn.part_no_name = pm.part_no_name
		WHERE pm.ref_panels IS NOT NULL
		GROUP BY pn.model_code
		ON CONFLICT (model_code) DO UPDATE SET 
			ref_panels = EXCLUDED.ref_panels,
			updated_at = now()
	`
	if _, err := conn.Exec(layoutQuery); err != nil {
		log.Printf("[Metadata] Warning: Layout Master update failed: %v. Ensure part_no/pnl_map tables exist.", err)
	} else {
		log.Printf("[Metadata] Layout Master updated.")
	}

	return nil
}
