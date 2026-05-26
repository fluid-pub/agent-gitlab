package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"fluid/agents/core/cpcredentials"
	"fluid/agents/core/enroll"
	coreexec "fluid/agents/core/execution"
	"fluid/agents/core/skillresult"
	"fluid/agents/gitlab/internal/config"
	"fluid/agents/gitlab/internal/gitlab"
)

type Agent struct {
	cfg     *config.Config
	core    *coreexec.Agent
	gitlab  *gitlab.Client
	allowed map[string]struct{}
}

func New(cfg *config.Config) (*Agent, error) {
	allowed := make(map[string]struct{}, len(cfg.Skills.Allowed))
	for _, s := range cfg.Skills.Allowed {
		allowed[strings.TrimSpace(s)] = struct{}{}
	}

	return &Agent{
		cfg:     cfg,
		gitlab:  gitlab.New(cfg.GitLab.URL, cfg.GitLab.Token, cfg.GitLab.APIv),
		allowed: allowed,
	}, nil
}

func (a *Agent) Start() error {
	coreCfg := coreexec.Config{
		WebSocketURL:     a.cfg.Controlplane.WebSocketURL,
		OrganizationUUID: a.cfg.Controlplane.OrganizationUUID,
		Token:            a.cfg.Controlplane.Token,
		Name:             "gitlab",
		AllowedSkills:    len(a.allowed),
		LogEventsEnabled: a.cfg.Logs.Enabled == nil || *a.cfg.Logs.Enabled,
		LogVerbosity:     a.cfg.Logs.Verbosity,
		RuntimeConfig:    runtimeConfigForControlPlane(a.cfg),
	}
	if a.cfg.ServiceCredentialsMode() == config.ServiceCredentialsControlPlane {
		coreCfg.AfterConnect = a.prefetchGitLabToken
	}
	a.core = coreexec.New(coreCfg, a.execute)
	return a.core.Start()
}

func (a *Agent) prefetchGitLabToken() error {
	rid := enroll.RunIDFromPrefetchSources()
	if strings.TrimSpace(rid) == "" {
		return fmt.Errorf("service_credentials=control_plane requires use_case_run_id (FLUID_USE_CASE_RUN_ID or FLUID_ENROLLMENT_EXTRA_ARGS JSON)")
	}
	skill := strings.TrimSpace(os.Getenv("FLUID_CP_CREDENTIAL_PREFETCH_SKILL_ID"))
	if skill == "" {
		skill = "gitlab.agent.health"
	}
	if _, ok := a.allowed[skill]; !ok {
		return fmt.Errorf("prefetch skill %q must appear in skills.allowed", skill)
	}
	origin, err := cpcredentials.HTTPOriginFromWebSocketURL(a.cfg.Controlplane.WebSocketURL)
	if err != nil {
		return fmt.Errorf("derive http origin: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	issued, err := cpcredentials.IssueGitLab(ctx, cpcredentials.IssueGitLabParams{
		HTTPOrigin:       origin,
		OrganizationUUID: a.cfg.Controlplane.OrganizationUUID,
		ConnectionToken:  a.cfg.Controlplane.Token,
		SkillID:          skill,
		RunID:            rid,
	})
	if err != nil {
		return err
	}
	a.gitlab.SetToken(issued.Token)
	return nil
}

func runtimeConfigForControlPlane(cfg *config.Config) map[string]interface{} {
	skills := map[string]interface{}{
		"allowed": cfg.Skills.Allowed,
	}
	if len(cfg.Skills.Definitions) > 0 {
		skills["definitions"] = cfg.Skills.Definitions
	}
	return map[string]interface{}{
		"agent": map[string]interface{}{
			"mode":    cfg.Agent.Mode,
			"name":    cfg.Agent.Name,
			"version": cfg.Agent.Version,
		},
		"skills": skills,
	}
}

func (a *Agent) Stop() {
	if a.core != nil {
		a.core.Stop()
	}
}

func (a *Agent) execute(skill string, payload map[string]interface{}, _ map[string]interface{}) (map[string]interface{}, error) {
	if _, ok := a.allowed[skill]; !ok {
		return nil, fmt.Errorf("skill not allowed: %s", skill)
	}

	switch skill {
	case "gitlab.create_branch":
		branch, _ := payload["branch"].(string)
		ref, _ := payload["ref"].(string)
		if ref == "" {
			ref = a.cfg.GitLab.TargetBranch
		}
		if err := a.gitlab.CreateBranch(a.cfg.GitLab.ProjectPath, branch, ref); err != nil {
			return nil, err
		}
		return skillresult.Success(map[string]interface{}{"branch": branch}), nil

	case "gitlab.commit_files":
		branch, _ := payload["branch"].(string)
		message, _ := payload["message"].(string)
		actions, _ := payload["actions"].([]interface{})
		normalized := make([]map[string]interface{}, 0, len(actions))
		for _, a := range actions {
			if m, ok := a.(map[string]interface{}); ok {
				normalized = append(normalized, m)
			}
		}
		if err := a.gitlab.CommitFiles(a.cfg.GitLab.ProjectPath, branch, message, normalized); err != nil {
			return nil, err
		}
		return skillresult.Success(map[string]interface{}{}), nil

	case "gitlab.create_mr":
		source, _ := payload["source_branch"].(string)
		target, _ := payload["target_branch"].(string)
		if target == "" {
			target = a.cfg.GitLab.TargetBranch
		}
		title, _ := payload["title"].(string)
		description, _ := payload["description"].(string)
		url, err := a.gitlab.CreateMergeRequest(a.cfg.GitLab.ProjectPath, source, target, title, description)
		if err != nil {
			return nil, err
		}
		return skillresult.Success(map[string]interface{}{"mr_url": url}), nil

	case "gitlab.agent.health":
		return skillresult.Success(map[string]interface{}{"connected": true}), nil

	case "gitlab.merge_request.approve":
		projectPath := stringFromPayload(payload, "project_path", a.cfg.GitLab.ProjectPath)
		iid, err := intFromPayload(payload["merge_request_iid"])
		if err != nil {
			return nil, err
		}
		res, err := a.gitlab.ApproveMergeRequest(projectPath, iid)
		if err != nil {
			return nil, err
		}
		return skillresult.Success(map[string]interface{}{
			"iid":     res.IID,
			"web_url": res.WebURL,
			"state":   res.State,
		}), nil

	case "gitlab.merge_request.approval_status":
		projectPath := stringFromPayload(payload, "project_path", a.cfg.GitLab.ProjectPath)
		iid, err := intFromPayload(payload["merge_request_iid"])
		if err != nil {
			return nil, err
		}
		approved, user, err := a.gitlab.MergeRequestApprovedByCurrentUser(projectPath, iid)
		if err != nil {
			return nil, err
		}
		out := map[string]interface{}{
			"merge_request_iid":        iid,
			"approved_by_current_user": approved,
		}
		if user != nil {
			out["current_user_id"] = user.ID
			out["current_username"] = user.Username
		}
		return skillresult.Success(out), nil

	case "gitlab.merge_request.status":
		projectPath := stringFromPayload(payload, "project_path", a.cfg.GitLab.ProjectPath)
		iid, err := intFromPayload(payload["merge_request_iid"])
		if err != nil {
			return nil, err
		}
		mr, err := a.gitlab.GetMergeRequest(projectPath, iid)
		if err != nil {
			var apiErr *gitlab.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return skillresult.Success(map[string]interface{}{
					"exists":            false,
					"merge_request_iid": iid,
					"state":             "not_found",
				}), nil
			}
			return nil, err
		}
		return skillresult.Success(map[string]interface{}{
			"exists":                true,
			"merge_request_iid":     mr.IID,
			"iid":                   mr.IID,
			"web_url":               mr.WebURL,
			"state":                 mr.State,
			"detailed_merge_status": mr.DetailedMergeStatus,
			"merge_error":           strings.TrimSpace(mr.MergeError),
		}), nil

	case "gitlab.merge_request.merge":
		projectPath := stringFromPayload(payload, "project_path", a.cfg.GitLab.ProjectPath)
		iid, err := intFromPayload(payload["merge_request_iid"])
		if err != nil {
			return nil, err
		}
		msg, _ := payload["merge_commit_message"].(string)
		res, err := a.gitlab.MergeMergeRequest(projectPath, iid, msg)
		if err != nil {
			return nil, err
		}
		return skillresult.Success(map[string]interface{}{
			"iid":     res.IID,
			"web_url": res.WebURL,
			"state":   res.State,
		}), nil

	case "gitlab.merge_request.assert_merged":
		projectPath := stringFromPayload(payload, "project_path", a.cfg.GitLab.ProjectPath)
		iid, err := intFromPayload(payload["merge_request_iid"])
		if err != nil {
			return nil, err
		}
		mr, err := a.gitlab.GetMergeRequest(projectPath, iid)
		if err != nil {
			return nil, err
		}
		state := strings.ToLower(strings.TrimSpace(mr.State))
		if state != "merged" {
			return nil, fmt.Errorf(
				"merge request not merged (state=%q detailed_merge_status=%q merge_error=%q web_url=%s)",
				mr.State,
				mr.DetailedMergeStatus,
				strings.TrimSpace(mr.MergeError),
				mr.WebURL,
			)
		}
		return skillresult.Success(map[string]interface{}{
			"iid":     mr.IID,
			"web_url": mr.WebURL,
			"state":   mr.State,
		}), nil

	case "gitlab.repo.checkout_mr":
		projectPath := stringFromPayload(payload, "project_path", a.cfg.GitLab.ProjectPath)
		iid, err := intFromPayload(payload["merge_request_iid"])
		if err != nil {
			return nil, err
		}
		targetPath, _ := payload["target_path"].(string)
		if strings.TrimSpace(targetPath) == "" {
			return nil, fmt.Errorf("target_path is required")
		}
		credsFile, _ := payload["credentials_env_file"].(string)
		if strings.TrimSpace(credsFile) == "" {
			return nil, fmt.Errorf("credentials_env_file is required")
		}
		envMap, err := gitlab.LoadEnvFile(credsFile)
		if err != nil {
			return nil, fmt.Errorf("load credentials env file: %w", err)
		}
		token := strings.TrimSpace(envMap["FLUID_GITLAB_SA_TOKEN"])
		if token == "" {
			token = strings.TrimSpace(envMap["GITLAB_TOKEN"])
		}
		if token == "" {
			return nil, fmt.Errorf("FLUID_GITLAB_SA_TOKEN or GITLAB_TOKEN missing in credentials env file")
		}
		host, err := a.resolveGitLabHost(payload)
		if err != nil {
			return nil, err
		}
		runAs := stringFromPayload(payload, "run_as", "")
		if err := gitlab.CheckoutMergeRequestHead(host, projectPath, token, targetPath, iid, runAs); err != nil {
			return nil, err
		}
		if chownTo := stringFromPayload(payload, "chown_repo_to", ""); chownTo != "" && strings.TrimSpace(runAs) == "" {
			if err := gitlab.ChownPathRecursive(targetPath, chownTo); err != nil {
				return nil, err
			}
		}
		return skillresult.Success(map[string]interface{}{
			"target_path":       targetPath,
			"merge_request_iid": iid,
			"ref":               fmt.Sprintf("merge-requests/%d/head", iid),
		}), nil

	default:
		return nil, fmt.Errorf("unsupported skill: %s", skill)
	}
}

func stringFromPayload(payload map[string]interface{}, key, fallback string) string {
	if v, ok := payload[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func intFromPayload(v interface{}) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case string:
		// Allow string IIDs from interpolated YAML payloads.
		var x int
		_, err := fmt.Sscanf(strings.TrimSpace(n), "%d", &x)
		if err != nil || x <= 0 {
			return 0, fmt.Errorf("merge_request_iid must be a positive integer")
		}
		return x, nil
	default:
		return 0, fmt.Errorf("merge_request_iid must be a number")
	}
}

func (a *Agent) resolveGitLabHost(payload map[string]interface{}) (string, error) {
	if h, ok := payload["gitlab_host"].(string); ok && strings.TrimSpace(h) != "" {
		return strings.TrimSpace(h), nil
	}
	u, err := url.Parse(a.cfg.GitLab.URL)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("gitlab_host missing and gitlab.url has no host")
	}
	return u.Host, nil
}
