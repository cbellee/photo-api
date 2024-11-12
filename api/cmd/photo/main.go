package main

// import packages
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/utils"
	"github.com/rs/cors"

	azlog "github.com/Azure/azure-sdk-for-go/sdk/azcore/log"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// vars
var (
	serviceName          = utils.GetEnvValue("SERVICE_NAME", "photoService")
	servicePort          = utils.GetEnvValue("SERVICE_PORT", "8080")
	uploadsContainerName = utils.GetEnvValue("UPLOADS_CONTAINER_NAME", "uploads")
	azureClientId        = utils.GetEnvValue("AZURE_CLIENT_ID", "")
	imagesContainerName  = utils.GetEnvValue("IMAGES_CONTAINER_NAME", "images")
	storageConfig        = models.StorageConfig{
		StorageAccount:       utils.GetEnvValue("STORAGE_ACCOUNT_NAME", ""),
		StorageAccountSuffix: utils.GetEnvValue("STORAGE_ACCOUNT_SUFFIX", "blob.core.windows.net"),
	}
	memoryLimitMb = int64(32)
	isProduction  = false
)

// main
func main() {
	// enable azure SDK logging
	azlog.SetListener(func(event azlog.Event, s string) {
		slog.Info("azlog", "event", event, "message", s)
	})

	// include only azidentity credential logs
	azlog.SetEvents(azidentity.EventAuthentication)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("current storage account", "name", storageConfig.StorageAccount)

	storageUrl := fmt.Sprintf("https://%s.%s", storageConfig.StorageAccount, storageConfig.StorageAccountSuffix)
	slog.Info("storage url", "url", storageUrl)

	// Dump environment variables
	// utils.DumpEnv()

	// check if running in Azure Container App
	if _, exists := os.LookupEnv("CONTAINER_APP_NAME"); exists {
		isProduction = true
	} else {
		slog.Info("'CONTAINER_APP_NAME' env var not found, running in local environment")
	}

	client, err := utils.CreateAzureBlobClient(storageUrl, isProduction, azureClientId)
	if err != nil {
		slog.Error("error creating blob client", "error", err)
		return
	}

	port := fmt.Sprintf(":%s", servicePort)
	api := http.NewServeMux()

	api.HandleFunc("GET /api", collectionsHandler(client, storageUrl))
	api.HandleFunc("GET /api/{collection}", collectionAlbums(client, storageUrl))
	api.HandleFunc("GET /api/{collection}/{album}", albumPhotosHandler(client, storageUrl))
	api.HandleFunc("POST /api/upload", uploadPhotoHandler(client, storageUrl))
	api.HandleFunc("GET /api/tags", tagListHandler(client, storageUrl))

	slog.Info("server listening", "name", serviceName, "port", port)
	handler := cors.Default().Handler(api)
	log.Fatal(http.ListenAndServe(port, handler))
}

func tagListHandler(client *azblob.Client, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		
		ctx := context.Background()

		blobTagList, err := utils.GetBlobTagList(client, imagesContainerName, storageUrl, ctx)
		if err != nil {
			slog.Error("error getting blob tag list", "error", err)
			return
		}

		slog.Info("blob tag map", "value", blobTagList)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blobTagList)
	}
}

func albumPhotosHandler(client *azblob.Client, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		

		collection := r.PathValue("collection")
		if collection == "" {
			slog.Error("empty queryString", "name", "collection")
		}

		album := r.PathValue("album")
		if album == "" {
			slog.Error("empty queryString", "name", "album")
		}

		// get photos with matching collection & album tags
		query := fmt.Sprintf("@container='%s' and Collection='%s' and Album='%s'", imagesContainerName, collection, album)
		filteredBlobs, err := queryBlobsByTags(client, storageUrl, query)
		if err != nil {
			slog.Error("Error getting blobs by tags", "error", err)
		}

		photos := []models.Photo{}

		for _, r := range filteredBlobs {
			width, err := strconv.ParseInt(r.MetaData["Width"], 10, 32)
			if err != nil {
				slog.Error("error converting string 'width' to int", "error", err)
			}

			height, err := strconv.ParseInt(r.MetaData["Height"], 10, 32)
			if err != nil {
				slog.Error("error converting string 'height' to int", "error", err)
			}

			photo := models.Photo{
				Src:         r.Path,
				Name:        r.Name,
				Width:       int32(width),
				Height:      int32(height),
				Album:       r.Tags["Album"],
				Collection:  r.Tags["Collection"],
				Description: r.Tags["Description"],
			}

			photos = append(photos, photo)
		}

		// retun JSON array of objects
		slog.Info("filtered photos", "metadata", photos)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}

func uploadPhotoHandler(client *azblob.Client, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()

		if r.Body == nil {
			http.Error(w, "no multipart form", http.StatusBadRequest)
			return
		}

		err := r.ParseMultipartForm(memoryLimitMb << 20) // 32Mb max memory size limit
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		md := []models.MetaData{}
		m := r.MultipartForm.Value["metadata"][0]
		err = json.Unmarshal([]byte(m), &md)
		if err != nil {
			slog.Error("error marshalling json", "error", err)
		}

		slog.Info("json data", "data", md)

		fh := r.MultipartForm.File["photos"]

		fileNameWithPrefix := fmt.Sprintf("%s/%s/%s", md[0].Collection, md[0].Album, fh[0].Filename)

		tags := make(map[string]string)
		tags["Name"] = fileNameWithPrefix
		tags["Description"] = md[0].Description
		tags["Collection"] = md[0].Collection
		tags["Album"] = md[0].Album

		// set album & collection image tags
		if md[0].CollectionImage {
		// Clear all photos with 'CollectionImage' tag set for this collection
			tags["CollectionImage"] = "true"
		} else {
			tags["CollectionImage"] = ""
		}

		if md[0].AlbumImage {
			// Clear all photos with 'AlbumImage' tag set for this album
			tags["AlbumImage"] = "true"
		} else {
			tags["AlbumImage"] = ""
		}

		file, err := fh[0].Open()
		if err != nil {
			slog.Error("error opening file", "filename", fh[0].Filename, "error", err)
		}

		buf := bytes.NewBuffer(nil)
		if _, err := io.Copy(buf, file); err != nil {
			slog.Error("error copying to buffer", "filename", fh[0].Filename, "error", err)
		}

		img, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
		if err != nil {
			log.Fatalln(err)
		}

		metadata := make(map[string]string)
		metadata["Height"] = fmt.Sprint(img.Height)
		metadata["Width"] = fmt.Sprint(img.Width)
		metadata["Size"] = strconv.Itoa(int(fh[0].Size))

		utils.SaveBlobStreamWithTagsMetadataAndContentType(
			client,
			ctx,
			buf.Bytes(),
			fileNameWithPrefix,
			uploadsContainerName,
			storageUrl,
			tags,
			metadata,
			md[0].Type,
		)
	}
}

func collectionsHandler(client *azblob.Client, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		// get photos with matching collection tags
		query := fmt.Sprintf("@container='%s' and CollectionImage='true'", imagesContainerName)
		slog.Info("query", "query", query)
		filteredBlobs, err := queryBlobsByTags(client, storageUrl, query)
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

			tags, err := utils.GetBlobTags(client, r.Name, imagesContainerName, storageUrl)
			if err != nil {
				slog.Error("error getting blob tagd", "error", err, "blobpath", r.Path)
			}

			photo := models.Photo{
				Src:        r.Path,
				Name:       r.Name,
				Width:      int32(width),
				Height:     int32(height),
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

func collectionAlbums(client *azblob.Client, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		
		collection := r.PathValue("collection")
		if collection == "" {
			slog.Error("empty queryString", "name", "collection")
		}

		// get album placeholder photos with matching tags
		query := fmt.Sprintf("@container='%s' and Collection='%s' and AlbumImage='true'", imagesContainerName, collection)
		filteredBlobs, err := queryBlobsByTags(client, storageUrl, query)
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

			tags, err := utils.GetBlobTags(client, r.Name, imagesContainerName, storageUrl)
			if err != nil {
				slog.Error("error getting blob tagd", "error", err, "blobpath", r.Path)
			}

			photo := models.Photo{
				Src:         r.Path,
				Name:        r.Name,
				Width:       int32(width),
				Height:      int32(height),
				Album:       tags["Album"],
				Collection:  tags["Collection"],
				Description: tags["Description"],
			}

			photos = append(photos, photo)
		}

		// retun JSON array of objects
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}

func queryBlobsByTags(client *azblob.Client, storageUrl string, query string) (blobResult []models.Blob, err error) {
	ctx := context.Background()
	var blobs []models.Blob

	resp, err := client.ServiceClient().FilterBlobs(ctx, query, nil)
	if err != nil {
		slog.Error("error getting blobs by tags", "error", err)
		return
	}

	for _, _blob := range resp.Blobs {
		blobPath := fmt.Sprintf("%s/%s/%s", storageUrl, imagesContainerName, *_blob.Name)
		slog.Info("blobPath", "path", blobPath)

		tags, err := utils.GetBlobTags(client, *_blob.Name, imagesContainerName, storageUrl)
		if err != nil {
			slog.Error("error getting blob tags", "blobPath", blobPath, "error", err)
			return nil, err
		}

		md, err := utils.GetBlobMetadata(client, *_blob.Name, *_blob.ContainerName, storageUrl)
		if err != nil {
			slog.Error("error getting metadata", "blobPath", blobPath, "error", err)
		}

		b := models.Blob{
			Name:     *_blob.Name,
			Path:     fmt.Sprintf("%s/%s/%s", storageUrl, imagesContainerName, *_blob.Name),
			Tags:     tags,
			MetaData: md,
		}

		blobs = append(blobs, b)
	}
	slog.Info("found blobs by tag query", "blobs", blobs)
	return blobs, nil
}
