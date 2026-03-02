# Kubernetes Deployment Manifests

This directory contains Kubernetes manifests that deploy the **photo-api** solution, translated from the project's `docker-compose.yml`. All resources are deployed into the `photo` namespace and managed via [Kustomize](https://kustomize.io/).

## Architecture Overview

```
┌──────────────┐     HTTP      ┌──────────────┐
│   photo-api  │──────────────▶│   blobemu    │
│  (Deployment)│               │ (StatefulSet) │
│  replicas: 2 │               │  port: 10000  │
│  port: 8080  │               └──────┬────────┘
└──────┬───────┘                      │
       │ OTLP                         │ AMQP publish
       │                              ▼
       │                       ┌──────────────┐
       │                       │  rabbitmq    │
       │                       │ (StatefulSet) │
       │                       │  port: 5672   │
       │                       └──────┬────────┘
       │                              │ Dapr binding
       │                              ▼
       │                       ┌──────────────┐
       │                       │  resize-api  │
       │                       │ (Deployment)  │
       │                       │  port: 8081   │
       │                       └──────┬────────┘
       │                              │ OTLP
       ▼                              ▼
┌─────────────────────────────────────────────┐
│            otel-collector                    │
│           (StatefulSet)                      │
│  Grafana :3000 │ OTLP gRPC :4317 │ HTTP :4318│
└─────────────────────────────────────────────┘
```

## Event Flow

1. A client uploads a photo to **photo-api** (`POST /api/upload`).
2. **photo-api** stores the blob in **blobemu** (`PUT /uploads/{blob}`).
3. **blobemu** publishes a `blob.created` event to **RabbitMQ**.
4. The Dapr sidecar on **resize-api** receives the event via the `queue-uploads` input binding.
5. **resize-api** downloads the original from **blobemu**, resizes it, and writes the result back to the `images` container.

## Manifest Files

### `namespace.yaml`

| Kind | Name |
|---|---|
| `Namespace` | `photo` |

Creates the dedicated namespace for the entire solution. All other resources target this namespace.

---

### `secrets.yaml`

| Kind | Name | Purpose |
|---|---|---|
| `Secret` | `rabbitmq-credentials` | `username` / `password` for the RabbitMQ broker |
| `Secret` | `grafana-credentials` | `username` / `password` for the Grafana admin UI |

Both secrets use `stringData` with placeholder values (`guest`/`admin`). **Replace these before deploying to a non-development cluster.**

---

### `rabbitmq.yaml`

| Kind | Name | Replicas | Ports |
|---|---|---|---|
| `Service` | `rabbitmq` | — | 5672 (AMQP), 15672 (Management UI) |
| `StatefulSet` | `rabbitmq` | 1 | 5672, 15672 |

- **Image:** `rabbitmq:3-management-alpine`
- **Credentials:** sourced from `rabbitmq-credentials` Secret via `secretKeyRef`.
- **Health checks:**
  - Readiness: `rabbitmq-diagnostics -q ping` (initial delay 15 s, period 10 s).
  - Liveness: same command (initial delay 30 s, period 15 s).
- **Storage:** 1 Gi `PersistentVolumeClaim` at `/var/lib/rabbitmq`.
- **Resources:** 100 m–500 m CPU, 256 Mi–512 Mi memory.

**Docker-compose equivalent:** `rabbitmq` service with the `healthcheck` block and `rabbitmq-data` named volume.

---

### `blobemu.yaml`

| Kind | Name | Replicas | Ports |
|---|---|---|---|
| `Service` | `blobemu` | — | 10000 (HTTP) |
| `StatefulSet` | `blobemu` | 1 | 10000 |

- **Image:** `blobemu:latest` — replace with your registry image.
- **Environment variables:**
  - `RABBITMQ_URL` — AMQP connection string using the in-cluster RabbitMQ service FQDN.
  - `RABBITMQ_EXCHANGE` / `RABBITMQ_ROUTING_KEY` / `RABBITMQ_QUEUE` — event routing config.
  - `BLOB_PUBLIC_URL` — the URL other services use to reach blobemu.
- **Health checks:**
  - Readiness: `GET /healthz` (initial delay 5 s, period 10 s).
  - Liveness: `GET /healthz` (initial delay 10 s, period 15 s).
- **Storage:** 5 Gi `PersistentVolumeClaim` at `/data` for blob and SQLite storage.
- **Resources:** 50 m–250 m CPU, 64 Mi–256 Mi memory.

**Docker-compose equivalent:** `blobemu` service with the `./data:/data` bind mount.

---

### `otel-collector.yaml`

| Kind | Name | Replicas | Ports |
|---|---|---|---|
| `Service` | `otel-collector` | — | 3000 (Grafana), 4317 (OTLP gRPC), 4318 (OTLP HTTP) |
| `StatefulSet` | `otel-collector` | 1 | 3000, 4317, 4318 |

- **Image:** `grafana/otel-lgtm:latest` (all-in-one Grafana + Loki + Tempo + Mimir + OTel Collector).
- **Credentials:** sourced from `grafana-credentials` Secret.
- **Storage:** 2 Gi `PersistentVolumeClaim` at `/var/lib/grafana`.
- **Resources:** 100 m–500 m CPU, 256 Mi–1 Gi memory.

**Docker-compose equivalent:** `otel-collector` service with the `grafana-data` named volume.

---

### `photo-api.yaml`

| Kind | Name | Replicas | Ports |
|---|---|---|---|
| `Service` | `photo-api` | — | 8080 (HTTP) |
| `Deployment` | `photo-api` | 2 | 8080 |

- **Image:** `photo-api:latest` — replace with your registry image.
- **Environment variables:**
  - `SERVICE_NAME` / `SERVICE_PORT` — application identity.
  - `OTEL_EXPORTER_OTLP_ENDPOINT` — points to the in-cluster OTel collector.
  - `EMULATED_STORAGE_URL` / `BLOB_EMULATOR_URL` — blobemu service FQDN.
- **Health checks:**
  - Readiness: `GET /readyz` (initial delay 5 s, period 10 s, timeout 6 s). This endpoint performs a lightweight blob-store connectivity check.
  - Liveness: `GET /healthz` (initial delay 5 s, period 15 s). Returns 200 if the process is running.
- **Resources:** 50 m–500 m CPU, 64 Mi–256 Mi memory.
- **Replicas:** 2 for availability (stateless service).

**Docker-compose equivalent:** `photo-api` service. `depends_on` ordering is replaced by Kubernetes readiness gates.

---

### `resize-api.yaml`

| Kind | Name | Replicas | Ports |
|---|---|---|---|
| `Service` | `resize-api` | — | 8081 (HTTP), 3500 (Dapr HTTP), 50001 (Dapr gRPC) |
| `Deployment` | `resize-api` | 1 | 8081 |

- **Image:** `resize-api:latest` — replace with your registry image.
- **Dapr sidecar injection** via pod annotations:
  - `dapr.io/enabled: "true"` — the Dapr operator injects the daprd sidecar automatically.
  - `dapr.io/app-id: "resize-api"` — application identity for service invocation.
  - `dapr.io/app-port: "8081"` — port daprd forwards requests to.
  - `dapr.io/app-protocol: "grpc"` — resize-api speaks gRPC.
  - `dapr.io/log-level: "info"`.
- **Environment variables:**
  - `OTEL_EXPORTER_OTLP_ENDPOINT` — OTel collector FQDN.
  - `EMULATED_STORAGE_URL` — blobemu service FQDN.
  - `IMAGES_CONTAINER_NAME` — target container for resized images (`images`).
  - `MAX_IMAGE_HEIGHT` / `MAX_IMAGE_WIDTH` — resize constraints (1200 × 1600).
  - `UPLOADS_QUEUE_BINDING` — Dapr binding component name (`queue-uploads`).
- **Resources:** 100 m–1000 m CPU, 128 Mi–512 Mi memory (higher limits for image processing).

**Docker-compose equivalent:** `resize-api` + `resize-dapr` services. The separate daprd container and `network_mode: "service:resize-dapr"` are replaced by Dapr sidecar injection.

---

### `dapr-component.yaml`

| Kind | Name |
|---|---|
| `Component` (dapr.io/v1alpha1) | `queue-uploads` |

- **Type:** `bindings.rabbitmq` — Dapr input binding.
- **Config:**
  - `host` — AMQP connection to the in-cluster RabbitMQ (`rabbitmq.photo.svc.cluster.local:5672`).
  - `queueName` — `blob-events` (matches what blobemu publishes to).
  - `durable: true`, `exclusive: false`, `direction: input`.

**Docker-compose equivalent:** the file-mounted `dapr/components/queue-uploads.yaml`. In Kubernetes with the Dapr operator, components are deployed as CRDs instead of files.

---

### `kustomization.yaml`

Kustomize entry point that applies all resources in dependency order:

1. `namespace.yaml` — creates the namespace first.
2. `secrets.yaml` — credentials must exist before workloads reference them.
3. `rabbitmq.yaml` — broker must be ready before event producers/consumers.
4. `blobemu.yaml` — storage must be ready before the APIs.
5. `otel-collector.yaml` — telemetry backend.
6. `dapr-component.yaml` — Dapr binding for resize-api.
7. `resize-api.yaml` — event consumer.
8. `photo-api.yaml` — API gateway.

A common label `app.kubernetes.io/part-of: photo-api` is applied to all resources.

## Resource Summary

| Resource Kind | Count | Names |
|---|---|---|
| `Namespace` | 1 | `photo` |
| `Secret` | 2 | `rabbitmq-credentials`, `grafana-credentials` |
| `Service` | 5 | `rabbitmq`, `blobemu`, `otel-collector`, `photo-api`, `resize-api` |
| `StatefulSet` | 3 | `rabbitmq`, `blobemu`, `otel-collector` |
| `Deployment` | 2 | `photo-api`, `resize-api` |
| `Component` | 1 | `queue-uploads` |
| **Total** | **14** | |

## Docker-Compose to Kubernetes Mapping

| Docker-Compose Concept | Kubernetes Equivalent |
|---|---|
| `services:` | `Deployment` or `StatefulSet` + `Service` |
| `ports:` | `Service` spec + container `ports` |
| `environment:` | Container `env` (with `secretKeyRef` for credentials) |
| `depends_on:` + `condition:` | Readiness/liveness probes; Kubernetes handles ordering |
| `volumes:` (named) | `PersistentVolumeClaim` via `volumeClaimTemplates` |
| `volumes:` (bind mount) | `PersistentVolumeClaim` (or `hostPath` for dev) |
| `network_mode: "service:X"` | Dapr sidecar injection (pod-level network sharing) |
| `healthcheck:` | `readinessProbe` / `livenessProbe` |
| daprd container | Dapr operator sidecar injection via annotations |
| `dapr/components/*.yaml` files | Dapr `Component` CRD |

## Prerequisites

1. **Container images** — build and push the three application images to a container registry, then update the `image:` fields in `blobemu.yaml`, `photo-api.yaml`, and `resize-api.yaml`.
2. **Dapr operator** — install in the cluster for automatic sidecar injection and component CRD support. See [Dapr on Kubernetes](https://docs.dapr.io/operations/hosting/kubernetes/kubernetes-deploy/).
3. **Secrets** — update `secrets.yaml` with production credentials before deploying outside of development.
4. **StorageClass** — ensure a default `StorageClass` exists in the cluster for dynamic PVC provisioning (most managed Kubernetes providers include one).

## Deploying

```bash
# Preview the rendered manifests
kubectl kustomize k8s/

# Apply to the cluster
kubectl apply -k k8s/

# Verify all pods are running
kubectl -n photo get pods

# Check readiness
kubectl -n photo get endpoints
```

## Accessing Services

After deployment, services are only accessible within the cluster via `ClusterIP`. To expose them externally:

```bash
# Port-forward the photo-api for local development
kubectl -n photo port-forward svc/photo-api 8080:8080

# Port-forward Grafana for observability
kubectl -n photo port-forward svc/otel-collector 3000:3000

# Port-forward RabbitMQ Management UI
kubectl -n photo port-forward svc/rabbitmq 15672:15672
```

For production, add an `Ingress` or `Gateway` resource to expose `photo-api` on port 8080.
