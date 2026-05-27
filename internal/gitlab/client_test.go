package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestClient_GetMergeRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v4/projects/g/p/merge_requests/42" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Fatalf("missing bearer")
		}
		_ = json.NewEncoder(w).Encode(MergeRequest{
			IID:          42,
			SourceBranch: "dep-bump",
			TargetBranch: "main",
			WebURL:       "https://gitlab.example/g/p/-/merge_requests/42",
			State:        "opened",
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", "v4")
	mr, err := c.GetMergeRequest("g/p", 42)
	if err != nil {
		t.Fatal(err)
	}
	if mr.SourceBranch != "dep-bump" || mr.IID != 42 {
		t.Fatalf("mr: %+v", mr)
	}
}

func TestClient_UsesPrivateTokenHeaderForGitLabPAT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "glpat-secret" {
			t.Fatalf("PRIVATE-TOKEN = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization should be empty for PAT, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(MergeRequest{IID: 42, State: "opened"})
	}))
	defer srv.Close()

	c := New(srv.URL, "glpat-secret", "v4")
	if _, err := c.GetMergeRequest("g/p", 42); err != nil {
		t.Fatal(err)
	}
}

func TestClient_UsesBearerHeaderForNonPATToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "" {
			t.Fatalf("PRIVATE-TOKEN should be empty for bearer token, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(MergeRequest{IID: 42, State: "opened"})
	}))
	defer srv.Close()

	c := New(srv.URL, "oauth-secret", "v4")
	if _, err := c.GetMergeRequest("g/p", 42); err != nil {
		t.Fatal(err)
	}
}

func TestClient_ApproveMergeRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v4/projects/g/p/merge_requests/7/approve" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(MergeResponse{IID: 7, WebURL: "https://x/m/7", State: "opened"})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", "v4")
	out, err := c.ApproveMergeRequest("g/p", 7)
	if err != nil {
		t.Fatal(err)
	}
	if out.IID != 7 || out.State != "opened" {
		t.Fatalf("got %+v", out)
	}
}

func TestClient_ApproveMergeRequestAlreadyApprovedByCurrentUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects/g/p/merge_requests/7/approve":
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "401 Unauthorized"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/user":
			_ = json.NewEncoder(w).Encode(User{ID: 42, Username: "bot"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/g/p/merge_requests/7/approvals":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"iid": 7,
				"approved_by": []map[string]interface{}{
					{"user": map[string]interface{}{"id": 42, "username": "bot"}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/g/p/merge_requests/7":
			_ = json.NewEncoder(w).Encode(MergeRequest{IID: 7, WebURL: "https://x/m/7", State: "opened"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "glpat-secret", "v4")
	out, err := c.ApproveMergeRequest("g/p", 7)
	if err != nil {
		t.Fatal(err)
	}
	if out.IID != 7 || out.State != "opened" {
		t.Fatalf("got %+v", out)
	}
}

func TestClient_ApproveMergeRequestUnauthorizedWhenNotAlreadyApproved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects/g/p/merge_requests/7/approve":
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "401 Unauthorized"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/user":
			_ = json.NewEncoder(w).Encode(User{ID: 42, Username: "bot"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/g/p/merge_requests/7/approvals":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"iid":         7,
				"approved_by": []map[string]interface{}{},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "glpat-secret", "v4")
	_, err := c.ApproveMergeRequest("g/p", 7)
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 APIError, got %T %v", err, err)
	}
}

func TestClient_MergeMergeRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v4/projects/g/p/merge_requests/7/merge" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(MergeResponse{IID: 7, WebURL: "https://x/m/7", State: "merged"})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", "v4")
	out, err := c.MergeMergeRequest("g/p", 7, "merge msg")
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "merged" {
		t.Fatalf("got %+v", out)
	}
}

func TestClient_ListProjectMergeRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v4/projects/g/p/merge_requests" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("per_page") != "20" || q.Get("state") != "opened" {
			t.Fatalf("unexpected query: %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]MergeRequest{{IID: 1, State: "opened"}})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", "v4")
	list, err := c.ListProjectMergeRequests("g/p", "opened")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].IID != 1 {
		t.Fatalf("list: %+v", list)
	}
}

func TestClient_ListMergeRequestPipelines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exp := "/api/v4/projects/g/p/merge_requests/3/pipelines"
		if r.Method != http.MethodGet || r.URL.Path != exp {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]Pipeline{{ID: 9, Status: "success", Ref: "main"}})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", "v4")
	pipes, err := c.ListMergeRequestPipelines("g/p", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(pipes) != 1 || pipes[0].ID != 9 {
		t.Fatalf("pipes: %+v", pipes)
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/env"
	if err := os.WriteFile(path, []byte("FLUID_GITLAB_SA_TOKEN=glpat-x\n# c\nEMPTY=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := LoadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if m["FLUID_GITLAB_SA_TOKEN"] != "glpat-x" {
		t.Fatalf("%+v", m)
	}
}
