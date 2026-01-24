#!/bin/bash
# offline-load.sh
# Load images and start services in offline environment (Docker)
# Usage: ./offline-load.sh

echo "Loading images..."
docker load -i backend.tar
docker load -i frontend.tar

echo "Starting services..."
# Ensure we use the prod config which now has no 'build' section
docker compose -f docker-compose.prod.yml up -d

echo "Services started!"
