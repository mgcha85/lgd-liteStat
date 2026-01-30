package database

import "time"

// GlassResult represents glass-level analysis result
type GlassResult struct {
	GlassID      string `json:"glass_id"`
	LotID        string `json:"lot_id"`
	WorkDate     string `json:"work_date"`
	TotalDefects int    `json:"total_defects"`
	GroupType    string `json:"group_type"` // "Target" or "Others"
}

// LotResult represents lot-level aggregation
type LotResult struct {
	LotID        string  `json:"lot_id"`
	GroupType    string  `json:"group_type"`
	GlassCount   int     `json:"glass_count"`
	TotalDefects int     `json:"total_defects"`
	AvgDefects   float64 `json:"avg_defects"`
	MaxDefects   int     `json:"max_defects"`
}

// DailyResult represents daily time series
type DailyResult struct {
	WorkDate     string  `json:"work_date"`
	GroupType    string  `json:"group_type"`
	GlassCount   int     `json:"glass_count"`
	TotalDefects int     `json:"total_defects"`
	AvgDefects   float64 `json:"avg_defects"`
}

// AnalysisLog represents a job execution log
type AnalysisLog struct {
	ID           string    `json:"id"`
	Facility     string    `json:"facility"`
	Status       string    `json:"status"`
	DefectName   string    `json:"defect_name"`
	StartDate    string    `json:"start_date"`
	EndDate      string    `json:"end_date"`
	TargetCount  int       `json:"target_count"`
	GlassCount   int       `json:"glass_count"` // Same as TotalGlasses?
	TotalDefects int       `json:"total_defects"`
	TotalGlasses int       `json:"total_glasses"`
	DurationMs   int64     `json:"duration_ms"`
	RequestTime  time.Time `json:"request_time"` // Added for repo
}

// HeatmapCell represents a cell in the heatmap
type HeatmapCell struct {
	GroupType    string  `json:"group_type"` // "Target" or "Others"
	X            string  `json:"x"`
	Y            string  `json:"y"`
	DefectRate   float64 `json:"defect_rate"`
	TotalDefects int     `json:"total_defects"`
	TotalGlasses int     `json:"total_glasses"`
}

// AnalysisMetrics contains summary statistics
type AnalysisMetrics struct {
	TargetTotalDefects       int     `json:"target_total_defects"`
	TargetGlassCount         int     `json:"target_glass_count"` // Renamed from TargetTotalGlasses
	TargetAvgDefectsPerGlass float64 `json:"target_avg_defects_per_glass"`
	OthersTotalDefects       int     `json:"others_total_defects"`
	OthersGlassCount         int     `json:"others_glass_count"` // Renamed from OthersTotalGlasses
	OthersAvgDefectsPerGlass float64 `json:"others_avg_defects_per_glass"`
	OverallDefectRate        float64 `json:"overall_defect_rate"`
	TargetDefectRate         float64 `json:"target_defect_rate"`
	OthersDefectRate         float64 `json:"others_defect_rate"`
	Delta                    float64 `json:"delta"`                 // overall - target
	SuperiorityIndicator     float64 `json:"superiority_indicator"` // positive if target < others
}

// ---------------------------------------------------------
// Analysis V2 Structs (Hierarchy Support)
// ---------------------------------------------------------

// AnalysisParamsV2 defines the request body for hierarchy analysis
type AnalysisParamsV2 struct {
	Facility           string   `json:"facility"`            // Required
	Start              string   `json:"start"`               // YYYY-MM-DD
	End                string   `json:"end"`                 // YYYY-MM-DD
	ProductIDs         []string `json:"product_ids"`         // Optional (If set, ignores Date/Model)
	DefectName         string   `json:"defect_name"`         // Optional (Filter specific defect)
	ModelCode          string   `json:"model_code"`          // Required if ProductIDs empty
	ProcessCode        string   `json:"process_code"`        // Optional
	EquipmentLineID    string   `json:"equipment_line_id"`    // Optional
	EquipmentMachineID string   `json:"equipment_machine_id"` // Optional
	EquipmentPathID    string   `json:"equipment_path_id"`    // Optional
	AnalysisLevel      string   `json:"analysis_level"`      // "line", "machine", "path" (Default: determine by params)
}

// HierarchyResult represents the aggregated result for a specific hierarchy node
type HierarchyResult struct {
	ProcessCode        string  `json:"process_code"`
	EquipmentLineID    string  `json:"equipment_line_id,omitempty"`
	EquipmentMachineID string  `json:"equipment_machine_id,omitempty"`
	EquipmentPathID    string  `json:"equipment_path_id,omitempty"`
	
	TotalDefects       int     `json:"total_defects"`
	TotalProducts      int     `json:"total_products"`
	DPU                float64 `json:"dpu"`
	PanelMap           []int   `json:"panel_map"`   // Aggregated Defect Map
	PanelAddrs         []string `json:"panel_addrs"` // Reference Addrs (Full Grid)
	
	// Daily DPU Trend
	DailyDPU []DailyDPUItem `json:"daily_dpu"`
}

type DailyDPUItem struct {
	WorkDate string  `json:"work_date"`
	DPU      float64 `json:"dpu"`
}
