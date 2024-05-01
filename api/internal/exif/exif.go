package exif

import (
	"bytes"
	"log/slog"

	"github.com/rwcarlsen/goexif/exif"
)

func GetExifJSON(image bytes.Buffer) string {
	exMeta, err := exif.Decode(bytes.NewReader(image.Bytes()))
	if err != nil {
		slog.Error("error reading exif data", "error", err)
		return ""
	}

	jsonByte, err := exMeta.MarshalJSON()
	if err != nil {
		slog.Error("error marshalling exif metadata to JSON")
		return ""
	}

	jsonString := string(jsonByte)
	return jsonString
}

func SetExif(image bytes.Buffer) {

}
