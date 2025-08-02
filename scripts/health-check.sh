#!/bin/bash

# Aether Backend Health Check Script
set -euo pipefail

# Default values
ENVIRONMENT="dev"
NAMESPACE=""
HOST=""
PORT="8080"
PROTOCOL="http"

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

Health check for Aether Backend services

OPTIONS:
    -e, --environment ENV    Target environment (dev|staging|testing|production) [default: dev]
    -n, --namespace NS       Override namespace
    -H, --host HOST         Override host (for external access)
    -p, --port PORT         Override port [default: 8080]
    -s, --secure            Use HTTPS instead of HTTP
    -h, --help              Show this help message

EXAMPLES:
    $0 -e dev                                    # Health check dev environment
    $0 -e production -H api.aether.com -s       # Health check production via external URL
    $0 -n aether-dev -H localhost -p 8080       # Health check via port forward

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
        -H|--host)
            HOST="$2"
            shift 2
            ;;
        -p|--port)
            PORT="$2"
            shift 2
            ;;
        -s|--secure)
            PROTOCOL="https"
            shift
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

if [[ -z "$HOST" ]]; then
    case $ENVIRONMENT in
        dev)
            HOST="aether-dev.local"
            ;;
        staging)
            HOST="aether-staging.aether.com"
            PROTOCOL="https"
            PORT="443"
            ;;
        testing)
            HOST="aether-testing.local"
            ;;
        production)
            HOST="api.aether.com"
            PROTOCOL="https"
            PORT="443"
            ;;
    esac
fi

BASE_URL="${PROTOCOL}://${HOST}:${PORT}"
if [[ "$PORT" == "443" && "$PROTOCOL" == "https" ]] || [[ "$PORT" == "80" && "$PROTOCOL" == "http" ]]; then
    BASE_URL="${PROTOCOL}://${HOST}"
fi

log_info "Health Check Configuration:"
log_info "  Environment: $ENVIRONMENT"
log_info "  Namespace: $NAMESPACE"
log_info "  Base URL: $BASE_URL"

# Check if curl is available
check_prerequisites() {
    if ! command -v curl &> /dev/null; then
        log_error "curl is not installed or not in PATH"
        exit 1
    fi
}

# Health check for main API
check_api_health() {
    log_info "Checking API health endpoint..."
    
    local health_url="${BASE_URL}/health"
    local response
    local http_code
    
    if response=$(curl -s -w "%{http_code}" "$health_url" 2>/dev/null); then
        http_code="${response: -3}"
        response="${response%???}"
        
        if [[ "$http_code" == "200" ]]; then
            log_success "API health check passed"
            log_info "Response: $response"
            return 0
        else
            log_error "API health check failed with HTTP $http_code"
            log_error "Response: $response"
            return 1
        fi
    else
        log_error "Failed to connect to API health endpoint"
        return 1
    fi
}

# Check API status endpoint
check_api_status() {
    log_info "Checking API status endpoint..."
    
    local status_url="${BASE_URL}/api/v1/status"
    local response
    local http_code
    
    if response=$(curl -s -w "%{http_code}" "$status_url" 2>/dev/null); then
        http_code="${response: -3}"
        response="${response%???}"
        
        if [[ "$http_code" == "200" ]]; then
            log_success "API status check passed"
            log_info "Response: $response"
            return 0
        else
            log_error "API status check failed with HTTP $http_code"
            log_error "Response: $response"
            return 1
        fi
    else
        log_error "Failed to connect to API status endpoint"
        return 1
    fi
}

# Check if running in Kubernetes, then check service endpoints
check_service_endpoints() {
    if ! command -v kubectl &> /dev/null; then
        log_info "kubectl not available, skipping service endpoint checks"
        return 0
    fi
    
    if ! kubectl cluster-info &> /dev/null; then
        log_info "Cannot connect to Kubernetes cluster, skipping service endpoint checks"
        return 0
    fi
    
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        log_info "Namespace '$NAMESPACE' does not exist, skipping service endpoint checks"
        return 0
    fi
    
    log_info "Checking service endpoints in Kubernetes..."
    
    local services=("aether-backend" "neo4j" "redis" "keycloak")
    local failed_services=()
    
    for service in "${services[@]}"; do
        if kubectl get service "$service" -n "$NAMESPACE" &> /dev/null; then
            local endpoints
            endpoints=$(kubectl get endpoints "$service" -n "$NAMESPACE" -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || echo "")
            
            if [[ -n "$endpoints" ]]; then
                log_success "Service '$service' has endpoints: $endpoints"
            else
                log_error "Service '$service' has no endpoints"
                failed_services+=("$service")
            fi
        else
            log_warning "Service '$service' not found"
        fi
    done
    
    if [[ ${#failed_services[@]} -gt 0 ]]; then
        log_error "Services without endpoints: ${failed_services[*]}"
        return 1
    fi
    
    return 0
}

# Performance test
run_performance_test() {
    log_info "Running basic performance test..."
    
    local health_url="${BASE_URL}/health"
    local start_time
    local end_time
    local duration
    
    start_time=$(date +%s.%N)
    
    if curl -s -f "$health_url" > /dev/null 2>&1; then
        end_time=$(date +%s.%N)
        duration=$(echo "$end_time - $start_time" | bc -l 2>/dev/null || echo "N/A")
        
        if [[ "$duration" != "N/A" ]]; then
            log_success "Response time: ${duration}s"
            
            # Check if response time is reasonable (less than 2 seconds)
            if (( $(echo "$duration < 2.0" | bc -l 2>/dev/null || echo 0) )); then
                log_success "Response time is acceptable"
            else
                log_warning "Response time is slow (>${duration}s)"
            fi
        fi
    else
        log_error "Performance test failed - endpoint not accessible"
        return 1
    fi
}

# Database connectivity check (if in cluster)
check_database_connectivity() {
    if ! command -v kubectl &> /dev/null; then
        log_info "kubectl not available, skipping database connectivity checks"
        return 0
    fi
    
    if ! kubectl cluster-info &> /dev/null; then
        log_info "Cannot connect to Kubernetes cluster, skipping database connectivity checks"
        return 0
    fi
    
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        log_info "Namespace '$NAMESPACE' does not exist, skipping database connectivity checks"
        return 0
    fi
    
    log_info "Checking database connectivity..."
    
    # Check Neo4j
    if kubectl get pod -l app=neo4j -n "$NAMESPACE" -o name | head -1 | read -r neo4j_pod; then
        neo4j_pod=$(basename "$neo4j_pod")
        if kubectl exec -n "$NAMESPACE" "$neo4j_pod" -- cypher-shell -u neo4j -p changeme "RETURN 1" &> /dev/null; then
            log_success "Neo4j connectivity check passed"
        else
            log_error "Neo4j connectivity check failed"
        fi
    else
        log_info "Neo4j pod not found, skipping connectivity check"
    fi
    
    # Check Redis
    if kubectl get pod -l app=redis -n "$NAMESPACE" -o name | head -1 | read -r redis_pod; then
        redis_pod=$(basename "$redis_pod")
        if kubectl exec -n "$NAMESPACE" "$redis_pod" -- redis-cli ping | grep -q PONG; then
            log_success "Redis connectivity check passed"
        else
            log_error "Redis connectivity check failed"
        fi
    else
        log_info "Redis pod not found, skipping connectivity check"
    fi
}

# Main execution
main() {
    log_info "Starting health check..."
    
    local health_check_failed=false
    
    check_prerequisites
    
    if ! check_api_health; then
        health_check_failed=true
    fi
    
    if ! check_api_status; then
        health_check_failed=true
    fi
    
    if ! check_service_endpoints; then
        health_check_failed=true
    fi
    
    if ! check_database_connectivity; then
        health_check_failed=true
    fi
    
    run_performance_test
    
    if [[ "$health_check_failed" == true ]]; then
        log_error "Health check failed!"
        exit 1
    else
        log_success "All health checks passed!"
    fi
}

# Run main function
main "$@"