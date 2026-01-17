#!/bin/bash

# Aether Backend Deployment Script
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Default values
ENVIRONMENT="dev"
BUILD_IMAGE=false
PUSH_IMAGE=false
NAMESPACE=""
REGISTRY="localhost:5000"
IMAGE_TAG=""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Usage function
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Deploy Aether Backend to Kubernetes

OPTIONS:
    -e, --environment ENV    Target environment (dev|staging|testing|production) [default: dev]
    -b, --build             Build Docker image before deployment
    -p, --push              Push Docker image to registry (requires --build)
    -r, --registry REGISTRY Docker registry URL [default: localhost:5000]
    -t, --tag TAG           Image tag [default: environment name]
    -n, --namespace NS      Override namespace
    -h, --help              Show this help message

EXAMPLES:
    $0 -e dev -b                    # Build and deploy to dev environment
    $0 -e production -b -p          # Build, push, and deploy to production
    $0 -e staging -t v1.2.3         # Deploy staging with specific tag

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--environment)
            ENVIRONMENT="$2"
            shift 2
            ;;
        -b|--build)
            BUILD_IMAGE=true
            shift
            ;;
        -p|--push)
            PUSH_IMAGE=true
            shift
            ;;
        -r|--registry)
            REGISTRY="$2"
            shift 2
            ;;
        -t|--tag)
            IMAGE_TAG="$2"
            shift 2
            ;;
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Validate environment
case $ENVIRONMENT in
    dev|staging|testing|production)
        ;;
    *)
        log_error "Invalid environment: $ENVIRONMENT"
        log_error "Valid environments: dev, staging, testing, production"
        exit 1
        ;;
esac

# Set defaults based on environment
if [[ -z "$IMAGE_TAG" ]]; then
    IMAGE_TAG="$ENVIRONMENT"
fi

if [[ -z "$NAMESPACE" ]]; then
    NAMESPACE="aether-$ENVIRONMENT"
fi

IMAGE_NAME="$REGISTRY/aether-backend:$IMAGE_TAG"

log_info "Deployment Configuration:"
log_info "  Environment: $ENVIRONMENT"
log_info "  Namespace: $NAMESPACE"
log_info "  Image: $IMAGE_NAME"
log_info "  Build Image: $BUILD_IMAGE"
log_info "  Push Image: $PUSH_IMAGE"

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check if kubectl is available
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed or not in PATH"
        exit 1
    fi
    
    # Check if kustomize is available
    if ! command -v kustomize &> /dev/null; then
        log_error "kustomize is not installed or not in PATH"
        exit 1
    fi
    
    # Check if Docker is available (if building)
    if [[ "$BUILD_IMAGE" == true ]]; then
        if ! command -v docker &> /dev/null; then
            log_error "docker is not installed or not in PATH"
            exit 1
        fi
        
        if ! docker info &> /dev/null; then
            log_error "Docker daemon is not running"
            exit 1
        fi
    fi
    
    # Check if kubectl can connect to cluster
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi
    
    log_success "All prerequisites met"
}

# Build Docker image
build_image() {
    if [[ "$BUILD_IMAGE" == true ]]; then
        log_info "Building Docker image: $IMAGE_NAME"
        
        cd "$PROJECT_ROOT"
        docker build -t "$IMAGE_NAME" .
        
        log_success "Docker image built successfully"
        
        if [[ "$PUSH_IMAGE" == true ]]; then
            log_info "Pushing Docker image to registry..."
            docker push "$IMAGE_NAME"
            log_success "Docker image pushed successfully"
        fi
    fi
}

# Deploy to Kubernetes
deploy_kubernetes() {
    log_info "Deploying to Kubernetes environment: $ENVIRONMENT"
    
    OVERLAY_PATH="$PROJECT_ROOT/deployments/overlays/$ENVIRONMENT"
    
    if [[ ! -d "$OVERLAY_PATH" ]]; then
        log_error "Overlay directory not found: $OVERLAY_PATH"
        exit 1
    fi
    
    # Update image in kustomization if building
    if [[ "$BUILD_IMAGE" == true ]]; then
        log_info "Updating image tag in kustomization..."
        cd "$OVERLAY_PATH"
        kustomize edit set image "aether-backend=$IMAGE_NAME"
    fi
    
    # Apply the manifests
    log_info "Applying Kubernetes manifests..."
    kubectl apply -k "$OVERLAY_PATH"
    
    # Wait for deployment to be ready
    log_info "Waiting for deployment to be ready..."
    kubectl wait --for=condition=available --timeout=300s deployment/aether-backend -n "$NAMESPACE" || {
        log_error "Deployment failed to become ready within 5 minutes"
        log_info "Recent events:"
        kubectl get events -n "$NAMESPACE" --sort-by='.lastTimestamp' | tail -10
        exit 1
    }
    
    log_success "Deployment completed successfully"
}

# Show deployment status
show_status() {
    log_info "Deployment Status:"
    
    echo
    log_info "Pods:"
    kubectl get pods -n "$NAMESPACE" -l app=aether-backend
    
    echo
    log_info "Services:"
    kubectl get services -n "$NAMESPACE"
    
    echo
    log_info "Ingress:"
    kubectl get ingress -n "$NAMESPACE"
    
    if [[ "$ENVIRONMENT" == "production" ]]; then
        echo
        log_info "HPA Status:"
        kubectl get hpa -n "$NAMESPACE"
    fi
}

# Main execution
main() {
    log_info "Starting deployment process..."
    
    check_prerequisites
    build_image
    deploy_kubernetes
    show_status
    
    log_success "Deployment process completed!"
    
    case $ENVIRONMENT in
        dev)
            log_info "Access the application at: http://aether-dev.local"
            ;;
        staging)
            log_info "Access the application at: https://aether-staging.aether.com"
            ;;
        testing)
            log_info "Access the application at: http://aether-testing.local"
            ;;
        production)
            log_info "Access the application at: https://api.aether.com"
            ;;
    esac
}

# Run main function
main "$@"