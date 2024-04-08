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
	"models"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/dapr/go-sdk/service/common"
	"golang.org/x/image/draw"
)

func ResizeImage(imgBytes []byte, imageFormat string, blobName string, maxHeight int, maxWidth int) (img []byte, h int, w int, err error) {
	var dst *image.RGBA
	var buf = new(bytes.Buffer)

	src, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return buf.Bytes(), 0, 0, err
	}

	height := src.Bounds().Dy()
	width := src.Bounds().Dx()

	var outWidth = 0
	var outHeight = 0

	if height > width { // if height > width, then the image is portrait so resize height to maxHeight
		newWidth := maxHeight * width / height
		outWidth = newWidth
		outHeight = height
		dst = image.NewRGBA((image.Rect(0, 0, newWidth, maxHeight)))
		slog.Info("resizing image", "name", blobName, "original_height", height, "original_width", width, "new_height", maxHeight, "new_width", newWidth)
	} else { // if height <= width, then the image is landscape or square so resize width to maxWidth
		newHeight := maxWidth * height / width
		outWidth = newHeight
		outHeight = width
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
			return nil, 0, 0, err
		}
	case "image/png":
		slog.Info("encoding jpeg", "name", blobName, "format", imageFormat)
		src, _ = png.Decode(bytes.NewReader(imgBytes))
		draw.NearestNeighbor.Scale(dst, dst.Rect, src, src.Bounds(), draw.Over, nil)
		err := png.Encode(buf, dst)
		if err != nil {
			slog.Error("error encoding png", "name", blobName, "error", err)
			return nil, 0, 0, err
		}
	case "image/gif":
		slog.Info("encoding jpeg", "name", blobName, "format", imageFormat)
		src, _ = gif.Decode(bytes.NewReader(imgBytes))
		draw.NearestNeighbor.Scale(dst, dst.Rect, src, src.Bounds(), draw.Over, nil)
		err := gif.Encode(buf, dst, nil)
		if err != nil {
			slog.Error("error encoding gif", "name", blobName, "error", err)
			return nil, 0, 0, err
		}
	}
	return buf.Bytes(), outHeight, outWidth, err
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

func GetBlobDirectories(credential *azidentity.DefaultAzureCredential, containerClient *container.Client, ctx context.Context, opt container.ListBlobsHierarchyOptions, m map[string][]string) map[string][]string {
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
				GetBlobDirectories(credential, containerClient, ctx, opt, m)
			}
		}
	}
	return m
}

func GetBlobTags(credential *azidentity.DefaultAzureCredential, blobPath string, container string, storageAccount string, storageAccountSuffix string) (tags map[string]string, err error) {
	ctx := context.Background()

	// check blob exists by trying to get blob properties
	storageUrl := fmt.Sprintf("https://%s.%s", storageAccount, storageAccountSuffix)
	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blockBlob, err := blockblob.NewClient(blobUrl, credential, nil)

	if err != nil {
		slog.Error("error creating block blob client", "blob_url", blobUrl, "error", err)
		return nil, err
	}

	_, err = blockBlob.GetProperties(ctx, nil)
	if err != nil {
		slog.Error("error getting blob properties", "blob_url", blobUrl, "error", err)
		return nil, err // blob doesn't exist
	}

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

func GetBlobMetadata(credential *azidentity.DefaultAzureCredential, blobPath string, container string, storageAccount string, storageAccountSuffix string) (metadata map[string]string, err error) {
	ctx := context.Background()

	// check blob exists by trying to get blob properties
	storageUrl := fmt.Sprintf("https://%s.%s", storageAccount, storageAccountSuffix)
	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blockBlob, err := blockblob.NewClient(blobUrl, credential, nil)

	if err != nil {
		slog.Error("error creating block blob client", "blob_url", blobUrl, "error", err)
		return nil, err
	}

	_, err = blockBlob.GetProperties(ctx, nil)
	if err != nil {
		slog.Error("error getting blob properties", "blob_url", blobUrl, "error", err)
		return nil, err // blob doesn't exist
	}

	// get blob tags
	mdResponse, err := blockBlob.GetProperties(ctx, nil)
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

func GetBlobTagList(blobClient *azblob.Client, containerName string, ctx context.Context) map[string][]string {
	pager := blobClient.NewListBlobsFlatPager(containerName, &azblob.ListBlobsFlatOptions{
		Include: container.ListBlobsInclude{
			Deleted:  false,
			Versions: false,
			Metadata: true,
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

			if !Contains(blobTagMap[collection], album) {
				blobTagMap[collection] = append(blobTagMap[collection], album)
			}
		}
	}
	return blobTagMap
}

func GetBlobStream(credential *azidentity.DefaultAzureCredential, ctx context.Context, blobPath string, container string, storageAccount string, storageAccountSuffix string) (bytes.Buffer, error) {
	buffer := bytes.Buffer{}

	// check blob exists by trying to get blob properties
	storageUrl := fmt.Sprintf("https://%s.%s", storageAccount, storageAccountSuffix)
	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blockBlob, err := blockblob.NewClient(blobUrl, credential, nil)

	if err != nil {
		slog.Error("error creating block blob client", "blob_url", blobUrl, "error", err)
		return buffer, err
	}

	_, err = blockBlob.GetProperties(ctx, nil)
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

func SaveBlobStreamWithTagsAndMetadata(credential *azidentity.DefaultAzureCredential, ctx context.Context, blobBytes []byte, blobPath string, container string, storageAccount string, storageAccountSuffix string, tags map[string]string, metadata map[string]string) (err error) {

	storageUrl := fmt.Sprintf("https://%s.%s", storageAccount, storageAccountSuffix)
	blobUrl := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobPath)
	blockBlob, err := blockblob.NewClient(blobUrl, credential, nil)
	if err != nil {
		slog.Error("error creating block blob client", "blob_url", blobUrl, "error", err)
		return err
	}

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

func GetExifData() {

}

func SetExifData(blobBytes []byte, name string, value string) {

}
