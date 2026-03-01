# Go Code Improvements

Summary of best-practice improvements applied to the `photo-api` and `blobemu` Go codebases.

## P0 — Critical

### 1. Fix VerifyToken error handling

**Files:** `api/internal/utils/utils.go`

`VerifyToken` previously swallowed JWT parse and keyfunc errors, returning `nil, nil` on failure. This meant callers could not distinguish a failed auth check from a missing token.

**Change:** The function now returns a wrapped error for every failure path (`fmt.Errorf("parsing JWT: %w", err)`, etc.). Callers can rely on `err != nil` to reject unauthenticated requests.

---

### 2. Graceful shutdown — photo-api

**Files:** `api/cmd/photo/main.go`

The HTTP server was started with `log.Fatal(srv.ListenAndServe())`, which called `os.Exit` and skipped all deferred cleanup (OTel flush, DB connections, etc.).

**Change:**
- `signal.NotifyContext` listens for SIGINT/SIGTERM.
- `srv.Shutdown(ctx)` drains in-flight requests (15 s timeout).
- OTel `TracerProvider.Shutdown` is called before exit.
- HTTP server timeouts added: `ReadHeaderTimeout 10s`, `WriteTimeout 60s`, `IdleTimeout 120s`.

---

### 3. Graceful shutdown — blobemu

**Files:** `blobemu/main.go`

Same pattern as photo-api: replaced `log.Fatal` with signal-based graceful shutdown and added HTTP server timeouts.

---

## P1 — High

### 4. Unify FilterBlobsByTags contract

**Files:** `api/internal/storage/azure.go`, `api/internal/storage/local.go`

`AzureBlobStore.FilterBlobsByTags` returned `[]models.Blob{}, nil` on empty results while `LocalBlobStore` returned `nil, nil`. Callers had to check both `len == 0` and `== nil`.

**Change:** Both implementations now return `nil, nil` when the result set is empty.

---

### 5. Cache JWKS keyfunc at startup

**Files:** `api/cmd/photo/main.go`, `api/internal/utils/utils.go`, `api/internal/handler/config.go`, `api/internal/handler/middleware.go`

A new JWKS keyfunc was created on every HTTP request by fetching the remote JWKS endpoint, adding latency and risking rate-limit errors.

**Change:**
- `keyfunc.NewDefaultCtx` is called once at startup; the resulting `Keyfunc` is stored in `handler.Config.JWTKeyfunc`.
- `VerifyToken` accepts an optional `jwtLib.Keyfunc` parameter and falls back to a one-shot fetch only if `nil`.
- The keyfunc's background goroutine is cancelled during shutdown.

---

### 6. HTTP server timeouts

**Files:** `api/cmd/photo/main.go`, `blobemu/main.go`

Neither server set `ReadHeaderTimeout`, `WriteTimeout`, or `IdleTimeout`, leaving them vulnerable to slow-client attacks.

**Change:** Applied as part of the graceful-shutdown work (see items 2 and 3 above).

---

### 7. Move storageUrl to struct initialisation

**Files:** `api/internal/storage/storage.go`, `api/internal/storage/azure.go`, `api/internal/storage/local.go`, `api/internal/storage/mock.go`, `api/internal/handler/{album,collection,photo,update,tags,upload}.go`, `api/cmd/photo/main.go`, `api/cmd/resize/handler.go`, `api/cmd/resize/main.go`, `api/internal/handler/handler_test.go`, `api/cmd/resize/main_test.go`

Every `BlobStore` interface method accepted a `storageUrl string` parameter even though the URL never changes after construction. This cluttered every call site (~30 locations) and made it easy to accidentally pass the wrong URL.

**Change:**
- Removed `storageUrl` from all 7 `BlobStore` interface methods.
- `NewAzureBlobStore(client, storageUrl)` and `NewLocalBlobStore(baseURL, publicURL)` now store the URL in the struct.
- All handler and test call sites simplified.

---

## P2 — Medium

### 8. Fix GetExifJSON memory (buffer copy-by-value)

**Files:** `api/internal/exif/exif.go`, `api/internal/handler/upload.go`, `api/internal/exif/exif_test.go`

`GetExifJSON(image bytes.Buffer)` accepted a `bytes.Buffer` **by value**, copying the entire underlying byte slice on every call. For a 10 MB upload this meant an unnecessary 10 MB allocation.

**Change:** Signature changed to `GetExifJSON(data []byte)`. The caller passes `buf.Bytes()` (a slice header — 24 bytes) instead of the full buffer struct.

---

### 9. Fix ResizeImage double-decode

**Files:** `api/internal/utils/utils.go`

`ResizeImage` called `image.Decode` (full pixel decode) just to read width/height, then decoded the image **again** with the format-specific decoder. For a 10 MP JPEG this roughly doubled memory and CPU usage.

**Change:**
- Dimensions are now read via `image.DecodeConfig` (header-only, no pixel allocation).
- The image is decoded once with the format-specific decoder.
- Unsupported formats now return an explicit error instead of silently producing an empty buffer.
- Fixed incorrect log messages that said "encoding jpeg" for PNG and GIF paths.

---

## P3 — Low

### 10. Compile regex once; replace Contains with slices.Contains

**Files:** `api/internal/utils/utils.go`, `api/internal/storage/local.go`

`StripInvalidTagCharacters` called `regexp.MustCompile` on every invocation. Two hand-rolled `Contains` / `contains` helpers duplicated `slices.Contains` from the standard library.

**Change:**
- Regex moved to package-level `var invalidTagCharsRe = regexp.MustCompile(…)`.
- `utils.Contains` now delegates to `slices.Contains` (marked deprecated).
- `local.contains` removed; call site replaced with `slices.Contains`.

---

### 11. Miscellaneous fixes

| Fix | File | Detail |
|-----|------|--------|
| Typo `StorgeURL` → `StorageURL` | `api/internal/models/models.go` | Struct field rename in `StorageConfig`. |
| Unreachable error check | `api/internal/utils/utils.go` | `GetBlobMetadata` had `if err != nil` before any error-producing call; dead code removed. |
| `DumpEnv` leaks secrets to stdout | `api/internal/utils/utils.go` | Replaced `fmt.Print` with `slog.Debug`; values only appear at debug log level. |
| `fmt.Printf` in `ListBlobHierarchy` | `api/internal/utils/utils.go` | Replaced with `slog.Debug` for consistent structured logging. |

---

## Verification

All changes were validated after each step:

```bash
# api tests (excludes cmd/face — requires C libs not available in CI)
cd api && go test $(go list ./... | grep -v cmd/face) -count=1

# blobemu tests
cd blobemu && go test ./... -count=1
```

All packages pass: `cmd/resize`, `internal/exif`, `internal/handler`, `internal/utils`, `blobemu`.
