package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"lgd-litestat/charting"
	"lgd-litestat/database"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type hierarchySession struct {
	Results []hierarchyResultWithChart
	Params  database.AnalysisParamsV2
}

type hierarchyResultWithChart struct {
	database.HierarchyResult
	ChartURL string `json:"chart_url,omitempty"`
}

var hierarchySessions = make(map[string]*hierarchySession)

func (h *Handler) AnalyzeHierarchyHandler(w http.ResponseWriter, r *http.Request) {
	var params database.AnalysisParamsV2
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if params.Facility == "" {
		http.Error(w, "Facility is required", http.StatusBadRequest)
		return
	}
	if len(params.ProductIDs) == 0 {
		if params.Start == "" || params.End == "" || params.ModelCode == "" || params.DefectName == "" {
			http.Error(w, "Either product_ids OR (start, end, model_code, defect_name) must be provided", http.StatusBadRequest)
			return
		}
	}

	log.Printf("Analyzing Hierarchy V2: %+v", params)

	results, err := h.db.AnalyzeHierarchy(params)
	if err != nil {
		log.Printf("Analysis Failed: %v", err)
		http.Error(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	gen := charting.NewGenerator()
	outputDir := "/app/data/images"
	sessionID := uuid.New().String()

	enriched := make([]hierarchyResultWithChart, 0, len(results))

	for i, res := range results {
		entry := hierarchyResultWithChart{HierarchyResult: res}

		if len(res.DailyDPU) > 0 {
			eqLabel := buildEquipmentLabel(res)
			filename := fmt.Sprintf("%s_dpu_%d.png", sessionID, i)
			title := fmt.Sprintf("DPU Trend - %s", eqLabel)

			if _, err := gen.SaveDPUTrendChart(res.DailyDPU, title, filename, outputDir); err != nil {
				log.Printf("Chart generation failed for %s: %v", eqLabel, err)
			} else {
				entry.ChartURL = "/api/images/" + filename
			}
		}

		enriched = append(enriched, entry)
	}

	hierarchySessions[sessionID] = &hierarchySession{
		Results: enriched,
		Params:  params,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "success",
		"data":       enriched,
		"count":      len(enriched),
		"session_id": sessionID,
	})
}

func (h *Handler) ExportHierarchyCharts(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]

	session, ok := hierarchySessions[sessionID]
	if !ok {
		respondError(w, http.StatusNotFound, "Session not found or expired")
		return
	}

	gen := charting.NewGenerator()
	zipBuf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuf)
	chartCount := 0

	for i, entry := range session.Results {
		if len(entry.DailyDPU) == 0 {
			continue
		}

		eqLabel := buildEquipmentLabel(entry.HierarchyResult)
		title := fmt.Sprintf("DPU Trend - %s", eqLabel)

		imgData, err := gen.GenerateDPUTrendChart(entry.DailyDPU, title)
		if err != nil {
			log.Printf("Export chart error [%d]: %v", i, err)
			continue
		}

		filename := fmt.Sprintf("%s_dpu_trend.png", eqLabel)
		f, err := zipWriter.Create(filename)
		if err != nil {
			continue
		}
		f.Write(imgData)
		chartCount++
	}

	zipWriter.Close()

	if chartCount == 0 {
		respondError(w, http.StatusNotFound, "No charts available for export")
		return
	}

	ts := time.Now().Format("20060102_150405")
	zipFilename := fmt.Sprintf("hierarchy_charts_%s.zip", ts)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", zipFilename))
	w.Header().Set("Content-Length", strconv.Itoa(zipBuf.Len()))
	w.Write(zipBuf.Bytes())
}

func buildEquipmentLabel(res database.HierarchyResult) string {
	label := res.ProcessCode
	if res.EquipmentLineID != "" {
		label += "_" + res.EquipmentLineID
	}
	if res.EquipmentMachineID != "" {
		label += "_" + res.EquipmentMachineID
	}
	if res.EquipmentPathID != "" {
		label += "_" + res.EquipmentPathID
	}
	return label
}
