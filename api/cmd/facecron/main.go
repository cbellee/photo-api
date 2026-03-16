// Command facecron scans all photos in the images container and detects
// faces for any that haven't been processed yet. It is designed to run as:
//
//   - An Azure Container Apps Job with a cron trigger (e.g. "0 2 * * *")
//   - A one-shot CLI: go run ./cmd/facecron
//   - Triggered via POST /api/admin/rescan-faces on the photo API
//
// It uses the same Pigo detector, fingerprint, and dHash pipeline as the
// real-time face service but processes photos in batches.
package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbellee/photo-api/internal/facedetect"
	"github.com/cbellee/photo-api/internal/facestore"
	"github.com/cbellee/photo-api/internal/storage"
	"github.com/cbellee/photo-api/internal/utils"

	"github.com/google/uuid"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	ctx := context.Background()

	// ── Configuration ───────────────────────────────────────────────
	cascadePath := utils.GetEnvValue("CASCADE_PATH", "cascade/facefinder")
	puplocPath := utils.GetEnvValue("PUPLOC_PATH", "cascade/puploc")
	flpDir := utils.GetEnvValue("FLP_DIR", "cascade/lps")
	faceStoreType := utils.GetEnvValue("FACE_STORE_TYPE", "sqlite")
	faceStoreDB := utils.GetEnvValue("FACE_STORE_DB", "/data/facestore.db")
	imagesContainer := utils.GetEnvValue("IMAGES_CONTAINER_NAME", "images")
	concurrency := runtime.NumCPU()
	landmarkTolerance := 0.35
	hashMaxHamming := 10

	storageURL := utils.GetEnvValue("EMULATED_STORAGE_URL", "")
	if storageURL == "" {
		storageAccount := utils.GetEnvValue("STORAGE_ACCOUNT_NAME", "")
		storageAccountSuffix := utils.GetEnvValue("STORAGE_ACCOUNT_SUFFIX", "blob.core.windows.net")
		storageURL = fmt.Sprintf("https://%s.%s", storageAccount, storageAccountSuffix)
	}
	azureClientID := utils.GetEnvValue("AZURE_CLIENT_ID", "")

	// ── Init detector ───────────────────────────────────────────────
	detector, err := facedetect.NewDetector(cascadePath, puplocPath, flpDir)
	if err != nil {
		slog.Error("failed to init detector", "error", err)
		os.Exit(1)
	}

	// ── Init face store ─────────────────────────────────────────────
	var faceStore facestore.FaceStore
	switch faceStoreType {
	case "table":
		slog.Error("Table Storage not yet supported in facecron — use sqlite for local dev")
		os.Exit(1)
	default:
		faceStore, err = facestore.NewSQLiteStore(faceStoreDB)
		if err != nil {
			slog.Error("failed to init face store", "error", err)
			os.Exit(1)
		}
	}
	defer faceStore.Close()

	// ── Init blob store ─────────────────────────────────────────────
	blobStore, err := storage.NewBlobStore(storageURL, azureClientID)
	if err != nil {
		slog.Error("failed to init blob store", "error", err)
		os.Exit(1)
	}

	// ── List all photos ─────────────────────────────────────────────
	slog.Info("listing photos", "container", imagesContainer)
	blobs, err := blobStore.FilterBlobsByTags(ctx,
		fmt.Sprintf("@container='%s' AND isDeleted='false'", imagesContainer),
		imagesContainer)
	if err != nil {
		slog.Error("failed to list photos", "error", err)
		os.Exit(1)
	}
	slog.Info("photos found", "count", len(blobs))

	// ── Process in parallel ─────────────────────────────────────────
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var processed, skipped, failed atomic.Int64

	for _, blob := range blobs {
		collection := blob.Tags["collection"]
		album := blob.Tags["album"]
		blobName := blob.Name
		// Extract just the filename from the full path.
		nameParts := strings.Split(blobName, "/")
		fileName := nameParts[len(nameParts)-1]

		ref := facestore.PhotoRef{Collection: collection, Album: album, Name: fileName}

		// Check if already processed.
		done, err := faceStore.HasPhotoBeenProcessed(ctx, ref)
		if err != nil {
			slog.Warn("error checking processed state", "ref", ref.Key(), "error", err)
		}
		if done {
			skipped.Add(1)
			continue
		}

		wg.Add(1)
		sem <- struct{}{} // acquire
		go func(blobName string, ref facestore.PhotoRef) {
			defer wg.Done()
			defer func() { <-sem }() // release

			imgBytes, err := blobStore.GetBlob(ctx, blobName, imagesContainer)
			if err != nil {
				slog.Error("download failed", "blob", blobName, "error", err)
				failed.Add(1)
				return
			}

			faces, err := detector.Detect(imgBytes)
			if err != nil {
				slog.Error("detection failed", "blob", blobName, "error", err)
				failed.Add(1)
				return
			}

			for _, df := range faces {
				fp := facedetect.ComputeFingerprint(df.Landmarks, df.BBoxW, df.BBoxH)
				fpSlice := fp[:]
				hash := facedetect.ComputeDHash(df.CroppedFace)

				matches, _ := faceStore.FindSimilarFaces(ctx, fpSlice, hash, landmarkTolerance, hashMaxHamming)

				var personID string
				if len(matches) > 0 {
					personID = matches[0].PersonID
				} else {
					personID = uuid.New().String()
				}

				face := facestore.Face{
					FaceID:              uuid.New().String(),
					PersonID:            personID,
					PhotoRef:            ref,
					BBox:                facestore.BBox{X: df.BBoxPctX, Y: df.BBoxPctY, W: df.BBoxPctW, H: df.BBoxPctH},
					LandmarkFingerprint: fpSlice,
					FaceHash:            hash,
					Confidence:          df.Confidence,
					CreatedAt:           time.Now().UTC(),
				}

				if err := faceStore.SaveFace(ctx, face); err != nil {
					slog.Error("save face failed", "faceID", face.FaceID, "error", err)
				}
			}

			processed.Add(1)
			if processed.Load()%50 == 0 {
				slog.Info("progress", "processed", processed.Load(), "skipped", skipped.Load(), "failed", failed.Load())
			}
		}(blobName, ref)
	}

	wg.Wait()
	slog.Info("facecron complete",
		"processed", processed.Load(),
		"skipped", skipped.Load(),
		"failed", failed.Load(),
		"total", len(blobs))
}

// Suppress unused import warning.
var _ = bytes.NewReader
