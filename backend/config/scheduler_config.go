package config

// SchedulerConfig holds scheduler settings
type SchedulerConfig struct {
	Enabled         bool `mapstructure:"enabled" json:"enabled"`
	IntervalMinutes int  `mapstructure:"interval_minutes" json:"interval_minutes"`
}
