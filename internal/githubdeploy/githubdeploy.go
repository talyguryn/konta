package githubdeploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiBaseURL = "https://api.github.com"
)

type Client struct {
	owner string
	repo  string
	token string
	http  *http.Client
}

func New(repoURL, token string) (*Client, error) {
	owner, repo, err := parseGitHubRepo(repoURL)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("github token is empty")
	}

	return &Client{
		owner: owner,
		repo:  repo,
		token: token,
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
	}, nil
}

func (c *Client) CreateDeploymentAndMarkInProgress(ctx context.Context, ref, environment string) (int64, error) {
	deploymentID, err := c.CreateDeployment(ctx, ref, environment)
	if err != nil {
		return 0, err
	}

	if err := c.CreateDeploymentStatus(ctx, deploymentID, "in_progress", "Konta deployment in progress"); err != nil {
		return deploymentID, err
	}

	return deploymentID, nil
}

func (c *Client) CreateDeployment(ctx context.Context, ref, environment string) (int64, error) {
	type request struct {
		Ref              string   `json:"ref"`
		Environment      string   `json:"environment"`
		AutoMerge        bool     `json:"auto_merge"`
		RequiredContexts []string `json:"required_contexts"`
		Description      string   `json:"description,omitempty"`
	}

	type response struct {
		ID int64 `json:"id"`
	}

	body := request{
		Ref:              ref,
		Environment:      environment,
		AutoMerge:        false,
		RequiredContexts: []string{},
		Description:      "Konta deployment started",
	}

	var out response
	if err := c.doJSON(ctx, http.MethodPost, c.endpoint("/deployments"), body, &out); err != nil {
		return 0, err
	}

	if out.ID == 0 {
		return 0, fmt.Errorf("github deployment response missing deployment id")
	}

	return out.ID, nil
}

func (c *Client) CreateDeploymentStatus(ctx context.Context, deploymentID int64, state, description string) error {
	type request struct {
		State       string `json:"state"`
		Description string `json:"description,omitempty"`
	}

	body := request{
		State:       state,
		Description: trimDescription(description),
	}

	return c.doJSON(ctx, http.MethodPost, c.endpoint(fmt.Sprintf("/deployments/%d/statuses", deploymentID)), body, nil)
}

func (c *Client) CreateCommitStatus(ctx context.Context, sha, state, description, targetURL string) error {
	type request struct {
		State       string `json:"state"`
		Context     string `json:"context"`
		Description string `json:"description,omitempty"`
		TargetURL   string `json:"target_url,omitempty"`
	}

	body := request{
		State:       state,
		Context:     "konta/deploy",
		Description: trimDescription(description),
		TargetURL:   strings.TrimSpace(targetURL),
	}

	return c.doJSON(ctx, http.MethodPost, c.endpoint(fmt.Sprintf("/statuses/%s", sha)), body, nil)
}

func (c *Client) CreateCommitComment(ctx context.Context, sha, body string) error {
	type request struct {
		Body string `json:"body"`
	}

	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("comment body is empty")
	}

	return c.doJSON(ctx, http.MethodPost, c.endpoint(fmt.Sprintf("/commits/%s/comments", sha)), request{Body: body}, nil)
}

func (c *Client) CompareURL(base, head string) string {
	base = strings.TrimSpace(base)
	head = strings.TrimSpace(head)
	if base == "" || head == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s", c.owner, c.repo, base, head)
}

func (c *Client) endpoint(path string) string {
	return fmt.Sprintf("%s/repos/%s/%s%s", apiBaseURL, c.owner, c.repo, path)
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, payload interface{}, out interface{}) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode github request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create github request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("github api error (%d): %s", resp.StatusCode, msg)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("failed to decode github response: %w", err)
		}
	}

	return nil
}

func parseGitHubRepo(repoURL string) (string, string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", "", fmt.Errorf("repository url is empty")
	}

	if strings.HasPrefix(repoURL, "git@github.com:") {
		path := strings.TrimPrefix(repoURL, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid github repository url: %s", repoURL)
		}
		return parts[0], parts[1], nil
	}

	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse repository url: %w", err)
	}

	if !strings.EqualFold(parsed.Host, "github.com") {
		return "", "", fmt.Errorf("github deployment status supports github.com repositories only")
	}

	path := strings.Trim(parsed.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid github repository url: %s", repoURL)
	}

	return parts[0], parts[1], nil
}

func trimDescription(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 140 {
		return value
	}
	return value[:137] + "..."
}
