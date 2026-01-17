# Neo4j Configuration for Aether Backend

## Overview
Neo4j is deployed with TLS-enabled Bolt protocol accessible through nginx-ingress TCP passthrough.

## Components

### 1. Neo4j StatefulSet (`neo4j.yaml`)
- **Image**: neo4j:5.15-community
- **Bolt Protocol**: Port 7687 with TLS REQUIRED
- **HTTP Browser**: Port 7474
- **TLS Certificates**: Self-signed from tas-ca-issuer
- **Init Container**: Prepares certificates in Neo4j's expected format
- **Health Probes**: TCP socket checks on port 7687

### 2. TLS Certificate (`neo4j-certificate.yaml`)
- **Type**: Self-signed certificate from tas-ca-issuer (ClusterIssuer)
- **DNS Names**:
  - neo4j.tas.scharber.com
  - neo4j.aether-be.svc.cluster.local
  - 192.168.68.240
- **Secret**: neo4j-bolt-tls (contains ca.crt, tls.crt, tls.key)

### 3. Services
- **Internal ClusterIP** (`neo4j`):
  - Port 7474 (HTTP)
  - Port 7473 (HTTPS)
  - Port 7687 (Bolt+TLS)

- **External NodePort** (`neo4j-external`):
  - Port 30687 → 7687 (Bolt+TLS)
  - Port 30473 → 7473 (HTTPS)

### 4. Ingress (`ingress.yaml`)
- **Neo4j Browser HTTP UI**: https://neo4j.tas.scharber.com
- **TLS**: Self-signed certificate
- **WebSocket Support**: Enabled for real-time features

### 5. TCP Passthrough Configuration
Located in: `/home/jscharber/eng/TAS/aether-shared/k8s-shared-infrastructure/ingress-controller.yaml`

- **TCP Services ConfigMap**: Maps port 7687 → aether-be/neo4j:7687
- **LoadBalancer Service**: Exposes port 7687 on 192.168.68.240
- **nginx-ingress Controller**: Configured with `--tcp-services-configmap` flag

## Connection Methods

### From Web Browser (Recommended)
1. **Open**: https://neo4j.tas.scharber.com/browser/
2. **Connect URL**: `bolt+s://neo4j.tas.scharber.com:7687`
3. **Username**: `neo4j`
4. **Password**: `password`

### From Applications (Inside Cluster)
```
URI: bolt+ssc://neo4j.aether-be.svc.cluster.local:7687
Username: neo4j
Password: password
```
Note: Use `bolt+ssc://` (self-signed certificate) for development environments with self-signed TLS certificates.

### From Applications (Outside Cluster)
```
URI: bolt+s://neo4j.tas.scharber.com:7687
Username: neo4j
Password: password
```
Note: External access via browser uses `bolt+s://` but requires trusting the CA certificate.

### Direct NodePort Access
```
URI: bolt+ssc://192.168.68.240:30687
Username: neo4j
Password: password
```

## Certificate Trust

The CA certificate is stored in the cluster as part of the tas-ca-issuer. To trust it in browsers:

```bash
# Extract CA certificate
kubectl get secret -n aether-be neo4j-bolt-tls -o jsonpath='{.data.ca\.crt}' | base64 -d > tas-root-ca.crt

# Import into browser's trusted Certificate Authorities
```

## Configuration Details

### Bolt TLS Configuration
```yaml
NEO4J_server_bolt_tls__level: "REQUIRED"
NEO4J_dbms_ssl_policy_bolt_enabled: "true"
NEO4J_dbms_ssl_policy_bolt_base__directory: "/var/lib/neo4j/certificates/bolt"
NEO4J_dbms_ssl_policy_bolt_private__key: "private.key"
NEO4J_dbms_ssl_policy_bolt_public__certificate: "public.crt"
NEO4J_dbms_ssl_policy_bolt_client__auth: "NONE"
```

### Resource Allocation
- **Memory Request**: 2Gi (Heap: 2G, Pagecache: 1G)
- **Memory Limit**: 4Gi
- **CPU Request**: 500m
- **CPU Limit**: 2000m

### Storage
- **neo4j-data**: 20Gi (Database files)
- **neo4j-logs**: 5Gi (Log files)
- **neo4j-import**: 10Gi (Import directory)
- **neo4j-plugins**: 1Gi (APOC and other plugins)

## Plugins
- **APOC**: Installed and configured
  - Export enabled
  - Import enabled

## Deployment

To deploy the full stack:
```bash
cd /home/jscharber/eng/TAS/aether-be
kubectl apply -k k8s/
```

To redeploy only Neo4j:
```bash
kubectl delete statefulset neo4j -n aether-be
kubectl apply -f k8s/neo4j.yaml
```

## Troubleshooting

### Check Neo4j Status
```bash
kubectl get pods -n aether-be -l app=neo4j
kubectl logs -n aether-be neo4j-0 -c neo4j --tail=50
```

### Verify TLS Certificate
```bash
kubectl get certificate -n aether-be neo4j-bolt-tls
openssl s_client -connect neo4j.tas.scharber.com:7687 -showcerts
```

### Test Bolt Connection
```bash
kubectl exec -n aether-be neo4j-0 -- cypher-shell -a bolt+s://localhost:7687 -u neo4j -p password "RETURN 1"
```

### Check TCP Passthrough
```bash
kubectl get configmap -n ingress-nginx tcp-services -o yaml
kubectl get svc -n ingress-nginx ingress-nginx-controller
```

## Notes

- Neo4j Browser requires secure connections (`bolt+s://`) for remote access
- The TCP passthrough configuration in nginx-ingress is essential for Bolt to work through the ingress
- Self-signed certificates are used - browsers will show a warning until the CA is trusted
- For production, consider using Let's Encrypt certificates with proper DNS configuration
