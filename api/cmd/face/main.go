package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/cbellee/photo-api/internal/facedetect"
	"github.com/cbellee/photo-api/internal/facestore"
	"github.com/cbellee/photo-api/internal/utils"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/dapr/go-sdk/service/common"
	daprd "github.com/dapr/go-sdk/service/grpc"
	"github.com/google/uuid"
)

var (
	serviceName = os.Getenv("SERVICE_NAME")
	servicePort = os.Getenv("SERVICE_PORT")

	imagesQueueBinding = utils.GetEnvValue("IMAGES_QUEUE_BINDING", "queue-images")
	cascadePath        = utils.GetEnvValue("CASCADE_PATH", "cascade/facefinder")
	puplocPath         = utils.GetEnvValue("PUPLOC_PATH", "cascade/puploc")
	flpDir             = utils.GetEnvValue("FLP_DIR", "cascade/lps")

	storageAccount       = utils.GetEnvValue("STORAGE_ACCOUNT_NAME", "")
	storageAccountSuffix = utils.GetEnvValue("STORAGE_ACCOUNT_SUFFIX", "blob.core.windows.net")
	emulatedStorageURL   = utils.GetEnvValue("EMULATED_STORAGE_URL", "")

	faceStoreType = utils.GetEnvValue("FACE_STORE_TYPE", "sqlite")
	faceStoreDB   = utils.GetEnvValue("FACE_STORE_DB", "/data/facestore.db")
	tableStoreURL = utils.GetEnvValue("TABLE_STORE_URL", "")

	landmarkTolerance = 0.35
	hashMaxHamming    = 10

	detector   *facedetect.Detector
	store      facestore.FaceStore
	blobClient *azblob.Client
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// ── Init Pigo detector ──────────────────────────────────────────
	var err error
	detector, err = facedetect.NewDetector(cascadePath, puplocPath, flpDir)
	if err != nil {
		slog.Error("failed to init face detector", "error", err)
		os.Exit(1)
	}
	slog.Info("face detector initialised", "cascade", cascadePath)

	// ── Init face store ─────────────────────────────────────────────
	switch faceStoreType {
	case "table":
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			slog.Error("failed to create Azure credential", "error", err)
			os.Exit(1)
		}
		store, err = facestore.NewTableStore(tableStoreURL, cred)
		if err != nil {
			slog.Error("failed to init Table Storage face store", "error", err)
			os.Exit(1)
		}
	default:
		store, err = facestore.NewSQLiteStore(faceStoreDB)
		if err != nil {
			slog.Error("failed to init SQLite face store", "error", err)
			os.Exit(1)
		}
	}
	defer store.Close()

	// ── Init blob client ────────────────────────────────────────────
	storageURL := emulatedStorageURL
	if storageURL == "" {
		storageURL = fmt.Sprintf("https://%s.%s", storageAccount, storageAccountSuffix)
	}

	if emulatedStorageURL != "" {
		// Emulated storage uses anonymous access.
		blobClient, err = azblob.NewClientWithNoCredential(storageURL, nil)
	} else {
		cred, err2 := azidentity.NewDefaultAzureCredential(nil)
		if err2 != nil {
			slog.Error("invalid credentials", "error", err2)
			os.Exit(1)
		}
		blobClient, err = azblob.NewClient(storageURL, cred, nil)
	}
	if err != nil {
		slog.Error("failed to create blob client", "error", err)
		os.Exit(1)
	}

	// ── Start Dapr gRPC service ─────────────────────────────────────
	port := fmt.Sprintf(":%s", servicePort)
	slog.Info("starting face service", "name", serviceName, "port", servicePort)

	s, err := daprd.NewService(port)
	if err != nil {
		slog.Error("failed to create Dapr service", "error", err)
		os.Exit(1)
	}

	if err := s.AddBindingInvocationHandler(imagesQueueBinding, faceHandler); err != nil {
		slog.Error("error adding binding handler", "error", err)
		os.Exit(1)
	}

	if err := s.Start(); err != nil {
		slog.Error("service error", "error", err)
		os.Exit(1)
	}
}

func faceHandler(ctx context.Context, in *common.BindingEvent) (out []byte, err error) {
	evt, err := utils.ConvertToEvent(in)
	if err != nil {
		slog.Error("error converting BindingEvent", "error", err)
		return nil, err
	}

	slog.Info("face handler invoked", "url", evt.Data.URL, "eventTime", evt.EventTime)

	// Parse blob path from event URL.
	// URL format: http(s)://<host>/<container>/<collection>/<album>/<filename>
	parts := strings.SplitN(evt.Data.URL, "//", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid blob URL: %s", evt.Data.URL)
	}
	pathParts := strings.SplitN(parts[1], "/", 2)
	if len(pathParts) < 2 {
		return nil, fmt.Errorf("invalid blob path: %s", evt.Data.URL)
	}
	fullPath := pathParts[1] // "container/collection/album/filename"
	segments := strings.SplitN(fullPath, "/", 4)
	if len(segments) < 4 {
		return nil, fmt.Errorf("expected container/collection/album/name, got %q", fullPath)
	}
	container := segments[0]
	collection := segments[1]
	album := segments[2]
	blobName := strings.Join(segments[1:], "/") // "collection/album/filename"

	ref := facestore.PhotoRef{Collection: collection, Album: album, Name: segments[3]}

	// Check if already processed.
	processed, err := store.HasPhotoBeenProcessed(ctx, ref)
	if err != nil {
		slog.Warn("error checking photo processed state", "error", err)
	}
	if processed {
		slog.Info("photo already processed, skipping", "ref", ref.Key())
		return nil, nil
	}

	// Download blob.
	resp, err := blobClient.DownloadStream(ctx, container, blobName, nil)
	if err != nil {
		slog.Error("error downloading blob", "blob", blobName, "error", err)
		return nil, err
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	retryReader := resp.NewRetryReader(ctx, &azblob.RetryReaderOptions{})
	if _, err := buf.ReadFrom(retryReader); err != nil {
		slog.Error("error reading blob", "error", err)
		return nil, err
	}
	retryReader.Close()

	imgBytes := buf.Bytes()

	// Detect faces.
	faces, err := detector.Detect(imgBytes)
	if err != nil {
		slog.Error("face detection failed", "blob", blobName, "error", err)
		return nil, err
	}
	slog.Info("faces detected", "blob", blobName, "count", len(faces))

	// Process each detected face.
	for _, df := range faces {
		fp := facedetect.ComputeFingerprint(df.Landmarks, df.BBoxW, df.BBoxH)
		fpSlice := fp[:]
		hash := facedetect.ComputeDHash(df.CroppedFace)

		// Try to find a similar existing face.
		matches, err := store.FindSimilarFaces(ctx, fpSlice, hash, landmarkTolerance, hashMaxHamming)
		if err != nil {
			slog.Warn("similarity search failed", "error", err)
		}

		var personID string
		if len(matches) > 0 {
			// Assign to the person of the first (best) match.
			personID = matches[0].PersonID
			slog.Info("face matched existing person", "personID", personID)
		} else {
			// Create a new unnamed person.
			personID = uuid.New().String()
			slog.Info("new face, creating person", "personID", personID)
		}

		face := facestore.Face{
			FaceID:   uuid.New().String(),
			PersonID: personID,
			PhotoRef: ref,
			BBox: facestore.BBox{
				X: df.BBoxPctX,
				Y: df.BBoxPctY,
				W: df.BBoxPctW,
				H: df.BBoxPctH,
			},
			LandmarkFingerprint: fpSlice,
			FaceHash:            hash,
			Confidence:          df.Confidence,
			CreatedAt:           time.Now().UTC(),
		}

		if err := store.SaveFace(ctx, face); err != nil {
			slog.Error("error saving face", "faceID", face.FaceID, "error", err)
			continue
		}
	}

	return nil, nil
}
