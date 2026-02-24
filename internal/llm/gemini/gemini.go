package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mmrzaf/evolver/internal/config"
	"github.com/mmrzaf/evolver/internal/plan"
	"github.com/mmrzaf/evolver/internal/repoctx"
)

// Client calls the Gemini API to generate repository evolution plans.
type Client struct {
	APIKey         string
	Model          string
	HTTP           *http.Client
	MaxAttempts    int
	RetryBaseDelay time.Duration
}

// NewClient creates a Gemini client.
func NewClient(apiKey, model string) *Client {
	return &Client{
		APIKey:         apiKey,
		Model:          model,
		HTTP:           &http.Client{Timeout: 60 * time.Second},
		MaxAttempts:    2,
		RetryBaseDelay: 300 * time.Millisecond,
	}
}

// GeneratePlan asks Gemini for a structured change plan for the repository.
func (c *Client) GeneratePlan(ctx *repoctx.Context, cfg *config.Config) (*plan.Plan, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return nil, fmt.Errorf("missing GEMINI_API_KEY")
	}
	slog.Info("gemini plan generation started", "model", c.Model, "max_attempts", c.MaxAttempts)
	prompt := buildPrompt(ctx, cfg)

	var lastErr error
	for attempt := 1; attempt <= c.MaxAttempts; attempt++ {
		attemptStartedAt := time.Now()
		slog.Info("gemini attempt started", "attempt", attempt, "max_attempts", c.MaxAttempts)
		text, err := c.generateContent(prompt)
		if err != nil {
			slog.Error("gemini request failed", "attempt", attempt, "max_attempts", c.MaxAttempts, "duration_ms", time.Since(attemptStartedAt).Milliseconds(), "error", err)
			lastErr = err
			if attempt < c.MaxAttempts {
				c.waitBeforeRetry(attempt)
				continue
			}
			break
		}

		p, err := parsePlan(text)
		if err == nil {
			slog.Info("gemini attempt succeeded", "attempt", attempt, "max_attempts", c.MaxAttempts, "duration_ms", time.Since(attemptStartedAt).Milliseconds())
			return p, nil
		}
		slog.Warn("gemini response parse failed", "attempt", attempt, "max_attempts", c.MaxAttempts, "duration_ms", time.Since(attemptStartedAt).Milliseconds(), "error", err)
		lastErr = err

		if attempt < c.MaxAttempts {
			prompt = buildFixupPrompt(ctx, cfg, text, err)
			c.waitBeforeRetry(attempt)
		}
	}
	slog.Error("gemini plan generation failed", "model", c.Model, "error", lastErr)
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("failed to generate plan")
}

// GenerateRepairPlan asks Gemini for a minimal repair plan based on a concrete verification failure.
func (c *Client) GenerateRepairPlan(ctx *repoctx.Context, cfg *config.Config, originalSummary string, failureContext string, capabilities []config.RepairCapability) (*plan.Plan, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return nil, fmt.Errorf("missing GEMINI_API_KEY")
	}
	slog.Info("gemini repair generation started", "model", c.Model, "max_attempts", c.MaxAttempts)

	prompt := buildRepairPrompt(ctx, cfg, originalSummary, failureContext, capabilities)
	var lastErr error

	for attempt := 1; attempt <= c.MaxAttempts; attempt++ {
		attemptStartedAt := time.Now()
		slog.Info("gemini repair attempt started", "attempt", attempt, "max_attempts", c.MaxAttempts)

		text, err := c.generateContent(prompt)
		if err != nil {
			slog.Error("gemini repair request failed", "attempt", attempt, "max_attempts", c.MaxAttempts, "duration_ms", time.Since(attemptStartedAt).Milliseconds(), "error", err)
			lastErr = err
			if attempt < c.MaxAttempts {
				c.waitBeforeRetry(attempt)
				continue
			}
			break
		}

		p, err := parsePlan(text)
		if err == nil {
			slog.Info("gemini repair attempt succeeded", "attempt", attempt, "max_attempts", c.MaxAttempts, "duration_ms", time.Since(attemptStartedAt).Milliseconds())
			return p, nil
		}

		slog.Warn("gemini repair response parse failed", "attempt", attempt, "max_attempts", c.MaxAttempts, "duration_ms", time.Since(attemptStartedAt).Milliseconds(), "error", err)
		lastErr = err
		if attempt < c.MaxAttempts {
			prompt = buildRepairFixupPrompt(cfg, failureContext, capabilities, text, err)
			c.waitBeforeRetry(attempt)
		}
	}

	slog.Error("gemini repair generation failed", "model", c.Model, "error", lastErr)
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("failed to generate repair plan")
}

func (c *Client) waitBeforeRetry(attempt int) {
	if c.RetryBaseDelay <= 0 {
		return
	}
	time.Sleep(time.Duration(attempt) * c.RetryBaseDelay)
}

func (c *Client) generateContent(prompt string) (string, error) {
	reqBody := map[string]any{
		"contents": []map[string]any{{"parts": []map[string]any{{"text": prompt}}}},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
		},
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", c.Model, c.APIKey)
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("gemini http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("gemini decode failed: %v", err)
	}
	if len(res.Candidates) == 0 || len(res.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from gemini")
	}
	return res.Candidates[0].Content.Parts[0].Text, nil
}

func parsePlan(text string) (*plan.Plan, error) {
	text = strings.TrimSpace(text)

	// Sometimes the model wraps JSON with fences. Strip common wrappers.
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	try := []string{text}

	// Best-effort salvage: extract the first JSON object from the response.
	if i := strings.Index(text, "{"); i != -1 {
		if j := strings.LastIndex(text, "}"); j != -1 && j > i {
			try = append(try, text[i:j+1])
		}
	}

	var lastErr error
	for _, candidate := range try {
		var p plan.Plan
		if err := json.Unmarshal([]byte(candidate), &p); err != nil {
			lastErr = err
			continue
		}
		return &p, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("invalid json")
	}
	return nil, fmt.Errorf("invalid json plan: %v", lastErr)
}

func buildPrompt(ctx *repoctx.Context, cfg *config.Config) string {
	d, _ := json.Marshal(ctx)
	return fmt.Sprintf(`You are an autonomous repository evolver.

Hard rules:
- Make small, incremental, reviewable changes.
- Stay under %d files changed, %d lines changed, %d new files.
- Workflow edits: %t.
- Output ONLY valid JSON matching this exact schema (no markdown, no commentary):
{"summary": "...", "files": [{"path": "...", "mode": "write", "content": "..."}], "changelog_entry": "- ...", "roadmap_update": "..."}

Repository context (JSON):
%s`, cfg.Budgets.MaxFilesChanged, cfg.Budgets.MaxLinesChanged, cfg.Budgets.MaxNewFiles, cfg.Security.AllowWorkflowEdits, string(d))
}

func buildFixupPrompt(ctx *repoctx.Context, cfg *config.Config, lastText string, parseErr error) string {
	return fmt.Sprintf(`Your previous response was invalid and could not be parsed as JSON.

Error:
%s

Return ONLY valid JSON matching this exact schema (no fences, no commentary):
{"summary": "...", "files": [{"path": "...", "mode": "write", "content": "..."}], "changelog_entry": "- ...", "roadmap_update": "..."}

Here is your previous response for correction:
%s`, parseErr.Error(), strings.TrimSpace(lastText))
}

func buildRepairPrompt(ctx *repoctx.Context, cfg *config.Config, originalSummary string, failureContext string, capabilities []config.RepairCapability) string {
	d, _ := json.Marshal(ctx)
	capsJSON, _ := json.Marshal(summarizeCapabilities(capabilities))

	return fmt.Sprintf(`You are repairing a repository change that failed verification.

Goal:
- Fix the verification failure with the smallest possible patch.
- Preserve the intended behavior unless the failure proves it is wrong.
- Do NOT rewrite unrelated files.
- Prefer edits only in files implicated by the error output.
- Do NOT change verification commands.
- You may optionally request project-allowed repair actions by ID from the provided list.
- Only use repair_actions when they directly address the failure.
- Keep changelog_entry and roadmap_update empty unless absolutely necessary.

Original change summary:
%s

Verification failure context:
%s

Available repair capabilities (JSON):
%s

Hard rules:
- Stay under %d files changed, %d lines changed, %d new files (cumulative budget still applies).
- Workflow edits: %t.
- Output ONLY valid JSON matching this exact schema (no markdown, no commentary):
{"summary": "...", "files": [{"path": "...", "mode": "write", "content": "..."}], "changelog_entry": "", "roadmap_update": "", "repair_actions": ["capability_id"]}
- repair_actions must contain only IDs from the provided capability list.
- If no repair action is needed, return repair_actions as [] or omit it.

Repository context (JSON):
%s`, strings.TrimSpace(originalSummary), strings.TrimSpace(failureContext), string(capsJSON), cfg.Budgets.MaxFilesChanged, cfg.Budgets.MaxLinesChanged, cfg.Budgets.MaxNewFiles, cfg.Security.AllowWorkflowEdits, string(d))
}

func buildRepairFixupPrompt(cfg *config.Config, failureContext string, capabilities []config.RepairCapability, lastText string, parseErr error) string {
	capsJSON, _ := json.Marshal(summarizeCapabilities(capabilities))
	return fmt.Sprintf(`Your repair response was invalid JSON.

Parse error:
%s

Verification failure context (for reference):
%s

Available repair capabilities (JSON):
%s

Return ONLY valid JSON matching this exact schema (no fences, no commentary):
{"summary": "...", "files": [{"path": "...", "mode": "write", "content": "..."}], "changelog_entry": "", "roadmap_update": "", "repair_actions": ["capability_id"]}

Previous invalid response:
%s`, parseErr.Error(), strings.TrimSpace(failureContext), string(capsJSON), strings.TrimSpace(lastText))
}

func summarizeCapabilities(caps []config.RepairCapability) []map[string]any {
	out := make([]map[string]any, 0, len(caps))
	for _, c := range caps {
		m := map[string]any{
			"id":          c.ID,
			"description": c.Description,
		}
		if len(c.AllowedFailureKinds) > 0 {
			m["allowed_failure_kinds"] = c.AllowedFailureKinds
		}
		out = append(out, m)
	}
	return out
}
