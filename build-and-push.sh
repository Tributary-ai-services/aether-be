#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REGISTRY="registry-api.tas.scharber.com"
IMAGE="${REGISTRY}/aether-backend"

echo "=== Building aether-be ==="

# Copy shared go-events module into build context
echo "Copying go-events module into build context..."
rm -rf "${SCRIPT_DIR}/go-events"
cp -r "${SCRIPT_DIR}/../aether-shared/go-events" "${SCRIPT_DIR}/go-events"

# Build
echo "Building Docker image..."
docker build -t "${IMAGE}:latest" "${SCRIPT_DIR}"

# Clean up
rm -rf "${SCRIPT_DIR}/go-events"

# Push
echo "Pushing to registry..."
docker push "${IMAGE}:latest"

echo "=== Done: ${IMAGE}:latest ==="
