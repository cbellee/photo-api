package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"

	exif "github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
	jpeg "github.com/dsoprea/go-jpeg-image-structure/v2"
)

func setExifTag(rootIB *exif.IfdBuilder, ifdPath, tagName, tagValue string) error {
	slog.Info("setExifTag(): ifdPath: %v, tagName: %v, tagValue: %v",
		ifdPath, tagName, tagValue)

	ifdIb, err := exif.GetOrCreateIbFromRootIb(rootIB, ifdPath)
	if err != nil {
		return fmt.Errorf("failed to get or create IB: %v", err)
	}

	if err := ifdIb.SetStandardWithName(tagName, tagValue); err != nil {
		return fmt.Errorf("failed to set tag '%s': %v", tagName, err)
	}

	return nil
}

func SetExifDescription(filepath string, description string) error {
	parser := jpeg.NewJpegMediaParser()
	intfc, err := parser.ParseFile(filepath)
	if err != nil {
		slog.Error("failed to parse JPEG file: %v", err)
		return err
	}

	sl := intfc.(*jpeg.SegmentList)

	rootIb, err := sl.ConstructExifBuilder()
	if err != nil {
		slog.Info("No EXIF; creating it from scratch")

		im, err := exifcommon.NewIfdMappingWithStandard()
		if err != nil {
			return fmt.Errorf("failed to create new IFD mapping with standard tags: %v", err)
		}
		ti := exif.NewTagIndex()
		if err := exif.LoadStandardTags(ti); err != nil {
			return fmt.Errorf("failed to load standard tags: %v", err)
		}

		rootIb = exif.NewIfdBuilder(im, ti, exifcommon.IfdStandardIfdIdentity,
			exifcommon.EncodeDefaultByteOrder)
		rootIb.AddStandardWithName("ProcessingSoftware", "photos-uploader")
	}

	// Set Description
	ifdPath := "IFD0"
	if err := setExifTag(rootIb, ifdPath, "ImageDescription", description); err != nil {
		return fmt.Errorf("failed to set tag %v: %v", "ImageDescription", err)
	}

	// Update the exif segment.
	if err := sl.SetExif(rootIb); err != nil {
		return fmt.Errorf("failed to set EXIF to jpeg: %v", err)
	}

	// Write the modified file
	b := new(bytes.Buffer)
	if err := sl.Write(b); err != nil {
		return fmt.Errorf("failed to create JPEG data: %v", err)
	}

	slog.Info("Number of image bytes: %v\n", len(b.Bytes()))

	// Save the file
	if err := os.WriteFile(filepath, b.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write JPEG file: %v", err)
	}

	slog.Info("Wrote exif tag '%s' to file: %v\n", "description", filepath)

	return nil
}
