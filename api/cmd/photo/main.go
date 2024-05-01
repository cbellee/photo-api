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
	"github.com/cbellee/photo-api/internal/models"
	"net/http"
	"os"
	"strconv"
	"github.com/cbellee/photo-api/internal/utils"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// vars
var (
	serviceName          = utils.GetEnvValue("SERVICE_NAME", "photoService")
	servicePort          = utils.GetEnvValue("SERVICE_PORT", "8080")
	uploadsContainerName = "uploads"
	imagesContainerName  = "images"
	storageConfig        = models.StorageConfig{
		StorageAccount: utils.GetEnvValue("STORAGE_ACCOUNT_NAME", "storqra2f23aqljtm.blob.core.windows.net"),
		//StorageAccount: utils.GetEnvValue("STORAGE_ACCOUNT_NAME", "127.0.0.1:10000/devstoreaccount1"),
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

	storageUrl := fmt.Sprintf("https://%s", storageConfig.StorageAccount)

	port := fmt.Sprintf(":%s", servicePort)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api", collectionsHandler(credential, storageUrl))
	mux.HandleFunc("GET /api/{collection}", collectionAlbums(credential, storageUrl))
	mux.HandleFunc("GET /api/{collection}/{album}", albumPhotosHandler(credential, storageUrl))
	mux.HandleFunc("POST /api/upload", uploadPhotoHandler(credential, storageUrl))
	mux.HandleFunc("GET /api/tags", tagListHandler(credential, storageUrl))

	slog.Info("server listening", "name", serviceName, "port", port)
	http.ListenAndServe(port, mux)
}

func enableCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
}

func tagListHandler(credential *azidentity.DefaultAzureCredential, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w)
		ctx := context.Background()

		blobTagList, err := utils.GetBlobTagList(credential, imagesContainerName, storageUrl, ctx)
		if err != nil {
			slog.Error("error getting blob tag list", "error", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blobTagList)
	}
}

func albumPhotosHandler(credential *azidentity.DefaultAzureCredential, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w)

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
		filteredBlobs, err := queryBlobsByTags(credential, storageUrl, query)
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

			photo := models.Photo{
				Src:        r.Path,
				Name:       r.Name,
				Width:      int32(width),
				Height:     int32(height),
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

func uploadPhotoHandler(credential *azidentity.DefaultAzureCredential, storageUrl string) http.HandlerFunc {
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

		collectionImage := r.MultipartForm.Value["collectionImage"][0]
		if collectionImage == "" {
			slog.Error("empty PathValue", "name", "collectionImage")
		}

		albumImage := r.MultipartForm.Value["albumImage"][0]
		if albumImage == "" {
			slog.Error("empty PathValue", "name", "albumImage")
		}

		slog.Info("CollectionImage: " + collectionImage)
		slog.Info("albumImage: " + albumImage)

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

			if collection == "undefined" || album == "undefined" {
				slog.Error("album and/or collection are 'undefined'")
				return
			}

			fileNameWithPrefix := fmt.Sprintf("%s/%s/%s", collection, album, f.Filename)

			tags := make(map[string]string)
			tags["Name"] = fileNameWithPrefix
			tags["Description"] = md[i].Description
			tags["Collection"] = collection
			tags["Album"] = album

			// set album & collection image tags
			if f.Filename == collectionImage {
				// TODO - Clear all photos with 'CollectionImage' tag set for this collection
				tags["CollectionImage"] = "true"
			}

			if f.Filename == albumImage {
				// TODO - Clear all photos with 'AlbumImage' tag set for this album
				tags["AlbumImage"] = "true"
			}

			file, err := f.Open()
			if err != nil {
				slog.Error("error opening file", "filename", f.Filename, "error", err)
			}

			buf := bytes.NewBuffer(nil)
			if _, err := io.Copy(buf, file); err != nil {
				slog.Error("error copying to buffer", "filename", f.Filename, "error", err)
			}

			img, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
			if err != nil {
				log.Fatalln(err)
			}

			metadata := make(map[string]string)
			metadata["Height"] = fmt.Sprint(img.Height)
			metadata["Width"] = fmt.Sprint(img.Width)
			metadata["Size"] = strconv.Itoa(int(f.Size))

			utils.SaveBlobStreamWithTagsMetadataAndContentType(
				credential,
				ctx,
				buf.Bytes(),
				fileNameWithPrefix,
				uploadsContainerName,
				storageUrl,
				tags,
				metadata,
				md[i].Type,
			)
		}
	}
}

func collectionsHandler(credential *azidentity.DefaultAzureCredential, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w)

		// get photos with matching collection tags
		query := fmt.Sprintf("@container='%s' and CollectionImage='true'", imagesContainerName)
		filteredBlobs, err := queryBlobsByTags(credential, storageUrl, query)
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

			tags, err := utils.GetBlobTags(credential, r.Name, imagesContainerName, storageUrl)
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

func collectionAlbums(credential *azidentity.DefaultAzureCredential, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enableCors(&w)

		collection := r.PathValue("collection")
		if collection == "" {
			slog.Error("empty queryString", "name", "collection")
		}

		// get album placeholder photos with matching tags
		query := fmt.Sprintf("@container='%s' and Collection='%s' and AlbumImage='true'", imagesContainerName, collection)
		filteredBlobs, err := queryBlobsByTags(credential, storageUrl, query)
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

			tags, err := utils.GetBlobTags(credential, r.Name, imagesContainerName, storageUrl)
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

func queryBlobsByTags(credential *azidentity.DefaultAzureCredential, storageUrl string, query string) (blobResult []models.Blob, err error) {
	ctx := context.Background()
	var blobs []models.Blob

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
		//blobPath := fmt.Sprintf("https://%s/%s/%s", storageUrl, imagesContainerName, *_blob.Name)
		blobPath := fmt.Sprintf("%s/%s/%s", storageUrl, imagesContainerName, *_blob.Name)
		slog.Info("blobPath", "path", blobPath)

		t := make(map[string]string)
		for _, tag := range _blob.Tags.BlobTagSet {
			t[*tag.Key] = *tag.Value
		}

		md, err := utils.GetBlobMetadata(credential, *_blob.Name, *_blob.ContainerName, storageUrl)
		if err != nil {
			slog.Error("error getting metadata", "blobPath", blobPath, "error", err)
		}

		b := models.Blob{
			Name:     *_blob.Name,
			//Path:     fmt.Sprintf("https://%s/%s/%s", storageUrl, imagesContainerName, *_blob.Name),
			Path:     fmt.Sprintf("%s/%s/%s", storageUrl, imagesContainerName, *_blob.Name),
			Tags:     t,
			MetaData: md,
		}

		blobs = append(blobs, b)
	}
	return blobs, nil
}
