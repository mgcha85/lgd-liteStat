package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"lgd-litestat/charting"
	"lgd-litestat/database"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

// ExportImages generates and zips charts for a specific analysis job
func (h *Handler) ExportImages(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["jobId"]
	equipmentID := r.URL.Query().Get("equipment_id")

	if jobID == "" || equipmentID == "" {
		respondError(w, http.StatusBadRequest, "jobId and equipment_id are required")
		return
	}

	// 1. Get Analysis Results (Try Job ID likely Cache Key for simple batch, or actual Job ID)
	// If the client sends a Cache Key as JobID (since we returned CacheKey/JobID in persistence), we handle it.
	// Actually, for consistency, let's assume JobID -> Status -> CacheKey logic if it exists,
	// OR if JobID looks like a hash (CacheKey), try to fetch directly.
	// But `AnalyzeBatch` might return just results without JobID if we used sync?
	// The new plan is `AnalyzeBatch` returns cached results immediately.
	// The client might need to know the 'CacheKey' (or JobID) to request images.
	// I will Ensure `AnalyzeBatch` returns `cache_key` in its response.

	// Check if jobID exists in analysis_jobs
	status, err := h.repo.GetAnalysisJobStatus(jobID)
	var cacheKey string
	if err == nil && status != nil {
		cacheKey = status.CacheKey
	} else {
		// Assume jobID IS the cacheKey provided by client (if we changed AnalyzeBatch to return it)
		cacheKey = jobID
	}

	results, err := h.repo.GetAnalysisCache(cacheKey)
	if err != nil || results == nil {
		respondError(w, http.StatusNotFound, "Analysis results not found or expired")
		return
	}

	// 2. Extract Data for Equipment
	// Since Cache might contain BatchResults (Map) or Single Results (fields)
	// We need to handle both cases.
	var dailyRes []database.DailyResult
	var glassRes []database.GlassResult
	var heatmapRes []database.HeatmapCell

	if len(results.BatchResults) > 0 {
		// Parse Batch Map based on EquipmentID
		var batchMap map[string]*database.AnalysisResults
		if err := json.Unmarshal(results.BatchResults, &batchMap); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to parse batch results")
			return
		}
		if res, ok := batchMap[equipmentID]; ok {
			// Unmarshal internal JSONs
			json.Unmarshal(res.DailyResults, &dailyRes) // Error ignored for brevity
			json.Unmarshal(res.GlassResults, &glassRes)
			json.Unmarshal(res.HeatmapResults, &heatmapRes)
		} else {
			respondError(w, http.StatusNotFound, "Equipment ID not found in batch results")
			return
		}
	} else {
		// Single Result (Legacy or Single Analysis)
		// We can check if `request_params` had this equipment?
		// Or just return what's there if it matches valid data.
		// For now, let's try to unmarshal fields directly.
		json.Unmarshal(results.DailyResults, &dailyRes)
		json.Unmarshal(results.GlassResults, &glassRes)
		json.Unmarshal(results.HeatmapResults, &heatmapRes)
	}

	if len(dailyRes) == 0 && len(glassRes) == 0 {
		respondError(w, http.StatusNotFound, "No data available for charting")
		return
	}

	// 3. Generate Charts
	gen := charting.NewGenerator()
	zipBuf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuf)

	// Helper to add file
	addFile := func(name string, data []byte) {
		f, err := zipWriter.Create(name)
		if err != nil {
			fmt.Printf("Zip create error: %v\n", err)
			return
		}
		f.Write(data)
	}

	// Daily Trend
	if img, err := gen.GenerateDailyTrend(dailyRes); err == nil {
		addFile("daily_trend.png", img)
	}

	// Heatmap (SVG)
	if img, err := gen.GenerateHeatmap(heatmapRes); err == nil {
		addFile("heatmap.svg", img)
	}

	// Scatter Plots (TODO: Implement Scatter in Generator)
	// For now, skip or placeholder
	// if img, err := gen.GenerateScatter(glassRes); err == nil {
	// 	addFile("scatter.png", img)
	// }

	zipWriter.Close()

	// 4. Serve ZIP
	filename := fmt.Sprintf("charts_%s_%s.zip", equipmentID, time.Now().Format("20060102_150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Length", strconv.Itoa(zipBuf.Len()))
	w.Write(zipBuf.Bytes())
}
