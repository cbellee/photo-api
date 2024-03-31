package main

// import packages
import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"models"
	"net/http"
	"os"
	"strconv"
	"utils"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// vars
var (
	serviceName         = utils.GetEnvValue("SERVICE_NAME", "photoService")
	servicePort         = utils.GetEnvValue("SERVICE_PORT", "8080")
	imagesContainerName = "images"
	storageConfig       = models.StorageConfig{
		StorageAccount: utils.GetEnvValue("STORAGE_ACCOUNT_NAME", "storqra2f23aqljtm"),
		Suffix:         "blob.core.windows.net",
	}
)

// main
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		slog.Error("invalid credentials", "error", err)
		return
	}

	port := fmt.Sprintf(":%s", servicePort)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/collections", collectionsHandler(credential))
	mux.HandleFunc("GET /api/collections/{collection}/albums", collectionAlbums(credential))
	mux.HandleFunc("GET /api/collections/{collection}/albums/{album}", albumPhotosHandler(credential))

	slog.Info("server listening", "name", serviceName, "port", port)
	http.ListenAndServe(port, mux)
}

func albumPhotosHandler(credential *azidentity.DefaultAzureCredential) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.Info("Request", "Body", r.Body)
		slog.Info("Request", "Method", r.Method)
		slog.Info("Raw Paths", "RawPath", r.URL.RawPath)
		slog.Info("QueryString", "Query", r.URL.Query())

		collection := r.PathValue("collection")
		if collection == "" {
			slog.Error("empty queryString", "name", "collection")
		}

		album := r.PathValue("album")
		if collection == "" {
			slog.Error("empty queryString", "name", "album")
		}

		// get photos with matching collection & album tags
		query := fmt.Sprintf("@container='%s' and Collection='%s' and Album='%s'", imagesContainerName, collection, album)
		filteredBlobs, err := queryBlobsByTags(credential, query)
		if err != nil {
			slog.Error("Error getting blobs by tags", "error", err)
		}

		photos := []models.Photo{}

		for _, r := range filteredBlobs {
			slog.Info("Filtered Blobs", "Name", r.Name, "Metadata", r.MetaData, "Tags", r.Tags, "Path", r.Path)
			width, err := strconv.ParseInt(r.MetaData["Width"], 10, 32)
			if err != nil {
				slog.Error("error converting string 'width' to int", "error", err)
			}

			height, err := strconv.ParseInt(r.MetaData["Height"], 10, 32)
			if err != nil {
				slog.Error("error converting string 'height' to int", "error", err)
			}

			ratio, err := strconv.ParseFloat(r.MetaData["Ratio"], 32)
			if err != nil {
				slog.Error("error converting string 'ratio' to float", "error", err)
			}

			photo := models.Photo{
				Src:        r.Path,
				Name:       r.Name,
				Width:      int32(width),
				Height:     int32(height),
				Ratio:      float32(ratio),
				Album:      r.Tags["Album"],
				Collection: r.Tags["Collection"],
			}

			photos = append(photos, photo)
		}

		// retun JSON array of objects
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}

func collectionsHandler(credential *azidentity.DefaultAzureCredential) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.Info("Request", "Body", r.Body)
		slog.Info("Request", "Method", r.Method)
		slog.Info("Raw Paths", "RawPath", r.URL.RawPath)
		slog.Info("QueryString", "Query", r.URL.Query())

		// get photos with matching collection tags
		query := fmt.Sprintf("@container='%s' and IsCollectionImage='true'", imagesContainerName)
		filteredBlobs, err := queryBlobsByTags(credential, query)
		if err != nil {
			slog.Error("Error getting blobs by tags", "error", err)
		}

		photos := []models.Photo{}

		for _, r := range filteredBlobs {
			slog.Info("Filtered Blobs", "Name", r.Name, "Metadata", r.MetaData, "Tags", r.Tags, "Path", r.Path)
			width, err := strconv.ParseInt(r.MetaData["Width"], 10, 32)
			if err != nil {
				slog.Error("error converting string 'width' to int", "error", err)
			}

			height, err := strconv.ParseInt(r.MetaData["Height"], 10, 32)
			if err != nil {
				slog.Error("error converting string 'height' to int", "error", err)
			}

			ratio, err := strconv.ParseFloat(r.MetaData["Ratio"], 32)
			if err != nil {
				slog.Error("error converting string 'ratio' to float", "error", err)
			}

			tags, err := utils.GetBlobTags(r.Name, imagesContainerName, storageConfig.StorageAccount, storageConfig.Suffix)
			if err != nil {
				slog.Error("error getting blob tagd", "error", err, "blobpath", r.Path)
			}

			photo := models.Photo{
				Src:        r.Path,
				Name:       r.Name,
				Width:      int32(width),
				Height:     int32(height),
				Ratio:      float32(ratio),
				Album:      tags["Album"],
				Collection: tags["Collection"],
			}

			photos = append(photos, photo)
		}

		// retun JSON array of objects
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}

func collectionAlbums(credential *azidentity.DefaultAzureCredential) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.Info("Request", "Body", r.Body)
		slog.Info("Request", "Method", r.Method)
		slog.Info("Raw Paths", "RawPath", r.URL.RawPath)
		slog.Info("QueryString", "Query", r.URL.Query())
		
		collection := r.PathValue("collection")
		if collection == "" {
			slog.Error("empty queryString", "name", "collection")
		}

		// get album placeholder photos with matching tags
		query := fmt.Sprintf("@container='%s' and Collection='%s' and IsAlbumImage='true'", imagesContainerName, collection)
		filteredBlobs, err := queryBlobsByTags(credential, query)
		if err != nil {
			slog.Error("Error getting blobs by tags", "error", err)
		}

		photos := []models.Photo{}

		for _, r := range filteredBlobs {
			slog.Info("Filtered Blobs", "Name", r.Name, "Metadata", r.MetaData, "Tags", r.Tags, "Path", r.Path)
			width, err := strconv.ParseInt(r.MetaData["Width"], 10, 32)
			if err != nil {
				slog.Error("error converting string 'width' to int", "error", err)
			}

			height, err := strconv.ParseInt(r.MetaData["Height"], 10, 32)
			if err != nil {
				slog.Error("error converting string 'height' to int", "error", err)
			}

			ratio, err := strconv.ParseFloat(r.MetaData["Ratio"], 32)
			if err != nil {
				slog.Error("error converting string 'ratio' to float", "error", err)
			}

			tags, err := utils.GetBlobTags(r.Name, imagesContainerName, storageConfig.StorageAccount, storageConfig.Suffix)
			if err != nil {
				slog.Error("error getting blob tagd", "error", err, "blobpath", r.Path)
			}

			photo := models.Photo{
				Src:        r.Path,
				Name:       r.Name,
				Width:      int32(width),
				Height:     int32(height),
				Ratio:      float32(ratio),
				Album:      tags["Album"],
				Collection: tags["Collection"],
			}

			photos = append(photos, photo)
		}

		// retun JSON array of objects
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}

func queryBlobsByTags(credential *azidentity.DefaultAzureCredential, query string) (blobResult []models.Blob, err error) {
	ctx := context.Background()
	var blobs []models.Blob

	storageUrl := fmt.Sprintf("https://%s.%s", storageConfig.StorageAccount, storageConfig.Suffix)
	client, err := azblob.NewClient(storageUrl, credential, nil)
	if err != nil {
		slog.Error("error creating blob client", err)
	}

	resp, err := client.ServiceClient().FilterBlobs(ctx, query, nil)
	if err != nil {
		slog.Error("error getting blobs by tags", err)
		return
	}

	for _, _blob := range resp.Blobs {
		blobPath := fmt.Sprintf("https://%s.%s/%s/%s", storageConfig.StorageAccount, storageConfig.Suffix, imagesContainerName, *_blob.Name)
		slog.Info("blobPath", "path", blobPath)

		t := make(map[string]string)
		for _, tag := range _blob.Tags.BlobTagSet {
			t[*tag.Key] = *tag.Value
		}

		md, err := utils.GetBlobMetadata(*_blob.Name, *_blob.ContainerName, storageConfig.StorageAccount, storageConfig.Suffix)
		if err != nil {
			slog.Error("error getting metadata", "blobPath", blobPath, "error", err)
		}

		b := models.Blob{
			Name:     *_blob.Name,
			Path:     fmt.Sprintf("https://%s.%s/%s/%s", storageConfig.StorageAccount, storageConfig.Suffix, imagesContainerName, *_blob.Name),
			Tags:     t,
			MetaData: md,
		}

		blobs = append(blobs, b)
	}
	return blobs, nil
}
