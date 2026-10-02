package service

import (
	"context"
	"errors"
	"testing"

	"asana_extractor/internal/extractor/domain"
	"asana_extractor/internal/extractor/port/mocks"

	"github.com/stretchr/testify/mock"
)

func TestExtractorRun_WritesAllEntities(t *testing.T) {
	client := mocks.NewMockAsanaClient(t)
	writer := mocks.NewMockWriter(t)

	users := []domain.User{
		{GID: "u1", Name: "Alice", Email: "alice@example.com"},
		{GID: "u2", Name: "Bob", Email: "bob@example.com"},
	}
	projects := []domain.Project{
		{GID: "p1", Name: "Project One", WorkspaceGID: "ws1"},
	}

	client.EXPECT().ListUsers(mock.Anything, "ws1").Return(users, nil).Once()
	client.EXPECT().ListProjects(mock.Anything, "ws1").Return(projects, nil).Once()
	writer.EXPECT().WriteUser(users[0]).Return(nil).Once()
	writer.EXPECT().WriteUser(users[1]).Return(nil).Once()
	writer.EXPECT().WriteProject(projects[0]).Return(nil).Once()

	e := NewExtractor(client, writer)
	if err := e.Run(context.Background(), "ws1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestExtractorRun_CollectsListErrors(t *testing.T) {
	client := mocks.NewMockAsanaClient(t)
	writer := mocks.NewMockWriter(t)

	usersErr := errors.New("users api down")
	projectsErr := errors.New("projects api down")

	client.EXPECT().ListUsers(mock.Anything, "ws1").Return(nil, usersErr).Once()
	client.EXPECT().ListProjects(mock.Anything, "ws1").Return(nil, projectsErr).Once()

	e := NewExtractor(client, writer)
	err := e.Run(context.Background(), "ws1")
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	if !errors.Is(err, usersErr) {
		t.Errorf("expected joined error to include users error: %v", err)
	}
	if !errors.Is(err, projectsErr) {
		t.Errorf("expected joined error to include projects error: %v", err)
	}
}

func TestExtractorRun_ContinuesPastSingleWriteFailure(t *testing.T) {
	client := mocks.NewMockAsanaClient(t)
	writer := mocks.NewMockWriter(t)

	users := []domain.User{{GID: "u1", Name: "Alice"}, {GID: "u2", Name: "Bob"}}

	client.EXPECT().ListUsers(mock.Anything, "ws1").Return(users, nil).Once()
	client.EXPECT().ListProjects(mock.Anything, "ws1").Return(nil, nil).Once()
	writer.EXPECT().WriteUser(users[0]).Return(errors.New("simulated write failure")).Once()
	writer.EXPECT().WriteUser(users[1]).Return(nil).Once()

	e := NewExtractor(client, writer)
	err := e.Run(context.Background(), "ws1")

	if err == nil {
		t.Fatal("expected error reported for failed write")
	}
}

func TestExtractorRun_WritesPartialResultsAlongsideListError(t *testing.T) {
	client := mocks.NewMockAsanaClient(t)
	writer := mocks.NewMockWriter(t)

	// Simulate a client that successfully fetched some pages before a
	// later page failed (per WR-01): the partial slice is returned
	// alongside the error, and Run must still write every entity that was
	// fetched instead of discarding them because of the list error.
	partialUsers := []domain.User{
		{GID: "u1", Name: "Alice", Email: "alice@example.com"},
		{GID: "u2", Name: "Bob", Email: "bob@example.com"},
	}
	partialProjects := []domain.Project{
		{GID: "p1", Name: "Project One", WorkspaceGID: "ws1"},
	}
	usersErr := errors.New("page 3 of users failed")
	projectsErr := errors.New("page 2 of projects failed")

	client.EXPECT().ListUsers(mock.Anything, "ws1").Return(partialUsers, usersErr).Once()
	client.EXPECT().ListProjects(mock.Anything, "ws1").Return(partialProjects, projectsErr).Once()
	writer.EXPECT().WriteUser(partialUsers[0]).Return(nil).Once()
	writer.EXPECT().WriteUser(partialUsers[1]).Return(nil).Once()
	writer.EXPECT().WriteProject(partialProjects[0]).Return(nil).Once()

	e := NewExtractor(client, writer)
	err := e.Run(context.Background(), "ws1")

	if err == nil {
		t.Fatal("expected aggregated error from the list failures")
	}
	if !errors.Is(err, usersErr) {
		t.Errorf("expected joined error to include users list error: %v", err)
	}
	if !errors.Is(err, projectsErr) {
		t.Errorf("expected joined error to include projects list error: %v", err)
	}
	// Mock expectations above already assert WriteUser/WriteProject were
	// called for every partial entity (mockery mocks fail the test via
	// t.Cleanup if an expected call never happens).
}

func TestExtractorRun_NoErrorsReturnsNil(t *testing.T) {
	client := mocks.NewMockAsanaClient(t)
	writer := mocks.NewMockWriter(t)

	client.EXPECT().ListUsers(mock.Anything, "ws1").Return(nil, nil).Once()
	client.EXPECT().ListProjects(mock.Anything, "ws1").Return(nil, nil).Once()

	e := NewExtractor(client, writer)
	if err := e.Run(context.Background(), "ws1"); err != nil {
		t.Fatalf("expected nil error for empty lists, got %v", err)
	}
}

func TestExtractorRun_TableDrivenWriteFailures(t *testing.T) {
	tests := []struct {
		name           string
		users          []domain.User
		projects       []domain.Project
		failUserGID    string
		failProjectGID string
		wantErr        bool
	}{
		{
			name:     "all succeed",
			users:    []domain.User{{GID: "u1"}},
			projects: []domain.Project{{GID: "p1"}},
			wantErr:  false,
		},
		{
			name:        "user write fails",
			users:       []domain.User{{GID: "u1"}},
			failUserGID: "u1",
			wantErr:     true,
		},
		{
			name:           "project write fails",
			projects:       []domain.Project{{GID: "p1"}},
			failProjectGID: "p1",
			wantErr:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := mocks.NewMockAsanaClient(t)
			writer := mocks.NewMockWriter(t)

			client.EXPECT().ListUsers(mock.Anything, "ws1").Return(tc.users, nil).Once()
			client.EXPECT().ListProjects(mock.Anything, "ws1").Return(tc.projects, nil).Once()

			for _, u := range tc.users {
				var err error
				if u.GID == tc.failUserGID {
					err = errors.New("simulated write failure")
				}
				writer.EXPECT().WriteUser(u).Return(err).Once()
			}
			for _, p := range tc.projects {
				var err error
				if p.GID == tc.failProjectGID {
					err = errors.New("simulated write failure")
				}
				writer.EXPECT().WriteProject(p).Return(err).Once()
			}

			e := NewExtractor(client, writer)
			err := e.Run(context.Background(), "ws1")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
