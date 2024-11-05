package utils

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResizeImage(t *testing.T) {
	// Create a dummy image
	img := image.NewRGBA(image.Rect(0, 0, 100, 200))
	cyan := color.RGBA{100, 200, 200, 0xff}

	for x := 0; x < img.Rect.Dx(); x++ {
		for y := 0; y < img.Rect.Dy(); y++ {
			img.Set(x, y, cyan)
		}
	}

	t.Run("portrait image", func(t *testing.T) {
		buf := new(bytes.Buffer)
		err := jpeg.Encode(buf, img, nil)
		assert.NoError(t, err)

		resizedImgBytes, err := ResizeImage(buf.Bytes(), "image/jpeg", "test.jpeg", 100, 50)
		assert.NoError(t, err)
		assert.NotEmpty(t, resizedImgBytes)

		resizedImg, _, err := image.Decode(bytes.NewReader(resizedImgBytes))
		assert.NoError(t, err)
		assert.Equal(t, 50, resizedImg.Bounds().Dx())
		assert.Equal(t, 100, resizedImg.Bounds().Dy())
	})

	t.Run("landscape image", func(t *testing.T) {
		buf := new(bytes.Buffer)
		err := png.Encode(buf, img)
		assert.NoError(t, err)

		resizedImgBytes, err := ResizeImage(buf.Bytes(), "image/png", "test.png", 50, 100)
		assert.NoError(t, err)
		assert.NotEmpty(t, resizedImgBytes)

		resizedImg, _, err := image.Decode(bytes.NewReader(resizedImgBytes))
		assert.NoError(t, err)
		assert.Equal(t, 100, resizedImg.Bounds().Dx())
		assert.Equal(t, 50, resizedImg.Bounds().Dy())
	})

	t.Run("gif image", func(t *testing.T) {
		buf := new(bytes.Buffer)
		err := gif.Encode(buf, img, nil)
		assert.NoError(t, err)

		resizedImgBytes, err := ResizeImage(buf.Bytes(), "image/gif", "test.gif", 100, 100)
		assert.NoError(t, err)
		assert.NotEmpty(t, resizedImgBytes)

		resizedImg, _, err := image.Decode(bytes.NewReader(resizedImgBytes))
		assert.NoError(t, err)
		assert.Equal(t, 100, resizedImg.Bounds().Dx())
		assert.Equal(t, 100, resizedImg.Bounds().Dy())
	})
}
