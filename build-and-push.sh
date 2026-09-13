#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REGISTRY="registry-api.tas.scharber.com"
IMAGE="${REGISTRY}/aether-backend"

echo "=== Building aether-be ==="

# Build
echo "Building Docker image..."
docker build -t "${IMAGE}:latest" "${SCRIPT_DIR}"

# Push
echo "Pushing to registry..."
docker push "${IMAGE}:latest"

echo "=== Done: ${IMAGE}:latest ==="
