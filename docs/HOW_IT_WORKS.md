# How Konta Works

This document explains the architecture and implementation details of Konta.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Git Repository                        │
│  (Your infrastructure as code)                          │
│                                                         │
│  vps0/                                                 │
│  ├── konta.yaml     ← Config with repo URL, interval  │
│  ├── apps/          ← Desired state (docker-compose)  │
│  │   ├── web/                                         │
│  │   └── api/                                         │
│  └── hooks/         ← Pre/post deploy scripts         │
└──────────────────┬──────────────────────────────────────┘
                   │
                   │ Konta polls every N seconds
                   │
┌──────────────────▼──────────────────────────────────────┐
│              Konta Daemon (on your server)              │
│                                                         │
│  1. Clone git repo locally                            │
│  2. Detect what's in apps/ (desired state)            │
│  3. Check running containers (actual state)           │
│  4. Reconcile: run docker compose up/down             │
│  5. Update state file                                 │
└─────────────────────────────────────────────────────────┘
```

## Core Reconciliation Loop

### 1. Load Configuration

Konta reads `konta.yaml` from your repo folder:

```yaml
version: v1
repository:
  url: https://github.com/user/infrastructure
  branch: main
  path: vps0           # ← Base path (apps folder will be inside)
  interval: 120         # ← Poll every 120 seconds
```

### 2. Clone Repository

```bash
git clone --single-branch --branch main --depth 1 \
  https://github.com/user/infrastructure \
  /tmp/konta-XXXXX
```

Only fetches the latest commit, saves bandwidth and time.

### 3. Detect Desired State

Scan the configured path (`vps0/apps/`) for subdirectories containing `docker-compose.yml`:

```
vps0/apps/
├── web/
│   └── docker-compose.yml    ← Project "web"
└── api/
    └── docker-compose.yml    ← Project "api"
```

**Desired state:**
- web (should be running)
- api (should be running)

### 4. Detect Actual State

Query Docker for running containers:

```bash
docker ps --filter "label=konta.managed=true" \
          --format "{{.Label \"com.docker.compose.project\"}}"
```

Only counts containers labeled with `konta.managed=true` (containers Konta created).

**Actual state:**
- web (running)
- api (not running)
- database (running, but NOT labeled "konta.managed=true" → ignored)

### 5. Reconcile State

For each project:

**Desired but not running:**
```bash
cd vps0/apps/web
docker compose up -d
# Sets label: konta.managed=true
```

**Running but not desired:**
```bash
cd vps0/apps/database  # (if this was somehow running)
docker compose down
```

**Already running:**
```bash
cd vps0/apps/api
docker compose up -d
# Updates containers if compose file changed
```

### 6. Update State File

Save the last successful deployment:

```json
{
  "last_commit": "abc123def456...",
  "last_deploy_time": "2026-02-12 14:30:45",
  "version": "v0.1.3"
}
```

Stored at: `/var/lib/konta/state.json`

### 7. Sleep & Repeat

```go
ticker := time.NewTicker(time.Duration(interval) * time.Second)
for range ticker.C {
    // Go to step 2, repeat
}
```

## Atomic Deployments

Konta ensures zero-downtime updates using symlinks:

### Old Way (Dangerous)

```bash
rm -rf /var/lib/apps
git clone repo /var/lib/apps
docker compose up
# ↑ If interrupted here, server is broken!
```

### Konta Way (Safe)

```bash
# Step 1: Clone to temporary directory
git clone repo /tmp/konta-abc123/

# Step 2: Run docker compose in temp
cd /tmp/konta-abc123/vps0/apps/web
docker compose up -d

# Step 3: Switch symlink atomically (instant!)
ln -sf /tmp/konta-abc123 /var/lib/konta/current

# Step 4: If error at any point, old symlink still active!
```

**Benefits:**
- ✅ Never broken state
- ✅ Instant rollback (just symlink back)
- ✅ Safe even if process dies
- ✅ Zero-downtime updates

## Idempotency

Running Konta 1000 times = same result as running once (unless Git changes):

```bash
# First run
docker compose up -d
# Starts containers

# Second run
docker compose up -d
# Already running, no change

# Third run
docker compose up -d
# Still no change, idempotent!
```

This is safe to:
- Run from cron every 1 second
- Call from webhooks
- Run manually anytime
- Run in tests

## Lock Management

Prevents concurrent Konta processes:

```bash
# Lock file
/var/run/konta.lock

# Process 1
flock /var/run/konta.lock ./reconcile  # Waits...
# Lock acquired, running

# Process 2
flock -n /var/run/konta.lock ./reconcile
# FAIL: Lock held by Process 1, exit immediately
```

Ensures only one deployment at a time.

## Hooks System

Execute custom scripts at deployment stages:

### Pre-Hook (before deploy)

```bash
#!/bin/bash
# vps0/hooks/pre.sh

# Validate compose files
docker compose config > /dev/null
if [ $? -ne 0 ]; then
    echo "Invalid docker-compose.yml"
    exit 1  # ← Abort deployment
fi

# Check disk space
DISK=$(df /var/lib/docker | awk 'NR==2 {print $5}' | cut -d'%' -f1)
if [ $DISK -gt 90 ]; then
    echo "Disk too full ($DISK%)"
    exit 1
fi

echo "Pre-deployment checks passed ✓"
```

**Exit code:**
- 0 = success, continue deployment
- non-zero = failure, abort deployment

### Success Hook (after successful deploy)

```bash
#!/bin/bash
# vps0/hooks/success.sh

# Send Slack notification
curl -X POST https://hooks.slack.com/xxx \
  -d '{"text":"Deployed successfully!"}'

# Update DNS records
systemctl restart dnsmasq

# Your custom logic here
```

Only runs if deployment succeeded.

### Failure Hook (after failed deploy)

```bash
#!/bin/bash
# vps0/hooks/failure.sh

# Send alert
curl -X POST https://hooks.slack.com/xxx \
  -d '{"text":"Deployment FAILED!"}'

# Page someone
PagerDuty alert...
```

Only runs if something went wrong.

### Post-Update Hook (after Konta binary update)

```bash
#!/bin/bash
# vps0/hooks/post_update.sh

# Reload configuration or restart services if needed
# This runs after successful Konta binary update

# Notify team
curl -X POST https://hooks.slack.com/xxx \
  -d '{"text":"Konta updated successfully!"}'

# Your post-update logic
```

Runs after Konta itself is updated to a new version (not after app deployments).

## State Tracking

Konta tracks deployment history:

```json
{
  "last_commit": "b78f061d99b0265bc4ddc02830c3d9bf3dcee02b",
  "last_deploy_time": "2026-02-12 14:30:45",
  "version": "v0.1.3"
}
```

Used to detect if anything changed:

```go
if newCommit != lastCommit {
    // Something in Git changed, reconcile
    Reconcile()
} else {
    // No changes, skip expensive operations
    return
}
```

## Label-Based Management

Konta only manages containers it created:

### Docker Compose Setup

When you run `docker compose up`, Konta sets labels:

```yaml
# vps0/apps/web/docker-compose.yml
services:
  nginx:
    image: nginx:alpine
```

**Becomes:**

```bash
docker run \
  --label "konta.managed=true" \
  --label "com.docker.compose.project=web" \
  nginx:alpine
```

### Why Labels?

1. **Safety** — Traefik, Watchtower, databases run without labels
2. **Coexistence** — Your existing containers unchanged
3. **Tracking** — Know which containers Konta created
4. **Granular control** — Remove/update only Konta containers

## Configuration File Format

```yaml
version: v1                      # Required: config version

repository:
  url: https://...               # Required: git repo
  branch: main                   # Optional: default "main"
  path: vps0                     # Optional: base path (default: repo root)
  interval: 120                  # Optional: default 120 (seconds)
  token: ghp_xxxx                # Optional: for private repos

deploy:
  atomic: true                   # Optional: default true
  remove_orphans: true           # Optional: default true

hooks:
  pre: pre.sh           # Optional: found in {path}/hooks/
  success: success.sh   # Optional: found in {path}/hooks/
  failure: failure.sh   # Optional: found in {path}/hooks/
  post_update: post_update.sh  # Optional: after konta binary update

logging:
  level: info                    # Optional: debug/info/warn/error
  format: text                   # Optional: text/json
```

## Multi-Server Architecture

Each server points to its own folder in the same repo:

### Server 1 (SPB)

```yaml
# spb/konta.yaml
repository:
  path: spb/apps      # ← Only watches spb/apps
  interval: 120
```

### Server 2 (GER)

```yaml
# ger/konta.yaml
repository:
  path: ger/apps      # ← Only watches ger/apps
  interval: 60        # Different interval!
```

**Result:**
- Both servers pull from same Git repo
- Each manages completely separate applications
- No interference between servers
- Different versions, timings, hooks

## File Structure on Server

```
/
├── /var/lib/konta/
│   ├── current          → /tmp/konta-abc123/  (symlink)
│   ├── releases/
│   │   ├── konta-v1/
│   │   ├── konta-v2/
│   │   └── konta-v3/
│   └── state.json
│
├── /etc/konta/
│   └── config.yaml      (loaded at startup)
│
├── /var/log/konta/
│   └── konta.log
│
└── /usr/local/bin/
    └── konta            (12MB static binary)
```

## Error Handling

### Transient Errors (network, Docker daemon)

```
[ERROR] Failed to clone repository: network timeout
[ERROR] Deployment error: docker daemon unreachable
→ Log error, continue polling
→ Retry in 120 seconds
```

### Configuration Errors (invalid YAML, missing files)

```
[ERROR] Failed to parse konta.yaml: syntax error
→ Log error, abort this cycle
→ Server state unchanged
→ Retry in 120 seconds
```

### Deployment Errors (compose file syntax error)

```
[ERROR] docker compose validation failed
[ERROR] Running pre-hook failure.sh
→ Keeps previous working state
→ Server not broken
→ Retry in 120 seconds
```

## Performance Characteristics

### Single Reconciliation Cycle

- Clone repo: ~500ms (shallow clone)
- Scan disk: ~100ms
- Docker queries: ~200ms
- Compose up/down: ~1-5 seconds (depends on your services)
- **Total:** ~2-6 seconds

With `interval: 120` (2 minutes):
- 1-2% CPU usage (mostly idle)
- Uses <10MB RAM
- Minimal I/O

### Network Usage

Per cycle:
- Git clone: ~200-500KB (depth=1)
- Docker pull images: Depends on image sizes
- **Total:** Minimal if no image updates

## Debugging

### Enable Debug Logging

```bash
# Temporary
RUST_LOG=debug konta run

# Permanent
logging:
  level: debug
```

### Check State

```bash
# Last deployment
cat /var/lib/konta/state.json

# Logs
journalctl -u konta -f
tail -f /var/log/konta/konta.log

# Running containers
docker ps --filter "label=konta.managed=true"
```

### Dry Run

```bash
konta run --dry-run
# Shows what WOULD happen, doesn't actually do it
```

### Manual Reconciliation

```bash
# Trigger immediately (don't wait for interval)
konta run

# Forces reconciliation even if nothing changed
git commit --allow-empty -m "Force reconciliation"
git push
```

## Security Considerations

### Token Handling

```bash
# Don't put token in config!
token: ghp_xxxx  # ❌ BAD

# Use environment variable instead
export KONTA_TOKEN=ghp_xxxx
konta run  # ✅ GOOD
```

### Secret Files

```bash
# Don't commit secrets to Git!
.env                    # ← Add to .gitignore
secrets/                # ← Add to .gitignore
private.key             # ← Add to .gitignore

# Create locally on server
echo "DB_PASSWORD=secret" > vps0/apps/web/.env
```

### Permissions

```bash
# Run as root (needs Docker access)
sudo konta install

# Config file permissions
ls -la /etc/konta/config.yaml
# -rw------- 1 root root  (600 permissions, root only)
```

## Limitations (v0.1)

- **Single node** — No multi-server orchestration
- **Polling only** — No webhook support (yet)
- **No encryption** — Keep secrets in env vars or private repos
- **No rollback command** — Use `git revert` instead
- **No metrics** — Plain JSON state, no Prometheus (planned)
- **One repo** — Can't orchestrate multiple Git repos
- **No RBAC** — All Docker access equal

## Future Improvements (v0.2+)

- REST API for status/logs
- Webhook support for instant deployments
- Prometheus metrics integration
- Multi-repo orchestration
- Secret encryption
- Rollback command
- Web dashboard
