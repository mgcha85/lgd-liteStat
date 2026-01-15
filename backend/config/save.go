package config

import (
	"sync"

	"github.com/spf13/viper"
)

var configMutex sync.Mutex

// UpdateAnalysisSettings updates analysis settings and saves to file
func (c *Config) UpdateAnalysisSettings(topN, defaultPage, maxPage int) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	c.Analysis.TopNLimit = topN
	c.Analysis.DefaultPageSize = defaultPage
	c.Analysis.MaxPageSize = maxPage

	viper.Set("analysis.top_n_limit", topN)
	viper.Set("analysis.default_page_size", defaultPage)
	viper.Set("analysis.max_page_size", maxPage)

	return viper.WriteConfig()
}

// UpdateDefectTerms updates the list of defect terms
func (c *Config) UpdateDefectTerms(terms []string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	c.Settings.DefectTerms = terms
	viper.Set("settings.defect_terms", terms)

	return viper.WriteConfig()
}
