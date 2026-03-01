#!/bin/bash
set -e

# Build images explicitly
echo "Building Backend Image..."
podman build -t lgd-litestat-backend:dev -f backend/Dockerfile.dev backend/

echo "Building Frontend Image..."
podman build -t lgd-litestat-frontend:dev -f frontend/Dockerfile.dev frontend/

echo "Building Python Scheduler Image (Legacy/Optional)..."
podman build -t lgd-litestat-python:dev -f python-scheduler/Dockerfile python-scheduler/

echo "Exporting to lgd-litestat-dev-images.tar..."
rm -f lgd-litestat-dev-images.tar
podman save -o lgd-litestat-dev-images.tar lgd-litestat-backend:dev lgd-litestat-frontend:dev lgd-litestat-python:dev

echo "=== Done! ==="
echo "Images saved to lgd-litestat-dev-images.tar"
ls -lh lgd-litestat-dev-images.tar
