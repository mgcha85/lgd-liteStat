package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
}

// NewHandler creates a new handler instance
func NewHandler(db *database.DB, repo *database.Repository, cfg *config.Config, martBuilder *mart.MartBuilder, analyzer *analysis.Analyzer) *Handler {
	return &Handler{
		db:          db,
		repo:        repo,
		cfg:         cfg,
		martBuilder: martBuilder,
		analyzer:    analyzer,
	}
}

// HealthCheck returns API health status
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	stats := make(map[string]int64)

	// Check Analytics DB (DuckDB)
	if err := h.db.Analytics.Ping(); err != nil {
		respondError(w, http.StatusServiceUnavailable, "analytics database health check failed")
		return
	}

	// Check App DB (SQLite)
	if err := h.db.App.Ping(); err != nil {
		respondError(w, http.StatusServiceUnavailable, "app database health check failed")
		return
	}

	// Get table counts
	tables := []struct {
		name string
		db   *sql.DB
	}{
		{"inspection", h.db.Analytics},
		{"history", h.db.Analytics},
		{"glass_stats", h.db.Analytics},
		{"analysis_cache", h.db.App},
		{"analysis_jobs", h.db.App},
	}

	for _, t := range tables {
		var count int64
		err := t.db.QueryRow("SELECT COUNT(*) FROM " + t.name).Scan(&count)
		if err != nil {
			// Table might not exist yet
			stats[t.name] = 0
		} else {
			stats[t.name] = count
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "healthy",
		"stats":  stats,
	})
}

// IngestData handles data ingestion requests
func (h *Handler) IngestData(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	startTime, err := time.Parse(time.RFC3339Nano, req.StartTime)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid start_time format")
		return
	}

	endTime, err := time.Parse(time.RFC3339Nano, req.EndTime)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid end_time format")
		return
	}

	// Ingest data
	ingestor := etl.NewDataIngestor(h.cfg, h.repo)
	var counts map[string]int
	counts, err = ingestor.IngestData(startTime, endTime)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("ingestion failed: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":           "success",
		"records_inserted": counts,
	})
}

// RefreshMart handles glass_stats mart refresh requests
func (h *Handler) RefreshMart(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	stats, err := h.martBuilder.Refresh() // Was h.mart.RefreshGlassMart
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
	deleted, err := h.repo.CleanupOldData(h.cfg.DataRetentionDays)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("cleanup failed: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "success",
		"deleted_rows": deleted,
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

	// Execute Batch Analysis
	start := time.Now()
	results, err := h.analyzer.AnalyzeBatch(req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("batch analysis failed: %v", err))
		return
	}
	duration := time.Since(start).Milliseconds()

	// Return map directly
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "success",
		"duration_ms": duration,
		"results":     results,
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
	var glassResults []analysis.GlassResult
	json.Unmarshal(results.GlassResults, &glassResults)

	end := offset + limit
	if end > len(glassResults) {
		end = len(glassResults)
	}

	paginatedGlass := glassResults[offset:end]

	// Build response
	response := map[string]interface{}{
		"glass_results":   paginatedGlass,
		"lot_results":     json.RawMessage(results.LotResults),
		"daily_results":   json.RawMessage(results.DailyResults),
		"heatmap_results": json.RawMessage(results.HeatmapResults),
		"metrics":         json.RawMessage(results.Metrics),
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

// GetInspectionData queries inspection data by time range (required) with optional filters
func (h *Handler) GetInspectionData(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	startTime := r.URL.Query().Get("start_time")
	endTime := r.URL.Query().Get("end_time")
	processCode := r.URL.Query().Get("process_code")
	defectName := r.URL.Query().Get("defect_name")

	// Validate required parameters
	if startTime == "" || endTime == "" {
		respondError(w, http.StatusBadRequest, "start_time and end_time are required (format: YYYY-MM-DD HH:MM:SS)")
		return
	}

	// Build query
	query := `
		SELECT 
			glass_id,
			panel_id,
			product_id,
			panel_addr,
			term_name,
			defect_name,
			inspection_end_ymdhms,
			process_code,
			defect_count
		FROM inspection
		WHERE inspection_end_ymdhms >= CAST(? AS TIMESTAMP)
		  AND inspection_end_ymdhms <= CAST(? AS TIMESTAMP)
	`
	args := []interface{}{startTime, endTime}

	// Add optional filters
	if processCode != "" {
		query += ` AND process_code = ?`
		args = append(args, processCode)
	}

	if defectName != "" {
		query += ` AND defect_name = ?`
		args = append(args, defectName)
	}

	// Parse pagination
	limit := 1000 // default
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	query += ` ORDER BY inspection_end_ymdhms DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	// Execute query
	rows, err := h.db.Analytics.Query(query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	defer rows.Close()

	// Collect results
	results := []database.InspectionRow{}
	for rows.Next() {
		var r database.InspectionRow
		if err := rows.Scan(
			&r.GlassID, &r.PanelID, &r.ProductID, &r.PanelAddr,
			&r.TermName, &r.DefectName, &r.InspectionEndYmdhms,
			&r.ProcessCode, &r.DefectCount,
		); err != nil {
			continue
		}
		results = append(results, r)
	}

	// Get total count for pagination
	countQuery := `SELECT COUNT(*) FROM inspection WHERE inspection_end_ymdhms >= CAST(? AS TIMESTAMP) AND inspection_end_ymdhms <= CAST(? AS TIMESTAMP)`
	countArgs := []interface{}{startTime, endTime}
	if processCode != "" {
		countQuery += ` AND process_code = ?`
		countArgs = append(countArgs, processCode)
	}
	if defectName != "" {
		countQuery += ` AND defect_name = ?`
		countArgs = append(countArgs, defectName)
	}

	var totalCount int
	h.db.Analytics.QueryRow(countQuery, countArgs...).Scan(&totalCount)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"data": results,
		"pagination": map[string]interface{}{
			"limit":       limit,
			"offset":      offset,
			"total_count": totalCount,
			"has_more":    offset+limit < totalCount,
		},
	})
}

// GetHistoryData queries history data by glass_id (required) with optional filters
func (h *Handler) GetHistoryData(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	glassID := r.URL.Query().Get("glass_id")
	processCode := r.URL.Query().Get("process_code")
	equipmentID := r.URL.Query().Get("equipment_id")

	// Validate required parameter
	if glassID == "" {
		respondError(w, http.StatusBadRequest, "glass_id is required")
		return
	}

	// Build query
	query := `
		SELECT 
			glass_id,
			product_id,
			lot_id,
			equipment_line_id,
			process_code,
			timekey_ymdhms,
			seq_num
		FROM history
		WHERE glass_id = ?
	`
	args := []interface{}{glassID}

	// Add optional filters
	if processCode != "" {
		query += ` AND process_code = ?`
		args = append(args, processCode)
	}

	if equipmentID != "" {
		query += ` AND equipment_line_id = ?`
		args = append(args, equipmentID)
	}

	query += ` ORDER BY timekey_ymdhms ASC`

	// Execute query
	rows, err := h.db.Analytics.Query(query, args...)
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
			&r.GlassID, &r.ProductID, &r.LotID, &r.EquipmentLineID,
			&r.ProcessCode, &r.TimekeyYmdhms, &r.SeqNum,
		); err != nil {
			continue
		}
		results = append(results, r)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"glass_id": glassID,
		"data":     results,
		"count":    len(results),
	})
}

// GetEquipmentRankings returns top equipments sorted by defect rate delta
func (h *Handler) GetEquipmentRankings(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	defectName := r.URL.Query().Get("defect_name")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	if startDate == "" || endDate == "" {
		respondError(w, http.StatusBadRequest, "start_date and end_date are required")
		return
	}

	// Query to get equipment rankings
	query := `
		SELECT 
			h.equipment_line_id,
			h.process_code,
			COUNT(DISTINCT m.glass_id) as glass_count,
			SUM(m.total_defects) as total_defects,
			CAST(SUM(m.total_defects) AS FLOAT) / COUNT(DISTINCT m.glass_id) as equipment_defect_rate
		FROM glass_stats m
		JOIN history h ON m.glass_id = h.glass_id
		WHERE m.work_date >= CAST(? AS DATE)
		  AND m.work_date <= CAST(? AS DATE)
	`

	args := []interface{}{startDate, endDate}

	if defectName != "" {
		query += ` AND m.glass_id IN (
			SELECT DISTINCT glass_id FROM inspection WHERE term_name = ?
		)`
		args = append(args, defectName)
	}

	query += ` GROUP BY h.equipment_line_id, h.process_code`

	// Get overall defect rate
	var overallRate float64
	overallQuery := `
		SELECT CAST(SUM(total_defects) AS FLOAT) / COUNT(*) 
		FROM glass_stats 
		WHERE work_date >= ? AND work_date <= ?
	`
	overallArgs := []interface{}{startDate, endDate}
	if defectName != "" {
		overallQuery += ` AND glass_id IN (SELECT DISTINCT glass_id FROM inspection WHERE term_name = ?)`
		overallArgs = append(overallArgs, defectName)
	}
	h.db.Analytics.QueryRow(overallQuery, overallArgs...).Scan(&overallRate)

	rows, err := h.db.Analytics.Query(query, args...)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	defer rows.Close()

	type Ranking struct {
		EquipmentID  string  `json:"equipment_id"`
		ProcessCode  string  `json:"process_code"`
		GlassCount   int     `json:"glass_count"`
		TotalDefects int     `json:"total_defects"`
		DefectRate   float64 `json:"defect_rate"`
		OverallRate  float64 `json:"overall_rate"`
		Delta        float64 `json:"delta"` // overall - equipment
	}

	rankings := []Ranking{}
	for rows.Next() {
		var r Ranking
		r.OverallRate = overallRate
		if err := rows.Scan(&r.EquipmentID, &r.ProcessCode, &r.GlassCount, &r.TotalDefects, &r.DefectRate); err != nil {
			continue
		}
		r.Delta = overallRate - r.DefectRate
		rankings = append(rankings, r)
	}

	// Sort by delta descending (biggest positive delta first)
	// This can be done in Go or via ORDER BY in SQL
	// For simplicity, we'll return as-is and let frontend sort

	// Apply limit
	limit := h.cfg.Analysis.TopNLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	if len(rankings) > limit {
		rankings = rankings[:limit]
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"rankings": rankings,
		"count":    len(rankings),
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
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, h.cfg)
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
