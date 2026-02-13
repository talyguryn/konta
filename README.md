> ⚠️ This is a public beta release. Use at your own risk. Feedback and contributions are welcome!

# Konta - GitOps tool for Docker Compose

A lightweight, idempotent deployment system for single-node servers.

Keep your infrastructure in Git. Deploy automatically. Stay simple.

**Perfect for:** Single VPS, Docker Compose stacks, self-hosted projects.

## Why

If you're like me, you want a simple way to manage your server infrastructure using Git. I prefer managing my projects on VPS with a few docker-compose files in separate dirs. But Also I want to have a single source of truth for my infrastructure in Git, and I want it to automatically deploy whenever I push changes. I'd love to use k3s + flux but it's overkill for a single VPS. I just want something simple, reliable, and Git-driven.

So I built Konta — a tiny, single binary tool that watches a Git repository and automatically deploys Docker Compose stacks on your server. It handles atomic updates, orphan cleanup, and hooks for pre/post deploy scripts. It's perfect for anyone who wants GitOps for their single-node Docker infrastructure without the complexity of Kubernetes.

Kónta is short for container, stylized with K letter.

## What is Konta?

Konta is a simple tool that:

1. **Stores your infrastructure in Git** (like Flux for Kubernetes, but simpler)
2. **Periodically checks for changes** (polling every 2 minutes by default)
3. **Automatically deploys new versions** (idempotent Docker Compose updates)
4. **Cleans up old containers** (orphan cleanup)
5. **Ensures atomic updates** (safe even if interrupted)

## Key Features

- ✅ **Single Static Binary** — No dependencies
- ✅ **Git-Driven** — One source of truth
- ✅ **Atomic Deployments** — Zero-downtime updates
- ✅ **Safe** — Only manages its own containers
- ✅ **Simple** — Just YAML + Docker Compose
- ✅ **Multi-Server** — One repo, multiple servers
- ✅ **Hooks** — Pre/post deploy scripts
- ✅ **Idempotent** — Safe to run repeatedly

## When to Use Konta?

| Use Konta if | Don't use Konta if |
|---|---|
| ✅ Single VPS/server | ❌ Need multi-node clustering |
| ✅ Using Docker Compose | ❌ Need 99.9% uptime SLA |
| ✅ Want Git as source of truth | ❌ Need RBAC/authorization |
| ✅ Simple, reliable deployments | ❌ Need complex orchestration |

## Comparison

| Feature | Konta | Kubernetes | Shell Scripts |
|---|---|---|---|
| GitOps | ✅ | ✅ | ❌ |
| Setup time | 5 min | Days | Hour |
| Learning curve | Easy | Steep | Easy |
| Docker Compose | ✅ | ❌ | ✅ |
| Single node | ✅ | ⚠️ | ✅ |
| Resource usage | 12MB | 1GB+ | 1MB |

## Quick Start (5 minutes)

### 1. Download & Install

**Recommended: One-line curl installer (like Homebrew):**
```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/talyguryn/konta/main/scripts/install.sh)"
```

**Or download manually:**
```bash
# Linux (AMD64)
curl -fsSL https://github.com/talyguryn/konta/releases/latest/download/konta-linux -o /usr/local/bin/konta

# macOS (Apple Silicon)
# curl -fsSL https://github.com/talyguryn/konta/releases/latest/download/konta-darwin-arm64 -o /usr/local/bin/konta

# macOS (Intel)
# curl -fsSL https://github.com/talyguryn/konta/releases/latest/download/konta-darwin-amd64 -o /usr/local/bin/konta

chmod +x /usr/local/bin/konta
```

### 2. Setup with One Command

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
  --repo https://github.com/yourname/infrastructure \
  --path apps
```

### 3. Example: Manage Konta Itself

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

This demonstrates real use: each git push to the infrastructure repository triggers only the affected services to update!

### 4. Configuration

Configuration is auto-generated at `/etc/konta/config.yaml`:
- ✅ Auto-configure hooks paths (looks in `{path}/hooks/`)
- ✅ Create config.lock with full backup for recovery
- ✅ Log the Konta version before each run
- ✅ Support dynamic interval updates (edit config and save)
- ✅ Detect config changes by comparing with lock file

**Configuration Example:**
```yaml
version: v1
repository:
  url: https://github.com/yourname/infrastructure
  branch: main
  path: apps
  interval: 120
  token: gh_xxxx  # Optional, or use KONTA_TOKEN env var
konta_updates: notify  # auto|notify|false - manage update checks
hooks:
  pre: spb/hooks/pre.sh           # Optional
  success: spb/hooks/success.sh   # Optional
  failure: spb/hooks/failure.sh   # Optional
deploy:
  atomic: true
logging:
  level: info  # debug|info|warn|error
```

**Update Behavior:**
- `auto` — Check for updates and auto-install (safe minor versions only)
- `notify` — Log when updates available, user decides when to update (default)
- `false` — Disable update checks entirely

### 5. Repository Structure

```
infrastructure/
├── vps0/                   # Your server
│   ├── apps/
│   │   ├── web/docker-compose.yml
│   │   └── api/docker-compose.yml
│   └── hooks/
│       ├── pre.sh
│       └── success.sh
```

### 6. Deploy!

```bash
git push
# Within your configured interval, Konta automatically deploys!
```

## Documentation

- **[USER_GUIDE.md](docs/USER_GUIDE.md)** — Complete reference (installation, features, troubleshooting, multi-server)
- **[HOW_IT_WORKS.md](docs/HOW_IT_WORKS.md)** — Architecture and implementation
- **[CONTRIBUTING.md](docs/CONTRIBUTING.md)** — Development setup and contributing

## Example Repository

See [examples/vps0/](examples/vps0/) for a complete, working example with Traefik, Nginx, and Whoami.

## License

MIT — Free for personal and commercial use.