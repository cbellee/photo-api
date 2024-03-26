package main

import (
	"context"
	"fmt"
	"log/slog"
	"models"
	"net/url"
	"os"
	"strconv"
	"strings"
	"utils"

	"github.com/dapr/go-sdk/service/common"
	daprd "github.com/dapr/go-sdk/service/grpc"
)

var (
	serviceName         = utils.GetEnvValue("SERVICE_NAME", "")
	servicePort         = utils.GetEnvValue("SERVICE_PORT", "")
	uploadsQueueBinding = utils.GetEnvValue("UPLOADS_QUEUE_BINDING", "")

	storageConfig = models.StorageConfig{
		StorageAccount:       utils.GetEnvValue("STORAGE_ACCOUNT_NAME", ""),
		StorageContainer:     utils.GetEnvValue("STORAGE_CONTAINER_NAME", ""),
		ThumbsContainerName:  utils.GetEnvValue("THUMBS_CONTAINER_NAME", "thumbs"),
		UploadsContainerName: utils.GetEnvValue("THUMBS_CONTAINER_NAME", "uploads"),
		ImagesContainerName:  utils.GetEnvValue("THUMBS_CONTAINER_NAME", "images"),
		Suffix:               "blob.core.windows.net",
	}
)

func main() {
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
	mth, err := strconv.Atoi(utils.GetEnvValue("MAX_THUMB_HEIGHT", "300"))
	if err != nil {
		slog.Error(err.Error())
	}
	mtw, err := strconv.Atoi(utils.GetEnvValue("MAX_THUMB_WIDTH", "300"))
	if err != nil {
		slog.Error(err.Error())
	}
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

	blobStream, err := utils.GetBlobStream(ctx, blobPath, container, storageConfig.StorageAccount, storageConfig.Suffix)
	if err != nil {
		slog.Error("error getting blob stream", "blob", blobPath, "error", err)
		return nil, err
	}

	tags, err := utils.GetBlobTags(blobPath, container, storageConfig.StorageAccount, storageConfig.Suffix)
	if err != nil {
		slog.Error("error getting blob tags", "blob", blobPath, "error", err)
		return nil, err
	}
	slog.Info("found blob tags", "blob_path", blobPath, "tags", tags)

	metadata, err := utils.GetBlobMetadata(blobPath, container, storageConfig.StorageAccount, storageConfig.Suffix)
	if err != nil {
		slog.Error("error getting blob metadata", "blob", blobPath, "error", err)
		return nil, err
	}
	slog.Info("found blob metadata", "blob_path", blobPath, "metadata", metadata)

	slog.Info("blob to resize", "blob_path", blobPath, "original_size", fmt.Sprint(len(blobStream.Bytes())))
	thumbBytes, err := utils.ResizeImage(blobStream.Bytes(), evt.Data.ContentType, blobPath, mth, mtw)
	if err != nil {
		slog.Error("error resizing image", "blob_path", blobPath, "error", err)
		return nil, err
	}

	imgSize := len(thumbBytes)
	slog.Info("resized blob", "blob_path", blobPath, "new_size", imgSize)

	blobName, blobPrefix := utils.GetBlobNameAndPrefix(blobPath)

	tags["IsThumb"] = "true"
	tags["ThumbUrl"] = fmt.Sprintf("https://%s.%s/%s/%s", storageConfig.StorageAccount, storageConfig.Suffix, storageConfig.ThumbsContainerName, blobPath)
	tags["Url"] = fmt.Sprintf("https://%s.%s/%s/%s", storageConfig.StorageAccount, storageConfig.Suffix, storageConfig.ImagesContainerName, blobPath)
	tags["Name"] = blobPrefix
	slog.Info("added blob tags", "blob_name", blobName, "tags", tags)

	// write thumbnail to blob storage

	// add image size to existing tags
	imgSizeStr := strconv.Itoa(imgSize)
	metadata["Size"] = imgSizeStr

	slog.Info("added blob metadata", "blob_name", blobName, "metadata", metadata)

	err = utils.SaveBlobStreamWithTagsAndMetadata(ctx, thumbBytes, blobPath, storageConfig.ThumbsContainerName, storageConfig.StorageAccount, storageConfig.Suffix, tags, metadata)
	if err != nil {
		slog.Error("error saving blob", "blob_path", blobPath, "error", err)
		return nil, err
	}

	// resize main image
	imgBytes, err := utils.ResizeImage(blobStream.Bytes(), evt.Data.ContentType, blobPath, mih, miw)
	if err != nil {
		slog.Error("error resizing image", "blob_path", blobPath, "error", err)
		return nil, err
	}

	tags["IsThumb"] = "false"

	err = utils.SaveBlobStreamWithTagsAndMetadata(ctx, imgBytes, blobPath, storageConfig.ImagesContainerName, storageConfig.StorageAccount, storageConfig.Suffix, tags, metadata)
	if err != nil {
		slog.Error("error saving blob", "blob_path", blobPath, "error", err)
		return nil, err
	}

	return nil, nil
}
