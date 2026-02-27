package main

// Config holds all application configuration for the resize service.
type Config struct {
	ServiceName         string
	ServicePort         string
	UploadsQueueBinding string
	AzureClientID       string
	ImagesContainerName string
	MaxImageHeight      int
	MaxImageWidth       int
	StorageAccount      string
	StorageSuffix       string
	StorageContainer    string
}
