// Package service contains the extraction orchestration logic. It depends
// only on module/port interfaces, never on concrete adapters.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"asana_extractor/internal/extractor/port"
)

// Extractor lists users and projects from an AsanaClient and persists each
// one individually via a Writer.
type Extractor struct {
	client port.AsanaClient
	writer port.Writer
}

// NewExtractor builds an Extractor. Logging uses slog.Default(); callers
// that want cycle summaries routed through a specific handler should call
// slog.SetDefault before running cycles (see cmd/backednsvc).
func NewExtractor(client port.AsanaClient, writer port.Writer) *Extractor {
	return &Extractor{client: client, writer: writer}
}

// Run lists users and projects for workspaceGID and writes each one. A
// single entity's write failure does not abort the run: failures are
// collected and returned together via errors.Join so one bad record never
// hides the rest.
func (e *Extractor) Run(ctx context.Context, workspaceGID string) error {
	var errs []error

	users, err := e.client.ListUsers(ctx, workspaceGID)
	if err != nil {
		errs = append(errs, fmt.Errorf("list users: %w", err))
	}
	usersWritten := 0
	for _, u := range users {
		if err := e.writer.WriteUser(u); err != nil {
			errs = append(errs, fmt.Errorf("write user %s: %w", u.GID, err))
		} else {
			usersWritten++
		}
	}

	projects, err := e.client.ListProjects(ctx, workspaceGID)
	if err != nil {
		errs = append(errs, fmt.Errorf("list projects: %w", err))
	}
	projectsWritten := 0
	for _, p := range projects {
		if err := e.writer.WriteProject(p); err != nil {
			errs = append(errs, fmt.Errorf("write project %s: %w", p.GID, err))
		} else {
			projectsWritten++
		}
	}

	slog.Info("extraction cycle summary",
		"workspace", workspaceGID,
		"users_fetched", len(users),
		"users_written", usersWritten,
		"projects_fetched", len(projects),
		"projects_written", projectsWritten,
		"errors", len(errs),
	)

	return errors.Join(errs...)
}
