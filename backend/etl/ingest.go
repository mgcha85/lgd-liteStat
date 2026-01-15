package etl

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"lgd-litestat/config"
	"lgd-litestat/database"
)

// DataIngestor handles data ingestion from source systems
type DataIngestor struct {
	config *config.Config
	repo   *database.Repository
}

// NewDataIngestor creates a new data ingestor
func NewDataIngestor(cfg *config.Config, repo *database.Repository) *DataIngestor {
	return &DataIngestor{
		config: cfg,
		repo:   repo,
	}
}

// IngestData ingests data from source systems for a given time range
func (d *DataIngestor) IngestData(startTime, endTime time.Time) (map[string]int, error) {
	counts := make(map[string]int)

	// For now, we'll use mock data if enabled
	if d.config.MockData.Enabled {
		return d.ingestMockData()
	}

	// TODO: Implement real data ingestion from source system
	// This would involve:
	// 1. Query source database using templated queries
	// 2. Transform the data
	// 3. Insert into DuckDB

	return counts, fmt.Errorf("real data ingestion not yet implemented - please use mock data mode")
}

// ingestMockData generates and inserts mock data
func (d *DataIngestor) ingestMockData() (map[string]int, error) {
	generator := NewMockDataGenerator(&d.config.MockData)

	// Generate inspection data
	inspectionData := generator.GenerateInspectionData()
	if err := d.repo.BulkInsertInspection(inspectionData); err != nil {
		return nil, fmt.Errorf("failed to insert inspection data: %w", err)
	}

	// Generate history data
	historyData := generator.GenerateHistoryData()
	if err := d.repo.BulkInsertHistory(historyData); err != nil {
		return nil, fmt.Errorf("failed to insert history data: %w", err)
	}

	return map[string]int{
		"inspection": len(inspectionData),
		"history":    len(historyData),
	}, nil
}

// TransformInspection transforms raw inspection data
// Computes defect_name from term_name and handles coordinates
func TransformInspection(raw map[string]interface{}) (database.InspectionRow, error) {
	row := database.InspectionRow{}

	// Extract fields
	// Helper: ProductID from product_id or glass_id (legacy)
	if v, ok := raw["product_id"].(string); ok {
		row.ProductID = v
	} else if v, ok := raw["glass_id"].(string); ok { // Legacy support
		row.ProductID = v
	}

	if v, ok := raw["panel_id"].(string); ok {
		row.PanelID = v
	}
	if v, ok := raw["model_code"].(string); ok {
		row.ModelCode = v
	}

	// Term Name logic: Source Term -> Derived DefectName
	if v, ok := raw["def_latest_summary_defect_term_name_s"].(string); ok {
		row.DefectLatestSummaryDefectTermNameS = v
		// Extract defect_name (elements 2 and 4, i.e., indices 1 and 3)
		row.DefectName = extractDefectName(v)
	} else if v, ok := raw["term_name"].(string); ok { // Legacy
		row.DefectLatestSummaryDefectTermNameS = v
		row.DefectName = extractDefectName(v)
	}

	// Panel Address Logic: panel_addr = panel_id - product_id
	if row.PanelID != "" && row.ProductID != "" {
		if strings.HasPrefix(row.PanelID, row.ProductID) {
			row.PanelAddr = strings.TrimPrefix(row.PanelID, row.ProductID)
			if len(row.PanelAddr) > 0 {
				// X is all but last char, Y is last char
				row.PanelX = row.PanelAddr[:len(row.PanelAddr)-1]
				row.PanelY = row.PanelAddr[len(row.PanelAddr)-1:]
			}
		} else {
			// Fallback if ID doesn't match expected pattern
			row.PanelAddr = row.PanelID
		}
	}

	if v, ok := raw["process_code"].(string); ok {
		row.ProcessCode = v
	}

	// Inspection Time
	if v, ok := raw["inspection_end_ymdhms"].(time.Time); ok {
		row.InspectionEndYmdhms = v
	} else if vStr, ok := raw["inspection_end_ymdhms"].(string); ok {
		// Try parsing if string
		if t, err := time.Parse(time.RFC3339, vStr); err == nil {
			row.InspectionEndYmdhms = t
		}
	}

	// X/Y Coordinates
	// Ensure float32 conversion from float64 (json default) or string
	if v, ok := raw["def_pnt_x"].(float64); ok {
		row.DefPntX = float32(v)
	}
	if v, ok := raw["def_pnt_y"].(float64); ok {
		row.DefPntY = float32(v)
	}

	// G/D Integers
	if v, ok := raw["def_pnt_g"].(float64); ok {
		row.DefPntG = uint32(v)
	}
	if v, ok := raw["def_pnt_d"].(float64); ok {
		row.DefPntD = uint32(v)
	}
	if v, ok := raw["def_size"].(float64); ok {
		row.DefSize = float32(v)
	}

	return row, nil
}

// DeduplicateHistory keeps only the last occurrence per product+process+equipment
func DeduplicateHistory(history []database.HistoryRow) []database.HistoryRow {
	// Build a map with composite key
	type key struct {
		productID string
		process   string
		equipment string
	}

	latest := make(map[key]database.HistoryRow)

	for _, row := range history {
		k := key{
			productID: row.ProductID,
			process:   row.ProcessCode,
			equipment: row.EquipmentLineID,
		}

		// Keep the one with latest time (seq_num removed/less relevant if strict time available)
		if existing, exists := latest[k]; exists {
			if row.MoveInYmdhms.After(existing.MoveInYmdhms) {
				latest[k] = row
			}
		} else {
			latest[k] = row
		}
	}

	// Convert map back to slice
	result := make([]database.HistoryRow, 0, len(latest))
	for _, row := range latest {
		result = append(result, row)
	}

	return result
}

// executeTemplateQuery executes a query template with parameters
func executeTemplateQuery(queryTemplate string, params map[string]interface{}) (string, error) {
	tmpl, err := template.New("query").Parse(queryTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse query template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("failed to execute query template: %w", err)
	}

	return buf.String(), nil
}

// extractDefectName extracts elements 2 and 4 from term_name (helper function)
func extractDefectName(termName string) string {
	parts := strings.Split(termName, "-")
	// Expected format: TYPE-DEFECT-SIZE-REASON (e.g., TYPE1-SPOT-SIZE-DARK)
	// We want parts[1] (SPOT) and parts[3] (DARK) -> SPOT-DARK
	if len(parts) < 4 {
		return termName
	}
	return parts[1] + "-" + parts[3]
}
