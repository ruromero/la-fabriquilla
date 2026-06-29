# la-fabriquilla

Autonomous software development orchestrator. Polls GitHub issues tagged `fabriquilla:ready`, drives them through a phased pipeline using local LLMs via any OpenAI-compatible API, and opens PRs with the results.

## Pipeline

1. **Gather** — collects repo context (file structure, docs, conventions) using read-only tools
2. **Research** — configurable model (e.g. Gemini) for external context gathering
3. **Plan** — configurable model via any OpenAI-compatible API decomposes the issue into an implementation plan
4. **Design** — produces API contracts, data models, file structure using read-only code navigation
5. **Code** — generates implementation files
6. **Review** — correctness + security + intent alignment review (must use a different model family than code generation)
7. **Iterate** — applies review feedback (max N loops)

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed data flow and package layout.

## Requirements

- Linux host (amd64)
- [Ollama](https://ollama.com) with models pulled (e.g. `gemma4-12b`, `qwen2.5-coder-14b`)
- GitHub App(s) installed on target repos (see [Authentication](#authentication))
- API keys for remote endpoints (Gemini, DeepSeek, etc.)
- Optional: [OpenShell](https://github.com/nicholasgasior/openshell) for sandboxed phase execution

## Installation

Install as a systemd service from GitHub Releases:

```bash
curl -sSL https://raw.githubusercontent.com/ruromero/la-fabriquilla/main/deploy/systemd/install.sh | sudo bash
```

Pin a specific version:

```bash
curl -sSL https://raw.githubusercontent.com/ruromero/la-fabriquilla/main/deploy/systemd/install.sh | sudo bash -s -- --version v0.1.0
```

The installer downloads pre-built binaries, creates a `fabriquilla` service user, installs the systemd unit, and sets up sandbox policies. After installing:

```bash
# 1. Copy your config
sudo cp config.json /etc/fabriquilla/config.json

# 2. Copy GitHub App PEM keys
sudo cp *.pem /etc/fabriquilla/keys/
sudo chown root:fabriquilla /etc/fabriquilla/keys/*.pem
sudo chmod 640 /etc/fabriquilla/keys/*.pem

# 3. Edit API keys
sudo vi /etc/fabriquilla/env

# 4. Start the service
sudo systemctl enable --now fabriquilla

# 5. Watch logs
journalctl -u fabriquilla -f
```

### Build from source

```bash
make build
./bin/dispatcher --config config.json
```

## Smoke Test

End-to-end pipeline smoke test. Two modes:

| Mode | Command | Requirements |
|------|---------|--------------|
| Full mock (CI) | `make smoke-test-ci` | None |
| Full mock (binary) | `make smoke-test` | None |
| Full | `./bin/smoke-test -mode full -config smoke-config.json` | Ollama + test repo + GitHub App |

## Authentication

### Three GitHub Apps (recommended)

The factory uses three separate GitHub Apps with scoped permissions, aligned to trust boundaries:

| App | Used by | Permissions |
|---|---|---|
| **fabriquilla-dispatcher** | dispatcher | Issues (Read & write), Contents (Read-only), Metadata (Read-only) |
| **fabriquilla-worker** | gatherer, coder | Contents (Read-only), Metadata (Read-only) |
| **fabriquilla-committer** | committer | Contents (Read & write), Pull requests (Read & write), Issues (Read & write), Metadata (Read-only) |

Setup for each app:

1. Create a GitHub App at **Settings > Developer settings > GitHub Apps**
   - Homepage URL: your repo URL
   - Disable Webhook (uncheck "Active")
   - Set only the permissions listed above for each app
2. Generate a private key and download the `.pem` file
3. Install the app on target repos — note the **Installation ID** from the URL (`github.com/settings/installations/<id>`)
4. Configure the `apps` map in `config.json` (see Configuration below)
5. Set private key paths via env vars or config

Env vars for private key paths:
- `FABRIQUILLA_DISPATCHER_KEY_PATH` — dispatcher app private key
- `FABRIQUILLA_WORKER_KEY_PATH` — worker app private key
- `FABRIQUILLA_COMMITTER_KEY_PATH` — committer app private key
- `GITHUB_APP_PRIVATE_KEY_PATH` — fallback for any app without an explicit path

### Single GitHub App (simpler alternative)

If you prefer a simpler setup, you can use a single GitHub App with all permissions. Configure it per-repo in `config.json` with `app_id` and `installation_id`. The factory will use this app for all binaries.

### PAT (fallback)

If `app_id` is not set, the orchestrator falls back to a static token:

```json
{"owner": "ruromero", "repo": "example", "token": "ghp_..."}
```

## Configuration

The orchestrator supports multiple repos in a single instance. All inference endpoints are defined in a shared `endpoints` registry. Models reference endpoints with `name@endpoint` syntax. Credentials are loaded from env vars referenced by `api_key_env`, not stored in the config file:

- `GEMINI_API_KEY` — Gemini API key (referenced by `api_key_env` in endpoint config)
- `DEEPSEEK_API_KEY` — DeepSeek API key
- `GITHUB_APP_PRIVATE_KEY_PATH` — fallback path to a GitHub App `.pem` file
- `FABRIQUILLA_DISPATCHER_KEY_PATH` — dispatcher app private key path
- `FABRIQUILLA_WORKER_KEY_PATH` — worker app private key path
- `FABRIQUILLA_COMMITTER_KEY_PATH` — committer app private key path

```json
{
  "default_model": "gemma4-12b:latest@ollama",
  "coder": {"model": "qwen2.5-coder-14b:latest@ollama"},
  "planner": {"model": "gemini-2.5-flash@gemini"},
  "researcher": {"model": "gemini-2.5-flash@gemini"},
  "arbiter": {"model": "deepseek-chat@deepseek"},
  "endpoints": {
    "ollama": {"base_url": "http://localhost:11434/v1"},
    "gemini": {"base_url": "https://generativelanguage.googleapis.com/v1beta/openai", "api_key_env": "GEMINI_API_KEY"},
    "deepseek": {"base_url": "https://api.deepseek.com/v1", "api_key_env": "DEEPSEEK_API_KEY"}
  },
  "poll_interval": "30s",
  "max_iterations": 3,
  "shadow_mode": false,
  "apps": {
    "dispatcher": {"app_id": 111111, "installation_id": 222222},
    "worker": {"app_id": 333333, "installation_id": 444444},
    "committer": {"app_id": 555555, "installation_id": 666666}
  },
  "repos": [
    {"owner": "ruromero", "repo": "la-fabriquilla"},
    {"owner": "ruromero", "repo": "example-repo"}
  ]
}
```

When `apps` is configured, each binary authenticates with its scoped App identity. The `repos` list no longer needs `app_id`/`installation_id` per repo (those are inherited from the app config). Per-repo auth fields are still supported as a fallback for single-app setups.

## Repo readiness

The factory will skip repos that don't meet minimum requirements:
- `CODEOWNERS` — protects security-critical paths from autonomous modification
- `.serena/` — Serena MCP project config for LSP-powered code navigation

Context documents are loaded from the `include_docs` list in the repo config. Defaults to `README.md`, `ARCHITECTURE.md`, `CONVENTIONS.md`. Missing files are skipped (a warning is logged if none are found). Subpaths are supported (e.g. `docs/ARCHITECTURE.md`).

```json
"repos": [
  {
    "owner": "example", "repo": "app",
    "include_docs": ["README.md", "docs/ARCHITECTURE.md", "CONVENTIONS.md", "AGENTS.md"]
  }
]
```

## GitHub labels

| Label | Meaning |
|-------|---------|
| `fabriquilla:ready` | Issue ready for the factory to pick up |
| `fabriquilla:in-progress` | Factory is working on this issue |
| `fabriquilla:needs-info` | Planner needs more info from human |
| `fabriquilla:needs-human` | Factory stuck, requires human intervention |
| `fabriquilla:done` | PR opened, ready for human merge |
| `fabriquilla:tracking` | Parent issue decomposed into sub-issues |
| `fabriquilla:blocked` | Sub-issue waiting on dependency |
| `fabriquilla:requirements` | Repo missing required files (ARCHITECTURE.md, etc.) |

## Deployment

### Systemd (recommended)

See [Installation](#installation) above. The dispatcher runs as a systemd service, executing phase binaries as subprocesses. When [OpenShell](https://github.com/nicholasgasior/openshell) is installed and `sandbox.enabled` is set in config, phases run inside isolated containers with per-phase network policies.

### Container (k8s / Docker)

A container image is published on every release:

```bash
docker pull ghcr.io/ruromero/fabriquilla:latest
```

Credentials are injected via environment or mounted files — never baked into images. See `deploy/k8s/` for example manifests.

Note: OpenShell sandboxing is not available inside containers without mounting the host container runtime socket.

Sandbox images are built from `deploy/sandbox-images/` (base + language-specific).
See ARCHITECTURE.md §5 for the image layout.

## Design

Built following [fullsend](https://github.com/fullsend-ai/fullsend) patterns:
- Security-first: input sanitization, credential isolation, no agent self-modification
- Decomposed review: correctness, security, intent alignment as separate agents
- Shadow mode: all PRs require human approval until graduation
- Zero framework cognition: orchestrator handles mechanics, LLMs handle judgment
- MCP integration: Serena (LSP) and Context7 (docs) as tool providers

## License

Apache License 2.0
