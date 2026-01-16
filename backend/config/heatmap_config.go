package config

import (
	"encoding/json"
	"os"
	"sync"
)

// HeatmapGridConfig defines X and Y axis labels for a specific model
type HeatmapGridConfig struct {
	XList []string `json:"x_list"`
	YList []string `json:"y_list"`
}

// HeatmapConfigManager manages heatmap grid configurations
type HeatmapConfigManager struct {
	configPath string
	mu         sync.RWMutex
	Configs    map[string]HeatmapGridConfig `json:"configs"` // Map ModelCode -> GridConfig
}

// NewHeatmapConfigManager creates a new manager
func NewHeatmapConfigManager(path string) *HeatmapConfigManager {
	return &HeatmapConfigManager{
		configPath: path,
		Configs:    make(map[string]HeatmapGridConfig),
	}
}

// Load reads the config from disk
func (m *HeatmapConfigManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize empty file if not exists
			m.Configs = make(map[string]HeatmapGridConfig)
			return m.saveInternal()
		}
		return err
	}

	if len(data) == 0 {
		m.Configs = make(map[string]HeatmapGridConfig)
		return nil
	}

	return json.Unmarshal(data, &m.Configs)
}

// Save writes the config to disk
func (m *HeatmapConfigManager) Save(configs map[string]HeatmapGridConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Configs = configs
	return m.saveInternal()
}

// saveInternal writes to disk (must hold lock)
func (m *HeatmapConfigManager) saveInternal() error {
	data, err := json.MarshalIndent(m.Configs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.configPath, data, 0644)
}

// GetConfig returns config for a model
func (m *HeatmapConfigManager) GetConfig(modelCode string) (HeatmapGridConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.Configs[modelCode]
	return cfg, ok
}

// GetAll returns all configs
func (m *HeatmapConfigManager) GetAll() map[string]HeatmapGridConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return copy
	copy := make(map[string]HeatmapGridConfig)
	for k, v := range m.Configs {
		copy[k] = v
	}
	return copy
}
