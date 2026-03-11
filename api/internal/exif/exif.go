package exif

import (
	"fmt"
	"io"

	"github.com/rwcarlsen/goexif/exif"
)

// GetExifJSON extracts EXIF metadata from an image reader and returns it as a JSON string.
// The caller should provide a reader positioned at the start of the image data.
func GetExifJSON(r io.Reader) (string, error) {
	exMeta, err := exif.Decode(r)
	if err != nil {
		return "", fmt.Errorf("reading exif data: %w", err)
	}

	jsonByte, err := exMeta.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("marshalling exif metadata to JSON: %w", err)
	}

	return string(jsonByte), nil
}
