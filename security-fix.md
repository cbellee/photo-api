# Security Fixes

## HIGH Severity

### 1. SQL Injection via Path Parameters
- **Files:** `api/internal/handler/sanitize.go` (new), `album.go`, `photo.go`, `update.go`
- **Issue:** User-supplied path values (`collection`, `album`) were interpolated directly into `fmt.Sprintf` tag-filter queries, allowing single-quote injection to manipulate the query.
- **Fix:** Added `validatePathParam()` with a regex whitelist (`[a-zA-Z0-9 \-_\.+]`), 256-char length limit, and non-empty check. Applied to all handlers that build tag-filter queries.

### 2. Path Traversal in Blobemu
- **File:** `blobemu/main.go`
- **Issue:** Blob names from the URL path were passed directly to filesystem operations (`os.ReadFile`, `os.WriteFile`) with no sanitization, allowing `../../etc/passwd`-style reads/writes.
- **Fix:** Added `validateBlobPath()` that rejects empty paths, `..` segments, and absolute paths. Applied to `listHandler`, `blobGetHandler`, and `blobPutHandler`.

### 3. No Body Size Limit on Blobemu Uploads
- **File:** `blobemu/main.go`
- **Issue:** `io.ReadAll(r.Body)` on blob PUT had no size limit, enabling denial-of-service via arbitrarily large uploads.
- **Fix:** Wrapped `r.Body` with `http.MaxBytesReader`. Upload limit is configurable via `MAX_BODY_SIZE_MB` env var (default: 100 MB). Query endpoint limited to 64 KB.

## MEDIUM Severity

### 4. Unrestricted Upload Content Types
- **Files:** `api/internal/handler/upload.go`, `blobemu/main.go`
- **Issue:** Any content type was accepted for uploads, potentially allowing executable or malicious file types.
- **Fix:** Added an `allowedImageTypes` whitelist (`image/jpeg`, `image/png`, `image/gif`, `image/webp`) enforced in both the photo-api `UploadHandler` and the blobemu `blobPutHandler`.

### 5. UpdateHandler Trusts Client-Supplied Blob Name
- **File:** `api/internal/handler/update.go`
- **Issue:** The `name` field from the JSON body was used as the blob path for tag updates, allowing a client to modify tags on any blob.
- **Fix:** Blob name is now derived server-side from the URL path parameters (`{collection}/{album}/{id}`). The body `name` field is overridden with the server-computed value.

### 6. Wildcard CORS on Blobemu
- **File:** `blobemu/main.go`
- **Issue:** `Access-Control-Allow-Origin: *` and `Access-Control-Allow-Headers: *` allowed any origin to make credentialed requests.
- **Fix:** Replaced the wildcard CORS middleware with `corsHandler()` that checks the request `Origin` against a configurable `CORS_ORIGINS` env var (default: `http://localhost:5173,http://localhost:8080`). Only matching origins receive the allow header.

### 7. Environment Variable Logging Exposes Secrets
- **File:** `api/internal/utils/utils.go`
- **Issue:** `DumpEnv()` logged every environment variable (including secrets). `GetEnvValue()` logged both the key and its value at INFO level.
- **Fix:** `DumpEnv()` is now a no-op. `GetEnvValue()` logs only the key name at DEBUG level, never the value.

## LOW Severity

### 8. Fragile Bearer Token Extraction
- **File:** `api/internal/utils/utils.go`
- **Issue:** `strings.Split(header, " ")[1]` would panic with an index-out-of-bounds if the Authorization header contained no space.
- **Fix:** Replaced with `strings.SplitN(header, " ", 2)` plus validation that exactly two parts exist and the first is `Bearer`.

### 9. Readiness Probe Leaks Internal Errors
- **File:** `api/cmd/photo/main.go`
- **Issue:** The `/readyz` endpoint returned the raw `err.Error()` string in the JSON response, potentially exposing internal details (database paths, connection strings).
- **Fix:** The endpoint now returns a generic `{"status":"unavailable"}` message. The error is still logged server-side for debugging.
