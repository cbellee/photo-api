package main

import (
	"log/slog"
	"path/filepath"

	recognizer "github.com/leandroveronezi/go-recognizer"
)

func DetectFaces(imageName string, modelsDir string, photosDir string, outputDir string) (outputImagePath string, e error) {
	rec := recognizer.Recognizer{}

	err := rec.Init(modelsDir)
	if err != nil {
		slog.Error(err.Error())
		return "", err
	}

	rec.Tolerance = 0.4
	rec.UseGray = true
	rec.UseCNN = false

	defer rec.Close()

	faces, err := rec.RecognizeMultiples(filepath.Join(photosDir, imageName))
	if err != nil {
		slog.Error(err.Error())
		return "", err
	}

	img, err := rec.DrawFaces2(filepath.Join(photosDir, imageName), faces)
	if err != nil {
		slog.Error(err.Error())
		return "", err
	}

	outputImagePath = filepath.Join(outputDir, "faces_"+imageName)
	slog.Info("saving imge to: " + outputImagePath)
	rec.SaveImage(outputImagePath, img)
	return outputImagePath, nil
}
