package api

import (
	"encoding/json"
	"log"
	"net/http"

	"lgd-litestat/database"
)

// AnalyzeHierarchyHandler handles the V2 analysis request
func (h *Handler) AnalyzeHierarchyHandler(w http.ResponseWriter, r *http.Request) {
	var params database.AnalysisParamsV2
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate Required Fields
	if params.Facility == "" {
		http.Error(w, "Facility is required", http.StatusBadRequest)
		return
	}
	// "ProductIDs" OR ("Start" + "End" + "ModelCode")
	if len(params.ProductIDs) == 0 {
		if params.Start == "" || params.End == "" || params.ModelCode == "" {
			http.Error(w, "Either product_ids OR (start, end, model_code) must be provided", http.StatusBadRequest)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"data":   results,
		"count":  len(results),
	})
}
