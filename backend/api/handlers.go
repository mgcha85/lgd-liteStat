package api

import (
	"bytes"
	"crypto/md5"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"lgd-litestat/analysis"
	"lgd-litestat/config"
	"lgd-litestat/database"
	"lgd-litestat/etl"
	"lgd-litestat/mart"

	"github.com/gorilla/mux"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	db          *database.DB
	repo        *database.Repository
	cfg         *config.Config
	martBuilder *mart.MartBuilder
	analyzer    *analysis.Analyzer
	ingestor    *etl.DataIngestor
}

// NewHandler creates a new API handler
func NewHandler(db *database.DB, repo *database.Repository, cfg *config.Config, mart *mart.MartBuilder, analyzer *analysis.Analyzer, ingestor *etl.DataIngestor) *Handler {
	return &Handler{
		db:          db,
		repo:        repo,
		cfg:         cfg,
		martBuilder: mart,
		analyzer:    analyzer,
		ingestor:    ingestor,
	}
}

// Helper to extract facility code
func (h *Handler) getFacility(r *http.Request) string {
	// 1. Query Param
	if fac := r.URL.Query().Get("facility_code"); fac != "" {
		return fac
	}
	// 2. Header
	if fac := r.Header.Get("X-Facility-Code"); fac != "" {
		return fac
	}
	// 3. Fallback to first facility or "default"
	if len(h.cfg.Settings.Facilities) > 0 {
		return h.cfg.Settings.Facilities[0]
	}
	return "default"
}

// HealthCheck checks API health
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	// Check SQLite
	if err := h.db.App.Ping(); err != nil {
		respondError(w, http.StatusServiceUnavailable, "App DB unhealthy")
		return
	}

	// Check Analytics (Default or All?)
	// Let's check all configured facilities
	for fac, conn := range h.db.Analytics {
		if err := conn.Ping(); err != nil {
			respondError(w, http.StatusServiceUnavailable, fmt.Sprintf("Analytics DB (%s) unhealthy", fac))
			return
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
		"mode":   "production",
	})
}

// IngestRequest defines the body for ingestion API
type IngestRequest struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

// IngestData triggers data ingestion
func (h *Handler) IngestData(w http.ResponseWriter, r *http.Request) {
	var req IngestRequest
	var startTime, endTime time.Time

	// Parse JSON body if present
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			if req.StartDate != "" {
				startTime, _ = time.Parse("2006-01-02", req.StartDate)
			}
			if req.EndDate != "" {
				endTime, _ = time.Parse("2006-01-02", req.EndDate)
				// Set to end of day
				endTime = endTime.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			}
		}
	}

	// Pass nil for facilities to use all configured facilities
	counts, err := h.ingestor.IngestData(startTime, endTime, nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("ingest failed: %v", err))
		return
	}
	respondJSON(w, http.StatusOK, counts)
}

// RefreshMart handles glass_stats mart refresh requests
func (h *Handler) RefreshMart(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	fac := h.getFacility(r)
	stats, err := h.martBuilder.Refresh(fac) // Was h.mart.RefreshGlassMart
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("mart refresh failed: %v", err))
		return
	}

	duration := time.Since(start)

	// stats, _ := h.mart.GetMartStats() -> stats are now returned by Refresh()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "success",
		"duration_ms":  duration.Milliseconds(),
		"rows_created": stats.TotalRows,
		"stats":        stats,
	})
}

// CleanupData handles old data cleanup requests
func (h *Handler) CleanupData(w http.ResponseWriter, r *http.Request) {
	err := h.repo.CleanupOldData(h.cfg.Retention.DataDays, h.cfg.Settings.Facilities)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("cleanup failed: %v", err))
		return
	}

	// Also clean analysis
	if err := h.repo.CleanupOldAnalysis(h.cfg.Retention.AnalysisDays); err != nil {
		log.Printf("Warning: analysis cleanup failed: %v", err)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "success",
	})
}

// RequestAnalysis handles analysis request creation
func (h *Handler) RequestAnalysis(w http.ResponseWriter, r *http.Request) {
	var req analysis.AnalysisRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate required fields
	if req.DefectName == "" || req.StartDate == "" || req.EndDate == "" {
		respondError(w, http.StatusBadRequest, "defect_name, start_date, and end_date are required")
		return
	}

	req.FacilityCode = h.getFacility(r)
	jobID, err := h.analyzer.RequestAnalysis(req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create analysis job: %v", err))
		return
	}

	respondJSON(w, http.StatusAccepted, map[string]interface{}{
		"job_id": jobID,
		"status": "pending",
	})
}

// AnalyzeBatch handles batch analysis request synchronously (optimized)
func (h *Handler) AnalyzeBatch(w http.ResponseWriter, r *http.Request) {
	// Parse BatchAnalysisRequest
	var req analysis.BatchAnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate
	if req.DefectName == "" || req.StartDate == "" || req.EndDate == "" || len(req.Targets) == 0 {
		respondError(w, http.StatusBadRequest, "defect_name, start_date, end_date, and targets are required")
		return
	}

	// 1. Generate Cache Key
	req.FacilityCode = h.getFacility(r)
	reqBytes, _ := json.Marshal(req)
	hash := md5.Sum(reqBytes)
	cacheKey := hex.EncodeToString(hash[:])

	// 2. Check Cache
	if cached, err := h.repo.GetAnalysisCache(cacheKey); err == nil && cached != nil {
		// Use cached results if available
		var results map[string]*database.AnalysisResults
		if err := json.Unmarshal(cached.BatchResults, &results); err == nil {
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"status":    "success",
				"cache_hit": true,
				"results":   results,
				"cache_key": cacheKey, // Return key for image export
			})
			return
		}
	}

	// 3. Execute Analysis
	start := time.Now()
	results, err := h.analyzer.AnalyzeBatch(req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("batch analysis failed: %v", err))
		return
	}
	duration := time.Since(start).Milliseconds()

	// 4. Save to Cache
	if resultsJSON, err := json.Marshal(results); err == nil {
		// Create AnalysisResults object
		cacheEntry := &database.AnalysisResults{
			BatchResults: json.RawMessage(resultsJSON),
			CreatedAt:    time.Now(),
		}
		// Save to cache (using string for req interface for now, or raw req)
		h.repo.SaveAnalysisCache(cacheKey, req, cacheEntry, 100)
	}

	// Calculate total glass count for logging
	totalGlassCount := 0
	for _, res := range results {
		var m database.AnalysisMetrics
		if err := json.Unmarshal(res.Metrics, &m); err == nil {
			totalGlassCount += m.TargetGlassCount + m.OthersGlassCount
		}
	}

	// Log Analysis
	h.repo.LogAnalysis(database.AnalysisLog{
		DefectName:  req.DefectName,
		StartDate:   req.StartDate,
		EndDate:     req.EndDate,
		TargetCount: len(req.Targets),
		GlassCount:  totalGlassCount,
		DurationMs:  duration,
		Status:      "success",
		Facility:    req.FacilityCode,
	})

	// Return map directly
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "success",
		"duration_ms": duration,
		"results":     results,
		"cache_key":   cacheKey, // Return key for image export
		"cache_hit":   false,
	})
}

// GetAnalysisStatus returns the status of an analysis job
func (h *Handler) GetAnalysisStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["jobId"]

	status, err := h.repo.GetAnalysisJobStatus(jobID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "job not found")
		} else {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get job status: %v", err))
		}
		return
	}

	respondJSON(w, http.StatusOK, status)
}

// GetAnalysisResults returns the results of a completed analysis job
func (h *Handler) GetAnalysisResults(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["jobId"]

	// Get job status first
	status, err := h.repo.GetAnalysisJobStatus(jobID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "job not found")
		} else {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get job status: %v", err))
		}
		return
	}

	if status.Status != "completed" {
		respondError(w, http.StatusConflict, "job is not completed yet")
		return
	}

	// Get results from cache
	results, err := h.repo.GetAnalysisCache(status.CacheKey)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get results: %v", err))
		return
	}

	// Parse query parameters for pagination
	limit := 100 // default
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = o
		}
	}

	// Apply pagination to glass results
	var glassResults []database.GlassResult
	json.Unmarshal(results.GlassResults, &glassResults)

	end := offset + limit
	if end > len(glassResults) {
		end = len(glassResults)
	}

	paginatedGlass := glassResults[offset:end]

	// Generate Charts if requested
	var images map[string]string
	if r.URL.Query().Get("include_images") == "true" {
		var dailyResults []database.DailyResult
		// daily_results is RawMessage, need to unmarshal
		if err := json.Unmarshal(results.DailyResults, &dailyResults); err == nil {
			// Filename: jobID_timestamp.png
			filename := fmt.Sprintf("%s_%d.png", jobID, time.Now().Unix())
			outputDir := "/app/data/images" // Hardcoded for Docker env

			if imgPath, err := analysis.SaveTrendChart(dailyResults, filename, outputDir); err == nil {
				images = map[string]string{
					"daily_trend": imgPath,
				}
			} else {
				// Log error but continue?
				fmt.Printf("Failed to generate chart: %v\n", err)
			}
		}
	}

	// Build response
	response := map[string]interface{}{
		"glass_results":   paginatedGlass,
		"lot_results":     json.RawMessage(results.LotResults),
		"daily_results":   json.RawMessage(results.DailyResults),
		"heatmap_results": json.RawMessage(results.HeatmapResults),
		"metrics":         json.RawMessage(results.Metrics),
		"images":          images, // Added images field (base64 png)
		"pagination": map[string]interface{}{
			"limit":       limit,
			"offset":      offset,
			"total_count": len(glassResults),
			"has_more":    end < len(glassResults),
		},
		"created_at": results.CreatedAt,
	}

	respondJSON(w, http.StatusOK, response)
}

// GetInspectionData retrieves raw inspection data
func (h *Handler) GetInspectionData(w http.ResponseWriter, r *http.Request) {
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	processCode := r.URL.Query().Get("process_code")
	defectName := r.URL.Query().Get("defect_name")

	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		fmt.Sscanf(o, "%d", &offset)
	}

	startTime, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		// Try fallback if empty?
		startTime = time.Now().AddDate(0, 0, -30)
	}
	endTime, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		endTime = time.Now()
	}

	fac := h.getFacility(r)
	results, totalCount, err := h.repo.GetInspectionData(startTime, endTime, processCode, defectName, limit, offset, fac)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"data": results,
		"pagination": map[string]interface{}{
			"limit":       limit,
			"offset":      offset,
			"total_count": totalCount,
			"has_more":    int64(offset+limit) < totalCount,
		},
	})
}

// GetHistoryData queries history data by product_id (required) with optional filters
func (h *Handler) GetHistoryData(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	productID := r.URL.Query().Get("product_id") // Was glass_id
	if productID == "" {
		productID = r.URL.Query().Get("glass_id") // Legacy support
	}
	processCode := r.URL.Query().Get("process_code")
	equipmentID := r.URL.Query().Get("equipment_id")

	// Validate required parameter
	if productID == "" {
		respondError(w, http.StatusBadRequest, "product_id is required")
		return
	}

	// Build query
	query := `
		SELECT 
			product_id,
			product_type_code,
			lot_id,
			equipment_line_id,
			process_code,
			move_in_ymdhms
		FROM lake_mgr.mas_pnl_prod_eqp_h
		WHERE product_id = ?
	`
	args := []interface{}{productID}

	// Add optional filters
	if processCode != "" {
		query += ` AND process_code = ?`
		args = append(args, processCode)
	}

	if equipmentID != "" {
		query += ` AND equipment_line_id = ?`
		args = append(args, equipmentID)
	}

	query += ` ORDER BY move_in_ymdhms ASC`

	// Execute query
	fac := h.getFacility(r)
	conn, err := h.db.GetAnalyticsDB(fac)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("db connection failed: %v", err))
		return
	}
	rows, err := conn.Query(query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	defer rows.Close()

	// Collect results
	results := []database.HistoryRow{}
	for rows.Next() {
		var r database.HistoryRow
		if err := rows.Scan(
			&r.ProductID, &r.ProductTypeCode, &r.LotID, &r.EquipmentLineID, // Using ProductType code as per struct
			&r.ProcessCode, &r.MoveInYmdhms,
		); err != nil {
			continue
		}
		results = append(results, r)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"product_id": productID,
		"data":       results,
		"count":      len(results),
	})
}

// GetEquipmentRankings returns top equipments sorted by defect rate delta
func (h *Handler) GetEquipmentRankings(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	defectName := r.URL.Query().Get("defect_name")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	limit := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	if startDate == "" || endDate == "" {
		respondError(w, http.StatusBadRequest, "start_date and end_date are required")
		return
	}

	// Call repository method
	startTime, _ := time.Parse("2006-01-02", startDate)
	end, _ := time.Parse("2006-01-02", endDate)
	fac := h.getFacility(r)

	rankings, totalCount, err := h.repo.GetEquipmentRankings(startTime, end, defectName, limit, fac)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"rankings": rankings,
		"count":    totalCount,
	})
}

// respondJSON sends a JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError sends an error response
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{
		"error": message,
	})
}

// ConfigUpdateRequest represents the body for config updates
type ConfigUpdateRequest struct {
	Analysis struct {
		TopNLimit       int `json:"top_n_limit"`
		DefaultPageSize int `json:"default_page_size"`
		MaxPageSize     int `json:"max_page_size"`
	} `json:"analysis"`
	Settings struct {
		DefectTerms []string `json:"defect_terms"`
	} `json:"settings"`
}

// GetConfig returns the current configuration
// GetConfig returns public configuration
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	publicConfig := struct {
		Settings config.SettingsConfig `json:"Settings"`
		MockData config.MockDataConfig `json:"MockData"`
	}{
		Settings: h.cfg.Settings,
		MockData: h.cfg.MockData,
	}
	respondJSON(w, http.StatusOK, publicConfig)
}

// UpdateConfig updates configuration settings
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update Analysis
	if err := h.cfg.UpdateAnalysisSettings(req.Analysis.TopNLimit, req.Analysis.DefaultPageSize, req.Analysis.MaxPageSize); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update analysis settings")
		return
	}

	// Update Settings
	if err := h.cfg.UpdateDefectTerms(req.Settings.DefectTerms); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update defect list")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// GetHeatmapConfig returns heatmap grid configuration
func (h *Handler) GetHeatmapConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, h.cfg.HeatmapManager.GetAll())
}

// UpdateHeatmapConfig updates heatmap grid configuration
func (h *Handler) UpdateHeatmapConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]config.HeatmapGridConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.cfg.HeatmapManager.Save(req); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save config: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// GetAnalysisLogs returns recent analysis performance logs
func (h *Handler) GetAnalysisLogs(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	logs, err := h.repo.GetRecentAnalysisLogs(limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get logs: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, logs)
}

// AnalyzeDateRange handles analysis by date range (Global Aggregation)
func (h *Handler) AnalyzeDateRange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StartDate  string `json:"start_date"`
		EndDate    string `json:"end_date"`
		DefectName string `json:"defect_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Calculate Global Metrics directly from DB
	// Reuse GetEquipmentRankings logic but for global?
	// Or use Analyzer?
	// For now, let's return the Ranking list as the "Analysis Result" for the date range
	// because it contains the breakdown by equipment.
	// But we should adapt to "Analyze" format?
	// The user likely wants to download data.
	// Let's call rankings query and return it.

	// Create a mock request to reuse existing function logic if possible
	// Actually, GetEquipmentRankings logic is perfect for "Date Range Analysis".
	// I'll wrap it or just guide user to it?
	// User asked for specific API. I will create a specific handler that calls the logic.
	// We'll return EquipmentRankings data structure.

	// Construct request for internal usage
	q := r.URL.Query()
	q.Set("start_date", req.StartDate)
	q.Set("end_date", req.EndDate)
	if req.DefectName != "" {
		q.Set("defect_name", req.DefectName)
	}
	r.URL.RawQuery = q.Encode()
	h.GetEquipmentRankings(w, r)
}

// AnalyzeGlass handles single glass analysis
func (h *Handler) AnalyzeGlass(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	glassID := vars["glassId"]

	// 1. Get History
	// 1. Get History
	// Facility from glassId lookup? Or pass facility param.
	// Usually dashboard context provides facility. Try query param/header.
	fac := h.getFacility(r)
	conn, err := h.db.GetAnalyticsDB(fac)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "db connection failed")
		return
	}

	hQuery := `SELECT product_id, lot_id, equipment_line_id, process_code, timekey_ymdhms FROM history WHERE glass_id = ? ORDER BY timekey_ymdhms ASC`
	hRows, err := conn.Query(hQuery, glassID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to query history")
		return
	}
	defer hRows.Close()

	type HistoryItem struct {
		ProcessCode string    `json:"process_code"`
		EquipmentID string    `json:"equipment_id"`
		Time        time.Time `json:"time"`
	}
	history := []HistoryItem{}
	var lotID string
	for hRows.Next() {
		var h HistoryItem
		var pid, lid string
		hRows.Scan(&pid, &lid, &h.EquipmentID, &h.ProcessCode, &h.Time)
		history = append(history, h)
		lotID = lid
	}

	// 2. Get Defect Stats
	var totalDefects int
	var workDate string
	err = conn.QueryRow(`
		SELECT total_defects, CAST(work_date AS VARCHAR) 
		FROM glass_stats WHERE glass_id = ?`, glassID).Scan(&totalDefects, &workDate)
	if err != nil && err != sql.ErrNoRows {
		respondError(w, http.StatusInternalServerError, "failed to query stats")
		return
	}

	// 3. Get Inspection Details
	iQuery := `SELECT defect_name, defect_count, inspection_end_ymdhms FROM inspection WHERE glass_id = ?`
	iRows, err := conn.Query(iQuery, glassID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to query inspection")
		return
	}
	defer iRows.Close()

	type DefectItem struct {
		Name  string    `json:"name"`
		Count int       `json:"count"`
		Time  time.Time `json:"time"`
	}
	defects := []DefectItem{}
	for iRows.Next() {
		var d DefectItem
		iRows.Scan(&d.Name, &d.Count, &d.Time)
		defects = append(defects, d)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"glass_id":      glassID,
		"lot_id":        lotID,
		"work_date":     workDate,
		"total_defects": totalDefects,
		"history":       history,
		"defects":       defects,
	})
}

// ExportAnalysis exports analysis results as CSV
func (h *Handler) ExportAnalysis(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["jobId"]

	// 1. Fetch Results
	results, err := h.repo.GetAnalysisCache(jobID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Analysis not found")
		return
	}

	// results is already *database.AnalysisResults
	// Proceed to generate CSV

	// 2. Generate CSV
	b := &bytes.Buffer{}
	writer := csv.NewWriter(b)

	// Header
	writer.Write([]string{"GlassID", "LotID", "WorkDate", "TotalDefects", "Group"})

	// Rows - Decode GlassResults
	var glassResults []database.GlassResult
	if len(results.GlassResults) > 0 {
		json.Unmarshal(results.GlassResults, &glassResults) // Ignore error
	}

	for _, g := range glassResults {
		writer.Write([]string{
			g.GlassID,
			g.LotID,
			g.WorkDate,
			fmt.Sprintf("%d", g.TotalDefects),
			g.GroupType,
		})
	}
	writer.Flush()

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment;filename=analysis_%s.csv", jobID))
	w.Write(b.Bytes())
}

// GetSchedulerConfig returns current scheduler settings
func (h *Handler) GetSchedulerConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, h.cfg.Scheduler)
}

// UpdateSchedulerConfig updates scheduler settings
func (h *Handler) UpdateSchedulerConfig(w http.ResponseWriter, r *http.Request) {
	var newCfg config.SchedulerConfig
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.cfg.Scheduler = newCfg
	// Note: In-memory update only.

	respondJSON(w, http.StatusOK, h.cfg.Scheduler)
}
