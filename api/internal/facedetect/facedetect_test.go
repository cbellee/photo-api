package facedetect

import (
	"image"
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeDHashDeterministic(t *testing.T) {
	// Create a small test image.
	img := image.NewGray(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x*8 + y) % 256)})
		}
	}

	hash1 := ComputeDHash(img)
	hash2 := ComputeDHash(img)

	assert.Equal(t, hash1, hash2, "dHash should be deterministic")
	assert.Len(t, hash1, 16, "dHash should be 16 hex chars (64 bits)")
}

func TestHammingDistanceIdentical(t *testing.T) {
	dist := HammingDistance("abcdef0123456789", "abcdef0123456789")
	assert.Equal(t, 0, dist)
}

func TestHammingDistanceDifferent(t *testing.T) {
	// Differ by 1 bit in the last nibble: 9 (1001) vs 8 (1000).
	dist := HammingDistance("abcdef0123456789", "abcdef0123456788")
	assert.Equal(t, 1, dist)
}

func TestHammingDistanceMaxDifference(t *testing.T) {
	dist := HammingDistance("0000000000000000", "ffffffffffffffff")
	assert.Equal(t, 64, dist)
}

func TestComputeFingerprintDimensions(t *testing.T) {
	landmarks := []LandmarkPoint{
		{Name: "left_eye", Row: 50, Col: 30},
		{Name: "right_eye", Row: 50, Col: 70},
		{Name: "lp44", Row: 70, Col: 50}, // nose tip
		{Name: "lp81", Row: 90, Col: 35}, // mouth left
		{Name: "lp82", Row: 90, Col: 65}, // mouth right
	}

	fp := ComputeFingerprint(landmarks, 100, 120)
	assert.Len(t, fp, 10, "fingerprint should have 10 dimensions")

	// All values should be finite.
	for i, v := range fp {
		assert.False(t, v != v, "dimension %d should not be NaN", i) // NaN != NaN
	}

	// Sanity: inter-eye / face width should be ~0.4 (40 pixels / 100 width).
	assert.InDelta(t, 0.4, fp[0], 0.05)
}

func TestComputeFingerprintMissingLandmarksReturnsZeros(t *testing.T) {
	// Missing right_eye is essential → zero vector.
	landmarks := []LandmarkPoint{
		{Name: "left_eye", Row: 50, Col: 30},
	}

	fp := ComputeFingerprint(landmarks, 100, 120)
	assert.Len(t, fp, 10)
	allZero := true
	for _, v := range fp {
		if v != 0 {
			allZero = false
		}
	}
	assert.True(t, allZero, "should return zero vector when landmarks are missing")
}
