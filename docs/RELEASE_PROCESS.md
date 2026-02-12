# Release Process

## GitHub Actions Workflows

This repository includes automated workflows for building and releasing Konta:

### CI Workflow (`.github/workflows/ci.yml`)

Runs on every push to `main` and on pull requests:
- ✅ Runs tests with race detector
- ✅ Runs `go vet` to catch common mistakes
- ✅ Runs `golangci-lint` for code quality
- ✅ Verifies builds for all platforms (Linux, macOS, Windows)

### Release Workflow (`.github/workflows/release.yml`)

Triggers automatically when you push a new version tag:
- ✅ Builds binaries for 5 platforms:
  - Linux AMD64
  - Linux ARM64
  - macOS AMD64 (Intel)
  - macOS ARM64 (Apple Silicon)
  - Windows AMD64
- ✅ Creates SHA256 checksums
- ✅ Creates GitHub Release with download instructions
- ✅ Uploads all binaries as release assets

## Creating a New Release

### 1. Update Version

Update the version in `cmd/konta/main.go`:

```go
const Version = "0.1.4"  // Change this
```

### 2. Update CHANGELOG.md

Document all changes in `CHANGELOG.md`:

```markdown
## [0.1.4] - 2026-02-12

### Added
- New feature X
- New feature Y

### Fixed
- Bug Z
```

### 3. Commit Changes

```bash
git add cmd/konta/main.go CHANGELOG.md
git commit -m "Release v0.1.4"
git push origin main
```

### 4. Create and Push Tag

```bash
# Create annotated tag
git tag -a v0.1.4 -m "Release v0.1.4"

# Push tag to trigger release workflow
git push origin v0.1.4
```

### 5. Watch the Build

1. Go to: https://github.com/talyguryn/konta/actions
2. You'll see the "Release" workflow running
3. Wait ~2-3 minutes for binaries to build
4. Release will appear at: https://github.com/talyguryn/konta/releases

## Release Workflow Output

The release will include:

- **konta-linux** - Linux AMD64 binary (most common)
- **konta-linux-arm64** - Linux ARM64 (Raspberry Pi, ARM servers)
- **konta-darwin-amd64** - macOS Intel
- **konta-darwin-arm64** - macOS Apple Silicon (M1/M2/M3)
- **konta-windows-amd64.exe** - Windows
- **checksums.txt** - SHA256 checksums for verification

## Testing Before Release

Before creating a tag, test locally:

```bash
# Run tests
go test ./...

# Build for all platforms
GOOS=linux GOARCH=amd64 go build ./cmd/konta
GOOS=linux GOARCH=arm64 go build ./cmd/konta
GOOS=darwin GOARCH=amd64 go build ./cmd/konta
GOOS=darwin GOARCH=arm64 go build ./cmd/konta
GOOS=windows GOARCH=amd64 go build ./cmd/konta

# Test the binary
./konta --version
./konta --help
```

## Automated Updates

Once releases are published, users can update with:

```bash
konta update
```

This command:
1. Checks GitHub API for latest release
2. Downloads the correct binary for user's OS/ARCH
3. Replaces the current binary
4. Shows success message

## Rolling Back

If you need to delete a bad release:

```bash
# Delete the tag locally
git tag -d v0.1.4

# Delete the tag remotely
git push origin :refs/tags/v0.1.4

# Delete the GitHub release manually in the web UI
# (or use GitHub CLI: gh release delete v0.1.4)
```

Then fix issues, bump version, and re-release.

## Versioning Scheme

We follow [Semantic Versioning](https://semver.org/):

- **MAJOR** (v1.0.0 → v2.0.0): Breaking changes
- **MINOR** (v0.1.0 → v0.2.0): New features, backwards compatible
- **PATCH** (v0.1.1 → v0.1.2): Bug fixes, backwards compatible

Examples:
- `v0.1.3` → `v0.1.4`: Bug fix
- `v0.1.4` → `v0.2.0`: New feature (multi-cloud support)
- `v0.9.0` → `v1.0.0`: First stable release

## Pre-releases

For testing, create pre-release tags:

```bash
git tag -a v0.2.0-rc1 -m "Release Candidate 1"
git push origin v0.2.0-rc1
```

The workflow will still build binaries, but you can manually mark it as "pre-release" in GitHub UI.
