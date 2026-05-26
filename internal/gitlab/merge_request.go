package gitlab

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// MergeRequest holds fields used by execution skills (checkout, merge, assert).
type MergeRequest struct {
	IID                 int    `json:"iid"`
	SourceBranch        string `json:"source_branch"`
	TargetBranch        string `json:"target_branch"`
	WebURL              string `json:"web_url"`
	State               string `json:"state"`
	MergeError          string `json:"merge_error"`
	DetailedMergeStatus string `json:"detailed_merge_status"`
}

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

type MergeRequestApprovals struct {
	IID       int `json:"iid"`
	Approvals []struct {
		User User `json:"user"`
	} `json:"approved_by"`
}

// GetMergeRequest loads a project MR by internal id (IID).
func (c *Client) GetMergeRequest(projectPath string, iid int) (*MergeRequest, error) {
	apiPath := fmt.Sprintf("/projects/%s/merge_requests/%d", c.projectID(projectPath), iid)
	var out MergeRequest
	if err := c.get(apiPath, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetCurrentUser() (*User, error) {
	var out User
	if err := c.get("/user", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetMergeRequestApprovals(projectPath string, iid int) (*MergeRequestApprovals, error) {
	apiPath := fmt.Sprintf("/projects/%s/merge_requests/%d/approvals", c.projectID(projectPath), iid)
	var out MergeRequestApprovals
	if err := c.get(apiPath, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MergeResponse is the subset of fields returned by GitLab after an accept-MR call.
type MergeResponse struct {
	IID    int    `json:"iid"`
	WebURL string `json:"web_url"`
	State  string `json:"state"`
}

// ApproveMergeRequest approves the merge request as the authenticated user (POST .../approve).
// See https://docs.gitlab.com/ee/api/merge_requests.html#approve-merge-request
func (c *Client) ApproveMergeRequest(projectPath string, iid int) (*MergeResponse, error) {
	apiPath := fmt.Sprintf("/projects/%s/merge_requests/%d/approve", c.projectID(projectPath), iid)
	var out MergeResponse
	if err := c.post(apiPath, map[string]interface{}{}, &out); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
			if approved, _, checkErr := c.MergeRequestApprovedByCurrentUser(projectPath, iid); checkErr == nil && approved {
				mr, mrErr := c.GetMergeRequest(projectPath, iid)
				if mrErr != nil {
					return &MergeResponse{IID: iid, State: "opened"}, nil
				}
				return &MergeResponse{IID: mr.IID, WebURL: mr.WebURL, State: mr.State}, nil
			}
		}
		return nil, err
	}
	return &out, nil
}

func (c *Client) MergeRequestApprovedByCurrentUser(projectPath string, iid int) (bool, *User, error) {
	user, err := c.GetCurrentUser()
	if err != nil {
		return false, nil, err
	}
	approvals, err := c.GetMergeRequestApprovals(projectPath, iid)
	if err != nil {
		return false, user, err
	}
	for _, approval := range approvals.Approvals {
		if approval.User.ID != 0 && approval.User.ID == user.ID {
			return true, user, nil
		}
	}
	return false, user, nil
}

// MergeMergeRequest accepts (merges) the merge request via the GitLab API (PUT .../merge).
func (c *Client) MergeMergeRequest(projectPath string, iid int, mergeCommitMessage string) (*MergeResponse, error) {
	apiPath := fmt.Sprintf("/projects/%s/merge_requests/%d/merge", c.projectID(projectPath), iid)
	body := map[string]interface{}{}
	if mergeCommitMessage != "" {
		body["merge_commit_message"] = mergeCommitMessage
	}
	var out MergeResponse
	if err := c.put(apiPath, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListProjectMergeRequests returns merge requests for a project (used for automation / probes).
func (c *Client) ListProjectMergeRequests(projectPath string, state string) ([]MergeRequest, error) {
	q := url.Values{}
	if state != "" {
		q.Set("state", state)
	}
	q.Set("per_page", "20")
	suffix := ""
	if enc := q.Encode(); enc != "" {
		suffix = "?" + enc
	}
	apiPath := fmt.Sprintf("/projects/%s/merge_requests%s", c.projectID(projectPath), suffix)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/%s%s", c.baseURL, c.apiVersion, apiPath), nil)
	if err != nil {
		return nil, err
	}
	c.setAuthHeader(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError(resp)
	}
	var out []MergeRequest
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
