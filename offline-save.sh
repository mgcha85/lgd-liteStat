#!/bin/bash
# offline-save.sh
# Save backend and frontend images to tar files for offline transfer
# Usage: ./offline-save.sh

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

# Save images (using docker-archive format for compatibility)
podman save --format docker-archive -o backend.tar lgd-litestat-backend:prod
podman save --format docker-archive -o frontend.tar lgd-litestat-frontend:prod

echo "Done! Transfer 'backend.tar', 'frontend.tar', and 'docker-compose.prod.yml' to the offline server."
