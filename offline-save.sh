#!/bin/bash
# offline-save.sh
# Save backend and frontend images to tar files for offline transfer
# Usage: ./offline-save.sh

# Cleanup previous files
rm -f backend.tar frontend.tar dev-base.tar

echo "Saving images..."

# Ensure images exist
if ! podman image exists lgd-litestat-backend:prod; then
    echo "Backend image not found! Building..."
    podman build -t lgd-litestat-backend:prod ./backend
fi

if ! podman image exists lgd-litestat-frontend:prod; then
    echo "Frontend image not found! Building..."
    podman build -f ./frontend/Dockerfile -t lgd-litestat-frontend:prod ./frontend
fi

# Save Prod Images (using docker-archive format for compatibility)
podman save --format docker-archive -o backend.tar lgd-litestat-backend:prod
podman save --format docker-archive -o frontend.tar lgd-litestat-frontend:prod

# Save Dev Base Images
echo "Saving Dev Base images (Golang & Node)..."
# Pull if not exists (optional, assuming they exist from dev run)
podman pull docker.io/library/golang:1.24-bookworm
podman pull docker.io/library/node:20-alpine

podman save --format docker-archive -o dev-base.tar docker.io/library/golang:1.24-bookworm docker.io/library/node:20-alpine

echo "Done! Transfer the following to the offline server:"
echo "1. Images: backend.tar, frontend.tar, dev-base.tar"
echo "2. Configs: docker-compose.prod.yml, docker-compose.dev.yml, offline-load.sh"
echo "3. Source Code (for Dev): backend/, frontend/ folders"
