package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"fluid/agents/core/enroll"
)

type Config struct {
	Agent        AgentConfig        `yaml:"agent"`
	Enrollment   EnrollmentConfig   `yaml:"enrollment,omitempty"`
	GitLab       GitLabConfig       `yaml:"gitlab"`
	Controlplane ControlplaneConfig `yaml:"controlplane"`
	Logs         LogsConfig         `yaml:"logs"`
	Skills       SkillsConfig       `yaml:"skills"`
	// ServiceCredentials is "local" (default) or "control_plane" (GitLab token from CP after connect).
	ServiceCredentials string `yaml:"service_credentials,omitempty"`
}

const (
	ServiceCredentialsLocal        = "local"
	ServiceCredentialsControlPlane = "control_plane"
	gitlabServiceCredsEnv          = "FLUID_SERVICE_CREDENTIALS"
)

// ServiceCredentialsMode returns normalized service_credentials (default: local). Override with FLUID_SERVICE_CREDENTIALS.
func (c *Config) ServiceCredentialsMode() string {
	if v := strings.TrimSpace(os.Getenv(gitlabServiceCredsEnv)); v != "" {
		return strings.ToLower(v)
	}
	s := strings.ToLower(strings.TrimSpace(c.ServiceCredentials))
	if s == "" {
		return ServiceCredentialsLocal
	}
	return s
}

// EnrollmentConfig holds optional first-boot enrollment overrides (read before POST /enrollment/enroll).
// Name is the control plane Agent.name (JSON "name" on enroll).
// DeprecatedDisplayName maps YAML key display_name (deprecated; prefer name).
type EnrollmentConfig struct {
	Name                  string `yaml:"name"`
	DeprecatedDisplayName string `yaml:"display_name,omitempty"`
}

type AgentConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Mode    string `yaml:"mode"`
}

type GitLabConfig struct {
	URL          string `yaml:"url"`
	Token        string `yaml:"token"`
	APIv         string `yaml:"api_version"`
	ProjectPath  string `yaml:"project_path"`
	TargetBranch string `yaml:"target_branch"`
}

type ControlplaneConfig struct {
	WebSocketURL     string `yaml:"websocket_url"`
	OrganizationUUID string `yaml:"organization_uuid"`
	Token            string `yaml:"token"`
}

type SkillsConfig struct {
	Allowed     []string               `yaml:"allowed"`
	Definitions map[string]interface{} `yaml:"definitions,omitempty"`
}

type LogsConfig struct {
	Enabled   *bool  `yaml:"enabled"`
	Verbosity string `yaml:"verbosity"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	cfg.resolveEnvVars()
	cfg.applyDefaults()
	// Do not call Validate here: first-boot enrollment supplies organization_uuid and token after POST /enrollment/enroll.
	return &cfg, nil
}

func (c *Config) resolveEnvVars() {
	c.ServiceCredentials = resolve(c.ServiceCredentials)
	c.Enrollment.Name = resolve(c.Enrollment.Name)
	c.Enrollment.DeprecatedDisplayName = resolve(c.Enrollment.DeprecatedDisplayName)
	c.GitLab.Token = resolve(c.GitLab.Token)
	c.Controlplane.WebSocketURL = resolve(c.Controlplane.WebSocketURL)
	c.Controlplane.OrganizationUUID = resolve(c.Controlplane.OrganizationUUID)
	c.Controlplane.Token = resolve(c.Controlplane.Token)
	c.resolveControlplaneFromRegistrationURL()
}

func (c *Config) resolveControlplaneFromRegistrationURL() {
	if c.Controlplane.WebSocketURL == "" {
		return
	}

	u, err := url.Parse(c.Controlplane.WebSocketURL)
	if err != nil {
		return
	}

	q := u.Query()
	if c.Controlplane.OrganizationUUID == "" {
		c.Controlplane.OrganizationUUID = q.Get("organization_uuid")
	}
	if c.Controlplane.Token == "" {
		c.Controlplane.Token = q.Get("token")
	}

	if q.Has("organization_uuid") || q.Has("token") {
		q.Del("organization_uuid")
		q.Del("token")
		u.RawQuery = q.Encode()
		c.Controlplane.WebSocketURL = u.String()
	}
}

func (c *Config) applyDefaults() {
	if c.Logs.Enabled == nil {
		v := true
		c.Logs.Enabled = &v
	}
	if strings.TrimSpace(c.Logs.Verbosity) == "" {
		c.Logs.Verbosity = "normal"
	}
}

func (c *Config) Validate() error {
	if c.Agent.Mode == "" {
		c.Agent.Mode = "execution"
	}
	if c.Agent.Mode != "execution" {
		return fmt.Errorf("agent.mode must be execution")
	}
	if c.Controlplane.WebSocketURL == "" || c.Controlplane.OrganizationUUID == "" || c.Controlplane.Token == "" {
		return fmt.Errorf("controlplane websocket_url, organization_uuid and token are required")
	}
	if c.GitLab.URL == "" || c.GitLab.ProjectPath == "" || c.GitLab.TargetBranch == "" {
		return fmt.Errorf("gitlab url, project_path and target_branch are required")
	}
	mode := c.ServiceCredentialsMode()
	switch mode {
	case ServiceCredentialsLocal, ServiceCredentialsControlPlane:
	default:
		return fmt.Errorf("service_credentials must be %q or %q (got %q)", ServiceCredentialsLocal, ServiceCredentialsControlPlane, mode)
	}
	if mode == ServiceCredentialsLocal && strings.TrimSpace(c.GitLab.Token) == "" {
		return fmt.Errorf("gitlab token is required when service_credentials=%s", ServiceCredentialsLocal)
	}
	if mode == ServiceCredentialsControlPlane && enroll.RunIDFromPrefetchSources() == "" {
		return fmt.Errorf("service_credentials=%s requires use_case_run_id (FLUID_USE_CASE_RUN_ID or FLUID_ENROLLMENT_EXTRA_ARGS JSON)", ServiceCredentialsControlPlane)
	}
	if len(c.Skills.Allowed) == 0 {
		return fmt.Errorf("skills.allowed must contain at least one skill")
	}
	return nil
}

func resolve(v string) string {
	if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
		key := strings.TrimSuffix(strings.TrimPrefix(v, "${"), "}")
		return os.Getenv(key)
	}
	return v
}
