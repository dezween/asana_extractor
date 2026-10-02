// Package domain holds the plain data types extracted from Asana.
package domain

// User represents an Asana workspace member.
type User struct {
	GID   string `json:"gid"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Project represents an Asana project.
type Project struct {
	GID          string `json:"gid"`
	Name         string `json:"name"`
	WorkspaceGID string `json:"workspace_gid"`
}
