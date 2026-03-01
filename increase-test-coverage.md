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
