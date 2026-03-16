package facedetect

import (
	"encoding/hex"
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"
)

// dHashSize is the width of the down-scaled image used for dHash.
// The resulting hash is (dHashSize-1)*dHashSize = 8*8 = 64 bits.
const dHashSize = 9

// ComputeDHash computes a 64-bit difference hash (dHash) of an image.
// The image is resized to 9×8 grayscale and adjacent horizontal pixel
// intensities are compared. The result is a 16-character hex string.
//
// dHash is fast, pure Go, and robust to minor scaling and compression
// artefacts. It is less robust to pose/rotation changes but works well
// as a second-pass filter after geometric fingerprint matching.
func ComputeDHash(img image.Image) string {
	// Resize to 9×8 grayscale.
	small := image.NewGray(image.Rect(0, 0, dHashSize, dHashSize-1))
	draw.BiLinear.Scale(small, small.Bounds(), img, img.Bounds(), draw.Over, nil)

	var hash uint64
	bit := uint(0)
	for y := 0; y < dHashSize-1; y++ {
		for x := 0; x < dHashSize-1; x++ {
			left := grayAt(small, x, y)
			right := grayAt(small, x+1, y)
			if left > right {
				hash |= 1 << bit
			}
			bit++
		}
	}

	// Encode as 16-char hex string (64 bits = 8 bytes).
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(hash >> (uint(i) * 8))
	}
	return hex.EncodeToString(b)
}

// HammingDistance computes the Hamming distance between two hex-encoded
// hash strings. If the strings have different lengths it returns MaxInt.
func HammingDistance(a, b string) int {
	ba, err1 := hex.DecodeString(a)
	bb, err2 := hex.DecodeString(b)
	if err1 != nil || err2 != nil || len(ba) != len(bb) {
		return math.MaxInt
	}
	dist := 0
	for i := range ba {
		xor := ba[i] ^ bb[i]
		for xor != 0 {
			dist += int(xor & 1)
			xor >>= 1
		}
	}
	return dist
}

func grayAt(img *image.Gray, x, y int) uint8 {
	c := img.GrayAt(x, y)
	return c.Y
}

// Ensure we pull in the color package for the draw operations.
var _ = color.Black
