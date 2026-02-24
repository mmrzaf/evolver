package ghapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// CreatePR creates a pull request on the current GitHub repository.
func CreatePR(head, title, body string) (string, error) {
	repo := strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY"))
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if repo == "" {
		return "", fmt.Errorf("missing GITHUB_REPOSITORY")
	}
	if token == "" {
		return "", fmt.Errorf("missing GITHUB_TOKEN")
	}

	base := getDefaultBranch(repo, token)
	slog.Info("creating pull request", "repo", repo, "head", head, "base", base)

	reqBody := map[string]string{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest("POST", fmt.Sprintf("https://api.github.com/repos/%s/pulls", repo), bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		slog.Error("create pull request failed", "repo", repo, "status_code", resp.StatusCode)
		return "", fmt.Errorf("github api http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var res struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", err
	}
	if res.HTMLURL == "" {
		return "", fmt.Errorf("github api: missing html_url in response")
	}
	slog.Info("pull request created", "repo", repo, "url", res.HTMLURL)
	return res.HTMLURL, nil
}

func getDefaultBranch(repo, token string) string {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s", repo), nil)
	if err != nil {
		return "main"
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "main"
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "main"
	}

	var res struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "main"
	}
	if res.DefaultBranch == "" {
		return "main"
	}
	return res.DefaultBranch
}
