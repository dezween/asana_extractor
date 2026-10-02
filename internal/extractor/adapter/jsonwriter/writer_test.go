package jsonwriter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"asana_extractor/internal/extractor/domain"
)

func TestWriteUser(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	w.now = func() time.Time { return fixed }

	u := domain.User{GID: "42", Name: "Alice", Email: "alice@example.com"}
	if err := w.WriteUser(u); err != nil {
		t.Fatalf("WriteUser: %v", err)
	}

	path := filepath.Join(dir, "users", fixed.Format(timestampFormat)+"_42.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}

	var got domain.User
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal written file: %v", err)
	}
	if got != u {
		t.Fatalf("written user = %+v, want %+v", got, u)
	}
}

func TestWriteProject(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	w.now = func() time.Time { return fixed }

	p := domain.Project{GID: "99", Name: "Project X", WorkspaceGID: "ws1"}
	if err := w.WriteProject(p); err != nil {
		t.Fatalf("WriteProject: %v", err)
	}

	path := filepath.Join(dir, "projects", fixed.Format(timestampFormat)+"_99.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}

	var got domain.Project
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal written file: %v", err)
	}
	if got != p {
		t.Fatalf("written project = %+v, want %+v", got, p)
	}
}

func TestWriteCreatesNestedDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep")
	w := NewWriter(dir)

	if err := w.WriteUser(domain.User{GID: "1", Name: "A", Email: "a@x.com"}); err != nil {
		t.Fatalf("WriteUser: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "users"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected nested dirs created with one file: entries=%v err=%v", entries, err)
	}
}

func TestWriteTwiceProducesTwoFiles(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	t1 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	t2 := t1.Add(30 * time.Second)
	calls := []time.Time{t1, t2}
	i := 0
	w.now = func() time.Time {
		ts := calls[i]
		i++
		return ts
	}

	u := domain.User{GID: "7", Name: "A", Email: "a@x.com"}
	if err := w.WriteUser(u); err != nil {
		t.Fatalf("first WriteUser: %v", err)
	}
	if err := w.WriteUser(u); err != nil {
		t.Fatalf("second WriteUser: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "users"))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 snapshot files for repeated writes of the same gid, got %d", len(entries))
	}
}

func TestWriteEmptyGIDFails(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	if err := w.WriteUser(domain.User{Name: "No GID"}); err == nil {
		t.Fatal("expected error for empty gid")
	}
}

func TestWriteMaliciousGIDRejected(t *testing.T) {
	cases := map[string]string{
		"path traversal":        "../../../etc/cron.d/x",
		"leading traversal":     "../escape",
		"embedded traversal":    "foo/../../bar",
		"path separator":        "foo/bar",
		"disallowed whitespace": "gid with spaces",
	}

	for name, gid := range cases {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "output")
			w := NewWriter(dir)

			err := w.WriteUser(domain.User{GID: gid, Name: "Attacker"})
			if err == nil {
				t.Fatalf("expected error for malicious gid %q, got nil", gid)
			}

			// Confirm the traversal attempt did not escape the parent of
			// the intended output tree (e.g. write a sibling file outside
			// dir), and that nothing was written inside dir either.
			escaped := filepath.Join(parent, "x")
			if _, statErr := os.Stat(escaped); statErr == nil {
				t.Fatalf("malicious gid %q escaped output dir: %s exists", gid, escaped)
			}
			if _, statErr := os.Stat(dir); statErr == nil {
				entries, _ := os.ReadDir(filepath.Join(dir, "users"))
				if len(entries) != 0 {
					t.Fatalf("expected no files written for malicious gid %q, found %d", gid, len(entries))
				}
			}
		})
	}
}
