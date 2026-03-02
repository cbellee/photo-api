# Test Coverage Improvements

## Summary

Overall test coverage increased from **28.0% → 55.9%** (nearly doubled).

## Per-Package Coverage

| Package | Before | After | Delta |
|---|---|---|---|
| `internal/handler` | 58.9% | 92.9% | **+34.0** |
| `internal/storage` | 0.0% | 71.3% | **+71.3** |
| `cmd/resize` | 26.0% | 44.2% | **+18.2** |
| `internal/utils` | 29.5% | 32.3% | **+2.8** |
| `internal/exif` | 40.0% | 40.0% | — |

## Per-Function Highlights

| Function | Before | After |
|---|---|---|
| `RequireRole` | 0% | **100%** |
| `UploadHandler` | 0% | **88.1%** |
| `Resize` | 43.2% | **86.4%** |
| `AlbumHandler` | 66.7% | **93.8%** |
| `CollectionHandler` | 83.3% | **88.1%** |
| `PhotoHandler` | 100% | **100%** |
| `TagListHandler` | 100% | **100%** |
| `UpdateHandler` | 76.9% | **92.3%** |
| `BlobsToPhotos` | 100% | **100%** |
| `NewLocalBlobStore` | 0% | **100%** |
| `LocalBlobStore.FilterBlobsByTags` | 0% | **85.7%** |
| `LocalBlobStore.GetBlobTags` | 0% | **78.6%** |
| `LocalBlobStore.SetBlobTags` | 0% | **85.7%** |
| `LocalBlobStore.GetBlobMetadata` | 0% | **78.6%** |
| `LocalBlobStore.GetBlobTagList` | 0% | **85.0%** |
| `LocalBlobStore.GetBlob` | 0% | **87.5%** |
| `LocalBlobStore.SaveBlob` | 0% | **90.5%** |
| `parseBlobRef` | 85.7% | **85.7%** |
| `ResizeImage` | 77.4% | **~82%** |

## New Test Files

### `api/internal/storage/mock_test.go` (~190 lines)

15 tests covering the `MockBlobStore` test double:

- **Default behaviour** — all 7 methods return the expected zero-value or error when no `Func` is configured
- **Custom func delegates** — all 7 `Func` fields correctly route to user-provided implementations
- **Thread safety** — 50 concurrent goroutines calling `FilterBlobsByTags` with correct call-tracking
- **Compile-time interface check** — `var _ BlobStore = (*MockBlobStore)(nil)`

### `api/internal/storage/local_test.go` (~290 lines)

15+ tests for `LocalBlobStore` using `httptest.Server`:

- `NewLocalBlobStore` — trailing-slash trimming, field initialisation
- `blobURL` — simple, nested, and space-encoded paths
- `FilterBlobsByTags` — success (verifies `publicURL` swap in `Path`), empty result, server error
- `GetBlobTags` — success, 404 error
- `SetBlobTags` — success with request-body verification, 403 error
- `GetBlobMetadata` — success, server error
- `GetBlobTagList` — success with album deduplication, error
- `GetBlob` — success, 404 error
- `SaveBlob` — success with header verification (tags, metadata, content-type), error, missing-content-type
- Context cancellation

### `api/internal/handler/upload_test.go` (~530 lines)

18 tests split across three areas:

**UploadHandler (8 tests)**
| Test | Status Code |
|---|---|
| Successful upload with valid JPEG | 201 |
| Nil request body | 400 |
| Missing metadata field | 400 |
| Invalid metadata JSON | 400 |
| Missing photo file | 400 |
| Invalid image data | 400 |
| `SaveBlob` storage failure | 500 |
| Tag stripping (special chars removed) | 201 |

**RequireRole middleware (5 tests)**
| Test | Status Code |
|---|---|
| No `Authorization` header | 401 |
| Invalid/malformed token | 401 |
| Valid token, wrong role | 403 |
| Valid token, correct role | 200 |
| Expired token | 401 |

Uses HMAC-signed JWTs with a test `Keyfunc` — no external JWKS fetch needed.

**Handler edge cases (5 tests)**
| Test | Handler | Condition |
|---|---|---|
| `AlbumHandler` storage error | Album | `FilterBlobsByTags` fails → 404 |
| `AlbumHandler` auto-assigns album image | Album | Missing `albumImage` tag gets set |
| `PhotoHandler` storage error | Photo | `FilterBlobsByTags` fails → 500 |
| `CollectionHandler` empty tag list | Collection | No collections → 404 |
| `UpdateHandler` GetBlobTags error | Update | Storage failure → 500 |

## Modified Test Files

### `api/cmd/resize/main_test.go`

Added happy-path and targeted error-path tests with a fully configured `MockBlobStore`:

| Test | Description |
|---|---|
| `TestResizeHandler_HappyPath` | Full end-to-end: GetBlob → GetBlobTags → GetBlobMetadata → ResizeImage → SaveBlob with a real 2000×1500 JPEG |
| `TestResizeHandler_HappyPath_SmallImage` | Image already within max dimensions still processed |
| `TestResizeHandler_GetBlobError` | `GetBlob` fails → error contains "downloading blob" |
| `TestResizeHandler_GetBlobTagsError` | `GetBlobTags` fails → error contains "getting blob tags" |
| `TestResizeHandler_SaveBlobError` | `SaveBlob` fails → error contains "saving resized blob" |

### `api/internal/utils/utils_test.go`

Added edge-case subtests to existing test functions:

| Test | Description |
|---|---|
| `ResizeImage/unsupported format` | `image/webp` → error "unsupported image format" |
| `ResizeImage/invalid image bytes` | Random bytes → error "decode image config" |
| `ResizeImage/empty image bytes` | Empty slice → error |
| `ResizeImage/square image` | 200×200 → 100×100 (landscape branch) |
| `StripInvalidTagValue/empty string` | `""` → `""` |

---

## Phase 3 — Targeted Gap Coverage

### Summary

Overall test coverage increased from **55.9% → 59.5%** (+3.6pp). Targeted Tier 1 (high-impact, easy) and Tier 2 (medium-impact) gaps identified by per-function coverage profiling.

### Per-Package Coverage

| Package | Before | After | Delta |
|---|---|---|---|
| `internal/exif` | 40.0% | 80.0% | **+40.0** |
| `internal/handler` | 92.9% | 93.9% | **+1.0** |
| `internal/storage` | 71.3% | 73.2% | **+1.9** |
| `internal/telemetry` | 0.0% | 13.0% | **+13.0** |
| `internal/utils` | 32.3% | 39.8% | **+7.5** |
| `cmd/resize` | 44.2% | 46.2% | **+2.0** |

### Per-Function Highlights

| Function | Before | After | Change |
|---|---|---|---|
| `GetExifJSON` | 40.0% | **80.0%** | +40pp |
| `extractToken` | 0.0% | **100.0%** | +100pp |
| `VerifyToken` | 0.0% | **58.3%** | +58.3pp |
| `GetCollectionImage` | 87.5% | **100.0%** | +12.5pp |
| `Resize` | 86.4% | **90.9%** | +4.5pp |
| `Shutdown` (telemetry) | 0.0% | **50.0%** | +50pp |
| `FilterBlobsByTags` (local) | 85.7% | **90.5%** | +4.8pp |
| `GetBlobTags` (local) | 78.6% | **85.7%** | +7.1pp |
| `GetBlobMetadata` (local) | 78.6% | **85.7%** | +7.1pp |
| `GetBlobTagList` (local) | 85.0% | **90.0%** | +5pp |
| `UploadHandler` | 88.1% | **91.0%** | +2.9pp |

### New Test Files

#### `api/internal/utils/token_test.go` (~110 lines)

8 tests for `extractToken` and `VerifyToken`:

| Test | Description |
|---|---|
| `TestExtractToken_NoAuthHeader` | Missing `Authorization` header → error |
| `TestExtractToken_ValidBearerToken` | `"Bearer my-token-value"` → returns `"my-token-value"` |
| `TestExtractToken_EmptyAuthHeader` | Empty header string → error |
| `TestVerifyToken_NoAuthHeader` | No header → error contains "no access token" |
| `TestVerifyToken_InvalidToken` | Malformed JWT string → error contains "parsing JWT" |
| `TestVerifyToken_ExpiredToken` | Expired HMAC JWT → error contains "parsing JWT" |
| `TestVerifyToken_ValidToken` | Valid HMAC JWT with `["photo.upload", "admin"]` → returns `MyClaims` |
| `TestVerifyToken_ValidTokenSingleRole` | Valid HMAC JWT with `["reader"]` → returns `MyClaims` |

Uses HMAC-signed JWTs with a test `Keyfunc` — no external JWKS fetch needed.

#### `api/internal/telemetry/telemetry_test.go` (~30 lines)

2 tests for nil-safe `Shutdown`:

| Test | Description |
|---|---|
| `TestProviders_Shutdown_NilProviders` | All three providers nil — `Shutdown` must not panic |
| `TestProviders_Shutdown_PartialNilProviders` | Explicit nil fields — `Shutdown` must not panic |

### Tests Added to Existing Files

#### `api/internal/exif/exif_test.go`

| Test | Description |
|---|---|
| `TestGetExifJSON_SuccessPath` | Hand-crafted minimal JPEG with EXIF APP1 segment (Make="Test") → valid JSON containing `"Make"` |

Includes `buildJPEGWithExif` helper that constructs a valid JPEG+EXIF byte sequence programmatically (SOI + APP1 with TIFF IFD0 entry + EOI).

#### `api/internal/handler/handler_test.go`

| Test | Description |
|---|---|
| `TestGetCollectionImage_EmptyResults` | `FilterBlobsByTags` returns empty `[]models.Blob{}` (not error) → error "no collection image found" |

#### `api/internal/handler/upload_test.go`

| Test | Description |
|---|---|
| `TestUploadHandler_ParseMultipartFormError_Returns400` | Body with wrong `Content-Type` (`text/plain`) triggers `ParseMultipartForm` error → 400 |

#### `api/cmd/resize/main_test.go`

| Test | Description |
|---|---|
| `TestResizeHandler_GetBlobMetadataError` | `GetBlobMetadata` returns error → error contains "getting blob metadata" |

#### `api/internal/storage/local_test.go`

4 malformed JSON response tests using `httptest.Server`:

| Test | Description |
|---|---|
| `TestLocalBlobStore_FilterBlobsByTags_MalformedJSON` | Invalid JSON body → error contains "decoding response" |
| `TestLocalBlobStore_GetBlobTags_MalformedJSON` | Invalid JSON → JSON decode error |
| `TestLocalBlobStore_GetBlobMetadata_MalformedJSON` | Invalid JSON → JSON decode error |
| `TestLocalBlobStore_GetBlobTagList_MalformedJSON` | Invalid JSON → JSON decode error |

### Remaining Gaps (Tier 3 — Low ROI)

These functions remain at 0% and require infrastructure that cannot be mocked easily:

- **Azure SDK wrappers** in `utils.go`: `GetBlobDirectories`, `GetBlobTags`, `SetBlobTags`, `GetBlobMetadata`, `GetBlobTagList`, `GetBlobStream`, `SaveBlobStreamWithTagsAndMetadata`, `SaveBlobStreamWithTagsMetadataAndContentType`, `ListBlobHierarchy` — all require a real Azure Blob Storage client
- **`telemetry.Init`** — requires a gRPC OTLP endpoint
- **`cmd/photo/main.go`** — application entry point

Realistic ceiling without an Azure emulator or gRPC test server: **~65-70%**
