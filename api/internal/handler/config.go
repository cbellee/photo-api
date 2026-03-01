package handler

import "github.com/golang-jwt/jwt/v5"

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
	// JWTKeyfunc is a cached keyfunc created once at startup from the JwksURL.
	// If nil, VerifyToken will fall back to creating a one-shot keyfunc.
	JWTKeyfunc           jwt.Keyfunc
}
