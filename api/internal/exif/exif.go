package exif

import (
	"bytes"
	"fmt"

	"github.com/rwcarlsen/goexif/exif"
)

// GetExifJSON extracts EXIF metadata from raw image bytes and returns it as a JSON string.
func GetExifJSON(data []byte) (string, error) {
	exMeta, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("reading exif data: %w", err)
	}

	jsonByte, err := exMeta.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("marshalling exif metadata to JSON: %w", err)
	}

	return string(jsonByte), nil
}
