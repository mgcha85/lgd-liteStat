package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Config holds all configuration for the application
type Config struct {
	// Database
	DBPath string

	// Source System
	SourceDBHost          string
	SourceDBPort          int
	SourceDBName          string
	SourceDBUser          string
	SourceDBPassword      string
	SourceInspectionTable string
	SourceHistoryTable    string

	// API Server
	APIPort string
	APIHost string

	// Logging
	LogLevel string

	// Data Retention
	DataRetentionDays int

	// Worker Pool
	WorkerPoolSize int

	// Cache
	CacheTTLHours int

	// Queries from YAML
	Queries QueryConfig

	// Analysis parameters
	Analysis AnalysisConfig

	// UI Settings (Defect lists, etc)
	Settings SettingsConfig `mapstructure:"settings"`

	// Display Mappings (Process names)
	ProcessMappings map[string]string `mapstructure:"process_mappings"`

	// Equipment Groups
	EquipmentGroups map[string][]string `mapstructure:"equipment_groups"`

	// Mock data settings
	MockData MockDataConfig `mapstructure:"mock_data"`

	// Heatmap Config Manager
	HeatmapManager *HeatmapConfigManager

	// Scheduler
	Scheduler SchedulerConfig `mapstructure:"scheduler"`

	// Retention
	Retention RetentionConfig `mapstructure:"retention"`
}

// RetentionConfig holds data retention settings
type RetentionConfig struct {
	AnalysisDays int    `mapstructure:"analysis_days"`
	DataDays     int    `mapstructure:"data_days"`
	CleanupTime  string `mapstructure:"cleanup_time"` // Format: "15:04"
}

// SettingsConfig holds UI-controllable settings
type SettingsConfig struct {
	DefectTerms []string `mapstructure:"defect_terms"`
	Facilities  []string `mapstructure:"facilities"`
}

// QueryConfig holds SQL query templates
type QueryConfig struct {
	Inspection string `mapstructure:"inspection"`
	History    string `mapstructure:"history"`
}

// AnalysisConfig holds analysis parameters
type AnalysisConfig struct {
	TopNLimit       int `mapstructure:"top_n_limit"`
	DefaultPageSize int `mapstructure:"default_page_size"`
	MaxPageSize     int `mapstructure:"max_page_size"`
}

// MockDataConfig holds mock data generation settings
type MockDataConfig struct {
	Enabled           bool     `mapstructure:"enabled"`
	InspectionRecords int      `mapstructure:"inspection_records"`
	HistoryRecords    int      `mapstructure:"history_records"`
	TimeRangeDays     int      `mapstructure:"time_range_days"`
	Products          []string `mapstructure:"products"`
	Processes         []string `mapstructure:"processes"`
	Equipments        []string `mapstructure:"equipments"`
	DefectTerms       []string `mapstructure:"defect_terms"`
}

// LoadConfig loads configuration from .env and config.yaml
func LoadConfig() (*Config, error) {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		// .env file is optional, only warn
		fmt.Println("Warning: .env file not found, using environment variables")
	}

	// Load YAML configuration
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("..")
	viper.AddConfigPath("../..") // For when running from subdirectories

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config.yaml: %w", err)
	}

	config := &Config{
		// Load from environment variables
		DBPath:                getEnv("DB_PATH", "./data/analytics.duckdb"),
		SourceDBHost:          getEnv("SOURCE_DB_HOST", "localhost"),
		SourceDBPort:          getEnvAsInt("SOURCE_DB_PORT", 5432),
		SourceDBName:          getEnv("SOURCE_DB_NAME", "manufacturing_db"),
		SourceDBUser:          getEnv("SOURCE_DB_USER", "etl_user"),
		SourceDBPassword:      getEnv("SOURCE_DB_PASSWORD", ""),
		SourceInspectionTable: getEnv("SOURCE_INSPECTION_TABLE", "inspection_log"),
		SourceHistoryTable:    getEnv("SOURCE_HISTORY_TABLE", "process_history"),
		APIPort:               getEnv("API_PORT", "8080"),
		APIHost:               getEnv("API_HOST", "0.0.0.0"),
		LogLevel:              getEnv("LOG_LEVEL", "info"),
		DataRetentionDays:     getEnvAsInt("DATA_RETENTION_DAYS", 365),
		WorkerPoolSize:        getEnvAsInt("WORKER_POOL_SIZE", 4),
		CacheTTLHours:         getEnvAsInt("CACHE_TTL_HOURS", 24),
	}

	// Load from YAML
	if err := viper.UnmarshalKey("queries", &config.Queries); err != nil {
		return nil, fmt.Errorf("failed to unmarshal queries: %w", err)
	}

	if err := viper.UnmarshalKey("analysis", &config.Analysis); err != nil {
		return nil, fmt.Errorf("failed to unmarshal analysis config: %w", err)
	}

	if err := viper.UnmarshalKey("settings", &config.Settings); err != nil {
		// Optional, strict error check maybe not needed
	}
	if err := viper.UnmarshalKey("process_mappings", &config.ProcessMappings); err != nil {
	}
	if err := viper.UnmarshalKey("equipment_groups", &config.EquipmentGroups); err != nil {
	}

	if err := viper.UnmarshalKey("mock_data", &config.MockData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mock_data config: %w", err)
	}

	// Initialize Heatmap Config Manager
	config.HeatmapManager = NewHeatmapConfigManager("config_heatmap.json") // Save in root
	if err := config.HeatmapManager.Load(); err != nil {
		fmt.Printf("Warning: Failed to load heatmap config: %v\n", err)
	}

	// Validate required fields
	if config.DBPath == "" {
		return nil, fmt.Errorf("DB_PATH is required")
	}

	return config, nil
}

// getEnv reads an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt reads an environment variable as int or returns a default value
func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}
