package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/cbellee/photo-api/internal/exif"
	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/utils"
	"github.com/rs/cors"

	azlog "github.com/Azure/azure-sdk-for-go/sdk/azcore/log"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

var (
	serviceName          = utils.GetEnvValue("SERVICE_NAME", "photoService")
	servicePort          = utils.GetEnvValue("SERVICE_PORT", "8080")
	uploadsContainerName = utils.GetEnvValue("UPLOADS_CONTAINER_NAME", "uploads")
	azureClientId        = utils.GetEnvValue("AZURE_CLIENT_ID", "")
	imagesContainerName  = utils.GetEnvValue("IMAGES_CONTAINER_NAME", "images")
	storageConfig        = models.StorageConfig{
		StorageAccount:       utils.GetEnvValue("STORAGE_ACCOUNT_NAME", "stor6aq2g56sfcosi"),
		StorageAccountSuffix: utils.GetEnvValue("STORAGE_ACCOUNT_SUFFIX", "blob.core.windows.net"),
	}
	memoryLimitMb = int64(32)
	isProduction  = false
	jwksURL       = utils.GetEnvValue("JWKS_URL", "https://login.microsoftonline.com/0cd02bb5-3c24-4f77-8b19-99223d65aa67/discovery/keys?appid=689078c3-c0ad-4c10-a0d3-1c430c2e471d")
	roleName      = utils.GetEnvValue("ROLE_NAME", "photo.upload")
)

func main() {
	corsString := utils.GetEnvValue("CORS_ORIGINS", "http://localhost:5173,https://gallery.bellee.net,https://photos.bellee.net,https://photos.bellee.net")
	corsOrigins := strings.Split(corsString, ",")
	slog.Info("cors origins", "origins", corsOrigins)

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

	api.HandleFunc("GET /api", collectionHandler(client, storageUrl))
	api.HandleFunc("GET /api/{collection}", albumHandler(client, storageUrl))
	api.HandleFunc("GET /api/{collection}/{album}", photoHandler(client, storageUrl))
	api.HandleFunc("POST /api/upload", uploadHandler(client, storageUrl, roleName, jwksURL))
	api.HandleFunc("PUT /api/update/{collection}/{album}/{id}", updateHandler(client, storageUrl, roleName, jwksURL))
	api.HandleFunc("GET /api/tags", tagListHandler(client, storageUrl))

	slog.Info("server listening", "name", serviceName, "port", port)

	opt := cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD"},
		AllowedHeaders:   []string{"Origin", "X-Requested-With", "Content-Type", "Accept", "Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	}

	c := cors.New(opt)
	handler := c.Handler(api)

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

		slog.Debug("blob tag map", "value", blobTagList)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(blobTagList)
	}
}

func photoHandler(client *azblob.Client, storageUrl string) http.HandlerFunc {
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
		query := fmt.Sprintf("@container='%s' AND collection='%s' AND album='%s' AND isDeleted='false'", imagesContainerName, collection, album)
		filteredBlobs, err := queryBlobsByTags(client, storageUrl, query)
		if err != nil {
			slog.Error("Error getting blobs by tags", "error", err)
		}

		photos := []models.Photo{}

		for _, fb := range filteredBlobs {
			width, err := strconv.ParseInt(fb.MetaData["Width"], 10, 32)
			if err != nil {
				slog.Error("error converting string 'width' to int", "error", err)
			}

			height, err := strconv.ParseInt(fb.MetaData["Height"], 10, 32)
			if err != nil {
				slog.Error("error converting string 'height' to int", "error", err)
			}

			isDeleted, err := strconv.ParseBool(fb.Tags["isDeleted"])
			if err != nil {
				isDeleted = false
			}

			albumImage, err := strconv.ParseBool(fb.Tags["albumImage"])
			if err != nil {
				albumImage = false
			}

			collectionImage, err := strconv.ParseBool(fb.Tags["collectionImage"])
			if err != nil {
				collectionImage = false
			}

			orienation, err := strconv.Atoi(fb.Tags["orientation"])
			if err != nil {
				orienation = 0
			}

			photo := models.Photo{
				Src:             fb.Path,
				Name:            fb.Name,
				Width:           int(width),
				Height:          int(height),
				Album:           fb.Tags["album"],
				Collection:      fb.Tags["collection"],
				Description:     fb.Tags["description"],
				ExifData:        fb.MetaData["Exifdata"],
				IsDeleted:       isDeleted,
				Orientation:     orienation,
				AlbumImage:      albumImage,
				CollectionImage: collectionImage,
			}

			photos = append(photos, photo)
		}

		// retun JSON array of objects
		slog.Debug("filtered photos", "metadata", photos)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}

func updateHandler(client *azblob.Client, storageUrl string, roleName string, jwksURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// verify JWT token
		claims, err := utils.VerifyToken(r, jwksURL)
		if err != nil {
			http.Error(w, "You are not authorized to perform this operation", http.StatusUnauthorized)
			return
		}

		// ensure the caller has the required role claim
		photoUploadClaim := slices.Contains(claims.Roles, roleName)
		if photoUploadClaim {
			slog.Debug("role claim found in token", "roles", claims.Roles)
		} else {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Body == nil {
			http.Error(w, "body is empty", http.StatusBadRequest)
			return
		}

		// get photo tags from storage account and compare with updated tags
		newTags := map[string]string{}
		err = json.NewDecoder(r.Body).Decode(&newTags)

		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// get photo tags from storage account and compare with updated tags
		currTags, err := utils.GetBlobTags(client, newTags["name"], imagesContainerName, storageUrl)
		if err != nil {
			slog.Error("error getting blob tags", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}

		/* currMetadata, err := utils.GetBlobMetadata(client, newTags["name"], imagesContainerName, storageUrl)
		if err != nil {
			slog.Error("error getting blob metadata", "error", err)
		} */

		// remote 'Url' tag from comparison
		delete(currTags, "Url")

		// add orientation to comparison
		//currTags["orientation"] = newTags["orientation"]

		if maps.Equal(currTags, newTags) {
			// return 304 Not Modified
			slog.Info("tags not modified", "tags", currTags)
			http.Error(w, "Tags not modified", http.StatusNotModified)
			return
		}

		// get current image with collectionImage tag set to 'true'
		currentCollectionImage, err := getCollectionImage(client, storageUrl, currTags["collection"])
		if err != nil {
			slog.Error("error getting collection image", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// set CollectionImage tag to 'false' on existing image if it has been set to 'true' on the current image
		if newTags["collectionImage"] == "true" && currentCollectionImage[0].Tags["name"] != newTags["name"] {
			slog.Info("collection image set on another image. Setting 'collectionImage'", "collection", currTags["collection"], "image", currentCollectionImage[0].Name)

			// set collectionImage tag to 'false' on existing image
			currentCollectionImage[0].Tags["collectionImage"] = "false"

			// update blob metadata for previous image
			err = utils.SetBlobTags(client, currentCollectionImage[0].Name, imagesContainerName, storageUrl, currentCollectionImage[0].Tags)
			if err != nil {
				slog.Error("error setting collectionImage tag", "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}

		// update blob metadata
		err = utils.SetBlobTags(client, newTags["name"], imagesContainerName, storageUrl, newTags)
		if err != nil {
			slog.Error("error updating blob metadata", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}
}

func uploadHandler(client *azblob.Client, storageUrl string, roleName string, jwksURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()

		// Verify JWT token
		claims, err := utils.VerifyToken(r, jwksURL)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// ensure the user has the required role claim
		photoUploadClaim := slices.Contains(claims.Roles, roleName)
		if photoUploadClaim {
			slog.Debug("role claim found in token", "roles", claims.Roles)
		} else {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Body == nil {
			http.Error(w, "Multipart form not found", http.StatusBadRequest)
			return
		}

		err = r.ParseMultipartForm(memoryLimitMb << 20) // 32Mb max memory size limit
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		it := models.ImageTags{}
		m := r.MultipartForm.Value["metadata"][0]
		err = json.Unmarshal([]byte(m), &it)
		if err != nil {
			slog.Error("error marshalling json", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}

		fh := r.MultipartForm.File["photo"]

		fileNameWithPrefix := fmt.Sprintf("%s/%s/%s", it.Collection, it.Album, fh[0].Filename)

		tags := make(map[string]string)
		tags["name"] = fileNameWithPrefix
		tags["description"] = it.Description
		tags["collection"] = it.Collection
		tags["album"] = it.Album
		tags["isDeleted"] = strconv.FormatBool(it.IsDeleted)

		// set album & collection image tags
		if it.CollectionImage {
			tags["collectionImage"] = "true"
		} else {
			tags["collectionImage"] = "false"
		}

		if it.AlbumImage {
			tags["albumImage"] = "true"
		} else {
			tags["albumImage"] = "false"
		}

		file, err := fh[0].Open()
		if err != nil {
			slog.Error("error opening file", "filename", fh[0].Filename, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}

		buf := bytes.NewBuffer(nil)
		if _, err := io.Copy(buf, file); err != nil {
			slog.Error("error copying to buffer", "filename", fh[0].Filename, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}

		img, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			log.Fatalln(err)
		}

		exifData := string("")
		exifData, err = exif.GetExifJSON(*buf)
		if err != nil {
			slog.Error("error getting exif data", "error", err)
			//http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}

		md := make(map[string]string)
		md["height"] = fmt.Sprint(img.Height)
		md["width"] = fmt.Sprint(img.Width)
		md["size"] = strconv.Itoa(int(fh[0].Size))
		md["exifData"] = exifData

		utils.SaveBlobStreamWithTagsMetadataAndContentType(
			client,
			ctx,
			buf.Bytes(),
			fileNameWithPrefix,
			uploadsContainerName,
			storageUrl,
			tags,
			md,
			it.Type,
		)
	}
}

func collectionHandler(client *azblob.Client, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		// get photos with matching collection tags
		var filteredBlobs []models.Blob

		query := fmt.Sprintf("@container='%s' and collectionImage='true'", imagesContainerName)
		slog.Debug("query", "query", query)
		filteredBlobs, err := queryBlobsByTags(client, storageUrl, query)
		if err != nil {
			slog.Error("Error getting blobs by tags", "error", err)
		}

		if len(filteredBlobs) <= 0 {
			// try a query without the collectionImage tag
			slog.Error("no collection images found, trying query without 'collectionImage' & will set a default placeholder collectionImage", "query", query)

			query := fmt.Sprintf("@container='%s'", imagesContainerName)
			filteredBlobs, err := queryBlobsByTags(client, storageUrl, query)
			if err != nil {
				slog.Error("Error getting blobs by tags", "error", err)
			}

			if len(filteredBlobs) <= 0 || filteredBlobs == nil {
				slog.Error("no collection images found", "query", query)
				http.Error(w, "No collection images found", http.StatusNotFound)
			} else {
				// set the first image as the collection image & write the tag back to the blob
				filteredBlobs[0].Tags["collectionImage"] = "true"
				err = utils.SetBlobTags(client, filteredBlobs[0].Name, imagesContainerName, storageUrl, filteredBlobs[0].Tags)
				if err != nil {
					slog.Error("error setting collectionImage tag", "error", err)
				}
			}
		}

		photos := []models.Photo{}

		if filteredBlobs == nil || len(filteredBlobs) <= 0 {
			http.Error(w, "No collection images found", http.StatusNotFound)
		}
		
		for _, r := range filteredBlobs {
			slog.Debug("Filtered Blobs", "Name", r.Name, "Metadata", r.MetaData, "Tags", r.Tags, "Path", r.Path)
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
				Width:      int(width),
				Height:     int(height),
				Album:      tags["album"],
				Collection: tags["collection"],
			}

			photos = append(photos, photo)
		}

		// retun JSON array of objects
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}

func albumHandler(client *azblob.Client, storageUrl string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		collection := r.PathValue("collection")
		if collection == "" {
			slog.Error("empty queryString", "name", "collection")
		}

		// get album placeholder photos with matching tags
		query := fmt.Sprintf("@container='%s' and collection='%s' and albumImage='true'", imagesContainerName, collection)
		filteredBlobs, err := queryBlobsByTags(client, storageUrl, query)
		if err != nil {
			slog.Error("Error getting blobs by tags", "error", err)
		}

		photos := []models.Photo{}

		for _, r := range filteredBlobs {
			slog.Debug("Filtered Blobs", "Name", r.Name, "Metadata", r.MetaData, "Tags", r.Tags, "Path", r.Path)
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
				Width:       int(width),
				Height:      int(height),
				Album:       tags["album"],
				Collection:  tags["collection"],
				Description: tags["description"],
			}

			photos = append(photos, photo)
		}

		// retun JSON array of objects
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(photos)
	}
}

func getCollectionImage(client *azblob.Client, storageUrl string, collection string) ([]models.Blob, error) {
	// get photos with matching collection tags
	query := fmt.Sprintf("@container='%s' and collection='%s' and collectionImage='true'", imagesContainerName, collection)
	filteredBlobs, err := queryBlobsByTags(client, storageUrl, query)
	if err != nil {
		slog.Error("Error getting blobs by tags", "error", err)
		return nil, err
	}

	if len(filteredBlobs) <= 0 {
		return []models.Blob{}, fmt.Errorf("no collection image found for collection: %s", collection)
	}

	return filteredBlobs, nil
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
	if len(blobs) <= 0 {	
		slog.Error("no blobs found", "query", query)
		return nil, fmt.Errorf("no blobs found for query: %s", query)
	}

	slog.Info("found blobs by tag query", "query", query, "num_blobs", len(blobs))
	return blobs, nil
}
