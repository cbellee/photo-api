# Go Best Practices — Refactoring Summary

A review of the entire Go project against [Effective Go](https://go.dev/doc/effective_go)
identified 18 findings across Critical, High, Medium, and Low priorities.
The face package was excluded from all changes.

---

## Completed Changes

### 1. Inline Azure SDK operations into `azure.go` (Critical)

**Problem:** `azure.go` delegated every blob SDK call (`GetBlobTags`, `SetBlobTags`,
`GetBlobMetadata`, `GetBlobTagList`, `GetBlob`) to one-liner wrappers in `utils.go`,
creating an unnecessary indirection layer.

**Fix:** All five methods now contain the full Azure SDK logic inline. The `utils`
import was removed from `azure.go` entirely — it has zero references to the utils
package.

**Files:** `api/internal/storage/azure.go`

---

### 2. Fix log-and-return anti-pattern — exif & helpers (High)

**Problem:** Functions logged with `slog.Error` and then returned the same error,
producing duplicate log lines (once at the call site, once by the caller's logging).

**Fix:**
- `exif.go` — replaced `slog.Error` + bare `return` with `fmt.Errorf("…: %w", err)`
  wrapping, removed the `slog` import entirely.
- `helpers.go` — removed `slog.Error` calls on width/height parse errors (values
  default to zero on failure, which is the desired behaviour). Removed `slog` import.

**Files:** `api/internal/exif/exif.go`, `api/internal/handler/helpers.go`

---

### 3. Consolidate telemetry bootstrap — `SetupLogger` (High)

**Problem:** Both `cmd/photo/main.go` and `cmd/resize/main.go` duplicated ~12 lines
of `slog.Logger` construction (JSON stdout handler + optional OTel log bridge via
`FanoutHandler`).

**Fix:** Created `telemetry.SetupLogger(serviceName, providers)` which builds the
fan-out logger in one call and sets it as the `slog` default.

**Files:** `api/internal/telemetry/logger.go` *(new)*

---

### 4. Consolidate blob store creation — `NewBlobStore` factory (High)

**Problem:** Both main packages duplicated ~15 lines of blob store creation logic
(check `BLOB_EMULATOR_URL`, detect production via `CONTAINER_APP_NAME`, create
the appropriate `BlobStore` implementation).

**Fix:** Created `storage.NewBlobStore(storageUrl, azureClientID)` factory.

**Files:** `api/internal/storage/factory.go` *(new)*

---

### 5. Move tracer to dedicated file (Medium)

**Problem:** `var tracer = otel.Tracer("photo-api")` was declared in `collection.go`
but used across the entire handler package.

**Fix:** Moved to its own `tracer.go` file with a doc comment. Removed the `var`
declaration and `go.opentelemetry.io/otel` import from `collection.go`.

**Files:** `api/internal/handler/tracer.go` *(new)*, `api/internal/handler/collection.go`

---

### 6. Named response types — `mutationResponse` (Medium)

**Problem:** Mutation endpoints (rename, soft-delete, restore) used
`map[string]interface{}` for JSON response bodies — no compile-time safety,
easy to misspell keys, unclear which fields exist.

**Fix:** Created `mutationResponse` struct with typed fields:

```go
type mutationResponse struct {
    Message           string   `json:"message"`
    Affected          int      `json:"affected"`
    Errors            []string `json:"errors,omitempty"`
    NewName           string   `json:"newName,omitempty"`
    CollectionDeleted bool     `json:"collectionDeleted,omitempty"`
}
```

Replaced all 12 `map[string]interface{}` occurrences (8 in `softdelete.go`,
4 in `rename.go`).

**Files:** `api/internal/handler/response.go` *(new)*,
`api/internal/handler/softdelete.go`, `api/internal/handler/rename.go`

---

### 7. Remove `joinStrings` wrapper in blobemu (Low)

**Problem:** `blobemu/store.go` wrapped `strings.Join` in a private function
`joinStrings` used at a single call site.

**Fix:** Replaced with `strings.Join(placeholders, ",")` directly and deleted the
wrapper.

**Files:** `blobemu/store.go`

---

## Outstanding (Not Yet Applied)

*All items completed — nothing outstanding.*

---

## Recently Completed

### 8. Delete dead code from `utils.go`

`azure.go` no longer delegates to utils, making ~15 functions dead code.
**Done:** Removed `GetBlobTags`, `SetBlobTags`, `GetBlobMetadata`, `GetBlobTagList`,
`GetBlobStream`, `GetBlobDirectories`, `SaveBlobStreamWithTagsAndMetadata`,
`SaveBlobStreamWithTagsMetadataAndContentType`, `ListBlobHierarchy`,
`GetBlobNameAndPrefix`, `Contains`, `RoundFloat`, `DumpEnv`. Also removed
unused imports (`math`, `slices`, `blob`, `blockblob`, `container`).
File reduced from ~540 to ~233 lines.

---

### 9. Fix log-and-return in `utils.go`

**Done:** `CreateAzureBlobClient` rewritten — removed all `slog.Error` calls
before returns, flattened `else` branches, uses `fmt.Errorf` with `%w` wrapping,
changed from named returns to plain returns. `VerifyToken` — removed two
`slog.Error` calls before error returns.

---

### 10. Simplify `cmd/photo/main.go`

**Done:** Uses `telemetry.SetupLogger("photo-api", providers)` and
`storage.NewBlobStore(storageUrl, azureClientId)`. Removed `models` import,
`_ = models.StorageConfig{}` hack, and direct `otelslog` import.

---

### 11. Simplify `cmd/resize/main.go`

**Done:** Uses `telemetry.SetupLogger` and `storage.NewBlobStore`. Removed
`otelslog` and `os` imports.

---

### 12. Update `utils_test.go`

**Done:** Removed tests for deleted functions (`TestGetBlobDirectories`,
`TestDirectoryPathParsing`, `TestGetBlobNameAndPrefix`, `TestRoundFloat`,
`TestContains`, `TestDumpEnv`, `TestAzureBlobFunctionInputValidation`).
Updated all `models.Event` struct literals to use named `models.EventData`
and `models.StorageDiagnosticsData` types with `ID`/`API`/`URL` field names.
Also updated `cmd/resize/main_test.go`. File reduced from ~1605 to ~590 lines.

---

### 13. Fix `models.Event` naming

**Done:** `Event.Id` → `Event.ID`, `Event.Data.Api` → `Event.Data.API`,
`Event.Data.Url` → `Event.Data.URL`. Extracted anonymous inline structs to
named types `EventData` and `StorageDiagnosticsData`. JSON tags (`json:"id"`,
`json:"api"`, `json:"url"`) added for wire compatibility. Updated all
references in `utils.go`, `cmd/resize/handler.go`, `cmd/face/main.go`,
`blobemu/publisher.go`, `utils_test.go`, and `cmd/resize/main_test.go`.

---

## New Files Created

| File | Purpose |
|------|---------|
| `api/internal/telemetry/logger.go` | `SetupLogger` — consolidated slog setup |
| `api/internal/storage/factory.go` | `NewBlobStore` — blob store factory |
| `api/internal/handler/tracer.go` | Package-level OTel tracer |
| `api/internal/handler/response.go` | `mutationResponse` named struct |
