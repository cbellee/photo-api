package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	recognizer "github.com/leandroveronezi/go-recognizer"
)

func addFile(rec *recognizer.Recognizer, Path, Id string) (count int) {
	err := rec.AddImageToDataset(Path, Id)
	if err != nil {
		slog.Error(err.Error())
		return
	}
	return 1
}

func RecogniseFaces(imageName string, modelsDir string, samplesDir string, photosDir string) (outputImagePath string, e error) {
	rec := recognizer.Recognizer{}
	err := rec.Init(modelsDir)
	slog.Info("modelsDir: " + modelsDir)

	if err != nil {
		slog.Error(err.Error())
		return
	}

	rec.Tolerance = 0.4
	rec.UseGray = true
	rec.UseCNN = false
	defer rec.Close()

	err = addSampleImages(samplesDir, rec)
	if err != nil {
		slog.Error(err.Error())
		return "", err
	}

	rec.SetSamples()
	faces, err := rec.ClassifyMultiples(filepath.Join(photosDir, imageName))
	if err != nil {
		slog.Error(err.Error())
		return
	}

	outputImagePath = filepath.Join(photosDir, imageName)
	if len(faces) > 0 {
		names := make([]string, 0)
		for _, face := range faces {
			names = append(names, face.Data.Id)
		}
		SetExifDescription(outputImagePath, fmt.Sprint(strings.Join(names[:], ";")))
	}

	return outputImagePath, nil
}

func addSampleImages(samplesDir string, rec recognizer.Recognizer) (err error) {
	counter := 0
	samples, err := os.ReadDir(samplesDir)
	if err != nil {
		slog.Error("error reading samples directory: %v", err.Error())
		return err
	}

	for _, file := range samples {
		slog.Info("Adding sample image: " + file.Name())
		personName := strings.Split(file.Name(), "_")[0]
		slog.Info("Person name: " + personName)
		counter += addFile(&rec, filepath.Join(samplesDir, file.Name()), personName)
	}
	slog.Info("Added " + fmt.Sprint(counter) + " sample images")
	return nil
}
