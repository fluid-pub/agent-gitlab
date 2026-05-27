# Changelog — agent-gitlab

All notable changes to **fluid-pub/agent-gitlab** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tag naming: `0.y.z` (no `v` prefix). Align `cmd/version.go` and `config/agent.example.yml` (`agent.version`) with the tag before release.

## [Unreleased]

## [0.1.0] - 2026-05-26

### Added

- Initial public release on **agent-core** (WebSocket execution, enrollment, `runtime_config` sync).
- Skills: GitLab API (`create_branch`, `commit_files`, `create_mr`, MR approve/merge/status/assert_merged, `agent.health`) and **`gitlab.repo.checkout_mr`** (local git + optional `run_as` / `chown_repo_to`).
- **`service_credentials`**: `local` (default) or `control_plane` (GitLab token prefetch via control plane after WSS connect).
- CI/CD via `fluid-pub/actions`, container image with **git** for checkout skills, GitHub Release asset `fluid-agent-gitlab-linux-amd64`.
- Local `gofmt` pre-commit hook matching CI.
