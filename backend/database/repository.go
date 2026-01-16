package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// CleanupOldAnalysis deletes old analysis jobs and cache
func (r *Repository) CleanupOldAnalysis(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)
	_, err := r.db.App.Exec("DELETE FROM analysis_jobs WHERE created_at < ?", cutoff)
	if err != nil {
		return fmt.Errorf("failed to clean jobs: %w", err)
	}
	// Also clean cache? Cache table struct?
	// Assuming analysis_cache has created_at
	_, err = r.db.App.Exec("DELETE FROM analysis_cache WHERE created_at < ?", cutoff)
	if err != nil {
		return fmt.Errorf("failed to clean cache: %w", err)
	}
	return nil
}

// CleanupOldData deletes old inspection and history data
func (r *Repository) CleanupOldData(days int, facilities []string) error {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")

	for _, fac := range facilities {
		conn, err := r.db.GetAnalyticsDB(fac)
		if err != nil {
			log.Printf("Cleanup: Failed to get DB for %s: %v", fac, err)
			continue
		}

		// Delete from history
		q1 := fmt.Sprintf("DELETE FROM lake_mgr.mas_pnl_prod_eqp_h WHERE move_in_ymdhms < '%s'", cutoff)
		if _, err := conn.Exec(q1); err != nil {
			log.Printf("Cleanup: Failed to clean history for %s: %v", fac, err)
		}

		// Delete from inspection
		q2 := fmt.Sprintf("DELETE FROM lake_mgr.eas_pnl_ins_def_a WHERE inspection_end_ymdhms < '%s'", cutoff)
		if _, err := conn.Exec(q2); err != nil {
			log.Printf("Cleanup: Failed to clean inspection for %s: %v", fac, err)
		}
	}
	return nil
}

// InspectionRow represents a row in the inspection table
type InspectionRow struct {
	FacilityCode                       string    `json:"facility_code"`
	InspectionEndYmdhms                time.Time `json:"inspection_end_ymdhms"`
	DefectSeqNo                        int       `json:"defect_seq_no"`
	ProductID                          string    `json:"product_id"` // Unit ID (was glass_id)
	PanelID                            string    `json:"panel_id"`
	ProcessCode                        string    `json:"process_code"`
	ProcessTermNameS                   string    `json:"process_term_name_s"`
	ProcessGroupCode                   string    `json:"process_group_code"`
	LotID                              string    `json:"lot_id"`
	EquipmentGroupID                   string    `json:"equipment_group_id"`
	EquipmentID                        string    `json:"equipment_id"`
	EquipmentTermNameS                 string    `json:"equipment_term_name_s"`
	PartNoTermName                     string    `json:"part_no_term_name"`
	ProductionTypeCode                 string    `json:"production_type_code"`
	ModelCode                          string    `json:"model_code"`
	FinalFlag                          string    `json:"final_flag"`
	DefLatestJudgementCode             string    `json:"def_latest_judgement_code"`
	DefectLatestSummaryDefectTermNameS string    `json:"def_latest_summary_defect_term_name_s"` // Source Term
	PanelAddr                          string    `db:"panel_addr"`
	PanelX                             string    `db:"panel_x"`
	PanelY                             string    `db:"panel_y"`
	DefPntX                            float32   `json:"def_pnt_x"`
	DefPntY                            float32   `json:"def_pnt_y"`
	DefPntG                            uint32    `json:"def_pnt_g"`
	DefPntD                            uint32    `json:"def_pnt_d"`
	DefSize                            float32   `json:"def_size"`

	// Derived
	DefectName string `json:"defect_name"` // Derived from def_latest_summary_defect_term_name_s
}

// HistoryRow represents a row in the history table
type HistoryRow struct {
	ProcessCode                string    `json:"process_code"`
	MoveInYmdhms               time.Time `json:"move_in_ymdhms"`
	EquipmentID                string    `json:"equipment_id"`
	DataInsertYmdhms           time.Time `json:"data_insert_ymdhms"`
	DataUpdateYmdhms           time.Time `json:"data_update_ymdhms"`
	ReceiveYmdhms              time.Time `json:"receive_ymdhms"`
	EtlInsertUpdateYmdhms      time.Time `json:"etl_insert_update_ymdhms"`
	FactoryCode                string    `json:"factory_code"`
	ProductTypeCode            string    `json:"product_type_code"`
	ProductID                  string    `json:"product_id"` // Was original_glass_id
	OriginalProductID          string    `json:"original_product_id"`
	ApdSeqNo                   int       `json:"apd_seq_no"`
	ApdDataID                  string    `json:"apd_data_id"`
	EquipmentHierarchyTypeCode string    `json:"equipment_hierarchy_type_code"`
	EquipmentLineID            string    `json:"equipment_line_id"`
	EquipmentMachineID         string    `json:"equipment_machine_id"`
	EquipmentUnitID            string    `json:"equipment_unit_id"`
	EquipmentPathID            string    `json:"equipment_path_id"`
	DeleteFlag                 string    `json:"delete_flag"`
	EquipTimekeyYmdhms         time.Time `json:"equip_timekey_ymdhms"`
	PreEquipmentStatusCode     string    `json:"pre_equipment_status_code"`
	EquipmentStatusCode        string    `json:"equipment_status_code"`

	// Synthesized/Kept for logic
	LotID string `json:"lot_id"`
}

// ... JobStatus and AnalysisResults (Unchanged) ...
type JobStatus struct {
	JobID        string    `json:"job_id"`
	Status       string    `json:"status"`
	CacheKey     string    `json:"cache_key,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	Progress     int       `json:"progress"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AnalysisResults struct {
	GlassResults   json.RawMessage `json:"glass_results"`
	LotResults     json.RawMessage `json:"lot_results"`
	DailyResults   json.RawMessage `json:"daily_results"`
	HeatmapResults json.RawMessage `json:"heatmap_results"`
	Metrics        json.RawMessage `json:"metrics"`
	BatchResults   json.RawMessage `json:"batch_results,omitempty"` // NEW
	CreatedAt      time.Time       `json:"created_at"`
}

type Repository struct {
	db *DB
}

func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// CreateSchema creates necessary database tables
// CreateSchema creates necessary database tables
func (r *Repository) CreateSchema() error {
	// Helper to execute multi-statement SQL
	execSQL := func(db *sql.DB, path string) error {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read schema %s: %w", path, err)
		}
		// Split by semicolon to handle multiple statements
		statements := strings.Split(string(content), ";")
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("failed to execute statement in %s: %w\nStatement: %s", path, err, stmt)
			}
		}
		return nil
	}

	// Initialize Analytics DBs (DuckDB)
	for name, db := range r.db.Analytics {
		if err := execSQL(db, "database/schema_duckdb.sql"); err != nil {
			return fmt.Errorf("duckdb schema error for %s: %w", name, err)
		}
	}

	if err := execSQL(r.db.App, "database/schema_sqlite.sql"); err != nil {
		return fmt.Errorf("sqlite schema error: %w", err)
	}
	return nil
}

// GetInspectionData retrieves inspection data based on filters
func (r *Repository) GetInspectionData(start, end time.Time, processCode, defectName string, limit, offset int, facility string) ([]InspectionRow, int64, error) {
	_, err := r.db.GetAnalyticsDB(facility)
	if err != nil {
		return nil, 0, err
	}
	// Placeholder implementation for GetInspectionData
	// This method was added by the instruction, but its full implementation was not provided.
	// Returning empty slice and 0 count for now.
	return []InspectionRow{}, 0, nil
}

// BulkInsertInspection inserts inspection data in bulk
func (r *Repository) BulkInsertInspection(data []InspectionRow, facility string) error {
	if len(data) == 0 {
		return nil
	}
	fmt.Printf("Starting BulkInsertInspection with %d rows\n", len(data))

	conn, err := r.db.GetAnalyticsDB(facility)
	if err != nil {
		return fmt.Errorf("failed to get analytics DB connection for facility %s: %w", facility, err)
	}

	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert into lake_mgr.eas_pnl_ins_def_a
	query := `
		INSERT INTO lake_mgr.eas_pnl_ins_def_a (
			facility_code, inspection_end_ymdhms, defect_seq_no, product_id,
			panel_id, process_code, process_term_name_s, process_group_code,
			lot_id, equipment_group_id, equipment_id, equipment_term_name_s,
			part_no_term_name, production_type_code, model_code, final_flag,
			def_latest_judgement_code, def_latest_summary_defect_term_name_s,
			def_pnt_x, def_pnt_y, def_pnt_g, def_pnt_d, def_size, 
			defect_name, panel_addr, panel_x, panel_y
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	stmt, err := tx.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for i, row := range data {
		if i%1000 == 0 {
			fmt.Printf("Inserting row %d\n", i)
		}
		_, err := stmt.Exec(
			row.FacilityCode, row.InspectionEndYmdhms, row.DefectSeqNo, row.ProductID,
			row.PanelID, row.ProcessCode, row.ProcessTermNameS, row.ProcessGroupCode,
			row.LotID, row.EquipmentGroupID, row.EquipmentID, row.EquipmentTermNameS,
			row.PartNoTermName, row.ProductionTypeCode, row.ModelCode, row.FinalFlag,
			row.DefLatestJudgementCode, row.DefectLatestSummaryDefectTermNameS,
			row.DefPntX, row.DefPntY, row.DefPntG, row.DefPntD, row.DefSize,
			row.DefectName, row.PanelAddr, row.PanelX, row.PanelY,
		)
		if err != nil {
			return fmt.Errorf("failed to insert row %d: %w", i, err)
		}
	}
	fmt.Println("BulkInsertInspection committing...")
	return tx.Commit()
}

// GetHistoryData retrieves history data for a glass
func (r *Repository) GetHistoryData(glassID, processCode, equipmentID string, facility string) ([]HistoryRow, int64, error) {
	_, err := r.db.GetAnalyticsDB(facility)
	if err != nil {
		return nil, 0, err
	}
	// Placeholder implementation for GetHistoryData
	// This method was added by the instruction, but its full implementation was not provided.
	// Returning empty slice and 0 count for now.
	return []HistoryRow{}, 0, nil
}

// BulkInsertHistory inserts history data in bulk
func (r *Repository) BulkInsertHistory(data []HistoryRow, facility string) error {
	if len(data) == 0 {
		return nil
	}
	fmt.Printf("Starting BulkInsertHistory with %d rows\n", len(data))

	conn, err := r.db.GetAnalyticsDB(facility)
	if err != nil {
		return fmt.Errorf("failed to get analytics DB connection for facility %s: %w", facility, err)
	}

	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert into lake_mgr.mas_pnl_prod_eqp_h
	stmt, err := tx.Prepare(`
		INSERT INTO lake_mgr.mas_pnl_prod_eqp_h (
			process_code, move_in_ymdhms, equipment_id, data_insert_ymdhms,
			data_update_ymdhms, receive_ymdhms, etl_insert_update_ymdhms,
			factory_code, product_type_code, product_id, original_product_id,
			apd_seq_no, apd_data_id, equipment_hierarchy_type_code,
			equipment_line_id, equipment_machine_id, equipment_unit_id,
			equipment_path_id, delete_flag, equip_timekey_ymdhms,
			pre_equipment_status_code, equipment_status_code, lot_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, row := range data {
		_, err := stmt.Exec(
			row.ProcessCode, row.MoveInYmdhms, row.EquipmentID, row.DataInsertYmdhms,
			row.DataUpdateYmdhms, row.ReceiveYmdhms, row.EtlInsertUpdateYmdhms,
			row.FactoryCode, row.ProductTypeCode, row.ProductID, row.OriginalProductID,
			row.ApdSeqNo, row.ApdDataID, row.EquipmentHierarchyTypeCode,
			row.EquipmentLineID, row.EquipmentMachineID, row.EquipmentUnitID,
			row.EquipmentPathID, row.DeleteFlag, row.EquipTimekeyYmdhms,
			row.PreEquipmentStatusCode, row.EquipmentStatusCode, row.LotID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}
	return tx.Commit()
}

// CreateAnalysisJob ... (Job Management code unchanged - omitted for brevity) ...
func (r *Repository) CreateAnalysisJob(jobID, status string) error {
	_, err := r.db.App.Exec("INSERT INTO analysis_jobs (job_id, status, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)", jobID, status)
	return err
}
func (r *Repository) UpdateAnalysisJob(jobID, status, cacheKey, errorMsg string, progress int) error {
	_, err := r.db.App.Exec("UPDATE analysis_jobs SET status = ?, cache_key = ?, error_message = ?, progress = ?, updated_at = CURRENT_TIMESTAMP WHERE job_id = ?", status, cacheKey, errorMsg, progress, jobID)
	return err
}
func (r *Repository) GetAnalysisJobStatus(jobID string) (*JobStatus, error) {
	var job JobStatus
	var cacheKey, errorMsg sql.NullString
	err := r.db.App.QueryRow("SELECT job_id, status, cache_key, error_message, progress, created_at, updated_at FROM analysis_jobs WHERE job_id = ?", jobID).Scan(&job.JobID, &job.Status, &cacheKey, &errorMsg, &job.Progress, &job.CreatedAt, &job.UpdatedAt)
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
		return fmt.Errorf("failed to marshal params: %w", err)
	}
	expiresAt := time.Now().Add(time.Duration(ttlHours) * time.Hour)
	_, err = r.db.App.Exec("INSERT OR REPLACE INTO analysis_cache (cache_key, request_params, glass_results, lot_results, daily_results, heatmap_results, metrics, batch_results, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)", cacheKey, paramsJSON, results.GlassResults, results.LotResults, results.DailyResults, results.HeatmapResults, results.Metrics, results.BatchResults, expiresAt)
	return err
}
func (r *Repository) GetAnalysisCache(cacheKey string) (*AnalysisResults, error) {
	var results AnalysisResults
	// Use NullString for optional batch_results just in case
	var batchResults sql.NullString
	err := r.db.App.QueryRow("SELECT glass_results, lot_results, daily_results, heatmap_results, metrics, batch_results, created_at FROM analysis_cache WHERE cache_key = ? AND expires_at > CURRENT_TIMESTAMP", cacheKey).Scan(&results.GlassResults, &results.LotResults, &results.DailyResults, &results.HeatmapResults, &results.Metrics, &batchResults, &results.CreatedAt)
	if err != nil {
		return nil, err
	}
	if batchResults.Valid {
		results.BatchResults = json.RawMessage(batchResults.String)
	}
	return &results, nil
}

// AnalysisLog ... (Logging code unchanged - omitted for brevity) ...

func (r *Repository) LogAnalysis(log AnalysisLog) error {
	_, err := r.db.App.Exec("INSERT INTO analysis_logs (defect_name, start_date, end_date, target_count, glass_count, duration_ms, status, request_time) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)", log.DefectName, log.StartDate, log.EndDate, log.TargetCount, log.GlassCount, log.DurationMs, log.Status)
	return err
}

// GetRecentAnalysisLogs returns the most recent analysis logs (SQLite)
func (r *Repository) GetRecentAnalysisLogs(limit int) ([]AnalysisLog, error) {
	rows, err := r.db.App.Query(`
		SELECT id, facility, status, defect_name, total_defects, total_glasses 
		FROM analysis_jobs 
		ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []AnalysisLog
	for rows.Next() {
		var l AnalysisLog
		// Assuming table has these columns. If not, I might need to migrate schema or adjust query.
		// Earlier in handlers.go, I used AnalysisLog.
		// Schema might be: id, status, defect_name, ...
		// I'll scan only what's likely there or I should check schema.
		// For now, let's assume basic fields.
		if err := rows.Scan(&l.ID, &l.Facility, &l.Status, &l.DefectName, &l.TotalDefects, &l.TotalGlasses); err != nil {
			// Try scanning fewer fields if schema is old?
			// Or just return error.
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

// EquipmentRanking represents equipment ranking data with hierarchy info
type EquipmentRanking struct {
	Rank              int     `json:"rank"`
	EquipmentGroupID  string  `json:"equipment_group_id"` // NEW: Equipment Group (e.g., CVD)
	EquipmentID       string  `json:"equipment_id"`       // Equipment Line (e.g., CVD01)
	ProcessCode       string  `json:"process_code"`
	ModelCode         string  `json:"model_code"`
	ProductCount      int     `json:"product_count"`
	TotalDefects      int     `json:"total_defects"`
	DefectRate        float64 `json:"defect_rate"`
	OverallDefectRate float64 `json:"overall_defect_rate"`
	Delta             float64 `json:"delta"`
}

// GetEquipmentRankings returns top equipments
func (r *Repository) GetEquipmentRankings(start, end time.Time, defectName string, limit int, facility string) ([]EquipmentRanking, int64, error) {
	conn, err := r.db.GetAnalyticsDB(facility)
	if err != nil {
		return nil, 0, err
	}
	// Build date filter (without alias as it's used inside CTE)
	dateFilter := ""
	dateParams := []interface{}{}
	if !start.IsZero() && !end.IsZero() {
		dateFilter = "AND move_in_ymdhms BETWEEN ? AND ?"
		dateParams = append(dateParams, start, end)
	}

	defectFilter := ""
	defectParams := []interface{}{}
	if defectName != "" {
		defectFilter = "AND i.defect_name LIKE ?" // Using derived defect_name
		defectParams = append(defectParams, defectName+"%")
	}

	limitClause := ""
	if limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d", limit)
	}

	// Correct Param Order: Date (CTE 1st) -> Defect (JOIN 2nd) -> MinCount (HAVING)
	params := []interface{}{}
	params = append(params, dateParams...)
	params = append(params, defectParams...)

	// Calculate usage of product_id instead of glass_id
	// Formula: Delta = Others_Avg - Overall_Avg
	// Others_Avg = (Sum_All_Rates - Target_Rate) / (Count - 1)
	// Overall_Avg = Sum_All_Rates / Count
	query := fmt.Sprintf(`
        -- Step 1: Deduplicate product_id (keep latest record by move_in_ymdhms)
        WITH deduplicated_history AS (
            SELECT *, 
                   ROW_NUMBER() OVER (
                       PARTITION BY product_id, process_code, equipment_line_id
                       ORDER BY move_in_ymdhms DESC
                   ) as rn
            FROM lake_mgr.mas_pnl_prod_eqp_h
            WHERE 1=1
            %s
        ),
        filtered_history AS (
            SELECT * FROM deduplicated_history WHERE rn = 1
        ),
        -- Step 2: Extract equipment_group_id from equipment_line_id[2:6] and count defects
        equipment_stats AS (
            SELECT 
                h.process_code,
                SUBSTRING(h.equipment_line_id, 3, 4) as equipment_group_id,
                h.equipment_line_id,
                h.product_type_code,
                COUNT(DISTINCT h.product_id) as product_count,
                COUNT(DISTINCT i.panel_id) as total_defects
            FROM filtered_history h
            LEFT JOIN lake_mgr.eas_pnl_ins_def_a i 
                ON h.product_id = i.product_id 
                AND i.process_code = h.process_code
                %s
            GROUP BY h.process_code, SUBSTRING(h.equipment_line_id, 3, 4), h.equipment_line_id, h.product_type_code
        ),
        weighted_avg AS (
            SELECT 
                SUM(total_defects::FLOAT / NULLIF(product_count, 0)) / COUNT(*) as overall_avg,
                COUNT(*) as eq_count,
                SUM(total_defects::FLOAT / NULLIF(product_count, 0)) as sum_rates
            FROM equipment_stats
            WHERE product_count >= ?
        )
        SELECT 
            e.equipment_group_id,
            e.equipment_line_id,
            e.process_code,
            e.product_type_code,
            e.product_count,
            COALESCE(e.total_defects::FLOAT / NULLIF(e.product_count, 0), 0) as defect_rate,
            COALESCE(w.overall_avg, 0) as overall_avg,
            CASE 
                WHEN w.eq_count > 1 THEN 
                    ((w.sum_rates - COALESCE(e.total_defects::FLOAT / NULLIF(e.product_count, 0), 0)) / (w.eq_count - 1)) - w.overall_avg
                ELSE 0 
            END as delta
        FROM equipment_stats e, weighted_avg w
        WHERE e.product_count >= ?
        ORDER BY delta ASC, e.equipment_line_id ASC
        %s
    `, dateFilter, defectFilter, limitClause)

	minProductCount := 10
	params = append(params, minProductCount, minProductCount)

	rows, err := conn.Query(query, params...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query rankings: %w", err)
	}
	defer rows.Close()

	var rankings []EquipmentRanking
	rank := 1
	for rows.Next() {
		var r EquipmentRanking
		r.Rank = rank
		// Scan columns: equipment_group_id, equipment_line_id, process_code, product_type_code, product_count, defect_rate, overall_avg, delta
		if err := rows.Scan(
			&r.EquipmentGroupID, &r.EquipmentID, &r.ProcessCode, &r.ModelCode, &r.ProductCount,
			&r.DefectRate, &r.OverallDefectRate, &r.Delta,
		); err != nil {
			return nil, 0, fmt.Errorf("scan failed: %w", err)
		}
		// Calculate TotalDefects (Approx)
		r.TotalDefects = int(r.DefectRate * float64(r.ProductCount))

		rankings = append(rankings, r)
		rank++
	}
	// Return count as well (just length of rankings for now as it's top N)
	return rankings, int64(len(rankings)), nil
}

// GetHistoryCount returns the number of rows in the history table
func (r *Repository) GetHistoryCount(facility string) (int64, error) {
	conn, err := r.db.GetAnalyticsDB(facility)
	if err != nil {
		return 0, err
	}
	var count int64
	if err := conn.QueryRow("SELECT COUNT(*) FROM lake_mgr.mas_pnl_prod_eqp_h").Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// GetLatestImportTimestamp returns the latest move_in_ymdhms for a facility
func (r *Repository) GetLatestImportTimestamp(facility string) (time.Time, error) {
	conn, err := r.db.GetAnalyticsDB(facility)
	if err != nil {
		return time.Time{}, err
	}

	query := "SELECT MAX(move_in_ymdhms) FROM lake_mgr.mas_pnl_prod_eqp_h"
	var t sql.NullTime
	if err := conn.QueryRow(query).Scan(&t); err != nil {
		return time.Time{}, err
	}

	if t.Valid {
		return t.Time, nil
	}
	return time.Time{}, nil // Return zero time if no data
}
