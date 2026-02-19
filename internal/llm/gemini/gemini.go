package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mmrzaf/evolver/internal/config"
	"github.com/mmrzaf/evolver/internal/plan"
	"github.com/mmrzaf/evolver/internal/repoctx"
)

// Client calls the Gemini API to generate repository evolution plans.
type Client struct {
	APIKey string
	Model  string
}

// NewClient creates a Gemini client.
func NewClient(apiKey, model string) *Client {
	return &Client{APIKey: apiKey, Model: model}
}

// GeneratePlan asks Gemini for a structured change plan for the repository.
func (c *Client) GeneratePlan(ctx *repoctx.Context, cfg *config.Config) (*plan.Plan, error) {
	prompt := buildPrompt(ctx, cfg)

	reqBody := map[string]any{
		"contents": []map[string]any{{"parts": []map[string]any{{"text": prompt}}}},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
		},
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", c.Model, c.APIKey)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	if len(res.Candidates) == 0 || len(res.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from gemini")
	}

	var p plan.Plan
	if err := json.Unmarshal([]byte(res.Candidates[0].Content.Parts[0].Text), &p); err != nil {
		return nil, fmt.Errorf("invalid json plan: %v", err)
	}
	return &p, nil
}

func buildPrompt(ctx *repoctx.Context, cfg *config.Config) string {
	d, _ := json.Marshal(ctx)
	return fmt.Sprintf(`You are an autonomous repository evolver.
Rules:
- Make small changes. Stay under %d files and %d lines.
- No workflow edits unless explicitly allowed.
- Output ONLY valid JSON matching this schema:
{"summary": "...", "files": [{"path": "...", "mode": "write", "content": "..."}], "changelog_entry": "- ...", "roadmap_update": "..."}

Context:
%s`, cfg.Budgets.MaxFilesChanged, cfg.Budgets.MaxLinesChanged, string(d))
}
