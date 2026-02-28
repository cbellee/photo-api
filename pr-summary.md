# PR Summary: `claude-refactor`

## What changed

**Major refactor of the photo-api codebase** — restructured the monolithic main.go into a clean layered architecture, added local development infrastructure, Kubernetes manifests, and a Helm chart.

## Architecture & Code Quality

- **Decomposed `photo/main.go`** (681-line reduction) into dedicated packages under `api/internal/`:
  - `handler/` — album, collection, photo, tags, update, upload handlers + middleware
  - `storage/` — `BlobStore` interface with Azure, local (blobemu), and mock implementations
  - `telemetry/` — OpenTelemetry initialisation
  - `models/` — shared data types
- **Added comprehensive tests** — 655-line handler test suite using the mock blob store; updated resize tests
- **Extracted resize-api config** into `config.go` + `handler.go` for separation of concerns

## Blob Emulator (`blobemu/`)

- New lightweight **Azure Blob Storage emulator** backed by SQLite + on-disk files
- REST API matching Azure patterns (`GET/PUT /{container}/{blob}`, tag queries)
- **RabbitMQ publisher** emitting `blob.created` events (replacing Azure Event Grid)
- **Performance fixes**: eliminated N+1 queries in `ListBlobs` and `FilterByTags` using JOINs/bulk fetches; added `PRAGMA busy_timeout` + `SetMaxOpenConns(1)` to fix `SQLITE_BUSY` under concurrency
- HTTP client timeout (30s) added to `LocalBlobStore` to prevent indefinite hangs

## Local Development Stack (`docker-compose.yml`)

- 5-service compose: `photo-api`, `resize-api` + Dapr sidecar, `blobemu`, `rabbitmq`, `otel-collector` (Grafana LGTM)
- Dapr binding component for RabbitMQ queue consumption

## Kubernetes Manifests (`k8s/`)

- Full Kustomize-based deployment: Namespace, Secrets, StatefulSets (rabbitmq, blobemu, otel-collector), Deployments (photo-api, resize-api), Services, Dapr Component CRD
- Readiness/liveness probes on all workloads; persistent volume claims for stateful services

## Helm Chart (`helm/photo-api/`)

- Parameterised chart with `values.yaml` covering all 5 services
- `photo-api` Service defaults to `LoadBalancer`; everything else `ClusterIP`
- Optional toggling of OTel collector and Dapr component
- Template helpers for labels, selectors, and inter-service FQDN generation
- GHCR push instructions included in documentation

## Infrastructure

- Added CORS rules to Azure Storage Account Bicep module
- OTel collector config for traces, metrics, and logs pipelines

## Documentation

- `api/api.md` — API endpoint reference
- `api/otel.md` — OpenTelemetry integration guide
- `api/rabbitmq.md` — RabbitMQ/Dapr event flow
- `blobemu/blobemu.md` — Blob emulator design and API
- `k8s/k8s.md` — Kubernetes manifest details
- `helm/helm.md` — Helm chart structure, values reference, and usage

## Stats

**66 files changed** — +6,786 / −1,868 lines
