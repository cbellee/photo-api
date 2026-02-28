# Photo API — Refactoring Changelog

## Overview

The `cmd/photo` service was refactored from a single 612-line `main.go` (with all handlers, helpers, auth logic, and global state) into a clean package structure with dependency injection, centralised error handling, and comprehensive behavioural tests.

---

## New Packages

### `internal/storage`

Provides a `BlobStore` interface that abstracts Azure Blob Storage operations, enabling dependency injection and testability.

| File | Purpose |
|---|---|
| `storage.go` | `BlobStore` interface — `FilterBlobsByTags`, `GetBlobTags`, `SetBlobTags`, `GetBlobMetadata`, `GetBlobTagList`, `SaveBlob` |
| `azure.go` | `AzureBlobStore` — production implementation wrapping `*azblob.Client` |
| `mock.go` | `MockBlobStore` — test double with injectable `XxxFunc` fields and `XxxCalls` call-tracking slices (thread-safe via `sync.Mutex`) |

### `internal/handler`

All HTTP handler logic extracted from `main.go` into focused, single-responsibility files.

| File | Purpose |
|---|---|
| `config.go` | `Config` struct replacing package-level global variables |
| `middleware.go` | `RequireRole` — JWT auth middleware (was duplicated inline in upload and update handlers) |
| `helpers.go` | `BlobsToPhotos` — centralised blob→Photo mapping (was duplicated 3× across handlers) |
| `tags.go` | `TagListHandler` — `GET /api/tags` |
| `collection.go` | `CollectionHandler` — `GET /api` (includes fallback query logic) |
| `album.go` | `AlbumHandler` — `GET /api/{collection}` |
| `photo.go` | `PhotoHandler` — `GET /api/{collection}/{album}` |
| `update.go` | `UpdateHandler` — `PUT /api/update/{collection}/{album}/{id}`, plus exported `GetCollectionImage` helper |
| `upload.go` | `UploadHandler` — `POST /api/upload` (multipart file upload with EXIF extraction) |
| `handler_test.go` | 23 behavioural tests using `MockBlobStore` |

---

## Changes to `cmd/photo/main.go`

**Before:** 612 lines containing all handlers, helpers, global variables, and inline auth checks.

**After:** ~124 lines — thin wiring layer that:

1. Initialises OpenTelemetry providers
2. Reads environment variables into a `handler.Config` struct
3. Sets up structured logging (bridged to OTel)
4. Creates the Azure blob client and `storage.NewAzureBlobStore`
5. Registers routes using the new handler functions
6. Starts the HTTP server with CORS and OTel middleware

---

## Bug Fixes (P0)

| Bug | Location | Fix |
|---|---|---|
| Missing `return` after `http.Error` | Multiple handlers | Every error branch now returns immediately after writing the HTTP error response |
| `log.Fatalln` in request path | Former `uploadHandler` | Replaced with `http.Error` + `return` — `log.Fatalln` calls `os.Exit(1)` and kills the entire process |
| Variable shadowing in collection fallback | Former `collectionHandler` | Changed `:=` to `=` so the fallback reassigns `filteredBlobs` instead of creating a shadowed local |
| File handle leak | Former `uploadHandler` | Added `defer file.Close()` after opening the multipart file |

---

## Eliminated Code Duplication

### Auth logic (was duplicated in upload + update handlers)

**Before:** Each handler contained ~15 lines of JWT verification and role checking inline.

**After:** Single `RequireRole(roleName, jwksURL, next)` middleware wraps protected routes in `main.go`:

```go
api.HandleFunc("POST /api/upload", handler.RequireRole(cfg.RoleName, cfg.JwksURL, handler.UploadHandler(store, cfg)))
api.HandleFunc("PUT /api/update/{collection}/{album}/{id}", handler.RequireRole(cfg.RoleName, cfg.JwksURL, handler.UpdateHandler(store, cfg)))
```

### Blob→Photo mapping (was duplicated 3×)

**Before:** `collectionHandler`, `albumHandler`, and `photoHandler` each had ~30 lines of identical `strconv.Parse*` + struct construction.

**After:** Single `BlobsToPhotos(blobs []models.Blob) []models.Photo` function in `helpers.go`, called from all three handlers.

---

## Removed Global Mutable State

The following package-level variables were replaced with `handler.Config` fields:

| Old global | Config field |
|---|---|
| `serviceName` | `Config.ServiceName` |
| `servicePort` | `Config.ServicePort` |
| `uploadsContainerName` | `Config.UploadsContainerName` |
| `imagesContainerName` | `Config.ImagesContainerName` |
| `jwksURL` | `Config.JwksURL` |
| `roleName` | `Config.RoleName` |
| `memoryLimitMb` | `Config.MemoryLimitMb` |
| `storageConfig` | Replaced by `Config.StorageUrl` + `Config.CorsOrigins` |

---

## Test Coverage

### Deleted

- `cmd/photo/main_test.go` — 931 lines of tests that mostly tested `net/http` stdlib behaviour with zero behavioural coverage of actual handler logic. Referenced removed global variables and functions.

### Added

- `internal/handler/handler_test.go` — 23 behavioural tests using `MockBlobStore`:

| Test | What it verifies |
|---|---|
| `TestBlobsToPhotos_ConvertsCorrectly` | All fields mapped correctly from blob tags/metadata |
| `TestBlobsToPhotos_EmptySlice` | Returns empty slice (not nil) |
| `TestBlobsToPhotos_InvalidMetadataDefaults` | Graceful defaults for unparseable width/height/booleans |
| `TestTagListHandler_ReturnsTagMap` | Happy path — 200 + correct JSON |
| `TestTagListHandler_ErrorReturns500` | Storage error → 500 |
| `TestPhotoHandler_ReturnsPhotos` | Correct query construction + 200 |
| `TestPhotoHandler_MissingCollection_Returns400` | Empty collection path param → 400 |
| `TestPhotoHandler_MissingAlbum_Returns400` | Empty album path param → 400 |
| `TestPhotoHandler_NoBlobsFound_Returns404` | Storage error → 404 |
| `TestAlbumHandler_ReturnsAlbums` | Correct query + tag enrichment + 200 |
| `TestAlbumHandler_MissingCollection_Returns400` | Empty collection → 400 |
| `TestAlbumHandler_NoBlobsFound_Returns404` | Storage error → 404 |
| `TestCollectionHandler_ReturnsCollections` | Happy path with `collectionImage='true'` query |
| `TestCollectionHandler_FallbackQuery` | First query fails → fallback query → sets `collectionImage` tag |
| `TestCollectionHandler_NoBlobs_Returns404` | Both queries fail → 404 |
| `TestUpdateHandler_UpdatesTags` | Tag update + 200 |
| `TestUpdateHandler_EmptyBody_Returns400` | Nil body → 400 |
| `TestUpdateHandler_InvalidJSON_Returns400` | Malformed JSON → 400 |
| `TestUpdateHandler_TagsUnchanged_Returns304` | No diff detected → 304 (no `SetBlobTags` call) |
| `TestUpdateHandler_SetsBlobTagsError_Returns500` | Storage write error → 500 |
| `TestUpdateHandler_SwapsCollectionImage` | Sets new `collectionImage`, clears old one (2 `SetBlobTags` calls) |
| `TestGetCollectionImage_Found` | Returns blobs with correct query |
| `TestGetCollectionImage_NotFound` | Returns error when no blobs match |
| `TestConfig_Defaults` | Config fixture has expected field values |

---

## File Summary

### New files

```
internal/storage/storage.go      — BlobStore interface
internal/storage/azure.go        — AzureBlobStore (production)
internal/storage/mock.go         — MockBlobStore (testing)
internal/handler/config.go       — Config struct
internal/handler/middleware.go    — RequireRole auth middleware
internal/handler/helpers.go      — BlobsToPhotos helper
internal/handler/tags.go         — TagListHandler
internal/handler/collection.go   — CollectionHandler
internal/handler/album.go        — AlbumHandler
internal/handler/photo.go        — PhotoHandler
internal/handler/update.go       — UpdateHandler + GetCollectionImage
internal/handler/upload.go       — UploadHandler
internal/handler/handler_test.go — 23 behavioural tests
```

### Modified files

```
cmd/photo/main.go — Rewritten from 612 → 124 lines (thin wiring layer)
```

### Deleted files

```
cmd/photo/main_test.go — 931 lines of legacy tests with zero behavioural coverage
```
