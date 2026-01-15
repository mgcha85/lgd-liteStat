package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

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

	if err := execSQL(r.db.Analytics, "database/schema_duckdb.sql"); err != nil {
		return fmt.Errorf("duckdb schema error: %w", err)
	}

	if err := execSQL(r.db.App, "database/schema_sqlite.sql"); err != nil {
		return fmt.Errorf("sqlite schema error: %w", err)
	}
	return nil
}

// BulkInsertInspection inserts inspection data in bulk
func (r *Repository) BulkInsertInspection(data []InspectionRow) error {
	if len(data) == 0 {
		return nil
	}
	fmt.Printf("Starting BulkInsertInspection with %d rows\n", len(data))

	tx, err := r.db.Analytics.Begin()
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

// BulkInsertHistory inserts history data in bulk
func (r *Repository) BulkInsertHistory(data []HistoryRow) error {
	if len(data) == 0 {
		return nil
	}
	fmt.Printf("Starting BulkInsertHistory with %d rows\n", len(data))

	tx, err := r.db.Analytics.Begin()
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
	_, err = r.db.App.Exec("INSERT OR REPLACE INTO analysis_cache (cache_key, request_params, glass_results, lot_results, daily_results, heatmap_results, metrics, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)", cacheKey, paramsJSON, results.GlassResults, results.LotResults, results.DailyResults, results.HeatmapResults, results.Metrics, expiresAt)
	return err
}
func (r *Repository) GetAnalysisCache(cacheKey string) (*AnalysisResults, error) {
	var results AnalysisResults
	err := r.db.App.QueryRow("SELECT glass_results, lot_results, daily_results, heatmap_results, metrics, created_at FROM analysis_cache WHERE cache_key = ? AND expires_at > CURRENT_TIMESTAMP", cacheKey).Scan(&results.GlassResults, &results.LotResults, &results.DailyResults, &results.HeatmapResults, &results.Metrics, &results.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &results, nil
}
func (r *Repository) CleanupOldData(retentionDays int) (map[string]int64, error) {
	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)
	deleted := make(map[string]int64)
	r.db.Analytics.Exec("DELETE FROM lake_mgr.eas_pnl_ins_def_a WHERE inspection_end_ymdhms < ?", cutoffDate)
	r.db.Analytics.Exec("DELETE FROM lake_mgr.mas_pnl_prod_eqp_h WHERE move_in_ymdhms < ?", cutoffDate)
	r.db.App.Exec("DELETE FROM analysis_cache WHERE expires_at < CURRENT_TIMESTAMP")
	r.db.App.Exec("DELETE FROM analysis_jobs WHERE created_at < ?", time.Now().AddDate(0, 0, -30))
	return deleted, nil
}

// AnalysisLog ... (Logging code unchanged - omitted for brevity) ...
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

func (r *Repository) LogAnalysis(log AnalysisLog) error {
	_, err := r.db.App.Exec("INSERT INTO analysis_logs (defect_name, start_date, end_date, target_count, glass_count, duration_ms, status, request_time) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)", log.DefectName, log.StartDate, log.EndDate, log.TargetCount, log.GlassCount, log.DurationMs, log.Status)
	return err
}
func (r *Repository) GetRecentAnalysisLogs(limit int) ([]AnalysisLog, error) {
	rows, err := r.db.App.Query("SELECT id, request_time, defect_name, start_date, end_date, target_count, glass_count, duration_ms, status FROM analysis_logs ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []AnalysisLog
	for rows.Next() {
		var l AnalysisLog
		if err := rows.Scan(&l.ID, &l.RequestTime, &l.DefectName, &l.StartDate, &l.EndDate, &l.TargetCount, &l.GlassCount, &l.DurationMs, &l.Status); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

// EquipmentRanking represents equipment ranking data
type EquipmentRanking struct {
	Rank              int     `json:"rank"`
	EquipmentID       string  `json:"equipment_id"`
	ProcessCode       string  `json:"process_code"`
	ProductCount      int     `json:"product_count"` // Was GlassCount
	TotalDefects      int     `json:"total_defects"`
	DefectRate        float64 `json:"defect_rate"`
	OverallDefectRate float64 `json:"overall_defect_rate"`
	Delta             float64 `json:"delta"`
}

// GetEquipmentRankings returns top equipment by defect rate
func (r *Repository) GetEquipmentRankings(minProductCount int, defectName string, startDate, endDate string, limit int) ([]EquipmentRanking, error) {
	// Build date filter
	dateFilter := ""
	dateParams := []interface{}{}
	if startDate != "" && endDate != "" {
		dateFilter = "AND h.move_in_ymdhms BETWEEN CAST(? AS TIMESTAMP) AND CAST(? AS TIMESTAMP)"
		dateParams = append(dateParams, startDate, endDate)
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

	// Correct Param Order: Defect (JOIN) -> Date (WHERE) -> MinCount (HAVING)
	params := []interface{}{}
	params = append(params, defectParams...)
	params = append(params, dateParams...)

	// Calculate usage of product_id instead of glass_id
	// Formula: Delta = Others_Avg - Overall_Avg
	// Others_Avg = (Sum_All_Rates - Target_Rate) / (Count - 1)
	// Overall_Avg = Sum_All_Rates / Count
	query := fmt.Sprintf(`
		WITH equipment_stats AS (
			SELECT 
				h.equipment_line_id,
				h.process_code,
				COUNT(DISTINCT h.product_id) as product_count, -- Distinct to handle child equipment duplicates
				COUNT(DISTINCT i.panel_id) as total_defects
			FROM lake_mgr.mas_pnl_prod_eqp_h h
			LEFT JOIN lake_mgr.eas_pnl_ins_def_a i ON h.product_id = i.product_id -- JOIN ON product_id
				AND i.process_code = h.process_code
				%s
			WHERE 1=1
			%s
			GROUP BY h.equipment_line_id, h.process_code
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
			e.equipment_line_id,
			e.process_code,
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
	`, defectFilter, dateFilter, limitClause)

	params = append(params, minProductCount, minProductCount)

	rows, err := r.db.Analytics.Query(query, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to query rankings: %w", err)
	}
	defer rows.Close()

	var rankings []EquipmentRanking
	rank := 1
	for rows.Next() {
		var r EquipmentRanking
		r.Rank = rank
		if err := rows.Scan(
			&r.EquipmentID, &r.ProcessCode, &r.ProductCount,
			&r.DefectRate, &r.OverallDefectRate, &r.Delta,
		); err != nil {
			return nil, err
		}
		// Calculate TotalDefects (Approx)
		r.TotalDefects = int(r.DefectRate * float64(r.ProductCount))

		rankings = append(rankings, r)
		rank++
	}

	return rankings, nil
}
