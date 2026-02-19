package ghapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// CreatePR creates a pull request on the current GitHub repository.
func CreatePR(head, title, body string) (string, error) {
	repo := os.Getenv("GITHUB_REPOSITORY")
	token := os.Getenv("GITHUB_TOKEN")

	reqBody := map[string]string{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  getDefaultBranch(repo, token),
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
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var res struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.HTMLURL, nil
}

func getDefaultBranch(repo, token string) string {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s", repo), nil)
	if err != nil {
		return "main"
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "main"
	}
	defer func() {
		_ = resp.Body.Close()
	}()

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
