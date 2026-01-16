package analysis

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"lgd-litestat/config"
	"lgd-litestat/database"
	"lgd-litestat/jobs"

	"strconv"
	"strings"

	"github.com/google/uuid"
)

// AnalysisRequest represents a user's analysis request
type AnalysisRequest struct {
	DefectName   string   `json:"defect_name"`
	StartDate    string   `json:"start_date"`
	EndDate      string   `json:"end_date"`
	ProcessCodes []string `json:"process_codes,omitempty"`
	EquipmentIDs []string `json:"equipment_ids,omitempty"`
	ModelCodes   []string `json:"model_codes,omitempty"`
	FacilityCode string   `json:"facility_code"` // NEW
}

// BatchAnalysisRequest for processing multiple equipments
type BatchAnalysisRequest struct {
	DefectName   string           `json:"defect_name"`
	StartDate    string           `json:"start_date"`
	EndDate      string           `json:"end_date"`
	Targets      []AnalysisTarget `json:"targets"`
	FacilityCode string           `json:"facility_code"` // NEW
}

type AnalysisTarget struct {
	EquipmentID string `json:"equipment_id"`
	ProcessCode string `json:"process_code"`
}

// Internal struct for in-memory processing
type GlassData struct {
	GlassID      string
	LotID        string
	WorkDate     string
	TotalDefects int
	PanelMap     []int64 // Assuming DuckDB returns slice, or we scan string and parse
}

// AnalysisMetrics moved to database

// Analyzer handles analysis execution
type Analyzer struct {
	db         *database.DB
	repo       *database.Repository
	cfg        *config.Config
	workerPool *jobs.WorkerPool
}

func NewAnalyzer(db *database.DB, repo *database.Repository, cfg *config.Config, workerPool *jobs.WorkerPool) *Analyzer {
	return &Analyzer{
		db:         db,
		repo:       repo,
		cfg:        cfg,
		workerPool: workerPool,
	}
}

// RequestAnalysis creates an async analysis job
func (a *Analyzer) RequestAnalysis(req AnalysisRequest) (string, error) {
	// 1. Get cache key
	cacheKey := generateCacheKey(req)

	// 2. Check cache (SQLite) -> Handled by Repo which uses db.App
	cached, err := a.repo.GetAnalysisCache(cacheKey)
	if err == nil && cached != nil {
		// Results exist in cache, create completed job immediately
		jobID := uuid.New().String()
		a.repo.CreateAnalysisJob(jobID, "completed")
		a.repo.UpdateAnalysisJob(jobID, "completed", cacheKey, "", 100)
		return jobID, nil
	}

	// 3. Create job (SQLite) -> Handled by Repo
	jobID := uuid.New().String()
	if err := a.repo.CreateAnalysisJob(jobID, "pending"); err != nil {
		return "", fmt.Errorf("failed to create job: %w", err)
	}

	// 4. Submit to worker pool
	a.workerPool.Submit(jobs.Job{
		ID: jobID,
		Execute: func() error {
			a.executeAnalysis(jobID, cacheKey, req)
			return nil
		},
	})

	return jobID, nil
}

// executeAnalysis runs the actual analysis (called by worker)
func (a *Analyzer) executeAnalysis(jobID, cacheKey string, req AnalysisRequest) error {
	// Update status to running
	if err := a.repo.UpdateAnalysisJob(jobID, "running", "", "", 0); err != nil {
		return err
	}

	// Execute analysis steps
	glassResults, err := a.queryGlassLevel(req)
	if err != nil {
		a.repo.UpdateAnalysisJob(jobID, "failed", "", err.Error(), 0)
		return err
	}
	a.repo.UpdateAnalysisJob(jobID, "running", "", "", 25)

	lotResults, err := a.queryLotLevel(req)
	if err != nil {
		a.repo.UpdateAnalysisJob(jobID, "failed", "", err.Error(), 25)
		return err
	}
	a.repo.UpdateAnalysisJob(jobID, "running", "", "", 50)

	dailyResults, err := a.queryDailyLevel(req)
	if err != nil {
		a.repo.UpdateAnalysisJob(jobID, "failed", "", err.Error(), 50)
		return err
	}
	a.repo.UpdateAnalysisJob(jobID, "running", "", "", 75)

	heatmapResults, err := a.queryHeatmap(req)
	if err != nil {
		a.repo.UpdateAnalysisJob(jobID, "failed", "", err.Error(), 75)
		return err
	}

	metrics := a.calculateMetrics(glassResults)
	fmt.Println("DEBUG: Metrics calculated")

	// Marshal results to JSON
	glassJSON, _ := json.Marshal(glassResults)
	lotJSON, _ := json.Marshal(lotResults)
	dailyJSON, _ := json.Marshal(dailyResults)
	heatmapJSON, _ := json.Marshal(heatmapResults)
	metricsJSON, _ := json.Marshal(metrics)

	results := &database.AnalysisResults{
		GlassResults:   glassJSON,
		LotResults:     lotJSON,
		DailyResults:   dailyJSON,
		HeatmapResults: heatmapJSON,
		Metrics:        metricsJSON,
		CreatedAt:      time.Now(),
	}

	// Save to cache
	fmt.Println("DEBUG: Saving to cache...")
	if err := a.repo.SaveAnalysisCache(cacheKey, req, results, a.cfg.CacheTTLHours); err != nil {
		fmt.Printf("DEBUG: Failed to save cache: %v\n", err)
		return err
	}
	fmt.Println("DEBUG: Cache saved")

	// Update job status
	if err := a.repo.UpdateAnalysisJob(jobID, "completed", cacheKey, "", 100); err != nil {
		fmt.Printf("DEBUG: Failed to update job status: %v\n", err)
		return err
	}
	fmt.Println("DEBUG: Job marked completed")

	return nil
}

// queryGlassLevel executes glass-level query
func (a *Analyzer) queryGlassLevel(req AnalysisRequest) ([]database.GlassResult, error) {
	conn, err := a.db.GetAnalyticsDB(req.FacilityCode)
	if err != nil {
		return nil, err
	}

	// Build query with dynamic aggregation
	query := `
		WITH glass_stats AS (
			SELECT 
				h.product_id,
				h.lot_id,
				strftime(h.move_in_ymdhms, '%Y-%m-%d') as work_date,
				COUNT(i.panel_id) as total_defects
			FROM lake_mgr.mas_pnl_prod_eqp_h h
			LEFT JOIN lake_mgr.eas_pnl_ins_def_a i ON h.product_id = i.product_id
			WHERE h.move_in_ymdhms >= CAST(? AS TIMESTAMP)
			  AND h.move_in_ymdhms <= CAST(? AS TIMESTAMP)
			GROUP BY h.product_id, h.lot_id, h.move_in_ymdhms
		),
		target_glasses AS (
			SELECT DISTINCT product_id
			FROM lake_mgr.mas_pnl_prod_eqp_h 
			WHERE 1=1
	`
	args := []interface{}{req.StartDate, req.EndDate}

	// Add equipment filter if provided
	if len(req.EquipmentIDs) > 0 {
		query += ` AND equipment_line_id IN (`
		for i, eq := range req.EquipmentIDs {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, eq)
		}
		query += ")"
	} else {
		return []database.GlassResult{}, nil
	}

	// Add Model Filter
	if len(req.ModelCodes) > 0 {
		query += ` AND h.product_type_code IN (`
		for i, mc := range req.ModelCodes {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, mc)
		}
		query += ")"
	}

	query += `
		)
		SELECT 
			m.product_id as glass_id,
			m.lot_id,
			CAST(m.work_date AS VARCHAR) as work_date,
			m.total_defects,
			CASE WHEN t.product_id IS NOT NULL THEN 'Target' ELSE 'Others' END AS group_type
		FROM glass_stats m
		LEFT JOIN target_glasses t ON m.product_id = t.product_id
	`
	// Defect filter handled in glass_stats join if needed, or WHERE clause here
	if req.DefectName != "" {
		query += ` WHERE m.product_id IN (
			SELECT DISTINCT product_id FROM lake_mgr.eas_pnl_ins_def_a 
			WHERE defect_name = ?
		)`
		args = append(args, req.DefectName)
	}

	query += ` ORDER BY m.work_date, m.product_id`

	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query glass level: %w", err)
	}
	defer rows.Close()

	var results []database.GlassResult
	for rows.Next() {
		var r database.GlassResult
		if err := rows.Scan(&r.GlassID, &r.LotID, &r.WorkDate, &r.TotalDefects, &r.GroupType); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	return results, nil
}

// queryLotLevel executes lot-level aggregation
func (a *Analyzer) queryLotLevel(req AnalysisRequest) ([]database.LotResult, error) {
	conn, err := a.db.GetAnalyticsDB(req.FacilityCode)
	if err != nil {
		return nil, err
	}
	// Similar dynamic aggregation
	query := `
		WITH glass_stats AS (
			SELECT 
				h.product_id,
				h.lot_id,
				COUNT(i.panel_id) as total_defects
			FROM lake_mgr.mas_pnl_prod_eqp_h h
			LEFT JOIN lake_mgr.eas_pnl_ins_def_a i ON h.product_id = i.product_id
			WHERE h.move_in_ymdhms >= CAST(? AS TIMESTAMP)
			  AND h.move_in_ymdhms <= CAST(? AS TIMESTAMP)
			GROUP BY h.product_id, h.lot_id
		),
		target_glasses AS (
			SELECT DISTINCT product_id
			FROM lake_mgr.mas_pnl_prod_eqp_h
			WHERE 1=1
	`
	args := []interface{}{req.StartDate, req.EndDate}

	if len(req.EquipmentIDs) > 0 {
		query += ` AND equipment_line_id IN (`
		for i, eq := range req.EquipmentIDs {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, eq)
		}
		query += ")"
	} else {
		return []database.LotResult{}, nil
	}

	query += `
		)
		SELECT
			m.lot_id,
			CASE WHEN t.product_id IS NOT NULL THEN 'Target' ELSE 'Others' END AS group_type,
			COUNT(*) as glass_count,
			SUM(m.total_defects) as total_defects,
			AVG(m.total_defects) as avg_defects,
			MAX(m.total_defects) as max_defects
		FROM glass_stats m
		LEFT JOIN target_glasses t ON m.product_id = t.product_id
	`
	if req.DefectName != "" {
		query += ` WHERE m.product_id IN (
			SELECT DISTINCT product_id FROM lake_mgr.eas_pnl_ins_def_a 
			WHERE defect_name = ?
		)`
		args = append(args, req.DefectName)
	}

	query += ` GROUP BY m.lot_id, group_type ORDER BY m.lot_id`

	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query lot level: %w", err)
	}
	defer rows.Close()

	results := []database.LotResult{}
	for rows.Next() {
		var r database.LotResult
		if err := rows.Scan(&r.LotID, &r.GroupType, &r.GlassCount, &r.TotalDefects, &r.AvgDefects, &r.MaxDefects); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// queryDailyLevel retrieves daily defect trends sorted by date
func (a *Analyzer) queryDailyLevel(req AnalysisRequest) ([]database.DailyResult, error) {
	conn, err := a.db.GetAnalyticsDB(req.FacilityCode)
	if err != nil {
		return nil, err
	}
	query := `
		SELECT 
			strftime(h.move_in_ymdhms, '%Y-%m-%d') as work_date,
			CASE WHEN t.product_id IS NOT NULL THEN 'Target' ELSE 'Others' END AS group_type,
			COUNT(DISTINCT i.panel_id) as total_defects,
			COUNT(DISTINCT h.product_id) as glass_count
		FROM lake_mgr.mas_pnl_prod_eqp_h h
		LEFT JOIN lake_mgr.eas_pnl_ins_def_a i ON h.product_id = i.product_id
			AND i.process_code = h.process_code
		LEFT JOIN (SELECT DISTINCT product_id FROM lake_mgr.mas_pnl_prod_eqp_h) t ON h.product_id = t.product_id
		WHERE h.move_in_ymdhms >= CAST(? AS TIMESTAMP)
		  AND h.move_in_ymdhms <= CAST(? AS TIMESTAMP)
	`
	args := []interface{}{req.StartDate, req.EndDate}

	// Filter by Equipment & Process (Multiple)
	if len(req.EquipmentIDs) > 0 {
		placeholders := strings.Repeat("?,", len(req.EquipmentIDs))
		placeholders = strings.TrimSuffix(placeholders, ",")
		query += fmt.Sprintf(" AND h.equipment_line_id IN (%s)", placeholders)
		for _, e := range req.EquipmentIDs {
			args = append(args, e)
		}
	}
	if len(req.ProcessCodes) > 0 {
		placeholders := strings.Repeat("?,", len(req.ProcessCodes))
		placeholders = strings.TrimSuffix(placeholders, ",")
		query += fmt.Sprintf(" AND h.process_code IN (%s)", placeholders)
		for _, p := range req.ProcessCodes {
			args = append(args, p)
		}
	}

	if len(req.ModelCodes) > 0 {
		query += ` AND h.product_type_code IN (`
		for i, mc := range req.ModelCodes {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, mc)
		}
		query += ")"
	}

	if req.DefectName != "" {
		query += ` AND i.defect_name = ?`
		args = append(args, req.DefectName)
	}

	query += ` GROUP BY work_date, group_type ORDER BY work_date ASC`

	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query fail: %w", err)
	}
	defer rows.Close()

	var results []database.DailyResult
	for rows.Next() {
		var r database.DailyResult
		var totalDefects, glassCount int
		// Scan matches SELECT order: work_date, group_type, total_defects, glass_count
		if err := rows.Scan(&r.WorkDate, &r.GroupType, &totalDefects, &glassCount); err != nil {
			return nil, err
		}
		r.TotalDefects = totalDefects
		r.GlassCount = glassCount
		if glassCount > 0 {
			r.AvgDefects = float64(totalDefects) / float64(glassCount)
		}
		results = append(results, r)
	}
	if len(results) > 0 {
		fmt.Printf("DEBUG: DailyResult[0].WorkDate = %s\n", results[0].WorkDate)
	}
	return results, nil
}

// queryHeatmap generates heatmap using Panel X/Y strings
func (a *Analyzer) queryHeatmap(req AnalysisRequest) ([]database.HeatmapCell, error) {
	conn, err := a.db.GetAnalyticsDB(req.FacilityCode)
	if err != nil {
		return nil, err
	}
	// Check for Grid Config (Single Model)
	var gridConfig config.HeatmapGridConfig
	useGrid := false
	if len(req.ModelCodes) == 1 && a.cfg.HeatmapManager != nil {
		if cfg, ok := a.cfg.HeatmapManager.GetConfig(req.ModelCodes[0]); ok {
			gridConfig = cfg
			useGrid = true
		}
	}

	var query string
	var args []interface{}

	if useGrid {
		// CTE Approach for Fixed Grid
		query = `
			WITH x_axis AS (SELECT UNNEST(?::VARCHAR[]) as x),
			     y_axis AS (SELECT UNNEST(?::VARCHAR[]) as y),
				 expected_cells AS (
					SELECT x.x, y.y, 'Target' as group_type FROM x_axis, y_axis
					UNION ALL
					SELECT x.x, y.y, 'Others' as group_type FROM x_axis, y_axis
				 ),
				 actual_data AS (
					SELECT 
						CASE WHEN t.product_id IS NOT NULL THEN 'Target' ELSE 'Others' END AS group_type,
						i.panel_x,
						i.panel_y,
						COUNT(i.panel_id) as total_defects,
						COUNT(DISTINCT h.product_id) as total_glasses
					FROM lake_mgr.mas_pnl_prod_eqp_h h
					JOIN lake_mgr.eas_pnl_ins_def_a i ON h.product_id = i.product_id
						AND i.process_code = h.process_code
					LEFT JOIN (SELECT DISTINCT product_id FROM lake_mgr.mas_pnl_prod_eqp_h) t ON h.product_id = t.product_id
					WHERE h.move_in_ymdhms >= CAST(? AS TIMESTAMP)
					  AND h.move_in_ymdhms <= CAST(? AS TIMESTAMP)
					  AND i.panel_x IS NOT NULL AND i.panel_y IS NOT NULL
		`
		// Args: XList, YList, StartDate, EndDate
		args = append(args, gridConfig.XList, gridConfig.YList, req.StartDate, req.EndDate)

		// Filters
		if len(req.EquipmentIDs) > 0 {
			query += ` AND h.equipment_line_id IN (`
			for i, eq := range req.EquipmentIDs {
				if i > 0 {
					query += ","
				}
				query += "?"
				args = append(args, eq)
			}
			query += ")"
		}
		if len(req.ModelCodes) > 0 {
			query += ` AND h.product_type_code IN (`
			for i, mc := range req.ModelCodes {
				if i > 0 {
					query += ","
				}
				query += "?"
				args = append(args, mc)
			}
			query += ")"
		}
		if req.DefectName != "" {
			query += ` AND i.defect_name = ?`
			args = append(args, req.DefectName)
		}

		query += ` GROUP BY group_type, i.panel_x, i.panel_y )
			SELECT 
				e.group_type,
				e.x,
				e.y,
				COALESCE(a.total_defects, 0) as total_defects,
				COALESCE(a.total_glasses, 0) as total_glasses
			FROM expected_cells e
			LEFT JOIN actual_data a ON e.x = a.panel_x AND e.y = a.panel_y AND e.group_type = a.group_type
			ORDER BY e.group_type, e.x, e.y
		`

	} else {
		// Dynamic Grid Approach (Original)
		query = `
			SELECT 
				CASE WHEN t.product_id IS NOT NULL THEN 'Target' ELSE 'Others' END AS group_type,
				i.panel_x,
				i.panel_y,
				COUNT(i.panel_id) as total_defects,
				COUNT(DISTINCT h.product_id) as total_glasses
			FROM lake_mgr.mas_pnl_prod_eqp_h h
			JOIN lake_mgr.eas_pnl_ins_def_a i ON h.product_id = i.product_id
				AND i.process_code = h.process_code
			LEFT JOIN (SELECT DISTINCT product_id FROM lake_mgr.mas_pnl_prod_eqp_h) t ON h.product_id = t.product_id
			WHERE h.move_in_ymdhms >= CAST(? AS TIMESTAMP)
			  AND h.move_in_ymdhms <= CAST(? AS TIMESTAMP)
			  AND i.panel_x IS NOT NULL AND i.panel_y IS NOT NULL
		`
		args = append(args, req.StartDate, req.EndDate)

		if len(req.EquipmentIDs) > 0 {
			query += ` AND h.equipment_line_id IN (`
			for i, eq := range req.EquipmentIDs {
				if i > 0 {
					query += ","
				}
				query += "?"
				args = append(args, eq)
			}
			query += ")"
		}

		if len(req.ModelCodes) > 0 {
			query += ` AND h.product_type_code IN (`
			for i, mc := range req.ModelCodes {
				if i > 0 {
					query += ","
				}
				query += "?"
				args = append(args, mc)
			}
			query += ")"
		}

		if req.DefectName != "" {
			query += ` AND i.defect_name = ?`
			args = append(args, req.DefectName)
		}

		query += ` GROUP BY group_type, i.panel_x, i.panel_y`
	}

	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query heatmap: %w", err)
	}
	defer rows.Close()

	results := []database.HeatmapCell{}
	for rows.Next() {
		var r database.HeatmapCell
		if err := rows.Scan(&r.GroupType, &r.X, &r.Y, &r.TotalDefects, &r.TotalGlasses); err != nil {
			return nil, err
		}
		if r.TotalGlasses > 0 {
			r.DefectRate = float64(r.TotalDefects) / float64(r.TotalGlasses)
		}
		results = append(results, r)
	}

	return results, nil
}

// AnalyzeBatch performs optimized batch analysis
func (a *Analyzer) AnalyzeBatch(req BatchAnalysisRequest) (map[string]*database.AnalysisResults, error) {
	fmt.Printf("AnalyzeBatch Start: %d targets, Start=%v, End=%v, Defect=%v\n", len(req.Targets), req.StartDate, req.EndDate, req.DefectName)

	conn, err := a.db.GetAnalyticsDB(req.FacilityCode)
	if err != nil {
		return nil, err
	}

	// 1. Fetch ALL Glass Stats for the period (Global Baseline) from lake_mgr
	query := `
		SELECT 
			h.product_id, 
			h.lot_id, 
			CAST(h.move_in_ymdhms AS VARCHAR), 
			COUNT(i.panel_id) as total_defects
		FROM lake_mgr.mas_pnl_prod_eqp_h h
		LEFT JOIN lake_mgr.eas_pnl_ins_def_a i ON h.product_id = i.product_id
		WHERE h.move_in_ymdhms >= CAST(? AS TIMESTAMP)
		  AND h.move_in_ymdhms <= CAST(? AS TIMESTAMP)
		GROUP BY h.product_id, h.lot_id, h.move_in_ymdhms
	`
	rows, err := conn.Query(query, req.StartDate, req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch global stats: %w", err)
	}
	defer rows.Close()

	globalData := make(map[string]GlassData)
	allGlassIDs := []string{}

	for rows.Next() {
		var g GlassData
		if err := rows.Scan(&g.GlassID, &g.LotID, &g.WorkDate, &g.TotalDefects); err != nil {
			return nil, err
		}
		// PanelMap removed as we use direct heatmap query
		globalData[g.GlassID] = g
		allGlassIDs = append(allGlassIDs, g.GlassID)
	}
	fmt.Printf("AnalyzeBatch: Fetched %d global rows from lake_mgr\n", len(allGlassIDs))

	// 2. Apply Defect Filter (if needed)
	// 2. Apply Defect Filter (if needed)
	if req.DefectName != "" {
		conn, err := a.db.GetAnalyticsDB(req.FacilityCode)
		if err != nil {
			return nil, fmt.Errorf("failed to get db: %w", err)
		}
		// Only keep glasses that have this defect
		defectQuery := `SELECT DISTINCT product_id FROM lake_mgr.eas_pnl_ins_def_a WHERE defect_name = ?`
		dRows, err := conn.Query(defectQuery, req.DefectName)
		if err != nil {
			return nil, fmt.Errorf("failed to query defect filter: %w", err)
		}
		defer dRows.Close()

		validGlassSet := make(map[string]bool)
		for dRows.Next() {
			var gid string
			dRows.Scan(&gid)
			validGlassSet[gid] = true
		}

		// Filter globalData
		filteredData := make(map[string]GlassData)
		filteredIDs := []string{}
		for gid, data := range globalData {
			if validGlassSet[gid] {
				filteredData[gid] = data
				filteredIDs = append(filteredIDs, gid)
			}
		}
		globalData = filteredData
		allGlassIDs = filteredIDs
		fmt.Printf("AnalyzeBatch: After Defect Filter (%s), %d rows remain\n", req.DefectName, len(allGlassIDs))
	}

	if len(allGlassIDs) == 0 {
		return map[string]*database.AnalysisResults{}, nil
	}

	// 3. Fetch History for Targets
	placeholders := make([]string, len(req.Targets))
	hArgs := make([]interface{}, len(req.Targets)*2)
	for i, t := range req.Targets {
		placeholders[i] = "(?, ?)"
		hArgs[i*2] = t.EquipmentID
		hArgs[i*2+1] = t.ProcessCode
	}

	hQuery := fmt.Sprintf(`
		SELECT product_id, equipment_line_id 
		FROM lake_mgr.mas_pnl_prod_eqp_h 
		WHERE (equipment_line_id, process_code) IN (%s)
	`, strings.Join(placeholders, ","))

	hRows, err := conn.Query(hQuery, hArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch history: %w", err)
	}
	defer hRows.Close()

	// Map: EquipmentID -> Set<GlassID>
	equipmentGlassMap := make(map[string]map[string]bool)
	for hRows.Next() {
		var gid, eqID string
		hRows.Scan(&gid, &eqID)
		if _, exists := globalData[gid]; exists {
			if _, ok := equipmentGlassMap[eqID]; !ok {
				equipmentGlassMap[eqID] = make(map[string]bool)
			}
			equipmentGlassMap[eqID][gid] = true
		}
	}

	// 3.5 Fetch Heatmaps (Pre-calculation - actually per target logic below, or global precalc?)
	// Since fetchHeatmaps is complex to batch for all targets distinctly if logic varies,
	// we will call queryHeatmap PER target or rely on a global one if targets share criteria.
	// Actually, batch request usually implies specific Equipments. Heatmaps differ per equipment (Target set differs).
	// We will calculate heatmap individually inside the loop or batched if optimized.
	// For now, let's skip pre-fetching and query per target inside the loop (or simple optimization).

	// 4. Compute Results
	results := make(map[string]*database.AnalysisResults)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, target := range req.Targets {
		wg.Add(1)
		go func(t AnalysisTarget) {
			defer wg.Done()

			// Identify Target Set
			targetSet := equipmentGlassMap[t.EquipmentID]

			// Build Result Lists
			var glassRes []database.GlassResult
			var lotResMap = make(map[string]*database.LotResult)
			var dailyResMap = make(map[string]*database.DailyResult)

			var targetDefects, othersDefects, targetCount, othersCount int
			targetSampleCounter := 0
			othersSampleCounter := 0

			totalRows := len(globalData)
			targetStride := 1
			if totalRows > 10000 {
				targetStride = 2
			}
			if totalRows > 100000 {
				targetStride = 10
			}
			othersStride := totalRows / 2000
			if othersStride < 1 {
				othersStride = 1
			}

			for _, g := range globalData {
				isTarget := targetSet[g.GlassID]
				groupType := "Others"
				if isTarget {
					groupType = "Target"
					targetDefects += g.TotalDefects
					targetCount++
					targetSampleCounter++
					if targetSampleCounter%targetStride == 0 {
						glassRes = append(glassRes, database.GlassResult{
							GlassID: g.GlassID, LotID: g.LotID, WorkDate: g.WorkDate, TotalDefects: g.TotalDefects, GroupType: groupType,
						})
					}
				} else {
					othersDefects += g.TotalDefects
					othersCount++
					othersSampleCounter++
					if othersSampleCounter%othersStride == 0 {
						glassRes = append(glassRes, database.GlassResult{
							GlassID: g.GlassID, LotID: g.LotID, WorkDate: g.WorkDate, TotalDefects: g.TotalDefects, GroupType: groupType,
						})
					}
				}

				// Aggregations
				lotKey := g.LotID + "|" + groupType
				if _, ok := lotResMap[lotKey]; !ok {
					lotResMap[lotKey] = &database.LotResult{LotID: g.LotID, GroupType: groupType}
				}
				lotResMap[lotKey].GlassCount++
				lotResMap[lotKey].TotalDefects += g.TotalDefects
				if g.TotalDefects > lotResMap[lotKey].MaxDefects {
					lotResMap[lotKey].MaxDefects = g.TotalDefects
				}

				dailyKey := g.WorkDate + "|" + groupType
				if _, ok := dailyResMap[dailyKey]; !ok {
					dailyResMap[dailyKey] = &database.DailyResult{WorkDate: g.WorkDate, GroupType: groupType}
				}
				dailyResMap[dailyKey].GlassCount++
				dailyResMap[dailyKey].TotalDefects += g.TotalDefects
			}

			// Finalize Aggregates
			lotResults := make([]database.LotResult, 0, len(lotResMap))
			for _, v := range lotResMap {
				v.AvgDefects = float64(v.TotalDefects) / float64(v.GlassCount)
				lotResults = append(lotResults, *v)
			}
			sort.Slice(lotResults, func(i, j int) bool {
				return lotResults[i].LotID < lotResults[j].LotID
			})

			dailyResults := make([]database.DailyResult, 0, len(dailyResMap))
			for _, v := range dailyResMap {
				v.AvgDefects = float64(v.TotalDefects) / float64(v.GlassCount)
				dailyResults = append(dailyResults, *v)
			}
			sort.Slice(dailyResults, func(i, j int) bool {
				return dailyResults[i].WorkDate < dailyResults[j].WorkDate
			})

			// Sort Glass Results
			sort.Slice(glassRes, func(i, j int) bool {
				return glassRes[i].WorkDate < glassRes[j].WorkDate
			})

			// Heatmap (Query Specific to Target)
			// Using queryHeatmap logic but specific to this equipment
			heatmapRes, err := a.queryHeatmap(AnalysisRequest{
				DefectName:   req.DefectName,
				StartDate:    req.StartDate,
				EndDate:      req.EndDate,
				EquipmentIDs: []string{t.EquipmentID}, // Target specific
				// ProcessCode matches target process?
			})
			if err != nil {
				fmt.Printf("Heatmap query failed: %v\n", err)
				heatmapRes = []database.HeatmapCell{}
			}

			// Metrics
			totalDefects := targetDefects + othersDefects
			totalCount := targetCount + othersCount
			var overallRate, targetRate, othersRate float64
			if totalCount > 0 {
				overallRate = float64(totalDefects) / float64(totalCount)
			}
			if targetCount > 0 {
				targetRate = float64(targetDefects) / float64(targetCount)
			}
			if othersCount > 0 {
				othersRate = float64(othersDefects) / float64(othersCount)
			}

			metrics := database.AnalysisMetrics{
				OverallDefectRate:    overallRate,
				TargetDefectRate:     targetRate,
				OthersDefectRate:     othersRate,
				Delta:                overallRate - targetRate,
				SuperiorityIndicator: othersRate - targetRate,
				TargetGlassCount:     targetCount,
				OthersGlassCount:     othersCount,
			}

			// JSON Marshal
			gJSON, _ := json.Marshal(glassRes)
			lJSON, _ := json.Marshal(lotResults)
			dJSON, _ := json.Marshal(dailyResults)
			hJSON, _ := json.Marshal(heatmapRes)
			mJSON, _ := json.Marshal(metrics)

			res := &database.AnalysisResults{
				GlassResults:   gJSON,
				LotResults:     lJSON,
				DailyResults:   dJSON,
				HeatmapResults: hJSON,
				Metrics:        mJSON,
				CreatedAt:      time.Now(),
			}

			mu.Lock()
			results[t.EquipmentID] = res
			mu.Unlock()
		}(target)
	}

	wg.Wait()
	return results, nil
}

// Helper struct for heatmap aggregation
type CellsAggregator struct {
	Counts       []int
	TotalGlasses int
}

func (c *CellsAggregator) Init() {
	// Initialize with size 100 for now (A0-J9) or dynamic?
	// Existing mock data is 100 panels (A-J, 0-9)
	c.Counts = make([]int, 100)
}
func (c *CellsAggregator) Add(panelMap []int64) {
	c.TotalGlasses++
	for i, val := range panelMap {
		if i < len(c.Counts) {
			c.Counts[i] += int(val)
		}
	}
}
func (c *CellsAggregator) ToCells(groupType string) []database.HeatmapCell {
	cells := []database.HeatmapCell{}
	if c.TotalGlasses == 0 {
		return cells
	}
	for i, defects := range c.Counts {
		// x: chr(65 + floor(i/10)), y: i%10
		x := string(rune('A' + (i / 10)))
		y := strconv.Itoa(i % 10)
		rate := float64(defects) / float64(c.TotalGlasses)
		cells = append(cells, database.HeatmapCell{
			GroupType:    groupType,
			X:            x,
			Y:            y,
			TotalDefects: defects,
			TotalGlasses: c.TotalGlasses,
			DefectRate:   rate,
		})
	}
	return cells
}

func parsePanelMap(s string) []int64 {
	// "[1, 0, 1]" -> []int64
	s = strings.Trim(s, "[]")
	if s == "" {
		return []int64{}
	}
	parts := strings.Split(s, ",")
	res := make([]int64, len(parts))
	for i, p := range parts {
		val, _ := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		res[i] = val
	}
	return res
}

func (a *Analyzer) calculateBatchMetrics(glassResults []database.GlassResult) database.AnalysisMetrics {
	// Reuse logic from calculateMetrics but avoid method call overhead if simpler
	return a.calculateMetrics(glassResults)
}

// buildProcessFilter generates SQL condition for process codes
func buildProcessFilter(codes []string) (string, []interface{}) {
	if len(codes) == 0 {
		return "", nil
	}

	// Check for single advanced filter string
	if len(codes) == 1 {
		input := strings.TrimSpace(codes[0])

		// Regex patterns
		// 1. Range: "100-200" or "100..200"
		// 2. Operators: ">100", "<=500"
		// 3. Double inequality: "0<x<6000"

		// Try simple operators
		if strings.HasPrefix(input, ">") || strings.HasPrefix(input, "<") {
			// e.g. "> 100"
			op := ""
			valStr := ""
			if strings.HasPrefix(input, ">=") {
				op = ">="
				valStr = input[2:]
			} else if strings.HasPrefix(input, "<=") {
				op = "<="
				valStr = input[2:]
			} else if strings.HasPrefix(input, ">") {
				op = ">"
				valStr = input[1:]
			} else if strings.HasPrefix(input, "<") {
				op = "<"
				valStr = input[1:]
			} else if strings.HasPrefix(input, "<>") {
				op = "<>"
				valStr = input[2:]
			}

			valStr = strings.TrimSpace(valStr)
			// Try to handle as number
			if _, err := strconv.Atoi(valStr); err == nil {
				// Numeric comparison
				return fmt.Sprintf("TRY_CAST(process_code AS INTEGER) %s ?", op), []interface{}{valStr}
			}
		}

		// Try "A < x < B" pattern
		if strings.Contains(input, "<") && strings.Count(input, "<") == 2 {
			parts := strings.Split(input, "<")
			if len(parts) == 3 {
				minVal := strings.TrimSpace(parts[0])
				maxVal := strings.TrimSpace(parts[2])
				// Assume Numeric
				return "TRY_CAST(process_code AS INTEGER) > ? AND TRY_CAST(process_code AS INTEGER) < ?", []interface{}{minVal, maxVal}
			}
		}

		// Try "-" range: "100-200" (Only if simple numbers)
		if strings.Contains(input, "-") {
			parts := strings.Split(input, "-")
			if len(parts) == 2 {
				minVal := strings.TrimSpace(parts[0])
				maxVal := strings.TrimSpace(parts[1])
				if _, err := strconv.Atoi(minVal); err == nil {
					return "TRY_CAST(process_code AS INTEGER) BETWEEN ? AND ?", []interface{}{minVal, maxVal}
				}
			}
		}
	}

	// Default: Standard IN clause
	placeholders := make([]string, len(codes))
	args := make([]interface{}, len(codes))
	for i, code := range codes {
		placeholders[i] = "?"
		args[i] = code
	}
	return fmt.Sprintf("process_code IN (%s)", strings.Join(placeholders, ",")), args
}

// calculateMetrics computes summary statistics
func (a *Analyzer) calculateMetrics(glassResults []database.GlassResult) database.AnalysisMetrics {
	var targetDefects, othersDefects, targetCount, othersCount int

	for _, r := range glassResults {
		if r.GroupType == "Target" {
			targetDefects += r.TotalDefects
			targetCount++
		} else {
			othersDefects += r.TotalDefects
			othersCount++
		}
	}

	totalDefects := targetDefects + othersDefects
	totalCount := targetCount + othersCount

	var overallRate, targetRate, othersRate float64
	if totalCount > 0 {
		overallRate = float64(totalDefects) / float64(totalCount)
	}
	if targetCount > 0 {
		targetRate = float64(targetDefects) / float64(targetCount)
	}
	if othersCount > 0 {
		othersRate = float64(othersDefects) / float64(othersCount)
	}

	delta := overallRate - targetRate
	superiority := othersRate - targetRate // Positive if target is better

	return database.AnalysisMetrics{
		OverallDefectRate:    overallRate,
		TargetDefectRate:     targetRate,
		OthersDefectRate:     othersRate,
		Delta:                delta,
		SuperiorityIndicator: superiority,
		TargetGlassCount:     targetCount,
		OthersGlassCount:     othersCount,
	}
}

// generateCacheKey creates a unique cache key from request parameters
func generateCacheKey(req AnalysisRequest) string {
	data, _ := json.Marshal(req)
	hash := md5.Sum(data)
	return fmt.Sprintf("%x", hash)
}
