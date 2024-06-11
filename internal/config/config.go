package config

import "time"

type DaemonConfig struct {
	Mode bool
	Port uint
}

type DatabaseConfig struct {
	PruneMaxAge time.Duration
}

type OllamaConfig struct {
	Cutoff time.Duration
	Enable bool
	Model  string
}

type Config struct {
	Daemon        DaemonConfig
	Database      DatabaseConfig
	Ollama        OllamaConfig
	DbDirname     string
	MaxAgeHours   uint
	NoRefreshMode bool
	OpmlFilename  string
	OutputMode    int
	RefreshTicker time.Duration
}
