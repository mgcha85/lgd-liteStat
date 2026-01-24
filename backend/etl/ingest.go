package etl

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"lgd-litestat/config"
	"lgd-litestat/database"

	_ "github.com/lib/pq" // Postgres driver
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
func (d *DataIngestor) IngestData(startTime, endTime time.Time, facilities []string, targets []string) (map[string]int, error) {
	counts := make(map[string]int)

	if len(facilities) == 0 {
		facilities = d.config.Settings.Facilities
		if len(facilities) == 0 {
			facilities = []string{"default"}
		}
	}

	// Determine targets
	doHistory := true
	doInspection := true
	if len(targets) > 0 {
		doHistory = false
		doInspection = false
		for _, t := range targets {
			if t == "history" {
				doHistory = true
			}
			if t == "inspection" {
				doInspection = true
			}
		}
	}

	totalInspection := 0
	totalHistory := 0

	// Connect to DB once if Real Mode
	var sourceDB *sql.DB
	var err error
	isRealMode := !d.config.MockData.Enabled

	if isRealMode {
		sourceDB, err = d.connectSourceDB()
		if err != nil {
			return nil, fmt.Errorf("failed to connect to source db: %w", err)
		}
		defer sourceDB.Close()
	}

	for _, fac := range facilities {
		// Determine Time Range per Facility
		t1, t2 := startTime, endTime

		// Auto-Incremental Mode
		if t1.IsZero() {
			latest, err := d.repo.GetLatestImportTimestamp(fac)
			if err != nil {
				fmt.Printf("Warning: Failed to get latest timestamp for %s: %v. Defaulting to full load.\n", fac, err)
			}
			if !latest.IsZero() {
				t1 = latest.Add(time.Second)
			} else {
				days := d.config.MockData.TimeRangeDays
				if days == 0 {
					days = 30
				}
				t1 = time.Now().AddDate(0, 0, -days)
			}
		}
		if t2.IsZero() {
			t2 = time.Now()
		}

		fmt.Printf("[%s] Ingesting data from %s to %s\n", fac, t1.Format(time.RFC3339), t2.Format(time.RFC3339))

		if !isRealMode {
			// Mock Mode
			c, err := d.ingestMockData(fac)
			if err != nil {
				return nil, err
			}
			if doInspection {
				totalInspection += c["inspection"]
			}
			if doHistory {
				totalHistory += c["history"]
			}
			continue
		}

		// REAL MODE
		// 1. Ingest History
		if doHistory {
			hCount, err := d.ingestHistory(sourceDB, fac, t1, t2)
			if err != nil {
				fmt.Printf("Error ingesting history for %s: %v\n", fac, err)
			}
			totalHistory += hCount
		}

		// 2. Ingest Inspection
		if doInspection {
			iCount, err := d.ingestInspection(sourceDB, fac, t1, t2)
			if err != nil {
				fmt.Printf("Error ingesting inspection for %s: %v\n", fac, err)
			}
			totalInspection += iCount
		}
	}

	counts["inspection"] = totalInspection
	counts["history"] = totalHistory
	return counts, nil
}

// ingestHistory handles real history data ingestion
func (d *DataIngestor) ingestHistory(db *sql.DB, facility string, t1, t2 time.Time) (int, error) {
	cols := d.config.Ingest.HistoryColumns
	if len(cols) == 0 {
		return 0, fmt.Errorf("no history columns configured")
	}

	colStr := strings.Join(cols, ", ")

	hasFacilityCol := false
	for _, c := range cols {
		if c == "facility_code" {
			hasFacilityCol = true
			break
		}
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE time_ymdhms BETWEEN $1 AND $2", colStr, d.config.Ingest.HistoryTable)
	args := []interface{}{t1, t2}

	if hasFacilityCol {
		query += " AND facility_code = $3"
		args = append(args, facility)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var historyData []database.HistoryRow

	// Scan logic
	values := make([]interface{}, len(cols))
	valuePtrs := make([]interface{}, len(cols))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		var row database.HistoryRow
		for i, colName := range cols {
			valStr := toString(values[i])
			switch colName {
			case "glass_id":
				row.ProductID = valStr
			case "product_id":
				row.ProductID = valStr
			case "factory_code":
				row.FactoryCode = valStr
			case "process_code":
				row.ProcessCode = valStr
			case "time_ymdhms":
				if t, ok := values[i].(time.Time); ok {
					row.MoveInYmdhms = t
				}
			case "equipment_hierarchy_type_code":
				row.EquipmentHierarchyTypeCode = valStr
			case "equipment_line_id":
				row.EquipmentLineID = valStr
				row.EquipmentID = valStr
			case "equipment_machine_id":
				row.EquipmentMachineID = valStr
			case "equipment_unit_id":
				row.EquipmentUnitID = valStr
			case "equipment_path_id":
				row.EquipmentPathID = valStr
			}
		}
		if row.ProductID != "" {
			historyData = append(historyData, row)
		}
	}

	if len(historyData) > 0 {
		if err := d.repo.BulkInsertHistory(historyData, facility); err != nil {
			return 0, err
		}
		fmt.Printf("Ingested %d history rows for %s\n", len(historyData), facility)
	}
	return len(historyData), nil
}

// ingestInspection handles real inspection data ingestion
func (d *DataIngestor) ingestInspection(db *sql.DB, facility string, t1, t2 time.Time) (int, error) {
	cols := d.config.Ingest.InspectionColumns
	if len(cols) == 0 {
		return 0, fmt.Errorf("no inspection columns configured")
	}

	colStr := strings.Join(cols, ", ")

	hasFacilityCol := false
	for _, c := range cols {
		if c == "facility_code" {
			hasFacilityCol = true
			break
		}
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE inspection_end_ymdhms BETWEEN $1 AND $2", colStr, d.config.Ingest.InspectionTable)
	args := []interface{}{t1, t2}

	if hasFacilityCol {
		query += " AND facility_code = $3"
		args = append(args, facility)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var inspectionData []database.InspectionRow
	values := make([]interface{}, len(cols))
	valuePtrs := make([]interface{}, len(cols))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		var row database.InspectionRow
		for i, colName := range cols {
			val := values[i]
			valStr := toString(val)

			switch colName {
			case "product_id":
				row.ProductID = valStr
			case "facility_code":
				row.FacilityCode = valStr
			case "process_code":
				row.ProcessCode = valStr
			case "inspection_end_ymdhms":
				if t, ok := val.(time.Time); ok {
					row.InspectionEndYmdhms = t
				}
			case "def_pnt_x":
				row.DefPntX = toFloat32(val)
			case "def_pnt_y":
				row.DefPntY = toFloat32(val)
			case "def_pnt_g":
				row.DefPntG = uint32(toFloat32(val))
			case "def_pnt_d":
				row.DefPntD = uint32(toFloat32(val))
			case "def_size":
				row.DefSize = toFloat32(val)
			case "def_latest_summary_defect_term_name_s":
				row.DefectLatestSummaryDefectTermNameS = valStr
				row.DefectName = extractDefectName(valStr)
			}
		}
		if row.ProductID != "" {
			inspectionData = append(inspectionData, row)
		}
	}

	if len(inspectionData) > 0 {
		if err := d.repo.BulkInsertInspection(inspectionData, facility); err != nil {
			return 0, err
		}
		fmt.Printf("Ingested %d inspection rows for %s\n", len(inspectionData), facility)
	}
	return len(inspectionData), nil
}

// ingestMockData generates and inserts mock data
func (d *DataIngestor) ingestMockData(facility string) (map[string]int, error) {
	generator := NewMockDataGenerator(&d.config.MockData)

	// Generate inspection data
	inspectionData := generator.GenerateInspectionData()
	if err := d.repo.BulkInsertInspection(inspectionData, facility); err != nil {
		return nil, fmt.Errorf("failed to insert inspection data for %s: %w", facility, err)
	}

	// Generate history data
	historyData := generator.GenerateHistoryData()
	if err := d.repo.BulkInsertHistory(historyData, facility); err != nil {
		return nil, fmt.Errorf("failed to insert history data for %s: %w", facility, err)
	}

	return map[string]int{
		"inspection": len(inspectionData),
		"history":    len(historyData),
	}, nil
}

// Helpers
func toString(val interface{}) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toFloat32(val interface{}) float32 {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return float32(v)
	case float32:
		return v
	case int64:
		return float32(v)
	case int:
		return float32(v)
	default:
		return 0
	}
}

func extractDefectName(termName string) string {
	parts := strings.Split(termName, "-")
	// Expected format: TYPE-DEFECT-SIZE-REASON (e.g., TYPE1-SPOT-SIZE-DARK)
	// We want parts[1] (SPOT) and parts[3] (DARK) -> SPOT-DARK
	if len(parts) < 4 {
		return termName
	}
	return parts[1] + "-" + parts[3]
}

// connectSourceDB establishes connection to source PostgreSQL
func (d *DataIngestor) connectSourceDB() (*sql.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		d.config.SourceDBHost, d.config.SourceDBPort, d.config.SourceDBUser,
		d.config.SourceDBPassword, d.config.SourceDBName)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}
