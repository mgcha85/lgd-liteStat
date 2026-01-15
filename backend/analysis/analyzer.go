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
}

// BatchAnalysisRequest for processing multiple equipments
type BatchAnalysisRequest struct {
	DefectName string           `json:"defect_name"`
	StartDate  string           `json:"start_date"`
	EndDate    string           `json:"end_date"`
	Targets    []AnalysisTarget `json:"targets"`
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

// GlassResult represents glass-level analysis result
type GlassResult struct {
	GlassID      string `json:"glass_id"`
	LotID        string `json:"lot_id"`
	WorkDate     string `json:"work_date"`
	TotalDefects int    `json:"total_defects"`
	GroupType    string `json:"group_type"` // "Target" or "Others"
}

// LotResult represents lot-level aggregation
type LotResult struct {
	LotID        string  `json:"lot_id"`
	GroupType    string  `json:"group_type"`
	GlassCount   int     `json:"glass_count"`
	TotalDefects int     `json:"total_defects"`
	AvgDefects   float64 `json:"avg_defects"`
	MaxDefects   int     `json:"max_defects"`
}

// DailyResult represents daily time series
type DailyResult struct {
	WorkDate     string  `json:"work_date"`
	GroupType    string  `json:"group_type"`
	GlassCount   int     `json:"glass_count"`
	TotalDefects int     `json:"total_defects"`
	AvgDefects   float64 `json:"avg_defects"`
}

// HeatmapCell represents a cell in the heatmap
type HeatmapCell struct {
	GroupType    string  `json:"group_type"` // "Target" or "Others"
	X            string  `json:"x"`
	Y            string  `json:"y"`
	DefectRate   float64 `json:"defect_rate"`
	TotalDefects int     `json:"total_defects"`
	TotalGlasses int     `json:"total_glasses"`
}

// AnalysisMetrics contains summary statistics
type AnalysisMetrics struct {
	OverallDefectRate    float64 `json:"overall_defect_rate"`
	TargetDefectRate     float64 `json:"target_defect_rate"`
	OthersDefectRate     float64 `json:"others_defect_rate"`
	Delta                float64 `json:"delta"`                 // overall - target
	SuperiorityIndicator float64 `json:"superiority_indicator"` // positive if target < others
	TargetGlassCount     int     `json:"target_glass_count"`
	OthersGlassCount     int     `json:"others_glass_count"`
}

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
func (a *Analyzer) queryGlassLevel(req AnalysisRequest) ([]GlassResult, error) {
	// Build query with target classification
	query := `
		WITH target_glasses AS (
			SELECT DISTINCT glass_id 
			FROM history 
			WHERE 1=1
	`

	args := []interface{}{}

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
		// If no equipment specified, return empty results
		return []GlassResult{}, nil
	}

	query += `
		)
		SELECT 
			m.glass_id,
			m.lot_id,
			CAST(m.work_date AS VARCHAR) as work_date,
			m.total_defects,
			CASE WHEN t.glass_id IS NOT NULL THEN 'Target' ELSE 'Others' END AS group_type
		FROM glass_stats m
		LEFT JOIN target_glasses t ON m.glass_id = t.glass_id
		WHERE m.work_date >= CAST(? AS DATE)
		  AND m.work_date <= CAST(? AS DATE)
	`

	args = append(args, req.StartDate, req.EndDate)

	// Add defect filter (via join with inspection table)
	if req.DefectName != "" {
		query += ` AND m.glass_id IN (
			SELECT DISTINCT glass_id FROM inspection WHERE term_name = ?
		)`
		args = append(args, req.DefectName)
	}

	// Add process filter
	if pFilter, pArgs := buildProcessFilter(req.ProcessCodes); pFilter != "" {
		query += fmt.Sprintf(" AND m.glass_id IN (SELECT DISTINCT glass_id FROM history WHERE %s)", pFilter)
		args = append(args, pArgs...)
	}

	query += ` ORDER BY m.work_date, m.glass_id`

	rows, err := a.db.Analytics.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query glass level: %w", err)
	}
	defer rows.Close()

	results := []GlassResult{}
	for rows.Next() {
		var r GlassResult
		if err := rows.Scan(&r.GlassID, &r.LotID, &r.WorkDate, &r.TotalDefects, &r.GroupType); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	return results, nil
}

// queryLotLevel executes lot-level aggregation
func (a *Analyzer) queryLotLevel(req AnalysisRequest) ([]LotResult, error) {
	// Similar to glass-level but grouped by lot
	query := `
		WITH target_glasses AS (
			SELECT DISTINCT glass_id 
			FROM history 
			WHERE 1=1
	`

	args := []interface{}{}

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
		return []LotResult{}, nil
	}

	query += `
		)
		SELECT
			m.lot_id,
			CASE WHEN t.glass_id IS NOT NULL THEN 'Target' ELSE 'Others' END AS group_type,
			COUNT(*) as glass_count,
			SUM(m.total_defects) as total_defects,
			AVG(m.total_defects) as avg_defects,
			MAX(m.total_defects) as max_defects
		FROM glass_stats m
		LEFT JOIN target_glasses t ON m.glass_id = t.glass_id
		WHERE m.work_date >= CAST(? AS DATE)
		  AND m.work_date <= CAST(? AS DATE)
	`

	args = append(args, req.StartDate, req.EndDate)

	if req.DefectName != "" {
		query += ` AND m.glass_id IN (
			SELECT DISTINCT glass_id FROM inspection WHERE term_name = ?
		)`
		args = append(args, req.DefectName)
	}

	// Add process filter
	if pFilter, pArgs := buildProcessFilter(req.ProcessCodes); pFilter != "" {
		query += fmt.Sprintf(" AND m.glass_id IN (SELECT DISTINCT glass_id FROM history WHERE %s)", pFilter)
		args = append(args, pArgs...)
	}

	query += ` GROUP BY m.lot_id, group_type ORDER BY m.lot_id`

	rows, err := a.db.Analytics.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query lot level: %w", err)
	}
	defer rows.Close()

	results := []LotResult{}
	for rows.Next() {
		var r LotResult
		if err := rows.Scan(&r.LotID, &r.GroupType, &r.GlassCount, &r.TotalDefects, &r.AvgDefects, &r.MaxDefects); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	return results, nil
}

// queryDailyLevel executes daily time series
func (a *Analyzer) queryDailyLevel(req AnalysisRequest) ([]DailyResult, error) {
	query := `
		WITH target_glasses AS (
			SELECT DISTINCT glass_id 
			FROM history 
			WHERE 1=1
	`

	args := []interface{}{}

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
		return []DailyResult{}, nil
	}

	query += `
		)
		SELECT 
			CAST(m.work_date AS VARCHAR) as work_date,
			CASE WHEN t.glass_id IS NOT NULL THEN 'Target' ELSE 'Others' END AS group_type,
			COUNT(*) as glass_count,
			SUM(m.total_defects) as total_defects,
			AVG(m.total_defects) as avg_defects
		FROM glass_stats m
		LEFT JOIN target_glasses t ON m.glass_id = t.glass_id
		WHERE m.work_date >= CAST(? AS DATE)
		  AND m.work_date <= CAST(? AS DATE)
	`

	args = append(args, req.StartDate, req.EndDate)

	if req.DefectName != "" {
		query += ` AND m.glass_id IN (
			SELECT DISTINCT glass_id FROM inspection WHERE term_name = ?
		)`
		args = append(args, req.DefectName)
	}

	// Add process filter
	if pFilter, pArgs := buildProcessFilter(req.ProcessCodes); pFilter != "" {
		query += fmt.Sprintf(" AND m.glass_id IN (SELECT DISTINCT glass_id FROM history WHERE %s)", pFilter)
		args = append(args, pArgs...)
	}

	query += ` GROUP BY m.work_date, group_type ORDER BY m.work_date`

	rows, err := a.db.Analytics.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query daily level: %w", err)
	}
	defer rows.Close()

	results := []DailyResult{}
	for rows.Next() {
		var r DailyResult
		if err := rows.Scan(&r.WorkDate, &r.GroupType, &r.GlassCount, &r.TotalDefects, &r.AvgDefects); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	return results, nil
}

// queryHeatmap generates panel position heatmap using Prism (UNNEST) architecture
func (a *Analyzer) queryHeatmap(req AnalysisRequest) ([]HeatmapCell, error) {
	// Build query using CTE and UNNEST
	query := `
		WITH target_glasses AS (
			SELECT DISTINCT glass_id 
			FROM history 
			WHERE 1=1
	`
	args := []interface{}{}

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
		// Optimization: If no equipment specified, heatmap might be huge?
		// But let's allow it if user wants full factory view?
		// Actually, req.EquipmentIDs is usually populated for "Analyze Equipment".
		// If empty, it's global.
	}

	query += `
		),
		glass_data AS (
			SELECT 
				m.panel_map,
				CASE WHEN t.glass_id IS NOT NULL THEN 'Target' ELSE 'Others' END AS group_type
			FROM glass_stats m
			LEFT JOIN target_glasses t ON m.glass_id = t.glass_id
			WHERE m.work_date >= CAST(? AS DATE)
			  AND m.work_date <= CAST(? AS DATE)
	`
	args = append(args, req.StartDate, req.EndDate)

	// Add defect filter
	if req.DefectName != "" {
		// Note: As discussed, this heatmap shows "Any Defect" density
		// but filtered to glasses that HAVE the specific defect?
		// No, usually heatmap shows distribution of THAT defect.
		// But our panel_map conflates defects.
		// However, we MUST filter by defect if requested, consistency with other charts.
		query += ` AND m.glass_id IN (
			SELECT DISTINCT glass_id FROM inspection WHERE term_name = ?
		)`
		args = append(args, req.DefectName)
	}

	// Add process filter (to glass_data)
	if pFilter, pArgs := buildProcessFilter(req.ProcessCodes); pFilter != "" {
		// process_code is in history, not glass_stats directly.
		// But we can filter by joining history or IN clause.
		// Since we want process_specific heatmap, we should filter glasses that went through that process.
		query += fmt.Sprintf(" AND m.glass_id IN (SELECT DISTINCT glass_id FROM history WHERE %s)", pFilter)
		args = append(args, pArgs...)
	}

	query += `
		),
		unnested AS (
			SELECT 
				group_type,
				UNNEST(panel_map) as defect_yn, 
				generate_subscripts(panel_map, 1) as idx 
			FROM glass_data
		)
		SELECT 
			group_type,
			chr(65 + cast(floor((idx-1)/10) as int)) as x,
			CAST(cast((idx-1)%10 as int) AS VARCHAR) as y,
			CAST(SUM(defect_yn) AS FLOAT) / NULLIF(COUNT(*), 0) as defect_rate,
			COALESCE(SUM(defect_yn), 0) as total_defects,
			COUNT(*) as total_glasses
		FROM unnested
		GROUP BY group_type, idx
		ORDER BY group_type, idx
	`

	// Execute query
	rows, err := a.db.Analytics.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query heatmap: %w", err)
	}
	defer rows.Close()

	results := []HeatmapCell{}
	for rows.Next() {
		var r HeatmapCell
		if err := rows.Scan(&r.GroupType, &r.X, &r.Y, &r.DefectRate, &r.TotalDefects, &r.TotalGlasses); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	return results, nil
}

// calculateMetrics computes summary statistics
func (a *Analyzer) calculateMetrics(glassResults []GlassResult) AnalysisMetrics {
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

	return AnalysisMetrics{
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

// AnalyzeBatch performs optimized batch analysis
func (a *Analyzer) AnalyzeBatch(req BatchAnalysisRequest) (map[string]*database.AnalysisResults, error) {
	fmt.Printf("AnalyzeBatch Start: %d targets, Start=%v, End=%v, Defect=%v\n", len(req.Targets), req.StartDate, req.EndDate, req.DefectName)
	// 1. Fetch ALL Glass Stats for the period (Global Baseline)
	// We scan PanelMap as string and parse it, because generic sql driver handling of arrays varies
	query := `
		SELECT glass_id, lot_id, CAST(work_date AS VARCHAR), total_defects, CAST(panel_map AS VARCHAR)
		FROM glass_stats
		WHERE work_date >= CAST(? AS DATE) AND work_date <= CAST(? AS DATE)
	`
	rows, err := a.db.Analytics.Query(query, req.StartDate, req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch global stats: %w", err)
	}
	defer rows.Close()

	globalData := make(map[string]GlassData)
	allGlassIDs := []string{}

	for rows.Next() {
		var g GlassData
		var panelMapStr string // We'll parse DuckDB list string representation
		if err := rows.Scan(&g.GlassID, &g.LotID, &g.WorkDate, &g.TotalDefects, &panelMapStr); err != nil {
			return nil, err
		}
		// DuckDB message(list) usually returns "[1, 0, ...]"
		g.PanelMap = parsePanelMap(panelMapStr)
		globalData[g.GlassID] = g
		allGlassIDs = append(allGlassIDs, g.GlassID)
	}
	fmt.Printf("AnalyzeBatch: Fetched %d global rows from glass_stats\n", len(allGlassIDs))

	// 2. Apply Defect Filter (if needed)
	// If DefectName provided, we only keep glasses that have this defect?
	// Consistent with previous logic: "m.glass_id IN (SELECT ... WHERE term_name = ?)"
	// So we filter the globalData map reduced to only those glasses.
	if req.DefectName != "" {
		defectQuery := `SELECT DISTINCT glass_id FROM inspection WHERE term_name = ?`
		dRows, err := a.db.Analytics.Query(defectQuery, req.DefectName)
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
	// We need to know which glasses belong to which equipment
	targetEquipments := []string{}
	for _, t := range req.Targets {
		targetEquipments = append(targetEquipments, t.EquipmentID)
	}

	// Chunk the query if too many equipments? 20 is fine.
	placeholders := make([]string, len(targetEquipments))
	args := make([]interface{}, len(targetEquipments))
	for i, eq := range targetEquipments {
		placeholders[i] = "?"
		args[i] = eq
	}

	// We also need to map history to process_code if strictly required,
	// but the user's "dashboard" usually implies specific process.
	// We select everything for these equipments.
	hQuery := fmt.Sprintf(`
		SELECT glass_id, equipment_line_id 
		FROM history 
		WHERE equipment_line_id IN (%s)
	`, strings.Join(placeholders, ","))

	hRows, err := a.db.Analytics.Query(hQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch history: %w", err)
	}
	defer hRows.Close()

	// Map: EquipmentID -> Set<GlassID>
	equipmentGlassMap := make(map[string]map[string]bool)
	for hRows.Next() {
		var gid, eqID string
		hRows.Scan(&gid, &eqID)
		if _, exists := globalData[gid]; exists { // Only include if in time range
			if _, ok := equipmentGlassMap[eqID]; !ok {
				equipmentGlassMap[eqID] = make(map[string]bool)
			}
			equipmentGlassMap[eqID][gid] = true
		}
	}

	// 3.5 Fetch Heatmaps (Pre-calculation)
	globalHeatmap, targetHeatmaps, err := a.fetchHeatmaps(req.Targets, req.StartDate, req.EndDate)
	if err != nil {
		fmt.Printf("Warning: fetchHeatmaps failed: %v\n", err)
		globalHeatmap = make(map[string]int)
		targetHeatmaps = make(map[string]map[string]int)
	}

	// 4. Compute Results Parallelly (or sequential loop)
	results := make(map[string]*database.AnalysisResults)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, target := range req.Targets {
		wg.Add(1)
		go func(t AnalysisTarget) {
			defer wg.Done()

			// Identify Target Set and Others Set
			targetSet := equipmentGlassMap[t.EquipmentID] // Set<GlassID>

			// Build Result Lists
			var glassRes []GlassResult
			var lotResMap = make(map[string]*LotResult)
			var dailyResMap = make(map[string]*DailyResult)

			// Metrics counters
			var targetDefects, othersDefects, targetCount, othersCount int

			// Sampling counters (independent for simple stride)
			targetSampleCounter := 0
			othersSampleCounter := 0

			// Adaptive Sampling
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
			// Cap removed to strict enforce max points.
			// If totalRows is 10M, stride will be 5000, yielding 2000 points.

			// Iterate SINGLE pass over globalData
			for _, g := range globalData {
				isTarget := targetSet[g.GlassID]
				groupType := "Others"

				if isTarget {
					groupType = "Target"
					targetDefects += g.TotalDefects
					targetCount++

					// Target Sampling: Adaptive
					targetSampleCounter++
					if targetSampleCounter%targetStride == 0 {
						glassRes = append(glassRes, GlassResult{
							GlassID:      g.GlassID,
							LotID:        g.LotID,
							WorkDate:     g.WorkDate,
							TotalDefects: g.TotalDefects,
							GroupType:    groupType,
						})
					}
				} else {
					othersDefects += g.TotalDefects
					othersCount++

					// Others Sampling: Adaptive
					// Since Others is HUGE, we need aggressive sampling
					othersSampleCounter++
					if othersSampleCounter%othersStride == 0 {
						glassRes = append(glassRes, GlassResult{
							GlassID:      g.GlassID,
							LotID:        g.LotID,
							WorkDate:     g.WorkDate,
							TotalDefects: g.TotalDefects,
							GroupType:    groupType,
						})
					}
				}

				// Lot Aggregation (FULL DATA)
				lotKey := g.LotID + "|" + groupType
				if _, ok := lotResMap[lotKey]; !ok {
					lotResMap[lotKey] = &LotResult{LotID: g.LotID, GroupType: groupType}
				}
				lr := lotResMap[lotKey]
				lr.GlassCount++
				lr.TotalDefects += g.TotalDefects
				if g.TotalDefects > lr.MaxDefects {
					lr.MaxDefects = g.TotalDefects
				}

				// Daily Aggregation (FULL DATA)
				dailyKey := g.WorkDate + "|" + groupType
				if _, ok := dailyResMap[dailyKey]; !ok {
					dailyResMap[dailyKey] = &DailyResult{WorkDate: g.WorkDate, GroupType: groupType}
				}
				dr := dailyResMap[dailyKey]
				dr.GlassCount++
				dr.TotalDefects += g.TotalDefects

				// Heatmap Aggregation (Moved to Pre-fetch)
			}

			// Finalize Lot Results
			finalLotRes := []LotResult{}
			// Sample Lot Results too? Lots are fewer (thousands), but maybe 10% is fine for others.
			lotCounter := 0
			for _, lr := range lotResMap {
				lr.AvgDefects = float64(lr.TotalDefects) / float64(lr.GlassCount)

				// Keep All Target Lots, Sample Others Lots (10%)
				if lr.GroupType == "Target" {
					finalLotRes = append(finalLotRes, *lr)
				} else {
					lotCounter++
					if lotCounter%10 == 0 {
						finalLotRes = append(finalLotRes, *lr)
					}
				}
			}

			// Finalize Daily Results (Keep All - it's small, < 365 rows)
			finalDailyRes := []DailyResult{}
			for _, dr := range dailyResMap {
				dr.AvgDefects = float64(dr.TotalDefects) / float64(dr.GlassCount)
				finalDailyRes = append(finalDailyRes, *dr)
			}
			// Sort by WorkDate
			sort.Slice(finalDailyRes, func(i, j int) bool {
				return finalDailyRes[i].WorkDate < finalDailyRes[j].WorkDate
			})

			// Heatmap Calculation (Using Pre-fetched Maps)
			targetMap := targetHeatmaps[t.EquipmentID]
			othersMap := make(map[string]int)

			for addr, count := range globalHeatmap {
				tCount := targetMap[addr]
				othersCount := count - tCount
				if othersCount > 0 {
					othersMap[addr] = othersCount
				}
			}

			finalHeatmap := []HeatmapCell{}
			finalHeatmap = append(finalHeatmap, generateHeatmapCells(targetMap, targetDefects, targetCount, "Target")...)
			finalHeatmap = append(finalHeatmap, generateHeatmapCells(othersMap, othersDefects, othersCount, "Others")...)

			// Calculate Metrics (Manually using counters)
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

			metrics := AnalysisMetrics{
				OverallDefectRate:    overallRate,
				TargetDefectRate:     targetRate,
				OthersDefectRate:     othersRate,
				Delta:                overallRate - targetRate,
				SuperiorityIndicator: othersRate - targetRate,
				TargetGlassCount:     targetCount,
				OthersGlassCount:     othersCount,
			}

			// Marshal
			gJSON, _ := json.Marshal(glassRes)
			lJSON, _ := json.Marshal(finalLotRes)
			dJSON, _ := json.Marshal(finalDailyRes)
			hJSON, _ := json.Marshal(finalHeatmap)
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
func (c *CellsAggregator) ToCells(groupType string) []HeatmapCell {
	cells := []HeatmapCell{}
	if c.TotalGlasses == 0 {
		return cells
	}
	for i, defects := range c.Counts {
		// x: chr(65 + floor(i/10)), y: i%10
		x := string(rune('A' + (i / 10)))
		y := strconv.Itoa(i % 10)
		rate := float64(defects) / float64(c.TotalGlasses)
		cells = append(cells, HeatmapCell{
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

func (a *Analyzer) calculateBatchMetrics(glassResults []GlassResult) AnalysisMetrics {
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

// fetchHeatmaps retrieves heatmap data from inspection table
// Returns:
// 1. globalHeatmap: map[panel_addr]count
// 2. targetHeatmaps: map[equipment_id]map[panel_addr]count
func (a *Analyzer) fetchHeatmaps(targets []AnalysisTarget, start, end string) (map[string]int, map[string]map[string]int, error) {
	// 1. Global Heatmap
	global := make(map[string]int)
	gQuery := `
		SELECT panel_addr, COUNT(*) 
		FROM inspection 
		WHERE inspection_end_ymdhms >= ? AND inspection_end_ymdhms <= ?
		GROUP BY panel_addr
	`
	// DuckDB timestamp comparison might need cast if input is string YYYY-MM-DD
	// Assuming start/end are "2025-06-01" -> Cast to TIMESTAMP or compare with dates?
	// The schema says inspection_end_ymdhms is TIMESTAMP.
	// We should append time to start/end or rely on casting.
	// Lets append time.
	startTime := start + " 00:00:00"
	endTime := end + " 23:59:59"

	rows, err := a.db.Analytics.Query(gQuery, startTime, endTime)
	if err != nil {
		return nil, nil, fmt.Errorf("global heatmap query failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var addr string
		var count int
		if err := rows.Scan(&addr, &count); err == nil {
			global[addr] = count
		}
	}

	// 2. Target Heatmaps
	// Optimization: If many targets, we can query by Equipment IDs.
	// But inspection table doesn't have equipment_id. History does.
	// Query: Join inspection and history using glass_id.
	// SELECT h.equipment_line_id, i.panel_addr, COUNT(*)
	// FROM inspection i
	// JOIN history h ON i.glass_id = h.glass_id
	// WHERE h.equipment_line_id IN (...) AND i.time ... GROUP BY ...

	targetMap := make(map[string]map[string]int)
	targetIds := []string{}
	for _, t := range targets {
		targetMap[t.EquipmentID] = make(map[string]int)
		targetIds = append(targetIds, t.EquipmentID)
	}

	if len(targetIds) > 0 {
		placeholders := make([]string, len(targetIds))
		args := make([]interface{}, len(targetIds)+2)
		args[0] = startTime
		args[1] = endTime
		for i, id := range targetIds {
			placeholders[i] = "?"
			args[i+2] = id
		}

		tQuery := fmt.Sprintf(`
			SELECT h.equipment_line_id, i.panel_addr, COUNT(*)
			FROM inspection i
			JOIN history h ON i.glass_id = h.glass_id
			WHERE i.inspection_end_ymdhms >= ? AND i.inspection_end_ymdhms <= ?
			  AND h.equipment_line_id IN (%s)
			GROUP BY h.equipment_line_id, i.panel_addr
		`, strings.Join(placeholders, ","))

		tRows, err := a.db.Analytics.Query(tQuery, args...)
		if err != nil {
			return nil, nil, fmt.Errorf("target heatmap query failed: %w", err)
		}
		defer tRows.Close()

		for tRows.Next() {
			var eq, addr string
			var count int
			if err := tRows.Scan(&eq, &addr, &count); err == nil {
				if _, ok := targetMap[eq]; ok {
					targetMap[eq][addr] = count
				}
			}
		}
	}

	return global, targetMap, nil
}

func generateHeatmapCells(data map[string]int, totalDefects, totalGlasses int, groupType string) []HeatmapCell {
	cells := []HeatmapCell{}
	if totalGlasses == 0 {
		return cells
	}

	// Assuming data is map[panel_addr]count
	for addr, count := range data {
		if len(addr) < 2 {
			continue
		}
		// Logic: y = last char, x = rest
		y := addr[len(addr)-1:]
		x := addr[:len(addr)-1]

		rate := 0.0
		// Rate = Defects / Total Glasses ? Or Defects / Total Defects?
		// Usually Defect Rate = Count / Total Glasses.
		if totalGlasses > 0 {
			rate = float64(count) / float64(totalGlasses)
		}

		cells = append(cells, HeatmapCell{
			GroupType:    groupType,
			X:            x,
			Y:            y,
			TotalDefects: count,
			TotalGlasses: totalGlasses,
			DefectRate:   rate,
		})
	}
	return cells
}
