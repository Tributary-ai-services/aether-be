#!/bin/bash

# Aether Backend Environment Validation Script
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Default values
ENVIRONMENT="dev"
NAMESPACE=""
TIMEOUT=300

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

Validate Aether Backend deployment in Kubernetes

OPTIONS:
    -e, --environment ENV    Target environment (dev|staging|testing|production) [default: dev]
    -n, --namespace NS       Override namespace
    -t, --timeout SECONDS   Timeout for health checks [default: 300]
    -h, --help              Show this help message

EXAMPLES:
    $0 -e dev                       # Validate dev environment
    $0 -e production -t 600         # Validate production with 10min timeout

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--environment)
            ENVIRONMENT="$2"
            shift 2
            ;;
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -t|--timeout)
            TIMEOUT="$2"
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
if [[ -z "$NAMESPACE" ]]; then
    NAMESPACE="aether-$ENVIRONMENT"
fi

log_info "Validation Configuration:"
log_info "  Environment: $ENVIRONMENT"
log_info "  Namespace: $NAMESPACE"
log_info "  Timeout: ${TIMEOUT}s"

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check if kubectl is available
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed or not in PATH"
        exit 1
    fi
    
    # Check if kubectl can connect to cluster
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi
    
    # Check if namespace exists
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        log_error "Namespace '$NAMESPACE' does not exist"
        exit 1
    fi
    
    log_success "All prerequisites met"
}

# Validate deployments
validate_deployments() {
    log_info "Validating deployments..."
    
    local deployments=("aether-backend" "neo4j" "redis" "keycloak" "kafka" "zookeeper")
    if [[ "$ENVIRONMENT" != "testing" ]]; then
        deployments+=("prometheus" "grafana")
    fi
    
    local failed_deployments=()
    
    for deployment in "${deployments[@]}"; do
        log_info "Checking deployment: $deployment"
        
        if ! kubectl get deployment "$deployment" -n "$NAMESPACE" &> /dev/null; then
            log_warning "Deployment '$deployment' not found"
            failed_deployments+=("$deployment")
            continue
        fi
        
        # Check if deployment is ready
        if ! kubectl wait --for=condition=available --timeout="${TIMEOUT}s" deployment/"$deployment" -n "$NAMESPACE" &> /dev/null; then
            log_error "Deployment '$deployment' is not ready"
            failed_deployments+=("$deployment")
        else
            log_success "Deployment '$deployment' is ready"
        fi
    done
    
    if [[ ${#failed_deployments[@]} -gt 0 ]]; then
        log_error "Failed deployments: ${failed_deployments[*]}"
        return 1
    fi
    
    log_success "All deployments are ready"
}

# Validate services
validate_services() {
    log_info "Validating services..."
    
    local services=("aether-backend" "neo4j" "redis" "keycloak" "kafka" "zookeeper")
    if [[ "$ENVIRONMENT" != "testing" ]]; then
        services+=("prometheus" "grafana")
    fi
    
    local failed_services=()
    
    for service in "${services[@]}"; do
        log_info "Checking service: $service"
        
        if ! kubectl get service "$service" -n "$NAMESPACE" &> /dev/null; then
            log_warning "Service '$service' not found"
            failed_services+=("$service")
            continue
        fi
        
        # Check if service has endpoints
        local endpoints
        endpoints=$(kubectl get endpoints "$service" -n "$NAMESPACE" -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || echo "")
        
        if [[ -z "$endpoints" ]]; then
            log_error "Service '$service' has no endpoints"
            failed_services+=("$service")
        else
            log_success "Service '$service' has endpoints"
        fi
    done
    
    if [[ ${#failed_services[@]} -gt 0 ]]; then
        log_error "Failed services: ${failed_services[*]}"
        return 1
    fi
    
    log_success "All services have endpoints"
}

# Validate health endpoints
validate_health() {
    log_info "Validating health endpoints..."
    
    # Port forward to aether-backend service
    log_info "Setting up port forwarding to aether-backend..."
    kubectl port-forward -n "$NAMESPACE" service/aether-backend 8080:8080 &
    local port_forward_pid=$!
    
    # Wait for port forwarding to be established
    sleep 5
    
    # Check health endpoint
    local health_check_passed=false
    for i in {1..10}; do
        if curl -s -f "http://localhost:8080/health" > /dev/null 2>&1; then
            log_success "Aether Backend health check passed"
            health_check_passed=true
            break
        else
            log_info "Health check attempt $i/10 failed, retrying..."
            sleep 5
        fi
    done
    
    # Clean up port forwarding
    kill $port_forward_pid 2>/dev/null || true
    
    if [[ "$health_check_passed" == false ]]; then
        log_error "Aether Backend health check failed"
        return 1
    fi
    
    log_success "Health endpoint validation completed"
}

# Validate persistent volumes
validate_storage() {
    log_info "Validating persistent volumes..."
    
    local pvcs=("neo4j-data-pvc" "redis-data-pvc" "keycloak-db-pvc" "kafka-data-pvc" "zookeeper-data-pvc")
    if [[ "$ENVIRONMENT" != "testing" ]]; then
        pvcs+=("prometheus-storage-pvc" "grafana-storage-pvc")
    fi
    
    local failed_pvcs=()
    
    for pvc in "${pvcs[@]}"; do
        log_info "Checking PVC: $pvc"
        
        if ! kubectl get pvc "$pvc" -n "$NAMESPACE" &> /dev/null; then
            log_warning "PVC '$pvc' not found"
            failed_pvcs+=("$pvc")
            continue
        fi
        
        # Check PVC status
        local status
        status=$(kubectl get pvc "$pvc" -n "$NAMESPACE" -o jsonpath='{.status.phase}')
        
        if [[ "$status" != "Bound" ]]; then
            log_error "PVC '$pvc' is not bound (status: $status)"
            failed_pvcs+=("$pvc")
        else
            log_success "PVC '$pvc' is bound"
        fi
    done
    
    if [[ ${#failed_pvcs[@]} -gt 0 ]]; then
        log_error "Failed PVCs: ${failed_pvcs[*]}"
        return 1
    fi
    
    log_success "All PVCs are bound"
}

# Show environment status
show_status() {
    log_info "Environment Status Summary:"
    
    echo
    log_info "Pods:"
    kubectl get pods -n "$NAMESPACE" -o wide
    
    echo
    log_info "Services:"
    kubectl get services -n "$NAMESPACE"
    
    echo
    log_info "Ingress:"
    kubectl get ingress -n "$NAMESPACE" 2>/dev/null || log_info "No ingress resources found"
    
    echo
    log_info "PVCs:"
    kubectl get pvc -n "$NAMESPACE"
    
    if [[ "$ENVIRONMENT" == "production" ]]; then
        echo
        log_info "HPA Status:"
        kubectl get hpa -n "$NAMESPACE" 2>/dev/null || log_info "No HPA resources found"
    fi
    
    echo
    log_info "Resource Usage:"
    kubectl top pods -n "$NAMESPACE" 2>/dev/null || log_info "Metrics server not available"
}

# Main execution
main() {
    log_info "Starting environment validation..."
    
    local validation_failed=false
    
    check_prerequisites
    
    if ! validate_deployments; then
        validation_failed=true
    fi
    
    if ! validate_services; then
        validation_failed=true
    fi
    
    if ! validate_storage; then
        validation_failed=true
    fi
    
    if ! validate_health; then
        validation_failed=true
    fi
    
    show_status
    
    if [[ "$validation_failed" == true ]]; then
        log_error "Environment validation failed!"
        exit 1
    else
        log_success "Environment validation passed!"
        
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
    fi
}

# Run main function
main "$@"