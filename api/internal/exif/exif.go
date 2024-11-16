package exif

import (
	"bytes"
	"log/slog"

	"github.com/rwcarlsen/goexif/exif"
)

func GetExifJSON(image bytes.Buffer) (string, error) {
	exMeta, err := exif.Decode(bytes.NewReader(image.Bytes()))
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
