// Package config loads PGScope configuration from flags, environment, and YAML.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	EnvDatabaseURL = "DATABASE_URL"
	EnvPGScopeDSN  = "PGSCOPE_DSN"
)

// Config holds runtime settings for PGScope collectors and CLI.
type Config struct {
	DSN                   string        `yaml:"dsn" json:"dsn,omitempty"`
	Format                string        `yaml:"format" json:"format"`
	Timeout               time.Duration `yaml:"timeout" json:"timeout"`
	ConnectTimeout        time.Duration `yaml:"connect_timeout" json:"connect_timeout"`
	StatementTimeout      time.Duration `yaml:"statement_timeout" json:"statement_timeout"`
	LockTimeout           time.Duration `yaml:"lock_timeout" json:"lock_timeout"`
	MaxConns              int32         `yaml:"max_conns" json:"max_conns"`
	MinConns              int32         `yaml:"min_conns" json:"min_conns"`
	LongQueryThreshold    time.Duration `yaml:"long_query_threshold" json:"long_query_threshold"`
	LongXactThreshold     time.Duration `yaml:"long_xact_threshold" json:"long_xact_threshold"`
	TopRelations          int           `yaml:"top_relations" json:"top_relations"`
	TopIndexes            int           `yaml:"top_indexes" json:"top_indexes"`
	TopQueries            int           `yaml:"top_queries" json:"top_queries"`
	TopVacuumTables       int           `yaml:"top_vacuum_tables" json:"top_vacuum_tables"`
	QueryPreviewLen       int           `yaml:"query_preview_len" json:"query_preview_len"`
	QuerySort             string        `yaml:"query_sort" json:"query_sort"`
	CollectQueryText      bool          `yaml:"collect_query_text" json:"collect_query_text"`
	SensitiveDataSafe     bool          `yaml:"sensitive_data_safe" json:"sensitive_data_safe"`
	DeepScan              bool          `yaml:"deep_scan" json:"deep_scan"`
	EnableQueries         bool          `yaml:"enable_queries" json:"enable_queries"`
	EnableVacuum          bool          `yaml:"enable_vacuum" json:"enable_vacuum"`
	EnableWAL             bool          `yaml:"enable_wal" json:"enable_wal"`
	EnableReplication     bool          `yaml:"enable_replication" json:"enable_replication"`
	EnableIndexes         bool          `yaml:"enable_indexes" json:"enable_indexes"`
	EnableHypoPG          bool          `yaml:"enable_hypopg" json:"enable_hypopg"`
	Color                 bool          `yaml:"color" json:"color"`
	Verbose               bool          `yaml:"verbose" json:"verbose"`
	ReadOnly              bool          `yaml:"read_only" json:"read_only"`
	ConfigPath            string        `yaml:"-" json:"-"`

	// Phase 4
	Monitoring MonitoringConfig `yaml:"monitoring" json:"monitoring"`
	History    HistoryConfig    `yaml:"history" json:"history"`
	Regression RegressionConfig `yaml:"regression" json:"regression"`
	Incidents  IncidentsConfig  `yaml:"incidents" json:"incidents"`
	Correlation CorrelationConfig `yaml:"correlation" json:"correlation"`
}

// MonitoringConfig controls continuous collection.
type MonitoringConfig struct {
	Enabled                 bool          `yaml:"enabled" json:"enabled"`
	DefaultInterval         time.Duration `yaml:"default_interval" json:"default_interval"`
	CollectionTimeout       time.Duration `yaml:"collection_timeout" json:"collection_timeout"`
	MaxConcurrentCollectors int           `yaml:"max_concurrent_collectors" json:"max_concurrent_collectors"`
}

// HistoryConfig controls local historical persistence.
type HistoryConfig struct {
	Enabled   bool              `yaml:"enabled" json:"enabled"`
	Backend   string            `yaml:"backend" json:"backend"` // sqlite
	Path      string            `yaml:"path" json:"path"`
	Retention RetentionConfig   `yaml:"retention" json:"retention"`
	StoreQueryText bool         `yaml:"store_query_text" json:"store_query_text"`
}

// RetentionConfig defines cleanup windows.
type RetentionConfig struct {
	RawMetrics        time.Duration `yaml:"raw_metrics" json:"raw_metrics"`
	AggregatedMetrics time.Duration `yaml:"aggregated_metrics" json:"aggregated_metrics"`
	Incidents         time.Duration `yaml:"incidents" json:"incidents"`
}

// RegressionConfig controls baseline comparison.
type RegressionConfig struct {
	Enabled          bool          `yaml:"enabled" json:"enabled"`
	BaselineWindow   time.Duration `yaml:"baseline_window" json:"baseline_window"`
	EvaluationWindow time.Duration `yaml:"evaluation_window" json:"evaluation_window"`
	MinSamples       int           `yaml:"min_samples" json:"min_samples"`
	Sensitivity      string        `yaml:"sensitivity" json:"sensitivity"` // low|medium|high
}

// IncidentsConfig controls incident lifecycle.
type IncidentsConfig struct {
	Enabled              bool          `yaml:"enabled" json:"enabled"`
	DeduplicationWindow  time.Duration `yaml:"deduplication_window" json:"deduplication_window"`
	AutoResolveAfter     time.Duration `yaml:"auto_resolve_after" json:"auto_resolve_after"`
}

// CorrelationConfig bounds the evidence graph.
type CorrelationConfig struct {
	Enabled                 bool `yaml:"enabled" json:"enabled"`
	MaxEvidenceNodes        int  `yaml:"max_evidence_nodes" json:"max_evidence_nodes"`
	MaxHypothesesPerIncident int `yaml:"max_hypotheses_per_incident" json:"max_hypotheses_per_incident"`
}

// Default returns production-safe defaults.
func Default() Config {
	return Config{
		Format:             "text",
		Timeout:            30 * time.Second,
		ConnectTimeout:     10 * time.Second,
		StatementTimeout:   15 * time.Second,
		LockTimeout:        2 * time.Second,
		MaxConns:           4,
		MinConns:           0,
		LongQueryThreshold: 5 * time.Minute,
		LongXactThreshold:  5 * time.Minute,
		TopRelations:       10,
		TopIndexes:         15,
		TopQueries:         20,
		TopVacuumTables:    25,
		QueryPreviewLen:    120,
		QuerySort:          "total-time",
		CollectQueryText:   true,
		SensitiveDataSafe:  true,
		DeepScan:           false,
		EnableQueries:      true,
		EnableVacuum:       true,
		EnableWAL:          true,
		EnableReplication:  true,
		EnableIndexes:      true,
		EnableHypoPG:       false,
		Color:              true,
		Verbose:            false,
		ReadOnly:           true,
		Monitoring: MonitoringConfig{
			Enabled: false, DefaultInterval: 30 * time.Second,
			CollectionTimeout: 10 * time.Second, MaxConcurrentCollectors: 3,
		},
		History: HistoryConfig{
			Enabled: false, Backend: "sqlite", Path: "",
			Retention: RetentionConfig{
				RawMetrics: 7 * 24 * time.Hour,
				AggregatedMetrics: 90 * 24 * time.Hour,
				Incidents: 365 * 24 * time.Hour,
			},
			StoreQueryText: false,
		},
		Regression: RegressionConfig{
			Enabled: true, BaselineWindow: time.Hour, EvaluationWindow: 10 * time.Minute,
			MinSamples: 5, Sensitivity: "medium",
		},
		Incidents: IncidentsConfig{
			Enabled: true, DeduplicationWindow: 10 * time.Minute, AutoResolveAfter: 15 * time.Minute,
		},
		Correlation: CorrelationConfig{
			Enabled: true, MaxEvidenceNodes: 1000, MaxHypothesesPerIncident: 10,
		},
	}
}

// LoadFile merges YAML configuration from path into cfg.
func LoadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	cfg.ConfigPath = path
	return nil
}

// ResolveDSN picks DSN from explicit value, then DATABASE_URL, then PGSCOPE_DSN.
func ResolveDSN(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if v := os.Getenv(EnvDatabaseURL); v != "" {
		return v, nil
	}
	if v := os.Getenv(EnvPGScopeDSN); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("database connection string required: set --dsn, %s, or %s", EnvDatabaseURL, EnvPGScopeDSN)
}

// ApplyDSN sets cfg.DSN after resolving environment defaults.
func (c *Config) ApplyDSN(explicit string) error {
	dsn, err := ResolveDSN(explicit)
	if err != nil {
		return err
	}
	c.DSN = dsn
	return nil
}

// Validate checks configuration constraints.
func (c *Config) Validate() error {
	if c.MaxConns < 1 {
		return fmt.Errorf("max_conns must be >= 1")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be > 0")
	}
	if c.StatementTimeout <= 0 {
		return fmt.Errorf("statement_timeout must be > 0")
	}
	switch c.Format {
	case "text", "json", "yaml":
	default:
		return fmt.Errorf("unsupported format %q (want text, json, or yaml)", c.Format)
	}
	if c.History.Enabled {
		switch c.History.Backend {
		case "", "sqlite":
			c.History.Backend = "sqlite"
		default:
			return fmt.Errorf("unsupported history.backend %q", c.History.Backend)
		}
	}
	switch c.Regression.Sensitivity {
	case "", "low", "medium", "high":
		if c.Regression.Sensitivity == "" {
			c.Regression.Sensitivity = "medium"
		}
	default:
		return fmt.Errorf("unsupported regression.sensitivity %q", c.Regression.Sensitivity)
	}
	if c.Monitoring.Enabled {
		if c.Monitoring.DefaultInterval < time.Second {
			return fmt.Errorf("monitoring.default_interval must be >= 1s")
		}
		if c.Monitoring.MaxConcurrentCollectors < 1 {
			return fmt.Errorf("monitoring.max_concurrent_collectors must be >= 1")
		}
	}
	return nil
}

// ResolveHistoryPath expands empty/default history path to ~/.pgscope/history.db.
func (c *Config) ResolveHistoryPath() (string, error) {
	p := c.History.Path
	if p == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve history path: %w", err)
		}
		p = home + "/.pgscope/history.db"
	}
	if len(p) >= 2 && p[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = home + p[1:]
	}
	return p, nil
}
