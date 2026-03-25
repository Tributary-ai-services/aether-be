# Neo4j Configuration for Aether Backend

## Overview
Neo4j is deployed with Bolt protocol accessible through nginx-ingress. TLS is terminated at the ingress layer (not by Neo4j itself).

## Components

### 1. Neo4j StatefulSet (`neo4j.yaml`)
- **Image**: neo4j:5.15-community
- **Bolt Protocol**: Port 7687, TLS DISABLED (ingress handles TLS termination)
- **HTTP Browser**: Port 7474
- **Advertised Address**: `neo4j-bolt.tas.scharber.com:443` (routes through ingress)
- **Health Probes**: TCP socket checks on port 7687

### 2. TLS Certificate (`neo4j-certificate.yaml`)
- **Type**: Self-signed certificate from tas-ca-issuer (ClusterIssuer)
- **DNS Names**:
  - neo4j.tas.scharber.com
  - neo4j.aether-be.svc.cluster.local
  - 192.168.68.240
- **Secret**: neo4j-bolt-tls (contains ca.crt, tls.crt, tls.key)
- **Note**: Used by the init container for legacy compatibility but Bolt TLS is disabled

### 3. Services
- **Internal ClusterIP** (`neo4j`):
  - Port 7474 (HTTP)
  - Port 7473 (HTTPS)
  - Port 7687 (Bolt, plain)

- **External NodePort** (`neo4j-external`):
  - Port 30687 → 7687 (Bolt)
  - Port 30473 → 7473 (HTTPS)

### 4. Ingresses (`ingress.yaml`)
- **Neo4j Browser HTTP UI**: https://neo4j.tas.scharber.com → port 7474
- **Neo4j Bolt WebSocket**: https://neo4j-bolt.tas.scharber.com → port 7687
- Both use TLS termination at the ingress (self-signed certs from tas-ca-issuer)

## Connection Methods

### From Web Browser (Recommended)
1. **Open**: https://neo4j.tas.scharber.com
2. **Connect URL**: `neo4j+s://neo4j-bolt.tas.scharber.com:443`
3. **Username**: `neo4j`
4. **Password**: `password`

### From Applications (Inside Cluster)
```
URI: bolt://neo4j.aether-be.svc.cluster.local:7687
Username: neo4j
Password: password
```
Note: Use plain `bolt://` — TLS is disabled on the Neo4j Bolt port. Internal cluster traffic does not need encryption.

### From Applications (Outside Cluster)
```
URI: neo4j+s://neo4j-bolt.tas.scharber.com:443
Username: neo4j
Password: password
```
Note: External access goes through the NGINX ingress which terminates TLS, then forwards plain traffic to Neo4j.

### Direct NodePort Access
```
URI: bolt://192.168.68.240:30687
Username: neo4j
Password: password
```
Note: NodePort exposes plain Bolt (no TLS).

## Configuration Details

### Bolt Configuration
```yaml
NEO4J_server_bolt_tls__level: "DISABLED"
NEO4J_dbms_ssl_policy_bolt_enabled: "false"
NEO4J_server_bolt_advertised__address: "neo4j-bolt.tas.scharber.com:443"
NEO4J_server_default__advertised__address: "neo4j-bolt.tas.scharber.com"
```

### Aether Backend ConfigMap
```yaml
NEO4J_URI: "bolt://neo4j.aether-be.svc.cluster.local:7687"
NEO4J_DATABASE: "neo4j"
NEO4J_TLS_INSECURE: "false"
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

### Test Bolt Connection (inside cluster)
```bash
kubectl exec -n aether-be neo4j-0 -- cypher-shell -a bolt://localhost:7687 -u neo4j -p password "RETURN 1"
```

### Test Bolt via Ingress
```bash
# From outside the cluster, use cypher-shell with TLS to the ingress
cypher-shell -a neo4j+s://neo4j-bolt.tas.scharber.com:443 -u neo4j -p password "RETURN 1"
```

### Check Ingresses
```bash
kubectl get ingress -n aether-be | grep neo4j
```

## Notes

- Neo4j Bolt TLS is DISABLED — TLS termination happens at the NGINX ingress layer
- Neo4j Browser requires `neo4j+s://` scheme when connecting through the ingress (browser enforces secure connections)
- Internal cluster services use plain `bolt://` for direct pod-to-pod communication
- For production, consider using Let's Encrypt certificates with proper DNS configuration
