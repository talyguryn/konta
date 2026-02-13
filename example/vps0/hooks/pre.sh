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

# Check docker-compose syntax for each app
APPS_DIR="example/vps0/apps"
if [ -d "$APPS_DIR" ]; then
    for app_dir in "$APPS_DIR"/*/; do
        if [ -f "${app_dir}docker-compose.yml" ]; then
            app_name=$(basename "$app_dir")
            if ! docker compose -f "${app_dir}docker-compose.yml" config > /dev/null 2>&1; then
                echo "❌ docker-compose.yml syntax error in $app_name"
                docker compose -f "${app_dir}docker-compose.yml" config 2>&1 | head -20
                exit 1
            fi
        fi
    done
    echo "✓ Docker Compose syntax is valid for all apps"
else
    echo "⚠️  Apps directory not found: $APPS_DIR"
fi
echo "All pre-deployment checks passed! ✅"
echo "Proceeding with deployment..."
