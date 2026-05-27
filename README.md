# fluid-pub/agent-gitlab

Fluid **execution agent** for **GitLab**: maintains a **persistent WSS** connection to the control plane (`/v1/agents/websocket`) for skills, logs, and `runtime_config` sync. GitLab REST API skills and **`gitlab.repo.checkout_mr`** (local **git**) run in this agent — not on the Linux interface agent.

## Repository layout

| Path | Role |
|------|------|
| `core/` | Git submodule → [`fluid-pub/agent-core`](https://github.com/fluid-pub/agent-core) |
| `cmd/` | Entrypoint and `cmd/version.go` (semver for releases) |
| `internal/` | GitLab API client, agent skills, config |
| `config/agent.example.yml` | Configuration template |
| `.github/workflows/` | CI and release via [`fluid-pub/actions`](https://github.com/fluid-pub/actions) |

## Local development

One-time per clone, enable the same **`gofmt`** check as CI:

```bash
./scripts/install-git-hooks.sh
```

```bash
git submodule update --init --recursive
cp config/agent.example.yml config/agent.yml
cp env.secrets.example env.secrets
# Set GITLAB_* and FLUID_CONTROLPLANE_* in env.secrets (never commit that file).
source env.secrets
make dev
```

`make dev` runs `go run ./cmd` with `-config config/agent.yml`.

**Monorepo Fluid** (`code/agents/gitlab/`): use `make monorepo-replace` so `go.mod` points at `../core` instead of the `core/` submodule. Do not commit that replace on `develop` (CI and releases use `replace => ./core`).

### Git in the Fluid workspace

```bash
cd code/agents/gitlab
git remote add origin git@github.com:fluid-pub/agent-gitlab.git   # if needed
git fetch origin
git checkout -B develop origin/develop
git submodule update --init --recursive
./scripts/install-git-hooks.sh
make monorepo-replace   # optional when using code/agents/core in the monorepo
```

Keep `env.secrets` and `config/agent.yml` local (gitignored).

## Control plane connection

| Phase | Transport | Purpose |
|-------|-----------|---------|
| Steady state | **WSS** (`controlplane.websocket_url`) | `skill_invoke` / `skill_result`, log events, `runtime_config` push |
| First boot only (optional) | HTTP `POST /api/v1/enrollment/enroll` | Exchange `FLUID_ENROLLMENT_TOKEN` for `organization_uuid` + connection token |

Durable credentials: `/etc/fluid/gitlab/credentials.yaml` (`-credentials` flag). Enroll with `agent_type: gitlab`, `principal: execution_agent`.

### Service credentials

- **`local`** (default): GitLab token from config / env / `FLUID_SERVICE_CREDENTIALS_ENV_FILE`.
- **`control_plane`**: after WSS connect, prefetch GitLab token from the control plane (requires `use_case_run_id` in enrollment extra args or `FLUID_USE_CASE_RUN_ID`).

## Container image

The published image includes **`git`** (and runs as **root**) so **`gitlab.repo.checkout_mr`** can clone under `/tmp/fluid/` workspaces. API-only deployments may still use the same image.

## Releases

Push a semver tag **without** `v` (e.g. `0.1.0`) matching `var Version` in `cmd/version.go`. The release workflow publishes:

- `ghcr.io/fluid-pub/agent-gitlab:<tag>`
- GitHub Release asset `fluid-agent-gitlab-linux-amd64` and `SHA256SUMS.txt`

Release builds pin **`agent-core`** to the **same semver tag** when `core_ref` is empty in the workflow (see `.github/workflows/release-on-semver-tag.yml`).

## Changelog

Release notes: [CHANGELOG.md](CHANGELOG.md).

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting. Repository automation includes Dependabot and CodeQL.
