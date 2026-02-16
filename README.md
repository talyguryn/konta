> ⚠️ This is a public beta release which can contain bugs and security issues. Use at your own risk. Feedback and contributions are welcome!

# Konta

**Lightweight GitOps for Docker Compose.**

Keep your VPS infrastructure in Git.
Sync it automatically.
No Kubernetes required.

## Table of Contents
- [Quick start](#quick-start)
- [Why Konta](#why-konta)
- [Who Is This For](#who-is-this-for)
- [Example Repository Structure](#example-repository-structure)
- [Usage](#usage)
  - [Repository for infrastructure](#repository-for-infrastructure)
  - [Installation](#installation)
  - [Bootstrap Konta with your repository](#bootstrap-konta-with-your-repository)
    - [Using private repositories](#using-private-repositories)
  - [Next steps](#next-steps)
- [Konta labels for containers](#konta-labels-for-containers)
- [Hooks](#hooks)
- [Commands](#commands)
- [Configuration file](#configuration-file)
- [Updates](#updates)
- [Roadmap and tasks](#roadmap-and-tasks)
- [Contributing](#contributing)
- [License](#license)

## Quick start

Download and use it now:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/talyguryn/konta/main/scripts/install.sh)"
```

Read the [documentation](https://github.com/talyguryn/konta#usage) below for installation instructions, configuration options, and best practices.

## Why Konta

If you manage one or a few VPS servers with Docker Compose, you probably:

- Keep projects in separate folders
- Use Traefik / Nginx for routing
- Deploy via SSH
- Forget what’s actually running
- Want an easy way to migrate to a new server

Kubernetes solves this — but at the cost of complexity and RAM.

**Konta gives you GitOps for single-node Docker servers.**

No YAML rewrites.
No cluster.
No control plane.
Works on 512MB RAM.

Your Git repository becomes the **single source of truth**.

## Who Is This For

Use Konta if:

- ✅ You run 1 or more VPS servers
- ✅ You use Docker Compose
- ✅ You want Git as source of truth
- ✅ You don’t want Kubernetes
- ✅ You need something simple and reproducible

Do **not** use Konta if:

- ❌ You need multi-node clustering
- ❌ You require advanced orchestration
- ❌ You need Kubernetes-native features

## Example Repository Structure

## Usage

Three steps to start using Konta:

- Create a Git repository for your infrastructure
- Install Konta on your server
- Point Konta to your repository and let it do the rest

### Repository for infrastructure

Konta made to work with your existed docker compose files. But it expects a specific structure in your Git repository for infrastructure.

You do not have to move all your projects at the start. Just create a new repo and follow the structure below adding projects one by one. Do not forget to add `konta.managed=true` label to your containers in docker-compose files.

Single server:

```
apps/
├── web/
│   └── docker-compose.yml
├── api/
│   ├── .env
│   └── docker-compose.yml
├── ...
hooks/
├── pre.sh
└── success.sh
```

Multi-server (optional):

```
vps-prod/
├── apps/
│   ├── web/
│   │   └── docker-compose.yml
│   ├── api/
│   │   ├── .env
│   │   └── docker-compose.yml
│   └── ...
└── hooks/
    ├── pre.sh
    └── success.sh
vps-stage/
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

Each folder inside `apps/` is treated as an independent Docker Compose stack.

### Installation

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

chmod +x /usr/local/bin/konta
```

Binaries are available on the [releases page](https://github.com/talyguryn/konta/releases/latest).

### Bootstrap Konta with your repository:

**Interactive mode (default):**
```bash
sudo konta bootstrap
```

**Or with CLI parameters (no prompts):**

```bash
sudo konta bootstrap --repo https://github.com/yourname/infrastructure
```

Shortest form with default values:

- `--branch main`
- `--path .`
- `--interval 120`
- `--konta_updates notify`


```bash
sudo konta bootstrap \
  --repo https://github.com/yourname/infrastructure \
  --branch main \
  --path vps0/apps \
  --interval 120 \
  --konta_updates notify
```

#### Using private repositories

For private repositories, use KONTA_TOKEN environment variable with [GitHub PAT](https://github.com/settings/tokens/new) (Personal Access Token).

- `Note` — name this token something like `Konta Deploy Token` to easily identify its purpose in your GitHub settings
- `Expiration` can be set to `No expiration` for long-term use, but decide based on your security needs
- `repo` scope enabled

```bash
export KONTA_TOKEN=gh_your_github_token
```

Or use it in bootstrap command in interactive mode or with `--token` param:

```bash
sudo konta bootstrap \
  --repo https://github.com/yourname/infrastructure \
  --token ghp_your_github_token
```

### Next steps

Done. Konta will now:

1. Check for changes every configured interval
2. Update containers that changed
3. Manage containers only with label `konta.managed=true` in docker-compose files
4. Call hooks from `hooks/` directory if they exist

You can run `konta journal` to see logs in real-time and check that everything is working as expected. Push changes to your Git repository and watch them deploy automatically!

Read the [Commands](#commands) section to learn how to manage Konta and check logs.

## Konta labels for containers

### konta.managed

Konta manages only containers with label `konta.managed=true` in docker-compose files. This allows you to have some containers in the same compose file that are not managed by Konta (e.g., for testing or manual management).

### konta.stopped

If you want Konta to disable a container and not start it, you can add the label `konta.stopped=true` to that service in your docker-compose file. This is useful for services that you want to keep defined in Git but not run on the server.

## Hooks

Konta supports lifecycle hooks that allow you to run custom scripts at different stages of the deployment process. You can place your hook scripts in the `hooks/` directory of your repository. Konta will look for the following scripts.

- `pre.sh` — Runs before any changes are applied. Use this for tasks like backing up data, sending notifications, or performing checks. If this script exits with a non-zero status, the deployment will be aborted, and the `failure.sh` hook will be triggered.
- `success.sh` — Runs after successful deployment. Use this for tasks like clearing caches, sending success notifications, or performing post-deploy checks.
- `failure.sh` — Runs if deployment fails. Use this for tasks like sending failure notifications, rolling back changes, or performing cleanup.
- `post_update.sh` — Runs after Konta itself is updated. Use this for tasks like restarting the Konta daemon or performing any necessary migrations.

## Commands

Konta shows the list of available commands when you run `konta` without arguments. Here are the main ones:

Setup and management:

- `konta bootstrap` — Set up Konta with your repository. This command initializes the configuration and starts the synchronization process.

Single run:

- `konta run` — Run a single synchronization cycle immediately. This is useful for testing or when you want to apply changes without waiting for the next scheduled interval.
- `konta run --dry-run` — Simulate a synchronization cycle without making any changes. This will show you what actions Konta would take based on the current state of the repository and server.
- `konta run --watch` — Run a synchronization cycle and then continue watching for changes in real-time. This is useful for debugging or when you want to see changes applied immediately as you push to Git.

Service commands:

- `konta journal (-j)` — View the Konta logs in real-time. This is useful for monitoring deployments and troubleshooting issues.
- `konta update` — Check for updates to Konta itself. If a new version is available, it will prompt you to install it. Use `-y` to auto-confirm updates (safe minor versions only).
- `konta version (-v)` — Show the current version of Konta.
- `konta help (-h)` — Show help information about commands and usage.
- `konta config [-e]` — View the current configuration settings. Use `-e` to edit the configuration file directly with nano.

Daemon management:

- `konta daemon [enable|disable|restart|status]` — Manage the Konta daemon. Also available as separate commands.
- `konta enable` — Enable the Konta daemon.
- `konta disable` — Disable the Konta daemon.
- `konta restart` — Restart the Konta daemon.
- `konta status` — Show the status of the Konta daemon from systemd.

Uninstallation:

- `konta uninstall` — Uninstall Konta from the server. This will stop the daemon and give the advice to remove the binary manually.

## Configuration file

Konta will create a configuration file at `/etc/konta/config.yaml` with the parameters you provided during bootstrap. You can edit this file to change settings like repository URL, branch, path, interval, and update behavior. Konta will detect changes to the config file and apply them without needing to restart the daemon.

Example config file:

```yaml
version: v1

# Connection and infrastructure directory parameters
repository:
  url: https://github.com/yourname/infrastructure
  token: ghp_xxxx
  branch: main
  path: vps0/apps
  interval: 60

# When atomic: true, Konta implements atomic/zero-downtime deployments using symlink-based switching. Instead of applying changes directly.
deploy:
  atomic: true

# Logging level for Konta's internal operations on journal. Options are debug, info, warn, error. Default is info. Set to debug for more verbose output during troubleshooting.
logging:
  level: info

# Update behavior for Konta itself. By default with `notify`, it will log when updates are available, and you can choose when to update. If set to 'auto', Konta will automatically check for updates and install them (safe minor versions only). Set to 'false' or omit to disable update checks entirely.
konta_updates: notify

# Optional. You can redefine hooks paths here if you want to use different names or locations for your hook scripts. By default, Konta looks for `pre.sh`, `success.sh`, `failure.sh`, and `post_update.sh` in the `{path}/hooks/` directory of your repository.
hooks:
  pre: pre.sh
  success: success.sh
  failure: failure.sh
  post_update: post_update.sh
```

## Updates

All new versions are avaliable on the [releases page](https://github.com/talyguryn/konta/releases).

You can check updates manually with command. If the new version is available, konta will ask you to install it.

```bash
konta update
```

Also konta can check for updates to itself and manage the update process. This is controlled by the `konta_updates` configuration option.

- `auto` — Check for updates and auto-install (safe minor versions only)
- `notify` (default) — Log when updates available, user decides when to update
- `false` or omit — Disable update checks entirely

When an update is available, Konta will log a message with the new version. If `auto` mode is enabled, it will attempt to download and install the update automatically. The update process is designed to be safe and will only auto-install minor versions to avoid breaking changes.

## Roadmap and tasks

`v1.0.0` should be stable and production-ready, but there are still some improvements and features to work on. Here are some of the tasks on the roadmap:

Improvements:
- [ ] simplify the help command output and make it more user-friendly
- [ ] add docs describes how Konta works

Security:
- [ ] check source code for security issues

Testing:
- [ ] add tests that simulate real-world scenarios (e.g., network failures, repo access issues, etc.)

Research:
- [ ] how to migrate to new repo structure if you want to change repo
- [ ] how to implement atomic deployments with zero downtime
- [ ] how to backup and restore docker volumes
- [ ] how to add a new server to existing repo without giving it access to all other servers
- [ ] check GitLab support

## Contributing

Contributions are welcome! Please open issues for bugs or feature requests, and submit pull requests for improvements.

## License

MIT — Free for personal and commercial use.