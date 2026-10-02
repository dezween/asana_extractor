package asana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, srv *httptest.Server, token string) *Client {
	t.Cleanup(srv.Close)
	c := NewClient(token, 6000) // high rate so tests aren't throttled
	c.baseURL = srv.URL
	t.Cleanup(c.limiter.stop)
	return c
}

func TestListUsers_Pagination(t *testing.T) {
	var calls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/123/users", func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		n := atomic.AddInt32(&calls, 1)

		switch {
		case n == 1 && offset == "":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(usersResponse{
				Data:     []userDTO{{GID: "1", Name: "Alice", Email: "alice@example.com"}},
				NextPage: &nextPage{Offset: "page2"},
			})
		case n == 2 && offset == "page2":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(usersResponse{
				Data:     []userDTO{{GID: "2", Name: "Bob", Email: "bob@example.com"}},
				NextPage: nil,
			})
		default:
			t.Errorf("unexpected call %d with offset %q", n, offset)
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")

	users, err := c.ListUsers(context.Background(), "123")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users across pages, got %d", len(users))
	}
	if users[0].GID != "1" || users[1].GID != "2" {
		t.Fatalf("unexpected users: %+v", users)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 HTTP calls (pagination), got %d", calls)
	}
}

func TestListProjects_Pagination(t *testing.T) {
	var calls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		if got := r.URL.Query().Get("workspace"); got != "ws1" {
			t.Errorf("expected workspace=ws1, got %q", got)
		}
		n := atomic.AddInt32(&calls, 1)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case n == 1 && offset == "":
			json.NewEncoder(w).Encode(projectsResponse{
				Data:     []projectDTO{{GID: "p1", Name: "Project One"}},
				NextPage: &nextPage{Offset: "next"},
			})
		case n == 2 && offset == "next":
			json.NewEncoder(w).Encode(projectsResponse{
				Data:     []projectDTO{{GID: "p2", Name: "Project Two"}},
				NextPage: nil,
			})
		default:
			t.Errorf("unexpected call %d offset %q", n, offset)
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")

	projects, err := c.ListProjects(context.Background(), "ws1")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	for _, p := range projects {
		if p.WorkspaceGID != "ws1" {
			t.Errorf("expected WorkspaceGID ws1, got %q", p.WorkspaceGID)
		}
	}
}

func TestAuthHeaderSent(t *testing.T) {
	var gotAuth string

	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/users", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usersResponse{Data: nil, NextPage: nil})
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "secret-token")

	if _, err := c.ListUsers(context.Background(), "ws"); err != nil {
		t.Fatalf("ListUsers: %v", err)
	}

	if gotAuth != "Bearer secret-token" {
		t.Fatalf("expected Authorization header 'Bearer secret-token', got %q", gotAuth)
	}
}

func TestRetryAfter429(t *testing.T) {
	var calls int32
	start := time.Now()

	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/users", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usersResponse{
			Data:     []userDTO{{GID: "1", Name: "Alice", Email: "a@example.com"}},
			NextPage: nil,
		})
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")

	users, err := c.ListUsers(context.Background(), "ws")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	elapsed := time.Since(start)

	if len(users) != 1 {
		t.Fatalf("expected 1 user after retry, got %d", len(users))
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected exactly 2 calls (1 retry), got %d", calls)
	}
	if elapsed < time.Second {
		t.Fatalf("expected to honor Retry-After of 1s, elapsed only %s", elapsed)
	}
}

func TestExhaustsRetriesOnPersistent429(t *testing.T) {
	var calls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/users", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")

	_, err := c.ListUsers(context.Background(), "ws")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if atomic.LoadInt32(&calls) != maxAttempts {
		t.Fatalf("expected %d attempts, got %d", maxAttempts, calls)
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := map[string]time.Duration{
		"":    time.Second,
		"0":   0,
		"3":   3 * time.Second,
		"abc": time.Second,
		"-1":  time.Second,
	}
	for input, want := range cases {
		if got := parseRetryAfter(input); got != want {
			t.Errorf("parseRetryAfter(%q) = %s, want %s", input, got, want)
		}
	}
}

func TestNonOKStatusReturnsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/users", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")

	_, err := c.ListUsers(context.Background(), "ws")
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestListUsers_PartialResultsReturnedOnPersistentLaterPageError(t *testing.T) {
	var calls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/users", func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		n := atomic.AddInt32(&calls, 1)

		if n == 1 && offset == "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(usersResponse{
				Data:     []userDTO{{GID: "1", Name: "Alice", Email: "alice@example.com"}},
				NextPage: &nextPage{Offset: "page2"},
			})
			return
		}
		// Every subsequent call (page 2 onward) fails persistently.
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")

	users, err := c.ListUsers(context.Background(), "ws")
	if err == nil {
		t.Fatal("expected error from failed later page")
	}
	if len(users) != 1 || users[0].GID != "1" {
		t.Fatalf("expected partial results [user 1] preserved alongside the error, got %+v", users)
	}
}

func TestListProjects_PartialResultsReturnedOnPersistentLaterPageError(t *testing.T) {
	var calls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		n := atomic.AddInt32(&calls, 1)

		if n == 1 && offset == "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(projectsResponse{
				Data:     []projectDTO{{GID: "p1", Name: "Project One"}},
				NextPage: &nextPage{Offset: "next"},
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")

	projects, err := c.ListProjects(context.Background(), "ws1")
	if err == nil {
		t.Fatal("expected error from failed later page")
	}
	if len(projects) != 1 || projects[0].GID != "p1" {
		t.Fatalf("expected partial results [project p1] preserved alongside the error, got %+v", projects)
	}
}

func TestTransient503RetrySucceeds(t *testing.T) {
	var calls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/users", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"errors":[{"message":"temporarily unavailable"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usersResponse{
			Data:     []userDTO{{GID: "1", Name: "Alice", Email: "a@example.com"}},
			NextPage: nil,
		})
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")

	users, err := c.ListUsers(context.Background(), "ws")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user after retry, got %d", len(users))
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected exactly 2 calls (1 retry), got %d", calls)
	}
}

func TestPersistent503ExhaustsRetries(t *testing.T) {
	var calls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/users", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"errors":[{"message":"still unavailable"}]}`))
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")

	_, err := c.ListUsers(context.Background(), "ws")
	if err == nil {
		t.Fatal("expected error after exhausting retries on persistent 503")
	}
	if atomic.LoadInt32(&calls) != maxAttempts {
		t.Fatalf("expected %d attempts, got %d", maxAttempts, calls)
	}
}

// flakyTransport fails the first failCount round trips with a transport-level
// error, then delegates to the underlying transport.
type flakyTransport struct {
	failCount  int
	calls      int32
	underlying http.RoundTripper
}

func (f *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n := atomic.AddInt32(&f.calls, 1)
	if int(n) <= f.failCount {
		return nil, fmt.Errorf("simulated network error (call %d)", n)
	}
	return f.underlying.RoundTrip(req)
}

func TestTransientNetworkErrorRetrySucceeds(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usersResponse{
			Data:     []userDTO{{GID: "1", Name: "Alice", Email: "a@example.com"}},
			NextPage: nil,
		})
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")
	ft := &flakyTransport{failCount: 1, underlying: http.DefaultTransport}
	c.httpClient = &http.Client{Transport: ft}

	users, err := c.ListUsers(context.Background(), "ws")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user after retrying past a network error, got %d", len(users))
	}
	if atomic.LoadInt32(&ft.calls) != 2 {
		t.Fatalf("expected exactly 2 attempts (1 retry), got %d", ft.calls)
	}
}

func TestPersistentNetworkErrorExhaustsRetries(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/users", func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should never be reached: transport always fails")
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")
	ft := &flakyTransport{failCount: 1000, underlying: http.DefaultTransport}
	c.httpClient = &http.Client{Transport: ft}

	_, err := c.ListUsers(context.Background(), "ws")
	if err == nil {
		t.Fatal("expected error after exhausting retries on persistent network error")
	}
	if atomic.LoadInt32(&ft.calls) != maxAttempts {
		t.Fatalf("expected %d attempts, got %d", maxAttempts, ft.calls)
	}
}

func TestPageLimitParam(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/workspaces/ws/users", func(w http.ResponseWriter, r *http.Request) {
		limit := r.URL.Query().Get("limit")
		if limit != strconv.Itoa(pageLimit) {
			t.Errorf("expected limit=%d, got %q", pageLimit, limit)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usersResponse{})
	})

	srv := httptest.NewServer(mux)
	c := newTestClient(t, srv, "test-token")

	if _, err := c.ListUsers(context.Background(), "ws"); err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
}
