// Package service contains the extraction orchestration logic. It depends
// only on module/port interfaces, never on concrete adapters.
package service

import (
	"context"
	"errors"
	"fmt"

	"asana_extractor/internal/extractor/port"
)

// Extractor lists users and projects from an AsanaClient and persists each
// one individually via a Writer.
type Extractor struct {
	client port.AsanaClient
	writer port.Writer
}

// NewExtractor builds an Extractor.
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
	for _, u := range users {
		if err := e.writer.WriteUser(u); err != nil {
			errs = append(errs, fmt.Errorf("write user %s: %w", u.GID, err))
		}
	}

	projects, err := e.client.ListProjects(ctx, workspaceGID)
	if err != nil {
		errs = append(errs, fmt.Errorf("list projects: %w", err))
	}
	for _, p := range projects {
		if err := e.writer.WriteProject(p); err != nil {
			errs = append(errs, fmt.Errorf("write project %s: %w", p.GID, err))
		}
	}

	return errors.Join(errs...)
}
