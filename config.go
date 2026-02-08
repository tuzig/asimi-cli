package main

import (
	"github.com/afittestide/asimi/internal/config"
)

// Type aliases - use types from internal/config as the single source of truth
type (
	Config        = config.Config
	StorageConfig = config.StorageConfig
	LoggingConfig = config.LoggingConfig
	LLMConfig     = config.LLMConfig
	HistoryConfig = config.HistoryConfig
	UIConfig      = config.UIConfig
	SessionConfig = config.SessionConfig
	SandboxConfig = config.SandboxConfig
	Mount         = config.Mount
)
