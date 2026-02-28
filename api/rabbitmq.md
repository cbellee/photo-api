# RabbitMQ Integration for Local / Kubernetes Development

## Overview

In production (Azure), uploaded images trigger a resize pipeline via:

```
Blob Storage → Event Grid → Storage Queue → Dapr binding → Resize Service
```

For local Docker and Kubernetes development, Azure Event Grid is unavailable. RabbitMQ replaces the Storage Queue as the message broker, while Dapr continues to abstract the transport — meaning **zero code changes** to the resize service.

```
Blob Emulator → RabbitMQ → Dapr (bindings.rabbitmq) → Resize Service
```

---

## Architecture Comparison

| Concern              | Azure (Production)                          | Local / Kubernetes                              |
|----------------------|---------------------------------------------|--------------------------------------------------|
| Blob storage         | Azure Blob Storage                          | Blob emulator (`blobemu`)                        |
| Event source         | Event Grid system topic on storage account  | Blob emulator publishes directly to RabbitMQ     |
| Message broker       | Azure Storage Queue                         | RabbitMQ                                         |
| Dapr component type  | `bindings.azure.storagequeues`              | `bindings.rabbitmq`                              |
| Dapr component name  | `queue-uploads`                             | `queue-uploads` (identical)                      |
| Resize service code  | Unchanged                                   | Unchanged                                        |

The key design principle is that Dapr's component model lets us swap the underlying transport (Storage Queue → RabbitMQ) purely through configuration, without touching application code.

---

## Changed / New Files

### 1. `blobemu/publisher.go` (new)

RabbitMQ publisher that fires Event-Grid-compatible `BlobCreated` events whenever a blob is uploaded to the emulator.

**Key details:**

- Connects to RabbitMQ on startup and declares a durable **topic exchange** (`blob-events`) with a bound **queue** (`blob-events`).
- On each blob upload, constructs a `BlobEvent` struct that mirrors the [Azure Event Grid schema](https://learn.microsoft.com/en-us/azure/event-grid/event-schema-blob-storage) with fields like `eventType: "Microsoft.Storage.BlobCreated"`, `subject`, `data.url`, `data.contentType`, etc.
- **Base64-encodes** the JSON payload before publishing. This is critical because Azure Storage Queues base64-encode message bodies, and the resize service's `ConvertToEvent()` function expects to base64-decode the Dapr `BindingEvent.Data` field. By encoding here, we preserve full compatibility.
- Publishing is **non-fatal** — if RabbitMQ is unreachable, the blob is still saved and an error is logged.
- If `RABBITMQ_URL` is not set, the publisher is `nil` and all publish calls are no-ops.

**Types defined:**

```go
type BlobEvent struct { ... }     // mirrors Event Grid event envelope
type BlobEventData struct { ... } // mirrors Event Grid blob event data
type Publisher struct { ... }     // holds AMQP connection, channel, config
type PublisherConfig struct { ... } // AMQP URL, exchange, queue, routing key, base URL
```

### 2. `blobemu/main.go` (modified)

- On startup, reads RabbitMQ configuration from environment variables and initialises a `*Publisher` (or `nil` if `RABBITMQ_URL` is unset).
- `blobPutHandler()` signature changed from `blobPutHandler(store *Store)` to `blobPutHandler(store *Store, pub *Publisher)`.
- After successfully saving a blob, calls `pub.PublishBlobCreated(container, blob, ct, len(data))`.

**New environment variables:**

| Variable               | Default              | Description                                |
|------------------------|----------------------|--------------------------------------------|
| `RABBITMQ_URL`         | _(empty — disabled)_ | AMQP connection string                     |
| `RABBITMQ_EXCHANGE`    | `blob-events`        | RabbitMQ exchange name                     |
| `RABBITMQ_ROUTING_KEY` | `blob.created`       | Routing key for published messages         |
| `RABBITMQ_QUEUE`       | `blob-events`        | Queue to declare and bind                  |
| `BLOB_PUBLIC_URL`      | `http://blobemu:10000` | URL prefix used in event `data.url` field |

### 3. `blobemu/go.mod` (modified)

Added dependency:

```
github.com/rabbitmq/amqp091-go v1.10.0
```

### 4. `dapr/components/queue-uploads.yaml` (new)

Dapr input binding component that configures the Dapr sidecar to consume from the RabbitMQ queue and deliver messages to the resize service.

```yaml
apiVersion: dapr.io/v1alpha1
kind: Component
metadata:
  name: queue-uploads          # matches UPLOADS_QUEUE_BINDING env var
spec:
  type: bindings.rabbitmq      # swaps bindings.azure.storagequeues
  version: v1
  metadata:
    - name: host
      value: "amqp://guest:guest@rabbitmq:5672/"
    - name: queueName
      value: "blob-events"
    - name: durable
      value: "true"
    - name: exclusive
      value: "false"
    - name: direction
      value: "input"
```

The component name `queue-uploads` is intentionally identical to the production Azure component name, so the resize service's `UPLOADS_QUEUE_BINDING` value doesn't need to change between environments.

### 5. `docker-compose.yml` (modified)

New services added:

- **`rabbitmq`** — `rabbitmq:3-management-alpine` with management UI on port 15672. Includes a health check (`rabbitmq-diagnostics ping`) so dependent services wait until RabbitMQ is ready.
- **`resize-service`** — Builds the resize Go binary. Uses `network_mode: "service:resize-dapr"` to share the Dapr sidecar's network namespace (required so Dapr can reach the app on `localhost:8081`).
- **`resize-dapr`** — `daprio/daprd:latest` sidecar configured with `--app-port 8081`, `--app-protocol grpc`, and `--resources-path /components` pointing to the mounted `dapr/components/` directory.

Updated services:

- **`blobemu`** — Now receives RabbitMQ environment variables and `depends_on: rabbitmq` with a health check condition.

New volumes:

- `rabbitmq-data` — Persists RabbitMQ state across restarts.

### 6. `Dockerfile` (modified)

Build command changed from:

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./${SERVICE_NAME}/main.go
```

to:

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./${SERVICE_NAME}/
```

This builds the full Go package (all `.go` files in the directory) instead of just `main.go`, which is required because the resize service has multiple source files (`main.go`, `handler.go`, `config.go`).

---

## Resize Service — No Changes Required

The resize service (`api/cmd/resize/`) is **completely unchanged**. It continues to:

1. Register a Dapr binding invocation handler for the `UPLOADS_QUEUE_BINDING` component name.
2. Receive `common.BindingEvent` messages from Dapr.
3. Base64-decode and JSON-unmarshal the event data via `utils.ConvertToEvent()`.
4. Process the blob (download, resize, save).

Dapr handles the transport swap transparently. The same handler code works whether the underlying binding is `bindings.azure.storagequeues` or `bindings.rabbitmq`.

---

## Message Flow (Local)

```
┌─────────────┐     PUT /{container}/{blob}     ┌─────────────┐
│  Photo API  │ ──────────────────────────────▶  │  Blob Emu   │
│  or Client  │                                  │  :10000      │
└─────────────┘                                  └──────┬──────┘
                                                        │
                                              PublishBlobCreated()
                                              (base64-encoded JSON)
                                                        │
                                                        ▼
                                                 ┌─────────────┐
                                                 │  RabbitMQ    │
                                                 │  :5672       │
                                                 │              │
                                                 │  exchange:   │
                                                 │  blob-events │
                                                 │  queue:      │
                                                 │  blob-events │
                                                 └──────┬──────┘
                                                        │
                                                  Dapr consumes
                                                  (bindings.rabbitmq)
                                                        │
                                                        ▼
                                                 ┌─────────────┐
                                                 │ Dapr Sidecar │
                                                 │ :50001 gRPC  │
                                                 └──────┬──────┘
                                                        │
                                              BindingEvent → handler
                                                        │
                                                        ▼
                                                 ┌─────────────┐
                                                 │   Resize    │
                                                 │   Service   │
                                                 │   :8081     │
                                                 └─────────────┘
```

---

## Running Locally

```bash
docker compose up --build
```

This starts all services. Upload a blob to the emulator and observe:

1. Blob emulator logs: `published BlobCreated event`
2. Dapr sidecar logs: message received from RabbitMQ binding
3. Resize service logs: `processing blob`, `resized image`

The RabbitMQ management UI is available at [http://localhost:15672](http://localhost:15672) (guest/guest).

---

## Kubernetes Deployment

For Kubernetes, the same Dapr component YAML (`dapr/components/queue-uploads.yaml`) can be applied as a Kubernetes resource. Update the `host` metadata value to point to your in-cluster RabbitMQ service:

```yaml
- name: host
  value: "amqp://guest:guest@rabbitmq.default.svc.cluster.local:5672/"
```

Apply it alongside the resize service deployment. Dapr's sidecar injector will automatically attach the sidecar and load the component.
