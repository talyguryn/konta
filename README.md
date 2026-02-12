# Konta - GitOps for Docker Compose

**GitOps for Docker Compose** — A lightweight, idempotent deployment system for single-node servers.

Keep your infrastructure in Git. Deploy automatically. Stay simple.

## What is Konta?

Konta is a simple tool that:

1. **Stores your infrastructure in Git** (like Flux for Kubernetes, but simpler)
2. **Periodically checks for changes** (polling every 2 minutes by default)
3. **Automatically deploys new versions** (idempotent Docker Compose updates)
4. **Cleans up old containers** (orphan cleanup)
5. **Ensures atomic updates** (safe even if interrupted)

**Perfect for:** Single VPS, Docker Compose stacks, self-hosted projects.

**Not for:** Multi-node clustering, high availability, enterprise RBAC.

## Quick Start (5 minutes)

### 1. Download & Install

```bash
wget https://github.com/kontacd/konta/releases/download/v0.1.3/konta-linux
chmod +x konta-linux
sudo mv konta-linux /usr/local/bin/konta

# Install on your server
konta install /path/to/repo/vps0
```

### 2. Repository Structure

```
infrastructure/
├── vps0/                    # Your server
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

## Key Features

✅ **Single Static Binary** — No dependencies
✅ **Git-Driven** — One source of truth
✅ **Atomic Deployments** — Zero-downtime updates
✅ **Safe** — Only manages its own containers
✅ **Simple** — Just YAML + Docker Compose
✅ **Multi-Server** — One repo, multiple servers
✅ **Hooks** — Pre/post deploy scripts
✅ **Idempotent** — Safe to run repeatedly

## Documentation

- **[USER_GUIDE.md](docs/USER_GUIDE.md)** — Complete reference (installation, features, troubleshooting, multi-server)
- **[HOW_IT_WORKS.md](docs/HOW_IT_WORKS.md)** — Architecture and implementation
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — Development setup and contributing

## Example Repository

See [examples/vps0/](examples/vps0/) for a complete, working example with Traefik, Nginx, and Whoami.

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

## Commands

```bash
konta install /path/to/repo    # First-time setup
konta run                       # Deploy once
konta run --dry-run             # Preview changes
konta status                    # Show last deployment
konta daemon                    # Run polling loop
```

## License

MIT — Free for personal and commercial use.

## Contributing

We'd love your help! See [CONTRIBUTING.md](CONTRIBUTING.md) to:
- Set up development environment
- Run tests
- Submit pull requests

---

**Ready to try?** Start with [USER_GUIDE.md](docs/USER_GUIDE.md) or clone [examples/vps0/](examples/vps0/).
- **Docker Compose** — Container orchestration (local/single node)
