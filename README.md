> ⚠️ This is a public beta release. Use at your own risk. Feedback and contributions are welcome!

# Konta - GitOps tool for Docker Compose

A lightweight, idempotent deployment system for single-node servers.

Keep your infrastructure in Git. Deploy automatically. Stay simple.

**✨ Perfect for:** Single VPS, Docker Compose stacks, self-hosted projects.

1. **Stores your infrastructure in Git** (like Flux for Kubernetes, but simpler)
2. **Periodically checks for changes** (polling every 2 minutes by default)
3. **Automatically deploys new versions** (idempotent Docker Compose updates)
4. **Cleans up old containers** (orphan cleanup)
5. **Ensures atomic updates** (safe even if interrupted)

Download and use it now:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/talyguryn/konta/main/scripts/install.sh)"
```

## Why

If you're like me, you want a simple way to manage your server infrastructure using Git. I prefer managing my projects on VPS with a few docker-compose files in separate dirs. But Also I want to have a single source of truth for my infrastructure in Git, and I want it to automatically deploy whenever I push changes. I'd love to use k3s + flux but it's overkill for a single VPS. I just want something simple, reliable, and Git-driven.

So I built Konta — a tiny, single binary tool that watches a Git repository and automatically deploys Docker Compose stacks on your server. It handles atomic updates, orphan cleanup, and hooks for pre/post deploy scripts. It's perfect for anyone who wants GitOps for their single-node Docker infrastructure without the complexity of Kubernetes.

Kónta is short for container, stylized with K letter.

## Key Features

- ✅ **Single Static Binary** — No dependencies
- ✅ **Git-Driven** — One source of truth
- ✅ **Atomic Deployments** — Zero-downtime updates
- ✅ **Safe** — Only manages its own containers
- ✅ **Simple** — Just YAML + Docker Compose
- ✅ **Multi-Server** — One repo, multiple servers
- ✅ **Hooks** — Pre/post deploy scripts
- ✅ **Idempotent** — Safe to run repeatedly

<!-- ## When to Use Konta?

| Use Konta if | Don't use Konta if |
|---|---|
| ✅ Single VPS | ❌ Need multi-node clustering |
| ✅ Using Docker Compose | ❌ Need 99.9% uptime SLA |
| ✅ Want Git as source of truth | ❌ Need RBAC/authorization |
| ✅ Simple, reliable deployments | ❌ Need complex orchestration | -->

<!-- ## Comparison

| Feature | Konta | Kubernetes | Shell Scripts |
|---|---|---|---|
| GitOps | ✅ | ✅ | ❌ |
| Setup time | 5 min | Days | Hour |
| Learning curve | Easy | Steep | Easy |
| Docker Compose | ✅ | ❌ | ✅ |
| Single node | ✅ | ⚠️ | ✅ |
| Resource usage | 12MB | 1GB+ | 1MB | -->

## Quick Start

### Prepare your Git repo for infrastructure

Here is an example structure for your infrastructure repository.

We describe server structure via two directories:
- `apps` directory contains sub-folders for each of your applications, each with its own `docker-compose.yml` file. This allows you to manage multiple services in a modular way.
- `hooks` (optional) directory contains scripts that will be executed at different stages of the deployment process (e.g., pre-deploy, post-deploy, on failure). This allows you to perform custom actions like notifications, backups, or health checks.

The path to root of the apps and hooks (server structure) will be used in the `--path` parameter during installation. Konta will look for docker-compose files in the `apps` directory and for hook scripts in the `hooks` directory (optional).

Simple single server structure repo:

```
apps/
├── web/
│   └── docker-compose.yml
└── api/
    ├── .env
    └── docker-compose.yml
hooks/
├── pre.sh
└── success.sh
```

Multi-server structure repo (recommended for multiple servers, but not required). Each server has its own directory with apps and hooks. This allows you to manage multiple servers from the same repository, while keeping their configurations organized and separate.

```
vps0/
├── apps/
│   ├── web/
│   │   └── docker-compose.yml
│   └── api/
│       ├── .env
│       └── docker-compose.yml
└── hooks/
    ├── pre.sh
    └── success.sh
vps-production/
├── apps/
│   ├── landing/
│   │   ├── html
│   │   │   └── index.html
│   │   └── docker-compose.yml
│   └── ...
└── hooks/
    ├── pre.sh
    ├── failure.sh
    └── success.sh
```

### Download Konta

**Recommended: One-line curl installer:**

Install the latest version of Konta with a single command. This script detects your OS and architecture, downloads the appropriate binary, and sets it up for you. Also it checks if Docker is installed and running, and prompts you to fix it if not.

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/talyguryn/konta/main/scripts/install.sh)"
```

**Or download binary manually:**

```bash
# Linux (AMD64)
curl -fsSL https://github.com/talyguryn/konta/releases/latest/download/konta-linux -o /usr/local/bin/konta

# macOS (Apple Silicon)
# curl -fsSL https://github.com/talyguryn/konta/releases/latest/download/konta-darwin-arm64 -o /usr/local/bin/konta

# macOS (Intel)
# curl -fsSL https://github.com/talyguryn/konta/releases/latest/download/konta-darwin-amd64 -o /usr/local/bin/konta

chmod +x /usr/local/bin/konta
```

Binaries are available on the [releases page](https://github.com/talyguryn/konta/releases/latest).

### Installation process

**Interactive mode (default):**
```bash
sudo konta install
```

**Or with CLI parameters (no prompts):**
```bash
sudo konta install \
  --repo https://github.com/yourname/infrastructure \
  --branch main \
  --path vps0/apps \
  --interval 120 \
  --konta_updates notify
```

Using KONTA_TOKEN for private repos:
```bash
export KONTA_TOKEN=gh_your_github_token
sudo konta install \
  --repo https://github.com/yourname/infrastructure
```

<!-- ### 3. Example: Manage Konta Itself

Here's a cool example - use Konta to manage Konta!

```bash
# Setup Konta to monitor this repository
sudo konta install \
  --repo https://github.com/talyguryn/konta \
  --path spb/apps \
  --branch main \
  --interval 120 \
  --konta_updates notify

# Start the daemon
sudo konta daemon enable

# Konta will now:
# 1. Check for changes every configured interval
# 2. Only restart containers that changed
# 3. Skip unchanged apps (beszel-agent, nginx, etc.)
# 4. Manage pre/success hooks from spb/hooks/
# 5. Check for Konta updates (notify mode: log only, no auto-install)
```

This demonstrates real use: each git push to the infrastructure repository triggers only the affected services to update! -->

## Configuration file

Configuration is auto-generated at `/etc/konta/config.yaml`.

- ✅ Auto-configure hooks paths (looks in `{path}/hooks/` for just filenames)
- ✅ Detect config changes by comparing with lock file
- ✅ Support dynamic interval updates (edit config and save)

**Example:**

```yaml
version: v1

repository:
  url: https://github.com/yourname/infrastructure
  branch: main
  path: spb              # Base path (omit or use . for repo root)
  interval: 120
  token: gh_xxxx         # Optional, or use KONTA_TOKEN env var

konta_updates: notify    # auto|notify|false - manage update checks

hooks:
  pre: pre.sh            # Optional, found in {path}/hooks/
  success: success.sh    # Optional, found in {path}/hooks/
  failure: failure.sh    # Optional, found in {path}/hooks/
  post_update: post_update.sh  # Optional, after Konta binary update

deploy:
  atomic: true

logging:
  level: info  # debug|info|warn|error
```

**Update Behavior:**
- `auto` — Check for updates and auto-install (safe minor versions only)
- `notify` (default) — Log when updates available, user decides when to update
- `false` or omit — Disable update checks entirely

## License

MIT — Free for personal and commercial use.