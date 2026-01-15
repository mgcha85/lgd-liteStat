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
// Computes defect_name from term_name and panel_addr from panel_id
func TransformInspection(raw map[string]interface{}) (database.InspectionRow, error) {
	row := database.InspectionRow{}

	// Extract fields
	if v, ok := raw["glass_id"].(string); ok {
		row.GlassID = v
	}
	if v, ok := raw["panel_id"].(string); ok {
		row.PanelID = v
	}
	if v, ok := raw["product_id"].(string); ok {
		row.ProductID = v
	}
	if v, ok := raw["term_name"].(string); ok {
		row.TermName = v
		// Extract defect_name (elements 2 and 4)
		row.DefectName = extractDefectName(v)
	}
	if v, ok := raw["process_code"].(string); ok {
		row.ProcessCode = v
	}
	if v, ok := raw["defect_count"].(int); ok {
		row.DefectCount = v
	}
	if v, ok := raw["inspection_end_ymdhms"].(time.Time); ok {
		row.InspectionEndYmdhms = v
	}

	// Compute panel_addr
	if row.PanelID != "" && row.ProductID != "" {
		row.PanelAddr = strings.TrimPrefix(row.PanelID, row.ProductID)
	}

	return row, nil
}

// DeduplicateHistory keeps only the last occurrence per glass+process+equipment
func DeduplicateHistory(history []database.HistoryRow) []database.HistoryRow {
	// Build a map with composite key
	type key struct {
		glassID   string
		process   string
		equipment string
	}

	latest := make(map[key]database.HistoryRow)

	for _, row := range history {
		k := key{
			glassID:   row.GlassID,
			process:   row.ProcessCode,
			equipment: row.EquipmentLineID,
		}

		// Keep the one with highest seq_num, or latest time if seq_num is same
		if existing, exists := latest[k]; exists {
			if row.SeqNum > existing.SeqNum ||
				(row.SeqNum == existing.SeqNum && row.TimekeyYmdhms.After(existing.TimekeyYmdhms)) {
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
	if len(parts) < 4 {
		return termName
	}
	return parts[1] + "-" + parts[3]
}
