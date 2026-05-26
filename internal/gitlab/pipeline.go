package gitlab

import "fmt"

// Pipeline is a minimal view of a GitLab CI pipeline (for listing / status checks).
type Pipeline struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
}

// ListMergeRequestPipelines returns pipelines for a merge request.
func (c *Client) ListMergeRequestPipelines(projectPath string, iid int) ([]Pipeline, error) {
	apiPath := fmt.Sprintf("/projects/%s/merge_requests/%d/pipelines", c.projectID(projectPath), iid)
	var out []Pipeline
	if err := c.get(apiPath, &out); err != nil {
		return nil, err
	}
	return out, nil
}
