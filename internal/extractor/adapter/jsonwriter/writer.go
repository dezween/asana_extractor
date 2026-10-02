// Package jsonwriter implements port.Writer by writing each entity to its
// own pretty-printed JSON file on the local filesystem.
package jsonwriter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"asana_extractor/internal/extractor/domain"
)

const (
	usersDir    = "users"
	projectsDir = "projects"
	dirPerm     = 0o755
	filePerm    = 0o644

	// timestampFormat sorts lexicographically in chronological order, so a
	// directory listing naturally groups each extraction cycle together.
	timestampFormat = "20060102T150405.000000000Z"
)

// validGID matches the expected shape of an Asana GID. Asana GIDs are
// normally API-assigned decimal strings, but this charset is intentionally
// a bit more permissive (letters, digits, underscore, hyphen) while still
// excluding anything that could be interpreted as a path separator or a
// traversal segment (e.g. "/", "..").
var validGID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Writer writes extracted entities under
// baseDir/{users,projects}/{timestamp}_{gid}.json. Every write uses a fresh
// timestamp, so repeated extraction cycles accumulate a history of snapshots
// per entity instead of overwriting the previous one.
type Writer struct {
	baseDir string
	now     func() time.Time
}

// NewWriter builds a Writer rooted at baseDir.
func NewWriter(baseDir string) *Writer {
	return &Writer{baseDir: baseDir, now: time.Now}
}

// WriteUser writes u to baseDir/users/{timestamp}_{gid}.json.
func (w *Writer) WriteUser(u domain.User) error {
	return w.writeEntity(usersDir, u.GID, u)
}

// WriteProject writes p to baseDir/projects/{timestamp}_{gid}.json.
func (w *Writer) WriteProject(p domain.Project) error {
	return w.writeEntity(projectsDir, p.GID, p)
}

func (w *Writer) writeEntity(subDir, gid string, v any) error {
	if gid == "" || !validGID.MatchString(gid) {
		return fmt.Errorf("jsonwriter: entity has invalid gid %q", gid)
	}

	dir := filepath.Join(w.baseDir, subDir)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("jsonwriter: create dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("jsonwriter: marshal entity %s: %w", gid, err)
	}

	filename := fmt.Sprintf("%s_%s.json", w.now().UTC().Format(timestampFormat), gid)
	path := filepath.Join(dir, filename)

	// Defense in depth: even though gid is already validated above, assert
	// the resolved path still lands inside dir before writing anything.
	if rel, err := filepath.Rel(dir, path); err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("jsonwriter: resolved path %s escapes %s", path, dir)
	}

	if err := os.WriteFile(path, data, filePerm); err != nil {
		return fmt.Errorf("jsonwriter: write file %s: %w", path, err)
	}

	return nil
}
