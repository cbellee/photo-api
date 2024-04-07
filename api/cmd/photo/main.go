package main

// import packages
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	serviceName          = utils.GetEnvValue("SERVICE_NAME", "photoService")
	servicePort          = utils.GetEnvValue("SERVICE_PORT", "8080")
	uploadsContainerName = "uploads"
	storageConfig        = models.StorageConfig{
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

	mux.HandleFunc("GET /api", collectionsHandler(credential))
	mux.HandleFunc("GET /api/{collection}", collectionAlbums(credential))
	mux.HandleFunc("GET /api/{collection}/{album}", albumPhotosHandler(credential))
	mux.HandleFunc("POST /api/upload", uploadPhotoHandler(credential))

	slog.Info("server listening", "name", serviceName, "port", port)
	http.ListenAndServe(port, mux)
}

func enableCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
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
		if album == "" {
			slog.Error("empty queryString", "name", "album")
		}

		// get photos with matching collection & album tags
		query := fmt.Sprintf("@container='%s' and Collection='%s' and Album='%s'", uploadsContainerName, collection, album)
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

func uploadPhotoHandler(credential *azidentity.DefaultAzureCredential) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		ctx := context.Background()
		enableCors(&w)

		r.PostFormValue("files")
		r.PostFormValue("metadata")

		collection := r.MultipartForm.Value["collection"][0]
		if collection == "" {
			slog.Error("empty PathValue", "name", "collection")
		}

		album := r.MultipartForm.Value["album"][0]
		if album == "" {
			slog.Error("empty PathValue", "name", "album")
		}

		r.ParseMultipartForm(32 << 20)

		md := []models.MetaData{}
		m := r.MultipartForm.Value["metadata"][0]
		err := json.Unmarshal([]byte(m), &md)
		if err != nil {
			slog.Error("error marshalling json", "error", err)
		}
		slog.Info("json data", "data", md)

		for i := 0; i < len(r.MultipartForm.File["files"]); i++ {
			f := r.MultipartForm.File["files"][i]

			fileNameWithPrefix := fmt.Sprintf("%s/%s/%s", "trips", "perth", f.Filename)

			tags := make(map[string]string)
			tags["Name"] = fileNameWithPrefix
			tags["Description"] = md[i].Description
			tags["Collection"] = collection
			tags["Album"] = album

			file, err := f.Open()
			if err != nil {
				slog.Error("error opening file", "filename", f.Filename, "error", err)
			}

			buf := bytes.NewBuffer(nil)
			if _, err := io.Copy(buf, file); err != nil {
				slog.Error("error copying to buffer", "filename", f.Filename, "error", err)
			}

			utils.SaveBlobStreamWithTagsAndMetadata(
				credential,
				ctx,
				buf.Bytes(),
				fileNameWithPrefix,
				uploadsContainerName,
				storageConfig.StorageAccount,
				storageConfig.Suffix,
				tags,
				nil)
		}
	}
}

func collectionsHandler(credential *azidentity.DefaultAzureCredential) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
/* 		slog.Info("Request", "Body", r.Body)
		slog.Info("Request", "Method", r.Method)
		slog.Info("Raw Paths", "RawPath", r.URL.RawPath)
		slog.Info("QueryString", "Query", r.URL.Query()) */

		// get photos with matching collection tags
		query := fmt.Sprintf("@container='%s' and IsCollectionImage='true'", uploadsContainerName)
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

			tags, err := utils.GetBlobTags(credential, r.Name, uploadsContainerName, storageConfig.StorageAccount, storageConfig.Suffix)
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
/* 		slog.Info("Request", "Body", r.Body)
		slog.Info("Request", "Method", r.Method)
		slog.Info("Raw Paths", "RawPath", r.URL.RawPath)
		slog.Info("QueryString", "Query", r.URL.Query()) */

		collection := r.PathValue("collection")
		if collection == "" {
			slog.Error("empty queryString", "name", "collection")
		}

		// get album placeholder photos with matching tags
		query := fmt.Sprintf("@container='%s' and Collection='%s' and IsAlbumImage='true'", uploadsContainerName, collection)
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

			tags, err := utils.GetBlobTags(credential, r.Name, uploadsContainerName, storageConfig.StorageAccount, storageConfig.Suffix)
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
		blobPath := fmt.Sprintf("https://%s.%s/%s/%s", storageConfig.StorageAccount, storageConfig.Suffix, uploadsContainerName, *_blob.Name)
		slog.Info("blobPath", "path", blobPath)

		t := make(map[string]string)
		for _, tag := range _blob.Tags.BlobTagSet {
			t[*tag.Key] = *tag.Value
		}

		md, err := utils.GetBlobMetadata(credential, *_blob.Name, *_blob.ContainerName, storageConfig.StorageAccount, storageConfig.Suffix)
		if err != nil {
			slog.Error("error getting metadata", "blobPath", blobPath, "error", err)
		}

		b := models.Blob{
			Name:     *_blob.Name,
			Path:     fmt.Sprintf("https://%s.%s/%s/%s", storageConfig.StorageAccount, storageConfig.Suffix, uploadsContainerName, *_blob.Name),
			Tags:     t,
			MetaData: md,
		}

		blobs = append(blobs, b)
	}
	return blobs, nil
}
