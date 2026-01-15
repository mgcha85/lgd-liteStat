package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// InspectionRow represents a row in the inspection table
type InspectionRow struct {
	GlassID             string    `json:"glass_id"`
	PanelID             string    `json:"panel_id"`
	ProductID           string    `json:"product_id"`
	PanelAddr           string    `json:"panel_addr"`
	TermName            string    `json:"term_name"`
	DefectName          string    `json:"defect_name"`
	InspectionEndYmdhms time.Time `json:"inspection_end_ymdhms"`
	ProcessCode         string    `json:"process_code"`
	DefectCount         int       `json:"defect_count"`
}

// HistoryRow represents a row in the history table
type HistoryRow struct {
	GlassID         string    `json:"glass_id"`
	ProductID       string    `json:"product_id"`
	LotID           string    `json:"lot_id"`
	EquipmentLineID string    `json:"equipment_line_id"`
	ProcessCode     string    `json:"process_code"`
	TimekeyYmdhms   time.Time `json:"timekey_ymdhms"`
	SeqNum          int       `json:"seq_num"`
}

// JobStatus represents analysis job status
type JobStatus struct {
	JobID        string    `json:"job_id"`
	Status       string    `json:"status"` // pending, running, completed, failed
	CacheKey     string    `json:"cache_key,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	Progress     int       `json:"progress"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AnalysisResults holds all analysis result sets
type AnalysisResults struct {
	GlassResults   json.RawMessage `json:"glass_results"`
	LotResults     json.RawMessage `json:"lot_results"`
	DailyResults   json.RawMessage `json:"daily_results"`
	HeatmapResults json.RawMessage `json:"heatmap_results"`
	Metrics        json.RawMessage `json:"metrics"`
	CreatedAt      time.Time       `json:"created_at"`
}

// Repository handles database operations
type Repository struct {
	db *DB
}

func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// CreateSchema creates necessary database tables
func (r *Repository) CreateSchema() error {
	// 1. Setup DuckDB Schema (Analytics)
	duckSchema, err := os.ReadFile("database/schema_duckdb.sql")
	if err != nil {
		return fmt.Errorf("failed to read duckdb schema: %w", err)
	}

	if _, err := r.db.Analytics.Exec(string(duckSchema)); err != nil {
		return fmt.Errorf("failed to create duckdb schema: %w", err)
	}

	// 2. Setup SQLite Schema (App)
	sqliteSchema, err := os.ReadFile("database/schema_sqlite.sql")
	if err != nil {
		return fmt.Errorf("failed to read sqlite schema: %w", err)
	}

	if _, err := r.db.App.Exec(string(sqliteSchema)); err != nil {
		return fmt.Errorf("failed to create sqlite schema: %w", err)
	}

	return nil
}

// BulkInsertInspection inserts inspection data in bulk
func (r *Repository) BulkInsertInspection(data []InspectionRow) error {
	if len(data) == 0 {
		return nil
	}

	tx, err := r.db.Analytics.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO inspection (
			glass_id, panel_id, product_id, panel_addr, term_name, defect_name,
			inspection_end_ymdhms, process_code, defect_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, row := range data {
		_, err := stmt.Exec(row.GlassID, row.PanelID, row.ProductID, row.PanelAddr, row.TermName, row.DefectName, row.InspectionEndYmdhms, row.ProcessCode, row.DefectCount)
		if err != nil {
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// BulkInsertHistory inserts history data in bulk
func (r *Repository) BulkInsertHistory(data []HistoryRow) error {
	if len(data) == 0 {
		return nil
	}

	tx, err := r.db.Analytics.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO history (
			glass_id, product_id, lot_id, equipment_line_id,
			process_code, timekey_ymdhms, seq_num
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, row := range data {
		_, err := stmt.Exec(
			row.GlassID, row.ProductID, row.LotID, row.EquipmentLineID,
			row.ProcessCode, row.TimekeyYmdhms, row.SeqNum,
		)
		if err != nil {
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// CreateAnalysisJob creates a new analysis job
func (r *Repository) CreateAnalysisJob(jobID, status string) error {
	_, err := r.db.App.Exec(`
		INSERT INTO analysis_jobs (job_id, status, created_at, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, jobID, status)
	return err
}

func (r *Repository) UpdateAnalysisJob(jobID, status, cacheKey, errorMsg string, progress int) error {
	_, err := r.db.App.Exec(`
		UPDATE analysis_jobs
		SET status = ?, cache_key = ?, error_message = ?, progress = ?, updated_at = CURRENT_TIMESTAMP
		WHERE job_id = ?
	`, status, cacheKey, errorMsg, progress, jobID)
	return err
}

func (r *Repository) GetAnalysisJobStatus(jobID string) (*JobStatus, error) {
	var job JobStatus
	var cacheKey, errorMsg sql.NullString

	err := r.db.App.QueryRow(`
		SELECT job_id, status, cache_key, error_message, progress, created_at, updated_at
		FROM analysis_jobs
		WHERE job_id = ?
	`, jobID).Scan(
		&job.JobID, &job.Status, &cacheKey, &errorMsg, &job.Progress, &job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if cacheKey.Valid {
		job.CacheKey = cacheKey.String
	}
	if errorMsg.Valid {
		job.ErrorMessage = errorMsg.String
	}

	return &job, nil
}

func (r *Repository) SaveAnalysisCache(cacheKey string, requestParams interface{}, results *AnalysisResults, ttlHours int) error {
	paramsJSON, err := json.Marshal(requestParams)
	if err != nil {
		return fmt.Errorf("failed to marshal request params: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(ttlHours) * time.Hour)

	_, err = r.db.App.Exec(`
		INSERT OR REPLACE INTO analysis_cache (
			cache_key, request_params, glass_results, lot_results,
			daily_results, heatmap_results, metrics, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)
	`,
		cacheKey, paramsJSON, results.GlassResults, results.LotResults,
		results.DailyResults, results.HeatmapResults, results.Metrics,
		expiresAt,
	)
	return err
}

func (r *Repository) GetAnalysisCache(cacheKey string) (*AnalysisResults, error) {
	var results AnalysisResults

	err := r.db.App.QueryRow(`
		SELECT glass_results, lot_results, daily_results, heatmap_results, metrics, created_at
		FROM analysis_cache
		WHERE cache_key = ? AND expires_at > CURRENT_TIMESTAMP
	`, cacheKey).Scan(
		&results.GlassResults, &results.LotResults, &results.DailyResults,
		&results.HeatmapResults, &results.Metrics, &results.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &results, nil
}

func (r *Repository) CleanupOldData(retentionDays int) (map[string]int64, error) {
	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)
	deleted := make(map[string]int64)

	// Clean inspection table
	result, err := r.db.Analytics.Exec(`DELETE FROM inspection WHERE inspection_end_ymdhms < ?`, cutoffDate)
	if err != nil {
		return nil, fmt.Errorf("failed to clean inspection: %w", err)
	}
	count, _ := result.RowsAffected()
	deleted["inspection"] = count

	// Clean history table
	result, err = r.db.Analytics.Exec(`DELETE FROM history WHERE timekey_ymdhms < ?`, cutoffDate)
	if err != nil {
		return nil, fmt.Errorf("failed to clean history: %w", err)
	}
	count, _ = result.RowsAffected()
	deleted["history"] = count

	// Clean glass_stats table
	result, err = r.db.Analytics.Exec(`DELETE FROM glass_stats WHERE work_date < ?`, cutoffDate)
	if err != nil {
		return nil, fmt.Errorf("failed to clean glass_stats: %w", err)
	}
	count, _ = result.RowsAffected()
	deleted["glass_stats"] = count

	// Clean expired cache
	result, err = r.db.App.Exec(`DELETE FROM analysis_cache WHERE expires_at < CURRENT_TIMESTAMP`)
	if err != nil {
		return nil, fmt.Errorf("failed to clean cache: %w", err)
	}
	count, _ = result.RowsAffected()
	deleted["analysis_cache"] = count

	// Clean old jobs (older than 30 days)
	jobCutoff := time.Now().AddDate(0, 0, -30)
	result, err = r.db.App.Exec(`DELETE FROM analysis_jobs WHERE created_at < ?`, jobCutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to clean jobs: %w", err)
	}
	count, _ = result.RowsAffected()
	deleted["analysis_jobs"] = count

	return deleted, nil
}

// AnalysisLog represents a row in the analysis_logs table
type AnalysisLog struct {
	ID          int64     `json:"id"`
	RequestTime time.Time `json:"request_time"`
	DefectName  string    `json:"defect_name"`
	StartDate   string    `json:"start_date"`
	EndDate     string    `json:"end_date"`
	TargetCount int       `json:"target_count"`
	GlassCount  int       `json:"glass_count"`
	DurationMs  int64     `json:"duration_ms"`
	Status      string    `json:"status"`
}

// LogAnalysis records an analysis request
func (r *Repository) LogAnalysis(log AnalysisLog) error {
	_, err := r.db.App.Exec(`
		INSERT INTO analysis_logs (
			defect_name, start_date, end_date, target_count, 
			glass_count, duration_ms, status, request_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, log.DefectName, log.StartDate, log.EndDate, log.TargetCount, log.GlassCount, log.DurationMs, log.Status)
	return err
}

// GetRecentAnalysisLogs retrieves the recent analysis logs
func (r *Repository) GetRecentAnalysisLogs(limit int) ([]AnalysisLog, error) {
	rows, err := r.db.App.Query(`
		SELECT id, request_time, defect_name, start_date, end_date, target_count, glass_count, duration_ms, status
		FROM analysis_logs
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []AnalysisLog
	for rows.Next() {
		var l AnalysisLog
		if err := rows.Scan(
			&l.ID, &l.RequestTime, &l.DefectName, &l.StartDate, &l.EndDate,
			&l.TargetCount, &l.GlassCount, &l.DurationMs, &l.Status,
		); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}
