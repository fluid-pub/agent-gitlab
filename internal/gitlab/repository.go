package gitlab

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// LoadEnvFile parses a simple KEY=value env file (one assignment per line, # comments).
func LoadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	env := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		env[k] = v
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return env, nil
}

// envForGitIdentity forces HOME/USER/LOGNAME for run_as so git never inherits root identity when euid drops.
func envForGitIdentity(env []string, login, home string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "USER=") || strings.HasPrefix(e, "LOGNAME=") {
			continue
		}
		filtered = append(filtered, e)
	}
	id := []string{
		fmt.Sprintf("HOME=%s", home),
		fmt.Sprintf("USER=%s", login),
		fmt.Sprintf("LOGNAME=%s", login),
	}
	return append(id, filtered...)
}

func newGitCmd(runAs string, gitArgs ...string) (*exec.Cmd, error) {
	cmd := exec.Command("git", gitArgs...)
	runAs = strings.TrimSpace(runAs)
	if runAs == "" {
		return cmd, nil
	}
	urec, err := user.Lookup(runAs)
	if err != nil {
		return nil, fmt.Errorf("run_as: unknown user %q: %w", runAs, err)
	}
	uid, err := parseUint32ID("uid", urec.Uid)
	if err != nil {
		return nil, fmt.Errorf("run_as: %w", err)
	}
	gid, err := parseUint32ID("gid", urec.Gid)
	if err != nil {
		return nil, fmt.Errorf("run_as: %w", err)
	}
	euid := uint32(os.Geteuid())
	if euid != 0 {
		if euid != uid {
			return nil, fmt.Errorf("run_as=%q requires a root gitlab agent (euid=%d)", runAs, euid)
		}
		return cmd, nil
	}
	if uid != 0 {
		h := strings.TrimSpace(urec.HomeDir)
		if h != "" {
			cmd.Env = envForGitIdentity(os.Environ(), urec.Username, h)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: uid, Gid: gid},
		}
	}
	return cmd, nil
}

func parseUint32ID(kind, value string) (uint32, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", kind, err)
	}
	return uint32(parsed), nil
}

func gitCombinedOutput(runAs string, gitArgs ...string) ([]byte, error) {
	cmd, err := newGitCmd(runAs, gitArgs...)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}

func gitCombinedOutputWithAuth(runAs, authHeader string, gitArgs ...string) ([]byte, error) {
	args := append([]string{"-c", "http.extraHeader=" + authHeader}, gitArgs...)
	return gitCombinedOutput(runAs, args...)
}

// CheckoutMergeRequestHead materializes the MR tip in targetPath using local git against GitLab HTTPS.
// Uses GitLab fetch ref merge-requests/<IID>/head (same-repo MRs).
// When runAs is non-empty and the agent is root, git runs with that POSIX login (Credential); the repo parent dir must be writable by that user (e.g. debian.workspace.prepare workspace_owner).
func CheckoutMergeRequestHead(host, projectPath, token, targetPath string, mrIID int, runAs string) error {
	if strings.TrimSpace(host) == "" || strings.TrimSpace(projectPath) == "" || strings.TrimSpace(token) == "" {
		return fmt.Errorf("host, project_path and token are required")
	}
	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}
	ref := fmt.Sprintf("merge-requests/%d/head", mrIID)
	branch := fmt.Sprintf("fluid-mr-%d", mrIID)
	remote := httpsGitRemoteURL(host, projectPath)
	authHeader := gitAuthHeader(token)

	gitDir := filepath.Join(targetAbs, ".git")
	if st, err := os.Stat(gitDir); err == nil && st.IsDir() {
		return fetchAndCheckoutExisting(targetAbs, remote, authHeader, ref, branch, runAs)
	}

	if err := os.MkdirAll(targetAbs, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(targetAbs)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("target_path must be empty or an existing git clone: %s", targetAbs)
	}

	if out, err := gitCombinedOutput(runAs, "-C", targetAbs, "init"); err != nil {
		return fmt.Errorf("git init: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := gitCombinedOutput(runAs, "-C", targetAbs, "remote", "add", "origin", remote); err != nil {
		return fmt.Errorf("git remote add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	fetchRef := fmt.Sprintf("%s:%s", ref, branch)
	if out, err := gitCombinedOutputWithAuth(runAs, authHeader, "-C", targetAbs, "fetch", "origin", fetchRef); err != nil {
		return fmt.Errorf("git fetch: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := gitCombinedOutput(runAs, "-C", targetAbs, "checkout", branch); err != nil {
		return fmt.Errorf("git checkout: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func fetchAndCheckoutExisting(dir, remote, authHeader, ref, branch, runAs string) error {
	if out, err := gitCombinedOutput(runAs, "-C", dir, "remote", "set-url", "origin", remote); err != nil {
		return fmt.Errorf("git remote set-url: %w: %s", err, strings.TrimSpace(string(out)))
	}
	fetchRef := fmt.Sprintf("%s:%s", ref, branch)
	if out, err := gitCombinedOutputWithAuth(runAs, authHeader, "-C", dir, "fetch", "origin", fetchRef); err != nil {
		return fmt.Errorf("git fetch: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := gitCombinedOutput(runAs, "-C", dir, "checkout", branch); err != nil {
		return fmt.Errorf("git checkout: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func httpsGitRemoteURL(host, projectPath string) string {
	host = strings.TrimSpace(host)
	projectPath = strings.Trim(projectPath, "/")
	u := url.URL{
		Scheme: "https",
		Host:   host,
		Path:   "/" + projectPath + ".git",
	}
	return u.String()
}

func gitAuthHeader(token string) string {
	creds := "oauth2:" + strings.TrimSpace(token)
	return "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}

// ChownPathRecursive runs chown -R uid:gid on path so a root git checkout can be handed to an
// unprivileged login (e.g. linux.script.run run_as). Path must resolve under /tmp/fluid/.
func ChownPathRecursive(path, login string) error {
	login = strings.TrimSpace(login)
	if login == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("chown path abs: %w", err)
	}
	clean := filepath.Clean(abs)
	if !strings.HasPrefix(clean, "/tmp/fluid/") && !strings.HasPrefix(clean, "/private/tmp/fluid/") {
		return fmt.Errorf("chown path must be under /tmp/fluid (got %s)", clean)
	}
	u, err := user.Lookup(login)
	if err != nil {
		return fmt.Errorf("chown_repo_to: lookup %q: %w", login, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("chown_repo_to: uid: %w", err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("chown_repo_to: gid: %w", err)
	}
	spec := fmt.Sprintf("%d:%d", uid, gid)
	out, err := exec.Command("chown", "-R", spec, clean).CombinedOutput()
	if err != nil {
		return fmt.Errorf("chown -R %s %s: %w: %s", spec, clean, err, strings.TrimSpace(string(out)))
	}
	return nil
}
