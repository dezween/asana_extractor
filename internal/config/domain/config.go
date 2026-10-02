package domain

import "time"

// Config holds all runtime configuration for the extractor.
type Config struct {
	AsanaToken   string
	WorkspaceGID string
	OutputDir    string
	Interval     time.Duration

	// CycleTimeout bounds a single extraction cycle (ListUsers + all
	// writes + ListProjects + all writes). This assumes the workspace's
	// full user+project pagination can complete within this budget; a
	// workspace large enough to need many sequential page fetches could
	// exceed it, in which case the in-flight cycle is cancelled (and, per
	// the Asana client's partial-result behavior, whatever pages were
	// already fetched are still written) and the next scheduled cycle
	// simply retries from the beginning.
	CycleTimeout time.Duration
}
