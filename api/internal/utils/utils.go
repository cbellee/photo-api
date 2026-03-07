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
	"math"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/cbellee/photo-api/internal/models"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	jwksKeyfunc "github.com/MicahParks/keyfunc/v3"
	"github.com/dapr/go-sdk/service/common"
	jwtLib "github.com/golang-jwt/jwt/v5"
	"golang.org/x/image/draw"
)

func CreateAzureBlobClient(storageUrl string, isProduction bool, azureClientId string) (client *azblob.Client, err error) {
	// If a connection string is provided (e.g. for local Docker), use it directly.
	if connStr := os.Getenv("STORAGE_CONNECTION_STRING"); connStr != "" {
		slog.Info("STORAGE_CONNECTION_STRING found, using connection string auth")
		client, err := azblob.NewClientFromConnectionString(connStr, nil)
		if err != nil {
			slog.Error("error creating blob client from connection string", "error", err)
			return nil, err
		}
		return client, nil
	}

	if isProduction {
		if azureClientId == "" {
			slog.Error("azureClientId is required in production")
			return nil, fmt.Errorf("azureClientId is required in production")
		}

		// use managed identity for authentication to avoid default short timeout
		var err error
		slog.Info("Azure Container App environment detected, using 'ManagedIdentityCredential'")

		clientId := azidentity.ClientID(azureClientId)
		opt := azidentity.ManagedIdentityCredentialOptions{
			ID: clientId,
		}

		credential, err := azidentity.NewManagedIdentityCredential(&opt)
		if err != nil {
			slog.Error("invalid DefaultCredential", "error", err)
			return nil, err
		} else {
			client, err = azblob.NewClient(storageUrl, credential, nil)
			if err != nil {
				slog.Error("error creating blob client", "error", err)
				return nil, err
			}
		}
	} else {
		// any othger environment detected
		var err error
		slog.Info("Other environment detected, using 'AzureCLICredential'")
		credential, err := azidentity.NewAzureCLICredential(nil)
		if err != nil {
			slog.Error("invalid credentials", "error", err)
			return nil, err
		} else {
			client, err = azblob.NewClient(storageUrl, credential, nil)
			if err != nil {
				slog.Error("error creating blob client", "error", err)
				return nil, err
			}
		}
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

	byt := make([]byte, base64.StdEncoding.DecodedLen(len(b.Data)))
	l, err := base64.StdEncoding.Decode(byt, b.Data)
	if err != nil {
		return evt, err
	}

	err = json.Unmarshal(byt[:l], &evt)
	if err != nil {
		return evt, err
	}
	slog.Info("unmarshalled event", "event_url", evt.Data.Url)

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

func GetBlobDirectories(containerClient *container.Client, ctx context.Context, opt container.ListBlobsHierarchyOptions, m map[string][]string) map[string][]string {
	pager := containerClient.NewListBlobsHierarchyPager("/", &opt)

	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			slog.Error("error while listing blobs", "error", err)
		}

		segment := resp.Segment
		if segment.BlobPrefixes != nil {
			for _, prefix := range segment.BlobPrefixes {
				str := strings.Split(strings.Trim(*prefix.Name, "/"), "/")
				if len(str) > 1 {
					m[str[0]] = append(m[str[0]], strings.Trim(str[1], "/"))
				}

				opt := container.ListBlobsHierarchyOptions{
					Prefix: prefix.Name,
				}
				GetBlobDirectories(containerClient, ctx, opt, m)
			}
		}
	}
	return m
}

func GetBlobTags(client *azblob.Client, blobPath string, container string, storageUrl string) (tags map[string]string, err error) {
	ctx := context.Background()
	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blockBlob := client.ServiceClient().NewContainerClient(container).NewBlockBlobClient(blobPath)

	// get blob tags
	tagResponse, err := blockBlob.GetTags(ctx, nil)
	if err != nil {
		slog.Error("error getting blob tags", "blob_url", blobUrl, "error", err)
		return nil, err
	}

	slog.Debug("got blob tags", "blob", blobPath, "tags", tagResponse.BlobTags)
	tags = make(map[string]string)
	for _, t := range tagResponse.BlobTags.BlobTagSet {
		tags[*t.Key] = *t.Value
	}

	return tags, nil
}

func SetBlobTags(client *azblob.Client, blobPath string, container string, storageUrl string, tags map[string]string) (err error) {
	ctx := context.Background()
	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blockBlob := client.ServiceClient().NewContainerClient(container).NewBlockBlobClient(blobPath)

	// set blob tags
	slog.Info("setting blob tags", "blob", blobPath)
	slog.Debug("tags", "blob", blobPath, "tags", tags)
	setTagResponse, err := blockBlob.SetTags(ctx, tags, nil)
	if err != nil {
		slog.Error("error setting blob tags", "blob_url", blobUrl, "error", err)
		return err
	}

	slog.Debug("set blob tags", "blob", blobPath, "tags", tags, "response", setTagResponse)
	return nil
}

func GetBlobMetadata(client *azblob.Client, blobPath string, container string, storageUrl string) (metadata map[string]string, err error) {
	ctx := context.Background()
	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blobClient := client.ServiceClient().NewContainerClient(container).NewBlockBlobClient(blobPath)

	// get blob tags
	mdResponse, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		slog.Error("error getting blob metadata", "blob_url", blobUrl, "error", err)
		return nil, err
	}

	m := make(map[string]string)
	for key, value := range mdResponse.Metadata {
		v := value
		m[key] = *v
	}

	slog.Debug("got blob metadata", "blob", blobPath, "metadata", m)
	return m, nil
}

func GetBlobTagList(client *azblob.Client, containerName string, storageUrl string, ctx context.Context) (map[string][]string, error) {
	pager := client.NewListBlobsFlatPager(containerName, &azblob.ListBlobsFlatOptions{
		Include: container.ListBlobsInclude{
			Deleted:  false,
			Versions: false,
			Metadata: false,
			Tags:     true,
		},
	})

	blobTagMap := make(map[string][]string)

	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			slog.Error("error while listing blobs", "error", err)
			break
		}

		for _, _blob := range resp.Segment.BlobItems {
			tags := *_blob.BlobTags
			album := ""
			collection := ""

			for _, t := range tags.BlobTagSet {
				if *t.Key == "collection" {
					collection = *t.Value
				}

				if *t.Key == "album" {
					album = *t.Value
				}
			}

			if !slices.Contains(blobTagMap[collection], album) {
				blobTagMap[collection] = append(blobTagMap[collection], album)
			}
		}
	}
	return blobTagMap, nil
}

func GetBlobStream(client *azblob.Client, ctx context.Context, blobPath string, container string, storageUrl string) (bytes.Buffer, error) {
	buffer := bytes.Buffer{}
	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blockBlob := client.ServiceClient().NewContainerClient(container).NewBlockBlobClient(blobPath)

	// ensure blob exists
	_, err := blockBlob.GetProperties(ctx, nil)
	if err != nil {
		slog.Error("error getting blob properties", "blob_url", blobUrl, "error", err)
		return buffer, err // blob doesn't exist
	}

	// get blob stream
	blobStream, err := blockBlob.DownloadStream(ctx, &blob.DownloadStreamOptions{})
	if err != nil {
		slog.Error("error getting blob stream", "blob_url", blobUrl, "error", err)
		return buffer, err
	}

	bytesRead, err := buffer.ReadFrom(blobStream.NewRetryReader(ctx, &azblob.RetryReaderOptions{}))
	if err != nil {
		slog.Error("error reading blob stream", "blob_url", blobUrl, "error", err, "bytes_read", bytesRead)
		return buffer, err
	}

	if err != nil {
		slog.Error("error reading blob stream", "blob_url", blobUrl, "error", err, "bytes_read", bytesRead)
	}

	slog.Info("got blob stream", "blob_url", blobUrl, "bytes_read", bytesRead)
	return buffer, nil
}

func SaveBlobStreamWithTagsAndMetadata(
	client *azblob.Client,
	ctx context.Context,
	blobBytes []byte,
	blobPath string,
	container string,
	storageUrl string,
	tags map[string]string,
	metadata map[string]string) (err error) {
	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blockBlob := client.ServiceClient().NewContainerClient(container).NewBlockBlobClient(blobPath)

	md := make(map[string]*string)
	for key, value := range metadata {
		v := value
		md[key] = &v
	}

	response, err := blockBlob.UploadStream(ctx, bytes.NewReader(blobBytes), &blockblob.UploadStreamOptions{
		Tags:     tags,
		Metadata: md,
	})
	if err != nil {
		slog.Error("error uploading blob stream", "blob_url", blobUrl, "error", err)
		return err
	}

	slog.Info("uploaded blob stream", "url", blobUrl)
	slog.Debug("uploaded blob stream", "blob_url", blobUrl, "tags", tags, "metadata", metadata, "response", response)
	return nil
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

func SaveBlobStreamWithTagsMetadataAndContentType(
	client *azblob.Client,
	ctx context.Context,
	blobBytes []byte,
	blobPath string,
	container string,
	storageUrl string,
	tags map[string]string,
	metadata map[string]string,
	contentType string) (err error) {

	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blockBlob := client.ServiceClient().NewContainerClient(container).NewBlockBlobClient(blobPath)
	slog.Debug("content-type", "type", contentType)

	md := make(map[string]*string)
	for key, value := range metadata {
		v := value
		md[key] = &v
	}

	response, err := blockBlob.UploadStream(ctx, bytes.NewReader(blobBytes), &blockblob.UploadStreamOptions{
		Tags:     tags,
		Metadata: md,
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: &contentType,
		},
	})
	if err != nil {
		slog.Error("error uploading blob stream", "blob_url", blobUrl, "error", err)
		return err
	}

	slog.Debug("uploaded blob stream", "blob_url", blobUrl, "tags", tags, "metadata", metadata, "response", response)
	return nil
}

func GetBlobNameAndPrefix(blobPath string) (string, string) {
	blobSplit := strings.Split(blobPath, "/")
	slog.Debug("blob_split", "split", blobSplit)

	blobName := blobSplit[len(blobSplit)-1]
	slog.Debug("blob_name", "name", blobName)

	blobPrefix := fmt.Sprintf("%s/%s/%s", blobSplit[len(blobSplit)-3], blobSplit[len(blobSplit)-2], blobSplit[len(blobSplit)-1])
	slog.Debug("blob_prefix", "prefix", blobPrefix)
	return blobName, blobPrefix
}

// Deprecated: Contains is replaced by slices.Contains from the standard library.
// Kept temporarily for backward compatibility.
func Contains(s []string, str string) bool {
	return slices.Contains(s, str)
}

func RoundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

// DumpEnv is intentionally removed — logging environment variables
// can expose secrets. Use targeted GetEnvValue calls instead.
func DumpEnv() {
	slog.Debug("DumpEnv called but disabled for security")
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
			slog.Error("failed to create keyfunc from JWKS URL", "error", err)
			return nil, fmt.Errorf("creating JWKS keyfunc: %w", err)
		}
		kf = k.Keyfunc
	}
	if cancel != nil {
		defer cancel()
	}

	parsedToken, err := jwtLib.ParseWithClaims(tokenString, &models.MyClaims{}, kf)
	if err != nil {
		slog.Error("error parsing JWT", "error", err)
		return nil, fmt.Errorf("parsing JWT: %w", err)
	}

	claims, ok := parsedToken.Claims.(*models.MyClaims)
	if !ok {
		return nil, fmt.Errorf("unexpected claims type in JWT")
	}

	slog.Debug("parsed token claims", "claims", claims)
	return claims, nil
}

func ListBlobHierarchy(client *azblob.Client, storageUrl string, containerName string, prefix *string, blobMap map[string]string) (err error) {
	serviceClient := client.ServiceClient()
	maxResults := int32(500)

	pager := serviceClient.NewContainerClient(containerName).NewListBlobsHierarchyPager("/", &container.ListBlobsHierarchyOptions{
		Include:    container.ListBlobsInclude{Metadata: true, Tags: true},
		Prefix:     prefix,
		MaxResults: &maxResults,
	})

	for pager.More() {
		resp, err := pager.NextPage(context.TODO())
		if err != nil {
			slog.Error("failed to list blobs", "error", err)
		}

		/* for _, item := range resp.Segment.BlobItems {
			fmt.Printf("Blob: %s\n", *item.Name)
		} */

		for _, prefix := range resp.Segment.BlobPrefixes {
			slog.Debug("virtual directory", "name", *prefix.Name)
			blobMap[*prefix.Name] = ""
			slog.Debug("blob map", "map", blobMap)
			ListBlobHierarchy(client, storageUrl, containerName, prefix.Name, blobMap)
		}
	}

	return nil
}
