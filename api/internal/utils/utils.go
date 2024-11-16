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
	"strings"

	"github.com/cbellee/photo-api/internal/models"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/MicahParks/keyfunc/v3"
	"github.com/dapr/go-sdk/service/common"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/image/draw"
)

func CreateAzureBlobClient(storageUrl string, isProduction bool, azureClientId string) (client *azblob.Client, err error) {
	if isProduction {
		if azureClientId == "" {
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
		slog.Info("Other environment detected, using 'DefaultCredential'")
		credential, err := azidentity.NewDefaultAzureCredential(nil)
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

func ResizeImage(imgBytes []byte, imageFormat string, blobName string, maxHeight int, maxWidth int) (img []byte, err error) {
	var dst *image.RGBA
	var buf = new(bytes.Buffer)

	src, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return buf.Bytes(), err
	}

	height := src.Bounds().Dy()
	width := src.Bounds().Dx()

	if height > width { // if height > width, then the image is portrait so resize height to maxHeight
		newWidth := maxHeight * width / height
		dst = image.NewRGBA((image.Rect(0, 0, newWidth, maxHeight)))
		slog.Info("resizing image", "name", blobName, "original_height", height, "original_width", width, "new_height", maxHeight, "new_width", newWidth)
	} else { // if height <= width, then the image is landscape or square so resize width to maxWidth
		newHeight := maxWidth * height / width
		dst = image.NewRGBA((image.Rect(0, 0, maxWidth, newHeight)))
		slog.Info("resizing image", "name", blobName, "original_height", height, "original_width", width, "new_height", newHeight, "new_width", maxWidth)
	}

	// detect image type from 'imageFormat' value
	switch imageFormat {
	case "image/jpeg":
		slog.Info("encoding jpeg", "name", blobName, "format", imageFormat)
		src, _ = jpeg.Decode(bytes.NewReader(imgBytes))
		draw.NearestNeighbor.Scale(dst, dst.Rect, src, src.Bounds(), draw.Over, nil)
		err := jpeg.Encode(buf, dst, nil)
		if err != nil {
			slog.Error("error encoding jpeg", "name", blobName, "error", err)
			return nil, err
		}
	case "image/png":
		slog.Info("encoding jpeg", "name", blobName, "format", imageFormat)
		src, _ = png.Decode(bytes.NewReader(imgBytes))
		draw.NearestNeighbor.Scale(dst, dst.Rect, src, src.Bounds(), draw.Over, nil)
		err := png.Encode(buf, dst)
		if err != nil {
			slog.Error("error encoding png", "name", blobName, "error", err)
			return nil, err
		}
	case "image/gif":
		slog.Info("encoding jpeg", "name", blobName, "format", imageFormat)
		src, _ = gif.Decode(bytes.NewReader(imgBytes))
		draw.NearestNeighbor.Scale(dst, dst.Rect, src, src.Bounds(), draw.Over, nil)
		err := gif.Encode(buf, dst, nil)
		if err != nil {
			slog.Error("error encoding gif", "name", blobName, "error", err)
			return nil, err
		}
	}
	return buf.Bytes(), err
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
		return value
	}
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

	slog.Info("got blob tags", "blob", blobPath, "tags", tagResponse.BlobTags)
	tags = make(map[string]string)
	for _, t := range tagResponse.BlobTags.BlobTagSet {
		tags[*t.Key] = *t.Value
	}

	return tags, nil
}

func GetBlobMetadata(client *azblob.Client, blobPath string, container string, storageUrl string) (metadata map[string]string, err error) {
	ctx := context.Background()
	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blobClient := client.ServiceClient().NewContainerClient(container).NewBlockBlobClient(blobPath)

	if err != nil {
		slog.Error("error creating block blob client", "blob_url", blobUrl, "error", err)
		return nil, err
	}

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

	slog.Info("got blob metadata", "blob", blobPath, "metadata", m)
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
				if *t.Key == "Collection" {
					collection = *t.Value
				}

				if *t.Key == "Album" {
					album = *t.Value
				}
			}

			if !Contains(blobTagMap[collection], album) {
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

	slog.Info("blob stream", "blob_url", blobUrl, "bytes_read", bytesRead)
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

	slog.Info("uploading blob with tags and metadata", "url", blobUrl, "tags", tags, "metadata", md)
	response, err := blockBlob.UploadStream(ctx, bytes.NewReader(blobBytes), &blockblob.UploadStreamOptions{
		Tags:     tags,
		Metadata: md,
	})
	if err != nil {
		slog.Error("error uploading blob stream", "blob_url", blobUrl, "error", err)
		return err
	}

	slog.Info("uploaded blob stream", "blob_url", blobUrl, "tags", tags, "metadata", metadata, "response", response)
	return nil
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
	slog.Info("content-type", "type", contentType)

	md := make(map[string]*string)
	for key, value := range metadata {
		v := value
		md[key] = &v
	}

	slog.Info("uploading blob with tags and metadata", "url", blobUrl, "tags", tags, "metadata", md)
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

	slog.Info("uploaded blob stream", "blob_url", blobUrl, "tags", tags, "metadata", metadata, "response", response)
	return nil
}

func GetBlobNameAndPrefix(blobPath string) (string, string) {
	blobSplit := strings.Split(blobPath, "/")
	slog.Info("blob_split", "split", blobSplit)

	blobName := blobSplit[len(blobSplit)-1]
	slog.Info("blob_name", "name", blobName)

	blobPrefix := fmt.Sprintf("%s/%s/%s", blobSplit[len(blobSplit)-3], blobSplit[len(blobSplit)-2], blobSplit[len(blobSplit)-1])
	slog.Info("blob_prefix", "prefix", blobPrefix)
	return blobName, blobPrefix
}

func Contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

func RoundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

func DumpEnv() {
	for _, e := range os.Environ() {
		fmt.Print(e)
	}
}

func extractToken(r *http.Request) (string, error) {
	accessToken := r.Header.Get("Authorization")
	if accessToken == "" {
		return "", fmt.Errorf("no access token found in request")
	}

	bearerToken := strings.Split(accessToken, " ")[1]
	return bearerToken, nil
}

func VerifyToken(r *http.Request, jwksURL string) (*models.MyClaims, error) {
	tokenString, err := extractToken(r)
	if err != nil {
		return nil, err
	}

	// Create a context that, when cancelled, ends the JWKS background refresh goroutine.
	ctx, cancel := context.WithCancel(context.Background())

	// Create the keyfunc.Keyfunc.
	k, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL}) // Context is used to end the refresh goroutine.
	if err != nil {
		slog.Error("Failed to create a keyfunc.Keyfunc from the server's URL.", "error", err)
	}

	claims := &models.MyClaims{}
	ok := false

	parsedToken, err := jwt.ParseWithClaims(tokenString, &models.MyClaims{}, k.Keyfunc)
	if err != nil {
		slog.Error("Error Parsing JWT", "error", err)
	} else if claims, ok = parsedToken.Claims.(*models.MyClaims); ok {
		fmt.Println(claims)
	} else {
		slog.Error("Error Parsing Claims", "error", err)
	}

	// End the background refresh goroutine when it's no longer needed.
	cancel()
	return claims, nil
}
