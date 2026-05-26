package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// MergeDotenvIntoGitLab parses KEY=value lines and applies GITLAB_TOKEN (or FLUID_GITLAB_TOKEN) onto cfg.GitLab.
func MergeDotenvIntoGitLab(cfg *Config, path string) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open service credentials env file: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		switch k {
		case "GITLAB_TOKEN", "FLUID_GITLAB_TOKEN":
			if v != "" {
				cfg.GitLab.Token = v
			}
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read service credentials env file: %w", err)
	}
	return nil
}
