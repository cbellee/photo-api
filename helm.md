# Photo API Helm Chart

## Overview

This Helm chart deploys the complete **photo-api** solution to Kubernetes as a single release. It packages five workloads, their services, persistent storage, secrets, and a Dapr component into one parameterised chart.

| Component | Kind | Service Type | Purpose |
|---|---|---|---|
| **photo-api** | Deployment | LoadBalancer | REST API serving the SPA frontend |
| **resize-api** | Deployment | ClusterIP | Dapr event-driven image resize worker |
| **blobemu** | StatefulSet | LoadBalancer | Lightweight Azure Blob Storage emulator (browser-accessible) |
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
    ├── blobemu.yaml                   # StatefulSet + Service (LoadBalancer)
    ├── otel-collector.yaml            # StatefulSet + Service (optional)
    ├── photo-api.yaml                 # Deployment + Service (LoadBalancer)
    ├── resize-api.yaml                # Deployment + Service + Dapr annotations
    └── dapr-component.yaml            # Dapr Component CRD (optional)
```

## Prerequisites

The following steps must be completed **before** installing the chart. Run all commands from the cluster control plane or a machine with `kubectl` access.

### 1. Default StorageClass

StatefulSets require a default StorageClass for dynamic PVC provisioning. Managed Kubernetes providers include one by default. For bare-metal / kubeadm clusters, install `local-path-provisioner`:

```bash
kubectl apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.30/deploy/local-path-storage.yaml

kubectl patch storageclass local-path \
  -p '{"metadata":{"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
```

### 2. Install Dapr (without Scheduler)

Dapr provides sidecar injection for `resize-api` and the `Component` CRD for the RabbitMQ binding. The Dapr **Scheduler** is not needed (no actors or workflow reminders are used) and can cause sidecar health-check failures if its PVC is not available — disable it:

```bash
helm repo add dapr https://dapr.github.io/helm-charts/
helm repo update

helm install dapr dapr/dapr \
  --namespace dapr-system \
  --create-namespace \
  --set dapr_scheduler.enabled=false
```

Verify:

```bash
kubectl -n dapr-system get pods
# Expected: dapr-operator, dapr-sidecar-injector, dapr-sentry, dapr-placement-server — all Running
```

### 3. Create Namespace

```bash
kubectl create namespace photo-api
```

### 4. Create GHCR Image Pull Secret

The container images are hosted on GitHub Container Registry (GHCR). Create a [Personal Access Token](https://github.com/settings/tokens) with the `read:packages` scope and export it as `GHCR_TOKEN`, then create the Kubernetes secret:

```bash
kubectl -n photo-api create secret docker-registry ghcr-pull-secret \
  --docker-server=ghcr.io \
  --docker-username=<GITHUB_USERNAME> \
  --docker-password="$GHCR_TOKEN"
```

### 5. Build and Push Container Images

Run from the repository root (`photo-api/`):

```bash
# Authenticate Docker to GHCR
echo "$GHCR_TOKEN" | docker login ghcr.io -u <GITHUB_USERNAME> --password-stdin

# Build
docker build -t ghcr.io/cbellee/photo-api/photo-api:latest \
  --build-arg SERVICE_NAME=photo --build-arg SERVICE_PORT=8080 .

docker build -t ghcr.io/cbellee/photo-api/resize-api:latest \
  --build-arg SERVICE_NAME=resize --build-arg SERVICE_PORT=8081 .

docker build -t ghcr.io/cbellee/photo-api/blobemu:latest \
  -f blobemu/Dockerfile blobemu/

# Push
docker push ghcr.io/cbellee/photo-api/photo-api:latest
docker push ghcr.io/cbellee/photo-api/resize-api:latest
docker push ghcr.io/cbellee/photo-api/blobemu:latest
```

> **Cross-compilation note:** If building on Apple Silicon (arm64) for amd64 nodes, the Dockerfiles already set `GOARCH=amd64`. No extra flags are needed.

## Installation

### Quick Start

```bash
helm install photo-api helm/photo-api \
  --namespace photo-api \
  --set photoApi.imagePullSecrets[0].name=ghcr-pull-secret \
  --set resizeApi.imagePullSecrets[0].name=ghcr-pull-secret \
  --set blobemu.imagePullSecrets[0].name=ghcr-pull-secret
```

### Set the Blobemu External URL

The `blobemu` service is exposed via LoadBalancer so the SPA (running in the browser) can fetch images directly. After installing, get the external IP:

```bash
kubectl -n photo-api get svc blobemu -w
# NAME      TYPE           CLUSTER-IP     EXTERNAL-IP   PORT(S)
# blobemu   LoadBalancer   10.x.x.x       172.16.0.5    10000:xxxxx/TCP
```

Then upgrade the release with the external URL:

```bash
export BLOBEMU_IP=$(kubectl -n photo-api get svc blobemu -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

helm upgrade photo-api helm/photo-api \
  --namespace photo-api \
  --set blobemu.externalUrl="http://${BLOBEMU_IP}:10000" \
  --set photoApi.imagePullSecrets[0].name=ghcr-pull-secret \
  --set resizeApi.imagePullSecrets[0].name=ghcr-pull-secret \
  --set blobemu.imagePullSecrets[0].name=ghcr-pull-secret
```

This sets `EMULATED_STORAGE_URL` (used in API responses / image paths) to the browser-reachable address, while `BLOB_EMULATOR_URL` (used for backend HTTP calls) remains the cluster-internal FQDN.

### Install with a Values Override File

```yaml
# values-local.yaml
photoApi:
  imagePullSecrets:
    - name: ghcr-pull-secret
resizeApi:
  imagePullSecrets:
    - name: ghcr-pull-secret
blobemu:
  externalUrl: "http://172.16.0.5:10000"
  imagePullSecrets:
    - name: ghcr-pull-secret
rabbitmq:
  auth:
    username: guest
    password: guest
```

```bash
helm install photo-api helm/photo-api \
  --namespace photo-api \
  -f values-local.yaml
```

### Upgrade

```bash
helm upgrade photo-api helm/photo-api \
  --namespace photo-api \
  --set photoApi.image.tag=v1.3.0
```

### Uninstall

```bash
helm uninstall photo-api --namespace photo-api
```

> **Note:** PersistentVolumeClaims created by StatefulSets are not deleted on uninstall. Delete them manually if needed:
> ```bash
> kubectl -n photo-api delete pvc -l app.kubernetes.io/part-of=photo-api
> ```

### Template Preview (Dry Run)

```bash
helm template my-release helm/photo-api --namespace photo-api

helm install photo-api helm/photo-api \
  --namespace photo-api --dry-run
```

### Lint

```bash
helm lint helm/photo-api
```

## Design Choices

### Template Helpers (`_helpers.tpl`)

| Helper | Purpose |
|---|---|
| `photo-api.name` | Chart name, truncated to 63 chars. Overridable via `nameOverride`. |
| `photo-api.fullname` | `<release>-<chart>` qualified name. Overridable via `fullnameOverride`. |
| `photo-api.labels` | Common labels: `helm.sh/chart`, `managed-by`, `part-of`, `version`. |
| `photo-api.selectorLabels` | Per-component selector labels: `app.kubernetes.io/name` + `app.kubernetes.io/instance`. |
| `photo-api.serviceFQDN` | Generates `<svc>.<namespace>.svc.cluster.local` for inter-service references. |

### Stateful vs Stateless Workloads

- **Deployments** — stateless services (`photo-api`, `resize-api`) that scale horizontally.
- **StatefulSets** — data-bearing services (`rabbitmq`, `blobemu`, `otel-collector`) that need stable network identities and persistent volumes.

### Service Types and External Access

- **photo-api** — `LoadBalancer` to expose the REST API externally.
- **blobemu** — `LoadBalancer` so the SPA (running in the user's browser) can fetch images directly. The `blobemu.externalUrl` value controls the URL embedded in API responses. When empty, the internal FQDN is used (only works for server-side consumers).
- **rabbitmq**, **otel-collector**, **resize-api** — `ClusterIP` (internal only).

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

| Value | Default | Effect when `false` |
|---|---|---|
| `otelCollector.enabled` | `true` | Skips the OTel/Grafana StatefulSet and Service |
| `daprComponent.enabled` | `true` | Skips the Dapr `Component` CRD |

### Dapr Integration

The Dapr sidecar is injected into `resize-api` via pod annotations. This requires the Dapr operator (installed in the Prerequisites section above). The Dapr **Scheduler** is disabled because this application does not use actors or workflow reminders, and the scheduler's PVC requirement can cause sidecar health-check failures on bare-metal clusters.

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
| `global.imagePullPolicy` | `Always` | Image pull policy for all containers |

### Photo API (`photoApi`)

| Key | Default | Description |
|---|---|---|
| `photoApi.replicaCount` | `2` | Number of replicas |
| `photoApi.image.repository` | `ghcr.io/cbellee/photo-api/photo-api` | Container image repository |
| `photoApi.image.tag` | `latest` | Container image tag |
| `photoApi.imagePullSecrets` | `[]` | List of image pull secret names (e.g. `[{name: ghcr-pull-secret}]`) |
| `photoApi.service.type` | `LoadBalancer` | Kubernetes Service type |
| `photoApi.service.port` | `8080` | Service and container port |
| `photoApi.env.serviceName` | `photo` | `SERVICE_NAME` env var |
| `photoApi.env.servicePort` | `"8080"` | `SERVICE_PORT` env var |
| `photoApi.resources` | 50m–500m CPU, 64Mi–256Mi mem | Resource requests/limits |

### Resize API (`resizeApi`)

| Key | Default | Description |
|---|---|---|
| `resizeApi.replicaCount` | `1` | Number of replicas |
| `resizeApi.image.repository` | `ghcr.io/cbellee/photo-api/resize-api` | Container image repository |
| `resizeApi.image.tag` | `latest` | Container image tag |
| `resizeApi.imagePullSecrets` | `[]` | List of image pull secret names (e.g. `[{name: ghcr-pull-secret}]`) |
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
| `blobemu.image.repository` | `ghcr.io/cbellee/photo-api/blobemu` | Container image repository |
| `blobemu.image.tag` | `latest` | Container image tag |
| `blobemu.imagePullSecrets` | `[]` | List of image pull secret names (e.g. `[{name: ghcr-pull-secret}]`) |
| `blobemu.externalUrl` | `""` | Browser-reachable URL (e.g. `http://172.16.0.5:10000`). When empty, the cluster-internal FQDN is used. |
| `blobemu.service.type` | `LoadBalancer` | Kubernetes Service type |
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

## Accessing Services

After installation, get the LoadBalancer IPs:

```bash
# Photo API
kubectl -n photo-api get svc photo-api -w
export PHOTO_API_IP=$(kubectl -n photo-api get svc photo-api -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
curl http://$PHOTO_API_IP:8080/healthz

# Blobemu (image serving)
kubectl -n photo-api get svc blobemu -w
export BLOBEMU_IP=$(kubectl -n photo-api get svc blobemu -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
curl http://$BLOBEMU_IP:10000/healthz

# Grafana (port-forward)
kubectl -n photo-api port-forward svc/otel-collector 3000:3000

# RabbitMQ Management (port-forward)
kubectl -n photo-api port-forward svc/rabbitmq 15672:15672
```

## Common Overrides

| Scenario | Override |
|---|---|
| Use ClusterIP for photo-api | `--set photoApi.service.type=ClusterIP` |
| Use ClusterIP for blobemu | `--set blobemu.service.type=ClusterIP` |
| Set blobemu external URL | `--set blobemu.externalUrl=http://172.16.0.5:10000` |
| Disable observability stack | `--set otelCollector.enabled=false` |
| Disable Dapr component | `--set daprComponent.enabled=false` |
| Disable Dapr sidecar on resize-api | `--set resizeApi.dapr.enabled=false` |
| Use a specific storage class | `--set rabbitmq.persistence.storageClass=managed-premium` |
| Scale photo-api | `--set photoApi.replicaCount=5` |
| Increase blob storage | `--set blobemu.persistence.size=20Gi` |

## Troubleshooting

### Dapr sidecar readiness probe returns 500

If the resize-api pod shows `1/2 Ready` and the daprd sidecar logs show `dapr initialized. Status: Running` but the readiness probe fails with 500, the Dapr Scheduler is likely unhealthy. Confirm:

```bash
kubectl -n dapr-system get pods | grep scheduler
```

Fix by reinstalling Dapr without the scheduler:

```bash
helm upgrade dapr dapr/dapr -n dapr-system --set dapr_scheduler.enabled=false
kubectl -n photo-api delete pod -l app.kubernetes.io/name=resize-api
```

### Dapr sidecar x509 certificate error

If daprd logs show `x509: certificate signed by unknown authority`, the Sentry CA was rotated (e.g. after a Dapr reinstall). Restart the control plane and app pods:

```bash
kubectl -n dapr-system rollout restart deploy
kubectl -n photo-api delete pod -l app.kubernetes.io/name=resize-api
```

### PVCs stuck in Pending

Ensure a default StorageClass exists:

```bash
kubectl get storageclass
```

If none is marked `(default)`, install local-path-provisioner as described in the Prerequisites section.

### AppArmor blocks container lifecycle (Ubuntu 24.04)

On Ubuntu 24.04 nodes, the `unprivileged_userns` AppArmor restriction can prevent container operations. Disable AppArmor on affected worker nodes:

```bash
sudo systemctl stop apparmor && sudo systemctl disable apparmor
sudo systemctl restart containerd
```
