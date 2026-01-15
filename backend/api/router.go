package api

import (
	"net/http"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

// SetupRouter creates and configures the HTTP router
func SetupRouter(h *Handler) *mux.Router {
	r := mux.NewRouter()

	// Health check
	r.HandleFunc("/health", h.HealthCheck).Methods("GET")
	r.HandleFunc("/api/health", h.HealthCheck).Methods("GET")

	// Data management endpoints
	r.HandleFunc("/api/ingest", h.IngestData).Methods("POST")
	r.HandleFunc("/api/mart/refresh", h.RefreshMart).Methods("POST")
	r.HandleFunc("/api/cleanup", h.CleanupData).Methods("POST")

	// Data query endpoints
	r.HandleFunc("/api/inspection", h.GetInspectionData).Methods("GET")
	r.HandleFunc("/api/history", h.GetHistoryData).Methods("GET")

	// Config Management
	r.HandleFunc("/api/config", h.GetConfig).Methods("GET")
	r.HandleFunc("/api/config", h.UpdateConfig).Methods("PUT")

	// Analysis endpoints
	// Create a subrouter for /api/analyze paths
	analyzeRouter := r.PathPrefix("/api/analyze").Subrouter()
	analyzeRouter.HandleFunc("", h.RequestAnalysis).Methods("POST") // Matches /api/analyze
	analyzeRouter.HandleFunc("/batch", h.AnalyzeBatch).Methods("POST")
	analyzeRouter.HandleFunc("/stream", h.AnalyzeStream).Methods("POST")
	analyzeRouter.HandleFunc("/{jobId}/status", h.GetAnalysisStatus).Methods("GET")
	analyzeRouter.HandleFunc("/{jobId}/results", h.GetAnalysisResults).Methods("GET")

	// Equipment rankings (for frontend dashboard)
	r.HandleFunc("/api/equipment/rankings", h.GetEquipmentRankings).Methods("GET")

	return r
}

// CORSMiddleware adds CORS headers
func CORSMiddleware() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return handlers.CORS(
			handlers.AllowedOrigins([]string{"*"}),
			handlers.AllowedMethods([]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}),
			handlers.AllowedHeaders([]string{"Content-Type", "Authorization"}),
		)(next)
	}
}

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)

			// Log request
			println(
				time.Now().Format("2006-01-02 15:04:05"),
				r.Method,
				r.RequestURI,
				wrapped.statusCode,
				duration.String(),
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
