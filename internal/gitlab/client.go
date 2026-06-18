package gitlab

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Client struct {
	mu         sync.RWMutex
	baseURL    string
	token      string
	apiVersion string
	http       *http.Client
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("gitlab API error status=%d", e.StatusCode)
	}
	if len(body) > 500 {
		body = body[:500] + "..."
	}
	return fmt.Sprintf("gitlab API error status=%d: %s", e.StatusCode, body)
}

func New(baseURL, token, apiVersion string) *Client {
	if apiVersion == "" {
		apiVersion = "v4"
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		apiVersion: apiVersion,
		http:       &http.Client{Timeout: 30 * time.Second},
	}
}

// SetToken replaces the bearer token used for subsequent GitLab API calls (e.g. after control-plane prefetch).
func (c *Client) SetToken(tok string) {
	c.mu.Lock()
	c.token = strings.TrimSpace(tok)
	c.mu.Unlock()
}

func (c *Client) bearer() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

func (c *Client) setAuthHeader(req *http.Request) {
	token := c.bearer()
	if strings.HasPrefix(token, "glpat-") || strings.HasPrefix(token, "gloas-") || strings.HasPrefix(token, "glcbt-") {
		req.Header.Set("PRIVATE-TOKEN", token)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
}

func (c *Client) projectID(path string) string {
	return url.PathEscape(path)
}

func (c *Client) CreateBranch(projectPath, branch, ref string) error {
	body := map[string]string{"branch": branch, "ref": ref}
	return c.post(fmt.Sprintf("/projects/%s/repository/branches", c.projectID(projectPath)), body, nil)
}

// GetRepositoryFile returns the decoded text of a file at ref (branch, tag, or commit SHA).
func (c *Client) GetRepositoryFile(projectPath, filePath, ref string) (string, error) {
	resp := map[string]interface{}{}
	path := fmt.Sprintf(
		"/projects/%s/repository/files/%s?ref=%s",
		c.projectID(projectPath),
		url.PathEscape(strings.TrimPrefix(filePath, "/")),
		url.QueryEscape(ref),
	)
	if err := c.get(path, &resp); err != nil {
		return "", err
	}
	encoding, _ := resp["encoding"].(string)
	content, _ := resp["content"].(string)
	if content == "" {
		return "", fmt.Errorf("repository file %q at ref %q is empty", filePath, ref)
	}
	if encoding == "base64" {
		raw, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return "", fmt.Errorf("decode repository file %q: %w", filePath, err)
		}
		return string(raw), nil
	}
	return content, nil
}

func (c *Client) CommitFiles(projectPath, branch, message string, actions []map[string]interface{}) error {
	resolved := make([]map[string]interface{}, 0, len(actions))
	for _, action := range actions {
		merged, err := c.resolveCommitAction(projectPath, branch, action)
		if err != nil {
			return err
		}
		resolved = append(resolved, merged)
	}
	body := map[string]interface{}{
		"branch":         branch,
		"commit_message": message,
		"actions":        resolved,
	}
	return c.post(fmt.Sprintf("/projects/%s/repository/commits", c.projectID(projectPath)), body, nil)
}

func (c *Client) resolveCommitAction(projectPath, branch string, action map[string]interface{}) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(action))
	for k, v := range action {
		out[k] = v
	}
	appendText, _ := out["append_content"].(string)
	appendText = strings.TrimSpace(appendText)
	if appendText == "" {
		delete(out, "append_content")
		return out, nil
	}
	act, _ := out["action"].(string)
	filePath, _ := out["file_path"].(string)
	if act != "update" || strings.TrimSpace(filePath) == "" {
		return nil, fmt.Errorf("append_content requires action update and file_path")
	}
	existing, err := c.GetRepositoryFile(projectPath, filePath, branch)
	if err != nil {
		return nil, err
	}
	out["content"] = strings.TrimRight(existing, "\n") + "\n\n" + appendText + "\n"
	delete(out, "append_content")
	return out, nil
}

func (c *Client) CreateMergeRequest(projectPath, source, target, title, description string) (string, error) {
	resp := map[string]interface{}{}
	body := map[string]interface{}{
		"source_branch":        source,
		"target_branch":        target,
		"title":                title,
		"description":          description,
		"remove_source_branch": true,
	}
	if err := c.post(fmt.Sprintf("/projects/%s/merge_requests", c.projectID(projectPath)), body, &resp); err != nil {
		return "", err
	}
	if v, ok := resp["web_url"].(string); ok {
		return v, nil
	}
	return "", fmt.Errorf("merge request created but missing web_url")
}

func (c *Client) get(path string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/%s%s", c.baseURL, c.apiVersion, path), nil)
	if err != nil {
		return err
	}
	c.setAuthHeader(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiError(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) post(path string, body interface{}, out interface{}) error {
	return c.doJSON(http.MethodPost, path, body, out)
}

func (c *Client) put(path string, body interface{}, out interface{}) error {
	return c.doJSON(http.MethodPut, path, body, out)
}

func (c *Client) doJSON(method, path string, body interface{}, out interface{}) error {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, fmt.Sprintf("%s/api/%s%s", c.baseURL, c.apiVersion, path), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	c.setAuthHeader(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiError(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func apiError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
}
