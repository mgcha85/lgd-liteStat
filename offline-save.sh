#!/bin/bash
# offline-save.sh
# Save backend and frontend images to tar files for offline transfer
# Usage: ./offline-save.sh [all|python]
# Default: all

MODE=${1:-all}

if [ "$MODE" == "python" ]; then
    echo "Processing Python Scheduler ONLY..."
    
    # 1. Cleanup
    rm -f python-scheduler.tar
    
    # 2. Build
    echo "Building Python Scheduler Image..."
    
    # Load .env variables for PIP config if present
    PIP_ARGS=""
    if [ -f "./python-scheduler/.env" ]; then
        echo "Loading build config from ./python-scheduler/.env"
        # Export variables temporarily
        export $(grep -v '^#' ./python-scheduler/.env | xargs)
        
        if [ -n "$PIP_INDEX_URL" ]; then
            PIP_ARGS="$PIP_ARGS --build-arg PIP_INDEX_URL=$PIP_INDEX_URL"
            echo "Detected PIP_INDEX_URL"
        fi
        if [ -n "$PIP_TRUSTED_HOST" ]; then
            PIP_ARGS="$PIP_ARGS --build-arg PIP_TRUSTED_HOST=$PIP_TRUSTED_HOST"
            echo "Detected PIP_TRUSTED_HOST"
        fi
    fi
    
    podman build -f ./python-scheduler/Dockerfile $PIP_ARGS -t lgd-litestat-python:dev ./python-scheduler
    
    # 3. Save
    echo "Saving Python Scheduler Image..."
    podman save --format docker-archive -o python-scheduler.tar lgd-litestat-python:dev
    
    echo "Done! Transfer 'python-scheduler.tar' to the offline server."
    exit 0
fi

# Cleanup previous files
rm -f backend.tar frontend.tar dev-base.tar dev-images.tar python-scheduler.tar

echo "Saving images (Full Mode)..."

# Ensure images exist
# Check if backend image exists, if not, user should run docker-compose build
if ! podman image exists lgd-litestat-backend:prod; then
    echo "Building backend prod image..."
    podman build -f ./backend/Dockerfile -t lgd-litestat-backend:prod ./backend
fi

if ! podman image exists lgd-litestat-frontend:prod; then
    echo "Building frontend prod image..."
    podman build -f ./frontend/Dockerfile -t lgd-litestat-frontend:prod ./frontend
fi

# Save Prod Images (using docker-archive format for compatibility)
podman save --format docker-archive -o backend.tar lgd-litestat-backend:prod
podman save --format docker-archive -o frontend.tar lgd-litestat-frontend:prod

# Save Dev Images (Build custom dev images with dependencies)
echo "Building Dev Images..."
podman build -f ./backend/Dockerfile.dev -t lgd-litestat-backend:dev ./backend
podman build -f ./frontend/Dockerfile.dev -t lgd-litestat-frontend:dev ./frontend
# Build Python Scheduler Image (Pass PIP args if needed)
PIP_ARGS=""
if [ -f "./python-scheduler/.env" ]; then
    export $(grep -v '^#' ./python-scheduler/.env | xargs)
    if [ -n "$PIP_INDEX_URL" ]; then PIP_ARGS="$PIP_ARGS --build-arg PIP_INDEX_URL=$PIP_INDEX_URL"; fi
    if [ -n "$PIP_TRUSTED_HOST" ]; then PIP_ARGS="$PIP_ARGS --build-arg PIP_TRUSTED_HOST=$PIP_TRUSTED_HOST"; fi
fi
podman build -f ./python-scheduler/Dockerfile $PIP_ARGS -t lgd-litestat-python:dev ./python-scheduler

echo "Saving Dev Images..."
podman save --format docker-archive -o dev-images.tar lgd-litestat-backend:dev lgd-litestat-frontend:dev lgd-litestat-python:dev

echo "Done! Transfer the following to the offline server:"
echo "1. Images: backend.tar, frontend.tar, dev-images.tar"
echo "2. Configs: docker-compose.prod.yml, docker-compose.dev.yml, offline-load.sh"
echo "3. Source Code (for Dev): backend/, frontend/ folders"
