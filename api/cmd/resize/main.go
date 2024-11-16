package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"log"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/utils"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/dapr/go-sdk/service/common"
	daprd "github.com/dapr/go-sdk/service/grpc"
)

var (
	serviceName          = utils.GetEnvValue("SERVICE_NAME", "")
	servicePort          = utils.GetEnvValue("SERVICE_PORT", "")
	uploadsQueueBinding  = utils.GetEnvValue("UPLOADS_QUEUE_BINDING", "")
	azureClientId        = utils.GetEnvValue("AZURE_CLIENT_ID", "")
	imagesContainerName  = utils.GetEnvValue("IMAGES_CONTAINER_NAME", "images")

	storageConfig = models.StorageConfig{
		StorageAccount:       utils.GetEnvValue("STORAGE_ACCOUNT_NAME", ""),
		StorageAccountSuffix: utils.GetEnvValue("STORAGE_ACCOUNT_SUFFIX", "blob.core.windows.net"),
		StorageContainer:     utils.GetEnvValue("STORAGE_CONTAINER_NAME", ""),
	}
	isProduction = false
	client       *azblob.Client
)

func main() {
	// create a new logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	port := fmt.Sprintf(":%s", servicePort)
	s, err := daprd.NewService(port)
	if err != nil {
		slog.Error("failed to create daprd service", "error", err)
		return
	}

	storageUrl := fmt.Sprintf("https://%s.%s", storageConfig.StorageAccount, storageConfig.StorageAccountSuffix)
	slog.Info("storage url", "url", storageUrl)

	if _, exists := os.LookupEnv("CONTAINER_APP_NAME"); exists {
		isProduction = true
	} else {
		slog.Info("'CONTAINER_APP_NAME' env var not found, running in local environment")
	}

	client, err = utils.CreateAzureBlobClient(storageUrl, isProduction, azureClientId)
	if err != nil {
		slog.Error("error creating blob client", "error", err)
		return
	}

	// add storage queue input binding invocation handler
	if err := s.AddBindingInvocationHandler(uploadsQueueBinding, ResizeHandler); err != nil {
		slog.Error("error adding storage queue binding handler", "error", err)
		return
	}
	slog.Info("added binding handler", "name", uploadsQueueBinding)

	// start the service
	slog.Info("starting service", "name", serviceName, "port", servicePort)
	if err := s.Start(); err != nil {
		slog.Error("server failed to start", "error", err)
		return
	}
}

func ResizeHandler(ctx context.Context, in *common.BindingEvent) (out []byte, err error) {
	// get env Vars
	mih, err := strconv.Atoi(utils.GetEnvValue("MAX_IMAGE_HEIGHT", "1200"))
	if err != nil {
		slog.Error(err.Error())
	}
	miw, err := strconv.Atoi(utils.GetEnvValue("MAX_IMAGE_WIDTH", "1600"))
	if err != nil {
		slog.Error(err.Error())
	}

	evt, err := utils.ConvertToEvent(in)
	if err != nil {
		slog.Error("error converting BindingEvent to struct", "error", err)
	}

	slog.Info("input binding handler",
		"name", uploadsQueueBinding,
		"subject", evt.Subject,
		"topic", evt.Topic,
		"event_time", evt.EventTime,
		"id", evt.Id,
		"api", evt.Data.Api,
		"type", evt.EventType,
		"content_length", evt.Data.ContentLength,
		"content_type", evt.Data.ContentType,
		"etag", evt.Data.ETag,
		"metadata", in.Metadata,
		"url", evt.Data.Url,
	)

	u, err := url.Parse(evt.Data.Url)
	if err != nil {
		slog.Error("error parsing event", "error", err)
	}

	path := strings.Split(u.Path, "/")
	blobPath := fmt.Sprintf("%s/%s/%s", path[len(path)-3], path[len(path)-2], path[len(path)-1])
	container := path[len(path)-4]
	collection := path[len(path)-3]
	album := path[len(path)-2]

	slog.Info("tag data", "container", container, "blob_path", blobPath, "Album", album, "Collection", collection)

	blobStream, err := utils.GetBlobStream(client, ctx, blobPath, container, client.URL())
	if err != nil {
		slog.Error("error getting blob stream", "blob", blobPath, "error", err)
		return nil, err
	}

	// get tags
	tags, err := utils.GetBlobTags(client, blobPath, container, client.URL())
	if err != nil {
		slog.Error("error getting blob tags", "blob", blobPath, "error", err)
		return nil, err
	}
	slog.Info("found blob tags", "blob_path", blobPath, "tags", tags)

	// get metadata
	metadata, err := utils.GetBlobMetadata(client, blobPath, container, client.URL())
	if err != nil {
		slog.Error("error getting blob metadata", "blob", blobPath, "error", err)
		return nil, err
	}
	slog.Info("found blob metadata", "blob_path", blobPath, "metadata", metadata)

	// resize image
	numBytes := blobStream.Len()
	slog.Info("blobStream buffer bytes", "numBytes", numBytes)

	imgBytes, err := utils.ResizeImage(blobStream.Bytes(), evt.Data.ContentType, blobPath, mih, miw)
	if err != nil {
		slog.Error("error resizing image", "blob_path", blobPath, "error", err)
		return nil, err
	}

	img, _, err := image.DecodeConfig(bytes.NewReader(imgBytes))
	if err != nil {
		log.Fatalln(err)
	}

	numImgBytes := len(imgBytes)
	slog.Info("resized bytes", "numImageBytes", numImgBytes)
	slog.Info("resized image dimensions", "height", img.Height, "width", img.Width)

	// add metadata
	imgSize := len(imgBytes)
	imgSizeStr := strconv.Itoa(imgSize)
	metadata["Size"] = imgSizeStr
	metadata["Height"] = fmt.Sprint(img.Height)
	metadata["Width"] = fmt.Sprint(img.Width)

	blobName, _ := utils.GetBlobNameAndPrefix(blobPath)
	slog.Info("added blob metadata", "blob_name", blobName, "metadata", metadata)

	// add tags
	tags["Url"] = fmt.Sprintf("%s/%s/%s", client.URL(), imagesContainerName, blobPath)

	slog.Info("added blob tags", "blob_name", blobName, "tags", tags)

	// save resized image
	err = utils.SaveBlobStreamWithTagsAndMetadata(client, ctx, imgBytes, blobPath, imagesContainerName, client.URL(), tags, metadata)
	if err != nil {
		slog.Error("error saving blob", "blob_path", blobPath, "error", err)
		return nil, err
	}

	return nil, nil
}
