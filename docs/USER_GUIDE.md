# Konta User Guide

Complete reference for installing, using, and troubleshooting Konta.

## Table of Contents

1. [Installation](#installation)
2. [Configuration](#configuration)
3. [Repository Structure](#repository-structure)
4. [Docker Compose Setup](#docker-compose-setup)
5. [Deployment Workflows](#deployment-workflows)
6. [Multi-Server Setup](#multi-server-setup)
7. [Hooks & Automation](#hooks--automation)
8. [Troubleshooting](#troubleshooting)
9. [Advanced Topics](#advanced-topics)

## Installation

### System Requirements

- **OS:** Linux (Ubuntu 18.04+, Debian 10+, CentOS 7+, etc.)
- **Docker:** Latest version with Docker Compose
- **Git:** Any recent version
- **Internet:** Outbound HTTPS access to GitHub

### Download Binary

```bash
# Download v0.1.3 (12MB, no dependencies)
wget https://github.com/kontacd/konta/releases/download/v0.1.3/konta-linux
chmod +x konta-linux
sudo mv konta-linux /usr/local/bin/konta

# Or build from source (see CONTRIBUTING.md)
```

### First-Time Setup

```bash
# Navigate to your infrastructure repository
cd /path/to/infrastructure

# Run configuration wizard
sudo konta install /path/to/infrastructure/vps0

# Answer prompts:
# Repository URL: https://github.com/yourname/infrastructure
# Branch: main
# Base path: vps0
# Polling interval: 120
# GitHub token: (paste if private repo) or press Enter
# Enable daemon? yes
```

### What Gets Created

- ✅ `/etc/konta/config.yaml` — Your configuration
- ✅ `/var/lib/konta/` — Deployment state and releases
- ✅ `/var/log/konta/` — Logs
- ✅ Systemd service `konta.service` (if you said yes to daemon)

### Verify Installation

```bash
# Check version
konta version
# Output: Konta v0.1.3

# Check daemon status
systemctl status konta

# View logs
journalctl -u konta -n 20
```

## Configuration

### konta.yaml Format

Create `vps0/konta.yaml` in your repository:

```yaml
version: v1

repository:
  # Where your Git repository is
  url: https://github.com/yourname/infrastructure

  # Which branch to track
  branch: main

  # Base path containing 'apps' folder (relative to repo root)
  # Omit or use '.' for repo root
  path: vps0

  # How often to check for changes (seconds)
  interval: 120

  # GitHub token for private repos (optional)
  # Better: set KONTA_TOKEN environment variable instead
  token: ghp_xxxxxxxxxxxx

deploy:
  # Zero-downtime updates using symlinks
  atomic: true

  # Remove containers not in Git
  remove_orphans: true

hooks:
  # Run before deployment (pre-flight checks)
  # Just filename, found in {path}/hooks/ directory
  pre: pre.sh

  # Run after successful deployment
  success: success.sh

  # Run if deployment fails
  failure: failure.sh

  # Run after successful Konta binary update
  post_update: post_update.sh

logging:
  # Log level: debug, info, warn, error
  level: info

  # Format: text or json (text is human-readable)
  format: text
```

### Using Environment Variables

For sensitive information, use environment variables instead of the config file:

```bash
# Set token from environment
export KONTA_TOKEN=ghp_xxxxxxxxxxxx
konta run

# The token in config.yaml (if present) is overridden
```

### Configuration Locations (Search Order)

Konta looks for config in this order:

1. `/etc/konta/config.yaml` (system-wide, highest priority)
2. `~/.konta/config.yaml` (user home directory)
3. `./konta.yaml` (current directory, lowest priority)

First found is used. Good for development (local `konta.yaml`) and production (`/etc/konta/config.yaml`).

## Repository Structure

### Single Server

```
infrastructure/
├── README.md                          # Documentation
├── .gitignore                         # Ignore secrets
│   **/.env
│   **/.key
│   secrets/
│
├── vps0/                              # Your server folder
│   ├── konta.yaml                    # Config
│   │
│   ├── apps/                         # Applications
│   │   ├── web/
│   │   │   ├── docker-compose.yml
│   │   │   ├── .env                  # (not in Git!)
│   │   │   ├── html/
│   │   │   └── data/                 # volumes
│   │   │
│   │   ├── api/
│   │   │   ├── docker-compose.yml
│   │   │   └── .env
│   │   │
│   │   └── database/
│   │       ├── docker-compose.yml
│   │       └── backups/
│   │
│   ├── hooks/                        # Scripts
│   │   ├── pre.sh                   # Run before deploy
│   │   ├── success.sh                # Run on success
│   │   └── failure.sh                # Run on failure
│   │
│   └── README.md                     # Server documentation
│
└── docs/                              # Shared documentation
    └── (guides, architecture, etc)
```

### Multiple Servers

```
infrastructure/
├── vps0/                 # First server (SPB)
│   ├── konta.yaml       # SPB config, interval: 120
│   ├── apps/
│   └── hooks/
│
├── vps1/                 # Second server (GER)
│   ├── konta.yaml       # GER config, interval: 60
│   ├── apps/
│   └── hooks/
│
├── vps2/                 # Third server (LON)
│   ├── konta.yaml
│   ├── apps/
│   └── hooks/
│
└── docs/
    ├── README.md
    └── INFRASTRUCTURE.md
```

Each server independently configured and deployed!

## Docker Compose Setup

### Minimal Example

```yaml
# vps0/apps/web/docker-compose.yml
version: '3.8'

services:
  nginx:
    image: nginx:1.21-alpine
    ports:
      - "80:80"
    volumes:
      - ./html:/usr/share/nginx/html:ro
    restart: unless-stopped
```

### With Environment Variables

```yaml
version: '3.8'

services:
  app:
    image: myapp:${APP_VERSION}
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=${DATABASE_URL}
      - API_KEY=${API_KEY}
      - LOG_LEVEL=info
    env_file:
      - .env
    restart: unless-stopped
```

**Add to .gitignore:**
```
vps0/apps/*/.env
**/.env
```

**Create locally on server:**
```bash
# On your server (vps0)
echo "APP_VERSION=1.2.3" > vps0/apps/app/.env
echo "DATABASE_URL=postgres://..." >> vps0/apps/app/.env
echo "API_KEY=secret_key_here" >> vps0/apps/app/.env
```

### With Volumes

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: myapp
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    restart: unless-stopped

volumes:
  postgres_data:
```

### With Networks

```yaml
version: '3.8'

services:
  web:
    image: nginx:alpine
    networks:
      - app-network
    ports:
      - "80:80"

  app:
    image: myapp:latest
    networks:
      - app-network
    environment:
      - NGINX_HOST=web

networks:
  app-network:
    driver: bridge
```

### Best Practices

✅ **DO:**
- Pin image versions: `nginx:1.21-alpine` (not `latest`)
- Use `.env` files for configuration
- Set `restart: unless-stopped` for production
- Use volumes for persistent data
- Use networks for service communication
- Keep compose files simple and readable

❌ **DON'T:**
- Use `image: latest` (unpredictable)
- Commit `.env` files to Git (security risk)
- Run privileged containers unless necessary
- Mount entire host filesystem
- Use host network mode without reason
- Mix configuration in image and compose file

## Deployment Workflows

### 1. Add New Application

```bash
# On your workstation
cd infrastructure

# Create structure
mkdir -p vps0/apps/blog/data
cd vps0/apps/blog

# Create docker-compose.yml
cat > docker-compose.yml << 'EOF'
version: '3.8'
services:
  app:
    image: wordpress:6.0-apache
    ports:
      - "8080:80"
    environment:
      - WORDPRESS_DB_HOST=db
      - WORDPRESS_DB_NAME=wordpress
      - WORDPRESS_DB_USER=wordpress
      - WORDPRESS_DB_PASSWORD=${DB_PASSWORD}
    volumes:
      - ./data:/var/www/html
    networks:
      - default

volumes:
  db_data:
EOF

# Create environment file
cat > .env << 'EOF'
DB_PASSWORD=your_secure_password_here
EOF

# Commit and push
git add vps0/apps/blog/
git commit -m "Add blog application"
git push

# On server: Automatically deployed within 2 minutes!
# Check logs:
ssh user@server "journalctl -u konta -f"
```

### 2. Update Application Version

```bash
# Edit docker-compose.yml
# Change: image: wordpress:6.0-apache
#      To: image: wordpress:6.2-apache

git commit -am "Update WordPress to 6.2"
git push

# Automatic deployment!
# Konta will:
# 1. Pull new image
# 2. Stop old container
# 3. Start new container with new image
# 4. Everything atomic, zero downtime
```

### 3. Change Configuration

```bash
# Edit environment variables
# Manual on server: edit vps0/apps/blog/.env
# Or Git-based approach (for non-secrets):

# In docker-compose.yml
environment:
  - LOG_LEVEL=debug  # Changed from info

git commit -am "Enable debug logging"
git push

# Automatic deployment + container restart
```

### 4. Remove Application

```bash
# Simply delete from Git
rm -rf vps0/apps/blog

git commit -am "Remove blog application"
git push

# Automatic deployment!
# Konta will:
# 1. Detect blog is gone from Git
# 2. Run: docker compose down
# 3. Remove containers and networks
```

### 5. Rename Application

```bash
# Git-rename
git mv vps0/apps/old-app vps0/apps/new-app

git commit -m "Rename old-app to new-app"
git push

# Automatic reconciliation!
# Old containers cleaned up, new ones created
```

### 6. Test Changes Locally

```bash
# Before pushing to Git:
cd vps0/apps/myapp

# Test the compose file
docker compose config

# Test it actually works
docker compose up
# (test manually)
docker compose down
```

## Multi-Server Setup

### Architecture

Each server points to its own folder in same Git repo:

```
infrastructure/
├── vps0/    # Server A (SPB, Russia)
├── vps1/    # Server B (GER, Germany)
└── vps2/    # Server C (LON, London)
```

### Configuration per Server

**vps0/konta.yaml** (SPB - faster, aggressive)
```yaml
repository:
  path: vps0
  interval: 60      # Check every minute
```

**vps1/konta.yaml** (GER - slower, conservative)
```yaml
repository:
  path: vps1
  interval: 300     # Check every 5 minutes
```

**vps2/konta.yaml** (LON - stable, production)
```yaml
repository:
  path: vps2/apps
  interval: 600     # Check every 10 minutes
```

### Installation per Server

```bash
# On SPB server
ssh root@spb-server
git clone https://github.com/yourname/infrastructure /opt/infra
cd /opt/infra
konta install ./vps0

# On GER server
ssh root@ger-server
git clone https://github.com/yourname/infrastructure /opt/infra
cd /opt/infra
konta install ./vps1

# On LON server
ssh root@lon-server
git clone https://github.com/yourname/infrastructure /opt/infra
cd /opt/infra
konta install ./vps2
```

### Independent Deployments

```bash
# Deploy only to SPB
mkdir -p vps0/apps/new-feature
# (create docker-compose.yml)
git add vps0/apps/new-feature
git commit -m "Test new-feature on SPB"
git push

# Result:
# - SPB deploys in ~60 seconds
# - GER and LON unaffected!
```

### Canary Deployments

```bash
# 1. Deploy to development server first (vps0/apps/feature)
# 2. Test for 1 week
# 3. Deploy to production servers (vps1/apps/feature, vps2/apps/feature)

# Or different versions:
# vps0/apps/api: image: api:v2.0.0  (bleeding edge)
# vps1/apps/api: image: api:v1.9.0  (stable)
# vps2/apps/api: image: api:v1.8.0  (LTS)
```

## Hooks & Automation

### Pre-Hook (Pre-Deployment Checks)

```bash
#!/bin/bash
# vps0/hooks/pre.sh

set -e  # Exit on error

echo "Running pre-deployment checks..."

# Check docker-compose syntax
docker compose config > /dev/null
echo "✓ Docker Compose config valid"

# Check disk space
DISK_USAGE=$(df /var/lib/docker | awk 'NR==2 {print $5}' | grep -oE '[0-9]+')
if [ $DISK_USAGE -gt 85 ]; then
    echo "✗ Disk usage too high: ${DISK_USAGE}%"
    exit 1
fi
echo "✓ Disk space OK"

# Check Docker daemon
docker ps > /dev/null 2>&1
echo "✓ Docker daemon responsive"

# Check internet connectivity
ping -c 1 8.8.8.8 > /dev/null 2>&1
echo "✓ Internet connectivity OK"

echo "All pre-deployment checks passed!"
exit 0
```

### Success Hook (Post-Deployment)

```bash
#!/bin/bash
# vps0/hooks/success.sh

echo "Deployment successful!"

# Send Slack notification
curl -X POST https://hooks.slack.com/services/YOUR/WEBHOOK/URL \
  -H 'Content-Type: application/json' \
  -d '{
    "text": "✅ Deployed successfully",
    "attachments": [
      {
        "color": "good",
        "fields": [
          {"title": "Server", "value": "vps0", "short": true},
          {"title": "Time", "value": "'$(date)'", "short": true}
        ]
      }
    ]
  }'

# Update external systems
# curl https://your-api.com/deployments/log ...

# Reload configurations
systemctl reload dnsmasq

exit 0
```

### Failure Hook (Error Handling)

```bash
#!/bin/bash
# vps0/hooks/failure.sh

echo "Deployment failed!"

# Send alert
curl -X POST https://hooks.slack.com/services/YOUR/WEBHOOK/URL \
  -H 'Content-Type: application/json' \
  -d '{
    "text": "❌ Deployment failed!",
    "attachments": [
      {
        "color": "danger",
        "fields": [
          {"title": "Server", "value": "vps0", "short": true},
          {"title": "Check logs", "value": "journalctl -u konta -n 50", "short": false}
        ]
      }
    ]
  }'

# Page team lead
# notify_pagerduty "Deployment failed on vps0"

exit 0
```

### Making Hooks Executable

```bash
chmod +x vps0/hooks/pre.sh
chmod +x vps0/hooks/success.sh
chmod +x vps0/hooks/failure.sh

git add vps0/hooks/
git commit -m "Add deployment hooks"
git push
```

## Troubleshooting

### Konta Not Running

```bash
# Check status
systemctl status konta

# View recent logs
journalctl -u konta -n 50 --no-pager

# Common fixes:
# 1. Is Docker running?
systemctl status docker

# 2. Is the config file valid?
cat /etc/konta/config.yaml

# 3. Is the Git repository accessible?
cd /tmp && git clone $(grep 'url:' /etc/konta/config.yaml | awk '{print $2}')
```

### "No changes detected" but I pushed changes

```bash
# Check if Konta sees your commit
konta run --dry-run

# Check last known commit
cat /var/lib/konta/state.json | grep last_commit

# Manually trigger:
# Option 1: Force by making empty commit
git commit --allow-empty -m "Trigger deployment"
git push

# Option 2: Manually run
konta run

# View logs
journalctl -u konta -f
```

### "Failed to clone" / Network issues

```bash
# Check internet connectivity
ping google.com

# Check if GitHub is accessible
curl -I https://github.com

# If using private repo:
# 1. Check token is set
echo $KONTA_TOKEN

# 2. Test token
curl -H "Authorization: token $KONTA_TOKEN" \
     https://api.github.com/user

# 3. Check SSH keys (if using SSH URL)
ssh -T git@github.com
```

### Containers not starting

```bash
# View last deployment logs
journalctl -u konta -n 100

# Check Docker logs
docker logs <container_name>

# Check compose file
cd /var/lib/konta/current/vps0/apps/myapp
docker compose config

# Try running compose manually
docker compose up --dry-run
```

### Configuration errors

```bash
# Validate YAML syntax
docker compose config
# or
cat /etc/konta/config.yaml | yq . 2>&1

# Check file permissions
ls -la /etc/konta/config.yaml
# Should be: -rw------- 1 root root

# Verify paths exist
ls -la /var/lib/konta/
ls -la /var/log/konta/
```

### Disk space issues

```bash
# Check disk usage
df -h /var/lib/docker

# Clean up old releases
ls -la /var/lib/konta/releases/
# Keep latest 3, remove older

# Remove dangling images
docker image prune

# Remove stopped containers
docker container prune
```

### High CPU usage

```bash
# Check if reconciliation is stuck
journalctl -u konta --since "5 minutes ago"

# Monitor in real-time
watch -n 1 'docker ps | wc -l'
watch -n 1 'ps aux | grep konta'

# If stuck, manually restart
systemctl restart konta

# Check logs for slow operations
journalctl -u konta -p warning
```

## Advanced Topics

### Systemd Integration

Create service automatically with `konta install`, or manually:

```ini
# /etc/systemd/system/konta.service
[Unit]
Description=Konta GitOps Daemon
After=docker.service
Wants=docker.service
Documentation=https://github.com/kontacd/konta

[Service]
Type=simple
ExecStart=/usr/local/bin/konta run --watch
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=konta
User=root
WorkingDirectory=/opt/infra

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable konta
sudo systemctl start konta
sudo systemctl status konta
```

### Cron-Based Polling (Alternative)

Instead of systemd daemon, run from cron (not recommended, but possible):

```bash
# Add to crontab
*/2 * * * * /usr/local/bin/konta run >> /var/log/konta/cron.log 2>&1

# "/2" = every 2 minutes
```

Issues:
- No continuous daemon
- May run twice simultaneously
- Not for production

### Private Git Repositories

```bash
# Option 1: GitHub Personal Access Token
export KONTA_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxx
konta install /path/to/repo

# Option 2: SSH keys (less reliable)
# Ensure SSH keys are in ~/.ssh
```

### Updating Konta

```bash
# Download new binary
wget https://github.com/kontacd/konta/releases/download/v0.2.0/konta-linux

# Stop daemon
sudo systemctl stop konta

# Replace binary
sudo mv konta-linux /usr/local/bin/konta
sudo chmod +x /usr/local/bin/konta

# Start daemon
sudo systemctl start konta

# Verify
konta version
```

### Viewing Deployment History

```bash
# Last 20 deployments
journalctl -u konta -n 20

# Deployments from past hour
journalctl -u konta --since "1 hour ago"

# All errors
journalctl -u konta -p err

# Real-time logs
journalctl -u konta -f
```

### Manual Reconciliation

```bash
# Dry-run (what would happen)
sudo konta run --dry-run

# Actual run
sudo konta run

# View status
sudo konta status
```

### Debugging Configuration

```bash
# Show what Konta sees
konta run --dry-run

# Check which config file is loaded
strace -e openat konta run 2>&1 | grep config.yaml

# View actual config used
cat /etc/konta/config.yaml

# Test Git access
git clone --depth 1 $(grep 'url:' /etc/konta/config.yaml | awk '{print $2}') /tmp/test
```

### Resource Limits

```bash
# Limit Konta process CPU/Memory in systemd
[Service]
CPUQuota=20%
MemoryMax=256M

# Reload after editing
systemctl daemon-reload
systemctl restart konta
```

---

**Need more help?** Check [HOW_IT_WORKS.md](HOW_IT_WORKS.md) for architecture details or open an issue on GitHub.
