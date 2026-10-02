package env

import (
	"testing"
	"time"
)

func TestLoad_RequiresToken(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "")
	t.Setenv("ASANA_WORKSPACE_GID", "ws1")

	_, err := NewLoader().Load()
	if err == nil {
		t.Fatal("expected error when ASANA_TOKEN is missing")
	}
}

func TestLoad_RequiresWorkspace(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "tok")
	t.Setenv("ASANA_WORKSPACE_GID", "")

	_, err := NewLoader().Load()
	if err == nil {
		t.Fatal("expected error when ASANA_WORKSPACE_GID is missing")
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "tok")
	t.Setenv("ASANA_WORKSPACE_GID", "ws1")
	t.Setenv("OUTPUT_DIR", "")
	t.Setenv("EXTRACT_INTERVAL", "")
	t.Setenv("CYCLE_TIMEOUT", "")

	cfg, err := NewLoader().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.OutputDir != defaultOutputDir {
		t.Errorf("expected default output dir %q, got %q", defaultOutputDir, cfg.OutputDir)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("expected default interval %s, got %s", defaultInterval, cfg.Interval)
	}
	if cfg.CycleTimeout != defaultCycleTimeout {
		t.Errorf("expected default cycle timeout %s, got %s", defaultCycleTimeout, cfg.CycleTimeout)
	}
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "tok")
	t.Setenv("ASANA_WORKSPACE_GID", "ws1")
	t.Setenv("OUTPUT_DIR", "/tmp/custom")
	t.Setenv("EXTRACT_INTERVAL", "30s")
	t.Setenv("CYCLE_TIMEOUT", "90s")

	cfg, err := NewLoader().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.OutputDir != "/tmp/custom" {
		t.Errorf("expected output dir /tmp/custom, got %q", cfg.OutputDir)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("expected interval 30s, got %s", cfg.Interval)
	}
	if cfg.CycleTimeout != 90*time.Second {
		t.Errorf("expected cycle timeout 90s, got %s", cfg.CycleTimeout)
	}
}

func TestLoad_InvalidInterval(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "tok")
	t.Setenv("ASANA_WORKSPACE_GID", "ws1")
	t.Setenv("EXTRACT_INTERVAL", "not-a-duration")

	_, err := NewLoader().Load()
	if err == nil {
		t.Fatal("expected error for invalid EXTRACT_INTERVAL")
	}
}

func TestLoad_InvalidCycleTimeout(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "tok")
	t.Setenv("ASANA_WORKSPACE_GID", "ws1")
	t.Setenv("CYCLE_TIMEOUT", "not-a-duration")

	_, err := NewLoader().Load()
	if err == nil {
		t.Fatal("expected error for invalid CYCLE_TIMEOUT")
	}
}

func TestLoad_TrimsWhitespaceFromValues(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "  tok\n")
	t.Setenv("ASANA_WORKSPACE_GID", "\tws1 \n")
	t.Setenv("OUTPUT_DIR", " /tmp/custom \n")
	t.Setenv("EXTRACT_INTERVAL", " 30s\n")
	t.Setenv("CYCLE_TIMEOUT", "\n90s\t")

	cfg, err := NewLoader().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.AsanaToken != "tok" {
		t.Errorf("expected trimmed token %q, got %q", "tok", cfg.AsanaToken)
	}
	if cfg.WorkspaceGID != "ws1" {
		t.Errorf("expected trimmed workspace gid %q, got %q", "ws1", cfg.WorkspaceGID)
	}
	if cfg.OutputDir != "/tmp/custom" {
		t.Errorf("expected trimmed output dir %q, got %q", "/tmp/custom", cfg.OutputDir)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("expected trimmed interval 30s, got %s", cfg.Interval)
	}
	if cfg.CycleTimeout != 90*time.Second {
		t.Errorf("expected trimmed cycle timeout 90s, got %s", cfg.CycleTimeout)
	}
}

func TestLoad_ZeroIntervalRejected(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "tok")
	t.Setenv("ASANA_WORKSPACE_GID", "ws1")
	t.Setenv("EXTRACT_INTERVAL", "0s")

	_, err := NewLoader().Load()
	if err == nil {
		t.Fatal("expected error for zero EXTRACT_INTERVAL")
	}
}

func TestLoad_NegativeIntervalRejected(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "tok")
	t.Setenv("ASANA_WORKSPACE_GID", "ws1")
	t.Setenv("EXTRACT_INTERVAL", "-5m")

	_, err := NewLoader().Load()
	if err == nil {
		t.Fatal("expected error for negative EXTRACT_INTERVAL")
	}
}

func TestLoad_ZeroCycleTimeoutRejected(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "tok")
	t.Setenv("ASANA_WORKSPACE_GID", "ws1")
	t.Setenv("CYCLE_TIMEOUT", "0s")

	_, err := NewLoader().Load()
	if err == nil {
		t.Fatal("expected error for zero CYCLE_TIMEOUT")
	}
}

func TestLoad_NegativeCycleTimeoutRejected(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "tok")
	t.Setenv("ASANA_WORKSPACE_GID", "ws1")
	t.Setenv("CYCLE_TIMEOUT", "-1m")

	_, err := NewLoader().Load()
	if err == nil {
		t.Fatal("expected error for negative CYCLE_TIMEOUT")
	}
}

func TestLoad_WhitespaceOnlyTokenStillRequired(t *testing.T) {
	t.Setenv("ASANA_TOKEN", "   \n")
	t.Setenv("ASANA_WORKSPACE_GID", "ws1")

	_, err := NewLoader().Load()
	if err == nil {
		t.Fatal("expected error when ASANA_TOKEN is whitespace-only")
	}
}
