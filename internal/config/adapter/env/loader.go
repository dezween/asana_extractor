// Package env implements port.Loader by reading configuration from
// environment variables.
package env

import (
	"fmt"
	"os"
	"strings"
	"time"

	"asana_extractor/internal/config/domain"
)

const (
	defaultOutputDir    = "./output"
	defaultInterval     = 5 * time.Minute
	defaultCycleTimeout = 2 * time.Minute

	// Names of the environment variables this loader reads.
	envAsanaToken       = "ASANA_TOKEN"
	envAsanaWorkspaceID = "ASANA_WORKSPACE_GID"
	envOutputDir        = "OUTPUT_DIR"
	envExtractInterval  = "EXTRACT_INTERVAL"
	envCycleTimeout     = "CYCLE_TIMEOUT"
)

// Loader reads Config fields from os.Getenv.
type Loader struct{}

// NewLoader constructs an env-backed config loader.
func NewLoader() *Loader {
	return &Loader{}
}

// Load reads ASANA_TOKEN, ASANA_WORKSPACE_GID, OUTPUT_DIR,
// EXTRACT_INTERVAL and CYCLE_TIMEOUT from the environment. ASANA_TOKEN and
// ASANA_WORKSPACE_GID are required; the rest fall back to defaults. Every
// value is trimmed of leading/trailing whitespace before use, since values
// sourced from a `.env` file commonly pick up a trailing newline or stray
// space from an editor.
func (l *Loader) Load() (*domain.Config, error) {
	token := strings.TrimSpace(os.Getenv(envAsanaToken))
	if token == "" {
		return nil, fmt.Errorf("env: %s is required", envAsanaToken)
	}

	workspaceGID := strings.TrimSpace(os.Getenv(envAsanaWorkspaceID))
	if workspaceGID == "" {
		return nil, fmt.Errorf("env: %s is required", envAsanaWorkspaceID)
	}

	outputDir := strings.TrimSpace(os.Getenv(envOutputDir))
	if outputDir == "" {
		outputDir = defaultOutputDir
	}

	interval := defaultInterval
	if raw := strings.TrimSpace(os.Getenv(envExtractInterval)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("env: invalid %s %q: %w", envExtractInterval, raw, err)
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("env: %s must be positive, got %q", envExtractInterval, raw)
		}
		interval = parsed
	}

	cycleTimeout := defaultCycleTimeout
	if raw := strings.TrimSpace(os.Getenv(envCycleTimeout)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("env: invalid %s %q: %w", envCycleTimeout, raw, err)
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("env: %s must be positive, got %q", envCycleTimeout, raw)
		}
		cycleTimeout = parsed
	}

	return &domain.Config{
		AsanaToken:   token,
		WorkspaceGID: workspaceGID,
		OutputDir:    outputDir,
		Interval:     interval,
		CycleTimeout: cycleTimeout,
	}, nil
}
