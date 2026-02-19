package gemini

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func withRedirectedGeminiAPI(t *testing.T, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	orig := http.DefaultClient.Transport
	http.DefaultClient.Transport = &rewriteTLSTransport{
		base:   srv.Client().Transport,
		target: u,
	}
	t.Cleanup(func() {
		http.DefaultClient.Transport = orig
	})
}

func TestBuildPromptIncludesBudgetsAndContext(t *testing.T) {
	ctx := &repoctx.Context{Files: []string{"a.go"}}
	cfg := &config.Config{Budgets: config.Budgets{MaxFilesChanged: 3, MaxLinesChanged: 99}}
	prompt := buildPrompt(ctx, cfg)

	if !strings.Contains(prompt, "Stay under 3 files and 99 lines") {
		t.Fatalf("expected prompt budgets, got %q", prompt)
	}
	if !strings.Contains(prompt, "\"Files\":[\"a.go\"]") {
		t.Fatalf("expected serialized context in prompt")
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
	withRedirectedGeminiAPI(t, srv)

	c := NewClient("k", "model")
	p, err := c.GeneratePlan(&repoctx.Context{}, &config.Config{Budgets: config.Budgets{MaxFilesChanged: 1, MaxLinesChanged: 10}})
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
	withRedirectedGeminiAPI(t, srv)

	c := NewClient("k", "model")
	_, err := c.GeneratePlan(&repoctx.Context{}, &config.Config{Budgets: config.Budgets{MaxFilesChanged: 1, MaxLinesChanged: 10}})
	if err == nil {
		t.Fatalf("expected error for empty response")
	}
}
