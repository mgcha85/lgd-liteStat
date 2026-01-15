package api

import (
	"encoding/json"
	"fmt"
	"lgd-litestat/analysis"
	"net/http"
	"sync"
)

// StreamResult represents a single line in NDJSON stream
type StreamResult struct {
	EquipmentID string      `json:"equipment_id"`
	Result      interface{} `json:"result,omitempty"` // Analysis results or error
	Error       string      `json:"error,omitempty"`
}

// AnalyzeStream handles batch analysis with server-side streaming (NDJSON)
func (h *Handler) AnalyzeStream(w http.ResponseWriter, r *http.Request) {
	// 1. Parse Request
	var req analysis.BatchAnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate (similar to AnalyzeBatch)
	if req.DefectName == "" || req.StartDate == "" || req.EndDate == "" || len(req.Targets) == 0 {
		respondError(w, http.StatusBadRequest, "defect_name, start_date, end_date, and targets are required")
		return
	}

	// 2. Set Streaming Headers
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Important for Nginx

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// 3. Concurrency Setup
	targets := req.Targets
	numWorkers := 5 // Limit max concurrency (SIMD-like parallelism)
	if len(targets) < numWorkers {
		numWorkers = len(targets)
	}

	jobs := make(chan analysis.AnalysisTarget, len(targets))
	results := make(chan StreamResult, len(targets))
	var wg sync.WaitGroup

	// 4. Start Workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for target := range jobs {

				batchReqSingle := analysis.BatchAnalysisRequest{
					DefectName: req.DefectName,
					StartDate:  req.StartDate,
					EndDate:    req.EndDate,
					Targets:    []analysis.AnalysisTarget{target},
				}

				resMap, err := h.analyzer.AnalyzeBatch(batchReqSingle)

				if err != nil {
					results <- StreamResult{
						EquipmentID: target.EquipmentID,
						Error:       err.Error(),
					}
				} else {
					// Extract the single result
					if res, ok := resMap[target.EquipmentID]; ok {
						results <- StreamResult{
							EquipmentID: target.EquipmentID,
							Result:      res,
						}
					} else {
						results <- StreamResult{
							EquipmentID: target.EquipmentID,
							Error:       "no result returned",
						}
					}
				}
			}
		}(i)
	}

	// 5. Enqueue Jobs
	go func() {
		for _, t := range targets {
			jobs <- t
		}
		close(jobs)
		wg.Wait()
		close(results) // Close results only when workers are done
	}()

	// 6. Stream Results to Client
	encoder := json.NewEncoder(w)
	for res := range results {
		if err := encoder.Encode(res); err != nil {
			fmt.Printf("Stream encode error: %v\n", err)
			return // Client likely disconnected
		}
		flusher.Flush()

		// Optional: Small delay if needed for UI smoothness / rate limit, but streaming is normally instant
		// time.Sleep(10 * time.Millisecond)
	}
}
