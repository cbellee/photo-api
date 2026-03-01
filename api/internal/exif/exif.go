package exif

import (
	"bytes"
	"log/slog"

	"github.com/rwcarlsen/goexif/exif"
)

// GetExifJSON extracts EXIF metadata from raw image bytes and returns it as a JSON string.
func GetExifJSON(data []byte) (string, error) {
	exMeta, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		slog.Error("error reading exif data", "error", err)
		return "", err
	}

	jsonByte, err := exMeta.MarshalJSON()
	if err != nil {
		slog.Error("error marshalling exif metadata to JSON")
		return "", err
	}

	jsonString := string(jsonByte)
	return jsonString, nil
}
