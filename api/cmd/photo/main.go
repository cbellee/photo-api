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

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
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
		StorageAccount: utils.GetEnvValue("STORAGE_ACCOUNT_NAME", ""),
	}
)

// main
func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("current storage account", "name", storageConfig.StorageAccount)

	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		slog.Error("invalid credentials", "error", err)
		return
	}

	opts := policy.TokenRequestOptions{}
	token, err := credential.GetToken(ctx, opts)
	if err != nil {
		slog.Error("error getting token", "error", err)
	}

	slog.Info("managed identity credential token", "token", token)

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

		slog.Info("blob tag map", "value", blobTagList)

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

		//r.PostFormValue("photos")
		//r.PostFormValue("metadata")

		err := r.ParseMultipartForm(32 << 20) // 32Mb
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		var collection = ""
		var album = ""
		var albumImage = ""
		var collectionImage = ""

		if val, ok := r.MultipartForm.Value["collection"]; ok {
			c := val
			if len(c) <= 0 {
				slog.Error("empty FormValue", "name", "collection")
				http.Error(w, "empty FormValue", http.StatusBadRequest)
			}
			collection = c[0]
		}

		if val, ok := r.MultipartForm.Value["album"]; ok {
			a := val
			if len(a) <= 0 {
				slog.Error("empty FormValue", "name", "album")
				http.Error(w, "empty FormValue", http.StatusBadRequest)
			}
			album = a[0]
		}

		if val, ok := r.MultipartForm.Value["albumImage"]; ok {
			ai := val
			if len(ai) <= 0 {
				slog.Error("empty FormValue", "name", "albumImage")
				http.Error(w, "empty FormValue", http.StatusBadRequest)
			}
			albumImage = ai[0]
		}

		if val, ok := r.MultipartForm.Value["collectionImage"]; ok {
			ci := val
			if len(ci) <= 0 {
				slog.Error("empty FormValue", "name", "collectionImage")
				http.Error(w, "empty FormValue", http.StatusBadRequest)
			}
			collectionImage = ci[0]
		}

		slog.Info("CollectionImage", "value", collectionImage)
		slog.Info("albumImage", "value", albumImage)

		md := []models.MetaData{}
		m := r.MultipartForm.Value["metadata"][0]
		err = json.Unmarshal([]byte(m), &md)
		if err != nil {
			slog.Error("error marshalling json", "error", err)
		}

		slog.Info("json data", "data", md)

		for i := 0; i < len(r.MultipartForm.File["photos"]); i++ {
			f := r.MultipartForm.File["photos"][i]

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
			} else {
				tags["CollectionImage"] = ""
			}

			if f.Filename == albumImage {
				// TODO - Clear all photos with 'AlbumImage' tag set for this album
				tags["AlbumImage"] = "true"
			} else {
				tags["AlbumImage"] = ""
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
			Name: *_blob.Name,
			//Path:     fmt.Sprintf("https://%s/%s/%s", storageUrl, imagesContainerName, *_blob.Name),
			Path:     fmt.Sprintf("%s/%s/%s", storageUrl, imagesContainerName, *_blob.Name),
			Tags:     t,
			MetaData: md,
		}

		blobs = append(blobs, b)
	}
	return blobs, nil
}
