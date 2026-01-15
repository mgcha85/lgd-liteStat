package etl

import (
	"fmt"
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

// GenerateInspectionData generates mock inspection data
func (m *MockDataGenerator) GenerateInspectionData() []database.InspectionRow {
	rows := make([]database.InspectionRow, 0, m.config.InspectionRecords)
	startDate := time.Now().AddDate(0, 0, -m.config.TimeRangeDays)

	// Generate glass IDs (about 70% of history records should have inspection data)
	glassIDCount := m.config.HistoryRecords

	for i := 0; i < m.config.InspectionRecords; i++ {
		glassID := fmt.Sprintf("G%08d", i%glassIDCount)
		product := m.config.Products[m.rand.Intn(len(m.config.Products))]
		panelSuffix := fmt.Sprintf("%s%d",
			string(rune('A'+m.rand.Intn(26))),
			m.rand.Intn(10))
		panelID := product + panelSuffix

		// Extract defect_name from term_name (elements 2 and 4)
		termName := m.config.DefectTerms[m.rand.Intn(len(m.config.DefectTerms))]
		defectName := extractDefectName(termName)

		// Random time within range
		randomDays := m.rand.Intn(m.config.TimeRangeDays)
		randomHours := m.rand.Intn(24)
		randomMinutes := m.rand.Intn(60)
		inspectionTime := startDate.AddDate(0, 0, randomDays).
			Add(time.Hour * time.Duration(randomHours)).
			Add(time.Minute * time.Duration(randomMinutes))

		// Random defect count (most have 1-3, some have more)
		defectCount := 1
		if m.rand.Float32() < 0.3 {
			defectCount = m.rand.Intn(5) + 1
		}

		row := database.InspectionRow{
			GlassID:             glassID,
			PanelID:             panelID,
			ProductID:           product,
			PanelAddr:           panelSuffix,
			TermName:            termName,
			DefectName:          defectName,
			InspectionEndYmdhms: inspectionTime,
			ProcessCode:         m.config.Processes[m.rand.Intn(len(m.config.Processes))],
			DefectCount:         defectCount,
		}

		rows = append(rows, row)
	}

	return rows
}

// GenerateHistoryData generates mock history data
func (m *MockDataGenerator) GenerateHistoryData() []database.HistoryRow {
	rows := make([]database.HistoryRow, 0, m.config.HistoryRecords)
	startDate := time.Now().AddDate(0, 0, -m.config.TimeRangeDays)

	// Generate unique glass IDs
	glassIDCount := m.config.HistoryRecords / 3 // Each glass goes through ~3 processes on average

	for glassIdx := 0; glassIdx < glassIDCount; glassIdx++ {
		glassID := fmt.Sprintf("G%08d", glassIdx)
		product := m.config.Products[m.rand.Intn(len(m.config.Products))]
		lotID := fmt.Sprintf("LOT%06d", glassIdx/30) // 30 glasses per lot

		// Each glass goes through 2-5 processes
		processCount := 2 + m.rand.Intn(4)
		baseTime := startDate.AddDate(0, 0, m.rand.Intn(m.config.TimeRangeDays))

		for processIdx := 0; processIdx < processCount; processIdx++ {
			processCode := m.config.Processes[m.rand.Intn(len(m.config.Processes))]
			equipment := m.config.Equipments[m.rand.Intn(len(m.config.Equipments))]

			// Process time increments for each step
			processTime := baseTime.Add(time.Hour * time.Duration(processIdx*2))

			// Some glasses may go through same equipment multiple times
			// This tests deduplication logic
			seqNum := 1
			if m.rand.Float32() < 0.1 { // 10% chance of duplicate
				seqNum = 2
				row1 := database.HistoryRow{
					GlassID:         glassID,
					ProductID:       product,
					LotID:           lotID,
					EquipmentLineID: equipment,
					ProcessCode:     processCode,
					TimekeyYmdhms:   processTime.Add(-time.Hour),
					SeqNum:          1,
				}
				rows = append(rows, row1)
			}

			row := database.HistoryRow{
				GlassID:         glassID,
				ProductID:       product,
				LotID:           lotID,
				EquipmentLineID: equipment,
				ProcessCode:     processCode,
				TimekeyYmdhms:   processTime,
				SeqNum:          seqNum,
			}

			rows = append(rows, row)
		}
	}

	return rows
}
