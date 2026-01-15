package etl

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"lgd-litestat/config"
	"lgd-litestat/database"
)

// MockDataGenerator generates realistic mock data for testing
type MockDataGenerator struct {
	config *config.MockDataConfig
	rand   *rand.Rand
}

// NewMockDataGenerator creates a new mock data generator
func NewMockDataGenerator(cfg *config.MockDataConfig) *MockDataGenerator {
	return &MockDataGenerator{
		config: cfg,
		rand:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// GenerateInspectionData generates mock inspection data with dynamic time-series trends
func (m *MockDataGenerator) GenerateInspectionData() []database.InspectionRow {
	var rows []database.InspectionRow
	startDate := time.Now().AddDate(0, 0, -m.config.TimeRangeDays)

	// History ID range
	productIDCount := m.config.HistoryRecords

	// Iterate Day by Day to control the Trend Shape
	// Use a sine wave to create "Seasons" or "Waves" of defect rates
	for day := 0; day < m.config.TimeRangeDays; day++ {
		// Trend Function: Base + Sine(Day) + Noise
		// Period of ~60 days (2*PI / 0.1 ~= 62)
		// Amplitude +/- 20, Base 40. Range approx 20-60 defects/day.
		trend := float64(day) * 0.1
		dailyBase := 40.0 + (20.0 * math.Sin(trend))
		noise := (m.rand.Float64() * 20.0) - 10.0
		dailyCount := int(dailyBase + noise)
		if dailyCount < 5 {
			dailyCount = 5 // Minimum floor
		}

		currentDate := startDate.AddDate(0, 0, day)

		for i := 0; i < dailyCount; i++ {
			// Random Product ID
			productID := fmt.Sprintf("G%08d", m.rand.Intn(productIDCount))
			productModel := m.config.Products[m.rand.Intn(len(m.config.Products))]

			// Panel Address Logic: PanelID = ProductID + Suffix (Addr)
			// Suffix e.g. "A1"
			// PanelAddr should be the suffix itself for logic consistency with Ingest
			// Or should PanelAddr be the suffix?
			// Ingest logic: PanelAddr = PanelID - ProductID.
			// So if PanelID = ProductID + "A1", PanelAddr = "A1".
			// PanelX = "A", PanelY = "1".

			suffix := fmt.Sprintf("%s%d",
				string(rune('A'+m.rand.Intn(26))),
				m.rand.Intn(10))
			panelID := productID + suffix
			panelAddr := suffix
			panelX := string(suffix[:len(suffix)-1])
			panelY := string(suffix[len(suffix)-1:])

			// Extract defect_name from term_name (elements 2 and 4)
			termName := m.config.DefectTerms[m.rand.Intn(len(m.config.DefectTerms))]
			defectName := extractDefectName(termName)

			// Random hour/minute within the day
			inspectionTime := currentDate.Add(time.Hour * time.Duration(m.rand.Intn(24))).
				Add(time.Minute * time.Duration(m.rand.Intn(60)))

			// Random coordinates and values
			defPntX := m.rand.Float32() * 100.0
			defPntY := m.rand.Float32() * 100.0
			defPntG := uint32(m.rand.Intn(100))
			defPntD := uint32(m.rand.Intn(10))
			defSize := m.rand.Float32() * 5.0

			row := database.InspectionRow{
				ProductID:                          productID, // Was GlassID
				PanelID:                            panelID,
				ModelCode:                          productModel,
				DefectLatestSummaryDefectTermNameS: termName,   // Source Term
				DefectName:                         defectName, // Derived
				InspectionEndYmdhms:                inspectionTime,
				ProcessCode:                        m.config.Processes[m.rand.Intn(len(m.config.Processes))],
				DefPntX:                            defPntX,
				DefPntY:                            defPntY,
				DefPntG:                            defPntG,
				DefPntD:                            defPntD,
				DefSize:                            defSize,
				PanelAddr:                          panelAddr,
				PanelX:                             panelX,
				PanelY:                             panelY,
			}

			rows = append(rows, row)
		}
		if day%100 == 0 {
			fmt.Printf("Generated Mock Data Day %d/%d\n", day, m.config.TimeRangeDays)
		}
	}

	return rows
}

// GenerateHistoryData generates mock history data
func (m *MockDataGenerator) GenerateHistoryData() []database.HistoryRow {
	rows := make([]database.HistoryRow, 0, m.config.HistoryRecords)
	startDate := time.Now().AddDate(0, 0, -m.config.TimeRangeDays)

	// Generate unique product IDs
	productIDCount := m.config.HistoryRecords / 3 // Each unit goes through ~3 processes on average

	for productIdx := 0; productIdx < productIDCount; productIdx++ {
		productID := fmt.Sprintf("G%08d", productIdx)
		model := m.config.Products[m.rand.Intn(len(m.config.Products))]
		lotID := fmt.Sprintf("LOT%06d", productIdx/30) // 30 units per lot

		// Each unit goes through 2-5 processes
		processCount := 2 + m.rand.Intn(4)
		baseTime := startDate.AddDate(0, 0, m.rand.Intn(m.config.TimeRangeDays))

		for processIdx := 0; processIdx < processCount; processIdx++ {
			processCode := m.config.Processes[m.rand.Intn(len(m.config.Processes))]
			equipment := m.config.Equipments[m.rand.Intn(len(m.config.Equipments))]

			// Process time increments for each step
			processTime := baseTime.Add(time.Hour * time.Duration(processIdx*2))

			row := database.HistoryRow{
				ProductID:       productID,
				ProductTypeCode: model, // Mapping product to product_type_code
				LotID:           lotID,
				EquipmentLineID: equipment,
				ProcessCode:     processCode,
				MoveInYmdhms:    processTime,
			}

			rows = append(rows, row)
		}
	}

	return rows
}

// RunMockGeneration orchestrates mock data generation and insertion
func RunMockGeneration(repo *database.Repository, cfg *config.Config) error {
	fmt.Println("Generating mock data...")

	defectTerms := cfg.Settings.DefectTerms
	if len(defectTerms) == 0 {
		defectTerms = []string{"TYPE1-SPOT-SIZE-DARK", "TYPE2-SCRATCH-LEN-LIGHT", "TYPE3-MURA-AREA-DIM"}
	}

	// Default Mock Config
	mockCfg := &config.MockDataConfig{
		HistoryRecords:    5000,
		InspectionRecords: 20000,
		TimeRangeDays:     400, // Increased to cover > 1 year
		Products:          []string{"PD-OLED-55", "PD-OLED-65", "PD-LCD-27"},
		Equipments:        []string{"EQ-dep-01", "EQ-dep-02", "EQ-photo-01", "EQ-etch-01"},
		Processes:         []string{"1000", "2000", "3000"},
		DefectTerms:       defectTerms,
	}

	generator := NewMockDataGenerator(mockCfg)
	// Force startDate to be relative to fixed "Current Date" (2026-01-16) to ensure coverage
	// Actually, NewMockDataGenerator uses time.Now() inside, so we need to rely on system time being correct (2026)
	// But let's verify GenerateInspectionData uses config.TimeRangeDays correctly.
	// It does: startDate := time.Now().AddDate(0, 0, -m.config.TimeRangeDays)
	// System time is 2026-01-16. So -400 days covers late 2024 to early 2026. This is perfect.

	// 1. History
	historyData := generator.GenerateHistoryData()
	fmt.Printf("Generated %d history records\n", len(historyData))
	if err := repo.BulkInsertHistory(historyData); err != nil {
		return fmt.Errorf("failed to insert history: %w", err)
	}

	// 2. Inspection
	inspectionData := generator.GenerateInspectionData()
	fmt.Printf("Generated %d inspection records\n", len(inspectionData))
	if err := repo.BulkInsertInspection(inspectionData); err != nil {
		return fmt.Errorf("failed to insert inspection: %w", err)
	}

	fmt.Println("Mock data generation complete!")
	return nil
}
