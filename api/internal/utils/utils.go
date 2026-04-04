package utils

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/cbellee/photo-api/internal/models"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	jwksKeyfunc "github.com/MicahParks/keyfunc/v3"
	"github.com/dapr/go-sdk/service/common"
	jwtLib "github.com/golang-jwt/jwt/v5"
	"golang.org/x/image/draw"
)

// CreateAzureBlobClient builds an [azblob.Client] using the best available
// credential strategy:
//
//  1. STORAGE_CONNECTION_STRING env var (local Docker / Azurite).
//  2. ManagedIdentityCredential when isProduction is true.
//  3. AzureCLICredential for all other environments.
func CreateAzureBlobClient(storageUrl string, isProduction bool, azureClientId string) (*azblob.Client, error) {
	// Connection-string auth (e.g. local Docker / Azurite).
	if connStr := os.Getenv("STORAGE_CONNECTION_STRING"); connStr != "" {
		slog.Info("STORAGE_CONNECTION_STRING found, using connection string auth")
		client, err := azblob.NewClientFromConnectionString(connStr, nil)
		if err != nil {
			return nil, fmt.Errorf("creating blob client from connection string: %w", err)
		}
		return client, nil
	}

	if isProduction {
		if azureClientId == "" {
			return nil, fmt.Errorf("azureClientId is required in production")
		}
		slog.Info("Azure Container App environment detected, using 'ManagedIdentityCredential'")

		clientID := azidentity.ClientID(azureClientId)
		credential, err := azidentity.NewManagedIdentityCredential(&azidentity.ManagedIdentityCredentialOptions{
			ID: clientID,
		})
		if err != nil {
			return nil, fmt.Errorf("creating managed identity credential: %w", err)
		}
		client, err := azblob.NewClient(storageUrl, credential, nil)
		if err != nil {
			return nil, fmt.Errorf("creating blob client with managed identity: %w", err)
		}
		return client, nil
	}

	// Non-production: use the Azure CLI credential.
	slog.Info("Other environment detected, using 'AzureCLICredential'")
	credential, err := azidentity.NewAzureCLICredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure CLI credential: %w", err)
	}
	client, err := azblob.NewClient(storageUrl, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("creating blob client with CLI credential: %w", err)
	}
	return client, nil
}

// ResizeImage scales imgBytes so the longer dimension fits within
// maxWidth/maxHeight while preserving the aspect ratio.
// It decodes the image only once (using image.DecodeConfig for cheap
// dimension lookup) and returns the re-encoded result.
func ResizeImage(imgBytes []byte, imageFormat string, blobName string, maxHeight int, maxWidth int) ([]byte, error) {
	// Get dimensions from the image header without a full decode.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, fmt.Errorf("decode image config: %w", err)
	}

	height := cfg.Height
	width := cfg.Width

	var dst *image.RGBA
	if height > width { // portrait — fit to maxHeight
		newWidth := maxHeight * width / height
		dst = image.NewRGBA(image.Rect(0, 0, newWidth, maxHeight))
		slog.Info("resizing image", "name", blobName, "original_height", height, "original_width", width, "new_height", maxHeight, "new_width", newWidth)
	} else { // landscape or square — fit to maxWidth
		newHeight := maxWidth * height / width
		dst = image.NewRGBA(image.Rect(0, 0, maxWidth, newHeight))
		slog.Info("resizing image", "name", blobName, "original_height", height, "original_width", width, "new_height", newHeight, "new_width", maxWidth)
	}

	// Decode once with the format-specific decoder.
	var src image.Image
	switch imageFormat {
	case "image/jpeg":
		src, err = jpeg.Decode(bytes.NewReader(imgBytes))
	case "image/png":
		src, err = png.Decode(bytes.NewReader(imgBytes))
	case "image/gif":
		src, err = gif.Decode(bytes.NewReader(imgBytes))
	default:
		return nil, fmt.Errorf("unsupported image format: %s", imageFormat)
	}
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", imageFormat, err)
	}

	slog.Info("scaling image", "name", blobName, "format", imageFormat)
	draw.NearestNeighbor.Scale(dst, dst.Rect, src, src.Bounds(), draw.Over, nil)

	// Encode the scaled image.
	buf := new(bytes.Buffer)
	switch imageFormat {
	case "image/jpeg":
		err = jpeg.Encode(buf, dst, nil)
	case "image/png":
		err = png.Encode(buf, dst)
	case "image/gif":
		err = gif.Encode(buf, dst, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", imageFormat, err)
	}

	return buf.Bytes(), nil
}

func ConvertToEvent(b *common.BindingEvent) (models.Event, error) {
	var evt models.Event
	if b == nil {
		return evt, fmt.Errorf("binding event is nil")
	}
	if len(b.Data) == 0 {
		return evt, fmt.Errorf("binding event data is empty")
	}

	// Azure Queue / Dapr delivery can surface either base64-encoded JSON or raw
	// JSON depending on the producer and binding configuration. Accept both.
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(b.Data)))
	decodedLen, decodeErr := base64.StdEncoding.Decode(decoded, b.Data)
	if decodeErr == nil {
		if err := json.Unmarshal(decoded[:decodedLen], &evt); err == nil {
			slog.Info("unmarshalled event", "event_url", evt.Data.URL)
			return evt, nil
		}
	}

	if err := json.Unmarshal(b.Data, &evt); err != nil {
		if decodeErr != nil {
			return evt, fmt.Errorf("parsing binding event payload: base64 decode failed: %w; raw json unmarshal failed: %v", decodeErr, err)
		}
		return evt, fmt.Errorf("parsing binding event payload as raw json: %w", err)
	}
	slog.Info("unmarshalled event", "event_url", evt.Data.URL)

	return evt, nil
}

func GetEnvValue(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		slog.Debug("env var found", "key", key)
		return value
	}
	slog.Warn("env var not found, using default", "key", key)
	return fallback
}

// invalidTagCharsRe matches characters not allowed in Azure blob tags.
var invalidTagCharsRe = regexp.MustCompile(`[^a-zA-Z0-9\s\./+-:+_]`)

// StripInvalidTagCharacters removes characters that are not valid in Azure
// blob tag values.  Valid characters are: a-z A-Z 0-9 space + - . / : = _
func StripInvalidTagCharacters(value string) string {
	if value == "" {
		slog.Warn("tag value is empty")
		return ""
	}

	if invalidTagCharsRe.MatchString(value) {
		return invalidTagCharsRe.ReplaceAllString(value, "")
	}

	return value
}

func extractToken(r *http.Request) (string, error) {
	accessToken := r.Header.Get("Authorization")
	if accessToken == "" {
		return "", fmt.Errorf("no access token found in request")
	}

	parts := strings.SplitN(accessToken, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", fmt.Errorf("malformed Authorization header")
	}
	return parts[1], nil
}

func VerifyToken(r *http.Request, jwksURL string, kf jwtLib.Keyfunc) (*models.MyClaims, error) {
	tokenString, err := extractToken(r)
	if err != nil {
		return nil, err
	}

	// Use the provided keyfunc if available; otherwise fall back to a one-shot fetch.
	var cancel context.CancelFunc
	if kf == nil {
		ctx, c := context.WithCancel(context.Background())
		cancel = c
		k, err := jwksKeyfunc.NewDefaultCtx(ctx, []string{jwksURL})
		if err != nil {
			cancel()
			return nil, fmt.Errorf("creating JWKS keyfunc: %w", err)
		}
		kf = k.Keyfunc
	}
	if cancel != nil {
		defer cancel()
	}

	parsedToken, err := jwtLib.ParseWithClaims(tokenString, &models.MyClaims{}, kf)
	if err != nil {
		return nil, fmt.Errorf("parsing JWT: %w", err)
	}

	claims, ok := parsedToken.Claims.(*models.MyClaims)
	if !ok {
		return nil, fmt.Errorf("unexpected claims type in JWT")
	}

	slog.Debug("parsed token claims", "claims", claims)
	return claims, nil
}
