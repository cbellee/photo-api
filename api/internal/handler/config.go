package handler

// Config holds all application configuration previously stored in package-level variables.
type Config struct {
	ServiceName          string
	ServicePort          string
	UploadsContainerName string
	ImagesContainerName  string
	StorageUrl           string
	MemoryLimitMb        int64
	JwksURL              string
	RoleName             string
	CorsOrigins          []string
}
