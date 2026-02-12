#!/bin/bash
# Pre-deployment checks for vps0
# Run before any deployment

set -e

echo "=== Running pre-deployment checks ==="

# Check Docker is running
if ! docker ps > /dev/null 2>&1; then
    echo "❌ Docker daemon is not running"
    exit 1
fi
echo "✓ Docker daemon is running"

# Check docker-compose syntax
if ! docker compose config > /dev/null 2>&1; then
    echo "❌ docker-compose.yml has syntax errors"
    exit 1
fi
echo "✓ Docker Compose syntax is valid"

# Check disk space (warn if >80%)
DISK_USAGE=$(df /var/lib/docker | awk 'NR==2 {print $5}' | grep -oE '[0-9]+')
if [ "$DISK_USAGE" -gt 80 ]; then
    echo "⚠️  Disk usage is high: ${DISK_USAGE}%"
fi
echo "✓ Disk space check passed"

# Check internet connectivity
if ! ping -c 1 8.8.8.8 > /dev/null 2>&1; then
    echo "⚠️  Internet connectivity might be limited"
fi
echo "✓ Network check passed"

echo ""
echo "All pre-deployment checks passed! ✅"
echo "Proceeding with deployment..."
