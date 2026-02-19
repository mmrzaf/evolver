package gemini

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"strings"
	"testing"

	"github.com/mmrzaf/evolver/internal/config"
	"github.com/mmrzaf/evolver/internal/repoctx"
)

type rewriteTLSTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (t *rewriteTLSTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	return t.base.RoundTrip(clone)
}

func redirectClientToServer(t *testing.T, c *Client, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	c.HTTP.Transport = &rewriteTLSTransport{
		base:   srv.Client().Transport,
		target: u,
	}
}

func TestBuildPromptIncludesBudgetsAndContext(t *testing.T) {
	ctx := &repoctx.Context{Files: []string{"a.go"}}
	cfg := &config.Config{
		Budgets:  config.Budgets{MaxFilesChanged: 3, MaxLinesChanged: 99, MaxNewFiles: 2},
		Security: config.Security{AllowWorkflowEdits: false},
	}
	prompt := buildPrompt(ctx, cfg)

	if !strings.Contains(prompt, "Stay under 3 files changed, 99 lines changed, 2 new files.") {
		t.Fatalf("expected prompt budgets, got %q", prompt)
	}
	if !strings.Contains(prompt, "\"Files\":[\"a.go\"]") {
		t.Fatalf("expected serialized context in prompt")
	}
	if !strings.Contains(prompt, "Workflow edits: false.") {
		t.Fatalf("expected workflow flag in prompt")
	}
}

func TestGeneratePlanSuccess(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]string{
							{"text": `{"summary":"safe change","files":[{"path":"a.txt","mode":"write","content":"ok"}],"changelog_entry":"- safe","roadmap_update":"done"}`},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer srv.Close()

	c := NewClient("k", "model")
	c.RetryBaseDelay = 0
	redirectClientToServer(t, c, srv)

	p, err := c.GeneratePlan(&repoctx.Context{}, &config.Config{
		Budgets:  config.Budgets{MaxFilesChanged: 1, MaxLinesChanged: 10, MaxNewFiles: 1},
		Security: config.Security{AllowWorkflowEdits: false},
	})
	if err != nil {
		t.Fatalf("generate plan: %v", err)
	}
	if p.Summary != "safe change" || len(p.Files) != 1 {
		t.Fatalf("unexpected plan: %+v", p)
	}
}

func TestGeneratePlanEmptyResponse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{}})
	}))
	defer srv.Close()

	c := NewClient("k", "model")
	c.RetryBaseDelay = 0
	redirectClientToServer(t, c, srv)

	_, err := c.GeneratePlan(&repoctx.Context{}, &config.Config{Budgets: config.Budgets{MaxFilesChanged: 1, MaxLinesChanged: 10, MaxNewFiles: 1}})
	if err == nil {
		t.Fatalf("expected error for empty response")
	}
}

func TestParsePlanStripsFences(t *testing.T) {
	p, err := parsePlan("```json\n{\"summary\":\"x\",\"files\":[],\"changelog_entry\":\"- x\",\"roadmap_update\":\"\"}\n```")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Summary != "x" {
		t.Fatalf("unexpected summary: %q", p.Summary)
	}
}

func TestGeneratePlanRetriesHTTPFailure(t *testing.T) {
	var calls int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			http.Error(w, "temporary outage", http.StatusServiceUnavailable)
			return
		}
		res := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]string{
							{"text": `{"summary":"retry ok","files":[],"changelog_entry":"- retry","roadmap_update":""}`},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer srv.Close()

	c := NewClient("k", "model")
	c.RetryBaseDelay = 0
	redirectClientToServer(t, c, srv)

	p, err := c.GeneratePlan(&repoctx.Context{}, &config.Config{
		Budgets:  config.Budgets{MaxFilesChanged: 1, MaxLinesChanged: 10, MaxNewFiles: 1},
		Security: config.Security{AllowWorkflowEdits: false},
	})
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if p.Summary != "retry ok" {
		t.Fatalf("unexpected plan after retry: %+v", p)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 calls, got %d", got)
	}
}
