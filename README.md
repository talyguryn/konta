> ⚠️ This is a public beta release. Use at your own risk. Feedback and contributions are welcome!

# Konta - GitOps for Docker Compose

A lightweight, idempotent deployment system for single-node servers.

Keep your infrastructure in Git. Deploy automatically. Stay simple.

**Perfect for:** Single VPS, Docker Compose stacks, self-hosted projects.

## Why

If you're like me, you want a simple way to manage your server infrastructure using Git. I prefer managing my projects on VPS with a few docker-compose files in separate dirs. But Also I want to have a single source of truth for my infrastructure in Git, and I want it to automatically deploy whenever I push changes. I'd love to use k3s + flux but it's overkill for a single VPS. I just want something simple, reliable, and Git-driven.

So I built Konta — a tiny, single binary tool that watches a Git repository and automatically deploys Docker Compose stacks on your server. It handles atomic updates, orphan cleanup, and hooks for pre/post deploy scripts. It's perfect for anyone who wants GitOps for their single-node Docker infrastructure without the complexity of Kubernetes.

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

```bash
wget https://github.com/kontacd/konta/releases/latest/download/konta-linux
chmod +x konta-linux
sudo mv konta-linux /usr/local/bin/konta

# Install on your server
konta install
```

### 2. Repository Structure

```
infrastructure/
├── vps0/                   # Your server
│   ├── konta.yaml          # Configuration
│   ├── apps/
│   │   ├── web/docker-compose.yml
│   │   └── api/docker-compose.yml
│   └── hooks/
│       ├── pre.sh
│       └── success.sh
```

### 3. Configuration (konta.yaml)

```yaml
version: v1
repository:
  url: https://github.com/yourname/infrastructure
  branch: main
  path: vps0/apps
  interval: 120
deploy:
  atomic: true
hooks:
  pre: vps0/hooks/pre.sh
  success: vps0/hooks/success.sh
```

### 4. Deploy!

```bash
git push
# Within 2 minutes, Konta automatically deploys!
```

## Documentation

- **[USER_GUIDE.md](docs/USER_GUIDE.md)** — Complete reference (installation, features, troubleshooting, multi-server)
- **[HOW_IT_WORKS.md](docs/HOW_IT_WORKS.md)** — Architecture and implementation
- **[CONTRIBUTING.md](docs/CONTRIBUTING.md)** — Development setup and contributing

## Example Repository

See [examples/vps0/](examples/vps0/) for a complete, working example with Traefik, Nginx, and Whoami.

## License

MIT — Free for personal and commercial use.