# Photo API Helm Chart

## Overview

This Helm chart deploys the complete **photo-api** solution to Kubernetes as a single release. It packages five workloads, their services, persistent storage, secrets, and a Dapr component into one parameterised chart.

| Component | Kind | Service Type | Purpose |
|---|---|---|---|
| **photo-api** | Deployment | LoadBalancer | REST API serving the SPA frontend |
| **resize-api** | Deployment | ClusterIP | Dapr event-driven image resize worker |
| **blobemu** | StatefulSet | ClusterIP | Lightweight Azure Blob Storage emulator |
| **rabbitmq** | StatefulSet | ClusterIP | AMQP message broker for blob events |
| **otel-collector** | StatefulSet | ClusterIP | Grafana LGTM observability stack |

## Chart Structure

```
helm/photo-api/
├── Chart.yaml                        # Chart metadata (v0.1.0, appVersion 1.0.0)
├── values.yaml                       # Default configuration values
└── templates/
    ├── _helpers.tpl                   # Reusable template helpers
    ├── NOTES.txt                      # Post-install usage instructions
    ├── secrets.yaml                   # RabbitMQ + Grafana credentials
    ├── rabbitmq.yaml                  # StatefulSet + Service
    ├── blobemu.yaml                   # StatefulSet + Service
    ├── otel-collector.yaml            # StatefulSet + Service (optional)
    ├── photo-api.yaml                 # Deployment + Service (LoadBalancer)
    ├── resize-api.yaml                # Deployment + Service + Dapr annotations
    └── dapr-component.yaml            # Dapr Component CRD (optional)
```

## Design Choices

### Template Helpers (`_helpers.tpl`)

Four named templates keep the manifests DRY:

| Helper | Purpose |
|---|---|
| `photo-api.name` | Chart name, truncated to 63 chars. Overridable via `nameOverride`. |
| `photo-api.fullname` | `<release>-<chart>` qualified name. Overridable via `fullnameOverride`. |
| `photo-api.labels` | Common labels on every resource: `helm.sh/chart`, `managed-by`, `part-of`, `version`. |
| `photo-api.selectorLabels` | Per-component selector labels: `app.kubernetes.io/name` + `app.kubernetes.io/instance`. |
| `photo-api.serviceFQDN` | Generates `<svc>.<namespace>.svc.cluster.local` for inter-service references. |

### Stateful vs Stateless Workloads

- **Deployments** are used for stateless services (`photo-api`, `resize-api`) that can scale horizontally.
- **StatefulSets** are used for data-bearing services (`rabbitmq`, `blobemu`, `otel-collector`) that need stable network identities and persistent volumes.

### Service Types

- `photo-api` defaults to **LoadBalancer** to expose the API externally.
- All other services default to **ClusterIP** (internal only). Each service type is configurable via values.

### Secrets Management

Credentials for RabbitMQ and Grafana are stored in Kubernetes `Secret` objects and injected via `secretKeyRef`. Default values (`guest`/`admin`) are suitable for development — override them for production.

### Persistent Storage

Each StatefulSet uses a `volumeClaimTemplate` for dynamic PVC provisioning:

| Service | Mount Path | Default Size | Purpose |
|---|---|---|---|
| rabbitmq | `/var/lib/rabbitmq` | 1 Gi | Message queue data |
| blobemu | `/data` | 5 Gi | Blob files + SQLite database |
| otel-collector | `/var/lib/grafana` | 2 Gi | Grafana dashboards + data |

Set `*.persistence.storageClass` to target a specific storage class, or leave empty to use the cluster default.

### Optional Components

Two components can be toggled off:

| Value | Default | Effect when `false` |
|---|---|---|
| `otelCollector.enabled` | `true` | Skips the OTel/Grafana StatefulSet and Service |
| `daprComponent.enabled` | `true` | Skips the Dapr `Component` CRD |

### Dapr Integration

The Dapr sidecar is injected into `resize-api` via pod annotations rather than a manually defined container. This requires the [Dapr operator](https://docs.dapr.io/operations/hosting/kubernetes/kubernetes-deploy/) to be installed in the cluster. Dapr configuration is parameterised:

```yaml
resizeApi:
  dapr:
    enabled: true
    appId: resize-api
    appPort: "8081"
    appProtocol: grpc
    logLevel: info
```

### Inter-Service Networking

All service-to-service URLs are constructed at template time using the `photo-api.serviceFQDN` helper:

```
amqp://guest:guest@rabbitmq.<namespace>.svc.cluster.local:5672/
http://blobemu.<namespace>.svc.cluster.local:10000
otel-collector.<namespace>.svc.cluster.local:4317
```

This ensures the chart works regardless of the target namespace.

### Health Probes

| Service | Readiness | Liveness |
|---|---|---|
| photo-api | `GET /readyz` (lightweight connectivity check) | `GET /healthz` |
| blobemu | `GET /healthz` | `GET /healthz` |
| rabbitmq | `rabbitmq-diagnostics -q ping` | `rabbitmq-diagnostics -q ping` |

## Values Reference

### Global

| Key | Default | Description |
|---|---|---|
| `nameOverride` | `""` | Override the chart name |
| `fullnameOverride` | `""` | Override the fully qualified release name |
| `global.imagePullPolicy` | `IfNotPresent` | Image pull policy for all containers |

### Photo API (`photoApi`)

| Key | Default | Description |
|---|---|---|
| `photoApi.replicaCount` | `2` | Number of replicas |
| `photoApi.image.repository` | `photo-api` | Container image repository |
| `photoApi.image.tag` | `latest` | Container image tag |
| `photoApi.service.type` | `LoadBalancer` | Kubernetes Service type |
| `photoApi.service.port` | `8080` | Service and container port |
| `photoApi.env.serviceName` | `photo` | `SERVICE_NAME` env var |
| `photoApi.env.servicePort` | `"8080"` | `SERVICE_PORT` env var |
| `photoApi.resources` | 50m–500m CPU, 64Mi–256Mi mem | Resource requests/limits |

### Resize API (`resizeApi`)

| Key | Default | Description |
|---|---|---|
| `resizeApi.replicaCount` | `1` | Number of replicas |
| `resizeApi.image.repository` | `resize-api` | Container image repository |
| `resizeApi.image.tag` | `latest` | Container image tag |
| `resizeApi.service.type` | `ClusterIP` | Kubernetes Service type |
| `resizeApi.service.port` | `8081` | Service and container port |
| `resizeApi.dapr.enabled` | `true` | Enable Dapr sidecar injection |
| `resizeApi.dapr.appId` | `resize-api` | Dapr application identity |
| `resizeApi.dapr.appPort` | `"8081"` | Port Dapr forwards to |
| `resizeApi.dapr.appProtocol` | `grpc` | Protocol (grpc/http) |
| `resizeApi.dapr.logLevel` | `info` | Dapr sidecar log level |
| `resizeApi.env.imagesContainerName` | `images` | Target container for resized images |
| `resizeApi.env.maxImageHeight` | `"1200"` | Max resize height in pixels |
| `resizeApi.env.maxImageWidth` | `"1600"` | Max resize width in pixels |
| `resizeApi.env.uploadsQueueBinding` | `queue-uploads` | Dapr binding component name |
| `resizeApi.resources` | 100m–1 CPU, 128Mi–512Mi mem | Resource requests/limits |

### Blob Emulator (`blobemu`)

| Key | Default | Description |
|---|---|---|
| `blobemu.image.repository` | `blobemu` | Container image repository |
| `blobemu.image.tag` | `latest` | Container image tag |
| `blobemu.service.port` | `10000` | Service and container port |
| `blobemu.rabbitmq.exchange` | `blob-events` | RabbitMQ exchange name |
| `blobemu.rabbitmq.routingKey` | `blob.created` | RabbitMQ routing key |
| `blobemu.rabbitmq.queue` | `blob-events` | RabbitMQ queue name |
| `blobemu.persistence.size` | `5Gi` | PVC size |
| `blobemu.persistence.storageClass` | `""` | Storage class (empty = default) |
| `blobemu.resources` | 50m–250m CPU, 64Mi–256Mi mem | Resource requests/limits |

### RabbitMQ (`rabbitmq`)

| Key | Default | Description |
|---|---|---|
| `rabbitmq.image.repository` | `rabbitmq` | Container image repository |
| `rabbitmq.image.tag` | `3-management-alpine` | Container image tag |
| `rabbitmq.service.amqpPort` | `5672` | AMQP port |
| `rabbitmq.service.managementPort` | `15672` | Management UI port |
| `rabbitmq.auth.username` | `guest` | Default username |
| `rabbitmq.auth.password` | `guest` | Default password |
| `rabbitmq.persistence.size` | `1Gi` | PVC size |
| `rabbitmq.persistence.storageClass` | `""` | Storage class (empty = default) |
| `rabbitmq.resources` | 100m–500m CPU, 256Mi–512Mi mem | Resource requests/limits |

### OpenTelemetry Collector (`otelCollector`)

| Key | Default | Description |
|---|---|---|
| `otelCollector.enabled` | `true` | Deploy the OTel/Grafana stack |
| `otelCollector.image.repository` | `grafana/otel-lgtm` | Container image repository |
| `otelCollector.image.tag` | `latest` | Container image tag |
| `otelCollector.service.grafanaPort` | `3000` | Grafana UI port |
| `otelCollector.service.otlpGrpcPort` | `4317` | OTLP gRPC receiver port |
| `otelCollector.service.otlpHttpPort` | `4318` | OTLP HTTP receiver port |
| `otelCollector.auth.username` | `admin` | Grafana admin username |
| `otelCollector.auth.password` | `admin` | Grafana admin password |
| `otelCollector.persistence.size` | `2Gi` | PVC size |
| `otelCollector.persistence.storageClass` | `""` | Storage class (empty = default) |
| `otelCollector.resources` | 100m–500m CPU, 256Mi–1Gi mem | Resource requests/limits |

### Dapr Component (`daprComponent`)

| Key | Default | Description |
|---|---|---|
| `daprComponent.enabled` | `true` | Deploy the Dapr Component CRD |
| `daprComponent.queueName` | `blob-events` | RabbitMQ queue name |
| `daprComponent.durable` | `"true"` | Queue durability |
| `daprComponent.exclusive` | `"false"` | Queue exclusivity |
| `daprComponent.direction` | `input` | Binding direction |

## Prerequisites

1. **Kubernetes cluster** (1.26+)
2. **Helm** (3.x)
3. **Dapr operator** — required for sidecar injection and Component CRD support. Install via:
   ```bash
   helm repo add dapr https://dapr.github.io/helm-charts/
   helm install dapr dapr/dapr --namespace dapr-system --create-namespace
   ```
4. **Container images** — build and push `photo-api`, `resize-api`, and `blobemu` to a registry, then set the `image.repository` values accordingly.
5. **StorageClass** — a default StorageClass must exist for dynamic PVC provisioning (included in most managed Kubernetes providers).

## Usage

### Install

```bash
helm install photo-api helm/photo-api \
  --namespace photo \
  --create-namespace
```

### Install with Custom Images

```bash
helm install photo-api helm/photo-api \
  --namespace photo \
  --create-namespace \
  --set photoApi.image.repository=myregistry.io/photo-api \
  --set photoApi.image.tag=v1.2.0 \
  --set resizeApi.image.repository=myregistry.io/resize-api \
  --set resizeApi.image.tag=v1.2.0 \
  --set blobemu.image.repository=myregistry.io/blobemu \
  --set blobemu.image.tag=v1.2.0
```

### Install with a Values Override File

```bash
# production-values.yaml
photoApi:
  replicaCount: 3
  image:
    repository: myregistry.io/photo-api
    tag: v1.2.0

rabbitmq:
  auth:
    username: prod-user
    password: s3cret-p@ss

otelCollector:
  auth:
    username: admin
    password: pr0d-gr@fana
```

```bash
helm install photo-api helm/photo-api \
  --namespace photo \
  --create-namespace \
  -f production-values.yaml
```

### Upgrade

```bash
helm upgrade photo-api helm/photo-api \
  --namespace photo \
  --set photoApi.image.tag=v1.3.0
```

### Uninstall

```bash
helm uninstall photo-api --namespace photo
```

> **Note:** PersistentVolumeClaims created by StatefulSets are not deleted on uninstall. Delete them manually if needed:
> ```bash
> kubectl -n photo delete pvc -l app.kubernetes.io/part-of=photo-api
> ```

### Template Preview (Dry Run)

```bash
# Render templates locally without deploying
helm template my-release helm/photo-api --namespace photo

# Server-side dry run with validation
helm install photo-api helm/photo-api \
  --namespace photo \
  --create-namespace \
  --dry-run
```

### Lint

```bash
helm lint helm/photo-api
```

## Accessing Services

After installation, follow the instructions printed by `NOTES.txt`:

```bash
# Photo API (LoadBalancer — wait for external IP)
kubectl -n photo get svc photo-api -w
export PHOTO_API_IP=$(kubectl -n photo get svc photo-api -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
curl http://$PHOTO_API_IP:8080/healthz

# Grafana (port-forward)
kubectl -n photo port-forward svc/otel-collector 3000:3000

# RabbitMQ Management (port-forward)
kubectl -n photo port-forward svc/rabbitmq 15672:15672
```

## Common Overrides

| Scenario | Override |
|---|---|
| Use ClusterIP instead of LoadBalancer | `--set photoApi.service.type=ClusterIP` |
| Disable observability stack | `--set otelCollector.enabled=false` |
| Disable Dapr component | `--set daprComponent.enabled=false` |
| Disable Dapr sidecar on resize-api | `--set resizeApi.dapr.enabled=false` |
| Use a specific storage class | `--set rabbitmq.persistence.storageClass=managed-premium` |
| Scale photo-api | `--set photoApi.replicaCount=5` |
| Increase blob storage | `--set blobemu.persistence.size=20Gi` |
