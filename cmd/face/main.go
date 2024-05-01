package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"github.com/cbellee/photo-api/internal/models"
	"os"
	"path/filepath"
	"strings"
	"github.com/cbellee/photo-api/internal/utils"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/dapr/go-sdk/service/common"
	daprd "github.com/dapr/go-sdk/service/grpc"
)

var (
	serviceName = os.Getenv("SERVICE_NAME")
	servicePort = os.Getenv("SERVICE_PORT")

	uploadsQueueBinding = utils.GetEnvValue("UPLOADS_QUEUE_BINDING", "")
	photosDir           = utils.GetEnvValue("PHOTOS_DIR", "photos")
	modelsDir           = utils.GetEnvValue("MODELS_DIR", "models")
	samplesDir          = utils.GetEnvValue("SAMPLES_DIR", "samples")

	storageConfig = models.StorageConfig{
		StorageAccount:   utils.GetEnvValue("STORAGE_ACCOUNT_NAME", ""),
		StorageContainer: utils.GetEnvValue("STORAGE_CONTAINER_NAME", ""),
	}
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	port := fmt.Sprintf(":%s", servicePort)
	slog.Info("starting service", "name", serviceName, "port", servicePort)

	s, err := daprd.NewService(port)
	if err != nil {
		slog.Error("failed starting service", "name", serviceName, "port", port, "error", err)
		return
	}

	// add storage queue input binding invocation handler
	if err := s.AddBindingInvocationHandler(uploadsQueueBinding, faceRecogniserHandler); err != nil {
		slog.Error("error adding storage queue binding handler", "error", err)
		return
	}

	slog.Info("server started", "name", serviceName, "port", port)
}

func faceRecogniserHandler(ctx context.Context, in *common.BindingEvent) (out []byte, err error) {
	evt, err := utils.ConvertToEvent(in)
	if err != nil {
		slog.Error("error converting BindingEvent to struct", "error", err)
	}

	slog.Info("binding", uploadsQueueBinding, "url", evt.Data.Url, "eventTime", evt.EventTime, "metadata", in.Metadata)

	storageUrl := fmt.Sprintf("https://%s/", storageConfig.StorageAccount)
	blobPath := strings.Split(evt.Data.Url, "https://")[1]
	blobNameTemp := strings.Split(blobPath, "/")
	blobName := blobNameTemp[len(blobNameTemp)-1]
	blobContainer := strings.Split(blobPath, "/")[1]

	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		slog.Error("Invalid credentials", "error", err)
		return nil, err
	}

	client, err := azblob.NewClient(storageUrl, credential, &azblob.ClientOptions{})
	if err != nil {
		slog.Error("error creating blob client", "error", err)
		return nil, err
	}

	blob, err := client.DownloadStream(ctx, blobName, blobContainer, &azblob.DownloadStreamOptions{})
	if err != nil {
		slog.Error("error downloading blob", "error", err)
		return nil, err
	}

	defer blob.Body.Close()
	blobData := bytes.Buffer{}
	retryReader := blob.NewRetryReader(ctx, &azblob.RetryReaderOptions{})

	_, err = blobData.ReadFrom(retryReader)
	if err != nil {
		slog.Error("error creading blob", "error", err)
	}

	err = retryReader.Close()
	if err != nil {
		slog.Error("error closing blob", "error", err)
	}

	localFilePath := filepath.Join(photosDir, blobName)
	err = os.WriteFile(localFilePath, blobData.Bytes(), 0644)
	if err != nil {
		slog.Error("error writing blob: %s", err)
	}

	result, err := RecogniseFaces(localFilePath, modelsDir, samplesDir, photosDir)
	if err != nil {
		slog.Error("error recognising faces", "file", localFilePath, "error", err)
	}

	slog.Info("face recognition result", result)
	return blobData.Bytes(), nil
}
