// Package facestore defines the storage abstraction for face detection and
// recognition data. Detected faces are stored alongside a lightweight
// geometric fingerprint (derived from Pigo landmarks) and a perceptual hash
// of the cropped face region. Together these enable a two-pass "hybrid"
// matching strategy that is fast and runs entirely in pure Go.
package facestore

import "time"

// BBox represents a bounding box as percentages (0.0–1.0) of the source image
// dimensions. This makes the coordinates resolution-independent.
type BBox struct {
	X float64 `json:"x"` // left edge   (0.0 = leftmost)
	Y float64 `json:"y"` // top edge    (0.0 = topmost)
	W float64 `json:"w"` // width
	H float64 `json:"h"` // height
}

// PhotoRef uniquely identifies a photo in blob storage.
type PhotoRef struct {
	Collection string `json:"collection"`
	Album      string `json:"album"`
	Name       string `json:"name"`
}

// Key returns a slash-separated string suitable for use as a map/table key.
func (p PhotoRef) Key() string {
	return p.Collection + "/" + p.Album + "/" + p.Name
}

// Person groups one or more detected faces that are believed to be the same
// individual. The Name field is empty until an admin labels the person.
type Person struct {
	PersonID        string `json:"personID"`
	Name            string `json:"name"`
	FaceCount       int    `json:"faceCount"`
	ThumbnailFaceID string `json:"thumbnailFaceID"`
}

// Face is a single detected face in a photo.
type Face struct {
	FaceID              string    `json:"faceID"`
	PersonID            string    `json:"personID"`
	PhotoRef            PhotoRef  `json:"photoRef"`
	BBox                BBox      `json:"bbox"`
	LandmarkFingerprint []float64 `json:"landmarkFingerprint"` // normalised geometric ratios (~10 dims)
	FaceHash            string    `json:"faceHash"`            // 64-bit perceptual dHash as hex string
	Confidence          float32   `json:"confidence"`
	CreatedAt           time.Time `json:"createdAt"`
}

// FaceOverlay is the minimal face data returned alongside a photo for
// rendering bounding-box overlays in the UI.
type FaceOverlay struct {
	FaceID     string `json:"faceID"`
	PersonID   string `json:"personID"`
	PersonName string `json:"personName"`
	BBox       BBox   `json:"bbox"`
}
