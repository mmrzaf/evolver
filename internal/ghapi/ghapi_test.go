package ghapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

type rewriteTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	return t.base.RoundTrip(clone)
}

func withRedirectedGitHubAPI(t *testing.T, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	orig := http.DefaultClient.Transport
	if orig == nil {
		orig = http.DefaultTransport
	}
	http.DefaultClient.Transport = &rewriteTransport{base: orig, target: u}
	t.Cleanup(func() {
		http.DefaultClient.Transport = orig
	})
}

func TestGetDefaultBranchReturnsValueFromAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/repo" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"default_branch": "develop"})
	}))
	defer srv.Close()
	withRedirectedGitHubAPI(t, srv)

	if got := getDefaultBranch("acme/repo", "token"); got != "develop" {
		t.Fatalf("expected develop, got %s", got)
	}
}

func TestCreatePRBuildsRequestAndReturnsURL(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/repo":
			_ = json.NewEncoder(w).Encode(map[string]string{"default_branch": "main"})
		case "/repos/acme/repo/pulls":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"html_url": "https://example/pr/1"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withRedirectedGitHubAPI(t, srv)

	t.Setenv("GITHUB_REPOSITORY", "acme/repo")
	t.Setenv("GITHUB_TOKEN", "abc")
	url, err := CreatePR("evolve/branch", "Improve safety", "Body")
	if err != nil {
		t.Fatalf("create PR: %v", err)
	}
	if url != "https://example/pr/1" {
		t.Fatalf("unexpected html url: %s", url)
	}
	if gotBody["base"] != "main" || gotBody["head"] != "evolve/branch" {
		t.Fatalf("unexpected PR payload: %#v", gotBody)
	}
	if !strings.Contains(gotBody["title"], "Improve safety") {
		t.Fatalf("expected title in payload")
	}
}

func TestGetDefaultBranchFallbackOnTransportError(t *testing.T) {
	orig := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, os.ErrDeadlineExceeded
	})
	t.Cleanup(func() { http.DefaultClient.Transport = orig })

	if got := getDefaultBranch("acme/repo", "token"); got != "main" {
		t.Fatalf("expected fallback branch main, got %s", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
