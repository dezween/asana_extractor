// Package port declares the interfaces the extraction service depends on.
package port

import (
	"context"

	"asana_extractor/internal/extractor/domain"
)

// AsanaClient retrieves entities from the Asana API.
type AsanaClient interface {
	ListUsers(ctx context.Context, workspaceGID string) ([]domain.User, error)
	ListProjects(ctx context.Context, workspaceGID string) ([]domain.Project, error)
}

// Writer persists extracted entities, one file per entity.
type Writer interface {
	WriteUser(domain.User) error
	WriteProject(domain.Project) error
}
