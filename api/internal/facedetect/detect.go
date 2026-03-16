// Package facedetect provides Pigo-based face detection with landmark extraction.
// It replaces the previous dlib/go-recognizer implementation with a pure-Go
// solution that has zero CGO dependencies.
package facedetect

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"os"

	pigo "github.com/esimov/pigo/core"
)

// DetectedFace holds the results of detecting a single face in an image.
type DetectedFace struct {
	// BBox is the bounding box in pixel coordinates (absolute).
	BBoxX, BBoxY, BBoxW, BBoxH int

	// BBoxPct holds the bounding box as percentages of image dimensions (0.0–1.0).
	BBoxPctX, BBoxPctY, BBoxPctW, BBoxPctH float64

	// Confidence is Pigo's quality score for this detection.
	Confidence float32

	// Landmarks holds detected facial landmark points (left eye, right eye,
	// nose tip, mouth left, mouth right). Each point is {Row, Col} in pixels.
	// May be nil if landmark cascades are not loaded.
	Landmarks []LandmarkPoint

	// CroppedFace is the cropped face region from the source image (with
	// padding), suitable for perceptual hashing.
	CroppedFace image.Image
}

// LandmarkPoint is a named facial landmark in pixel coordinates.
type LandmarkPoint struct {
	Name string
	Row  int
	Col  int
}

// Detector wraps Pigo classifiers for face detection and landmark extraction.
type Detector struct {
	classifier  *pigo.Pigo
	puplocCasc  *pigo.PuplocCascade
	flpCascades map[string][]*pigo.FlpCascade

	// Detection parameters (tunable via env vars / options).
	MinSize     int
	MaxSize     int
	ShiftFactor float64
	ScaleFactor float64
	MinQuality  float32
	IoUThresh   float64
}

// NewDetector creates a Detector by loading the required Pigo cascade files.
//
//   - cascadePath: path to the face detection cascade binary (e.g. "cascade/facefinder")
//   - puplocPath:  path to the pupil localization cascade (e.g. "cascade/puploc"); "" to skip
//   - flpDir:      path to the facial landmark points cascade directory (e.g. "cascade/lps"); "" to skip
func NewDetector(cascadePath, puplocPath, flpDir string) (*Detector, error) {
	cascadeFile, err := os.ReadFile(cascadePath)
	if err != nil {
		return nil, fmt.Errorf("face: read cascade %q: %w", cascadePath, err)
	}

	pg := pigo.NewPigo()
	classifier, err := pg.Unpack(cascadeFile)
	if err != nil {
		return nil, fmt.Errorf("face: unpack cascade: %w", err)
	}

	d := &Detector{
		classifier:  classifier,
		MinSize:     40,
		MaxSize:     800,
		ShiftFactor: 0.1,
		ScaleFactor: 1.1,
		MinQuality:  5.0,
		IoUThresh:   0.2,
	}

	// Optionally load pupil localization cascade.
	if puplocPath != "" {
		puplocBytes, err := os.ReadFile(puplocPath)
		if err != nil {
			slog.Warn("face: cannot read puploc cascade, pupils will not be detected", "path", puplocPath, "error", err)
		} else {
			plc := pigo.NewPuplocCascade()
			d.puplocCasc, err = plc.UnpackCascade(puplocBytes)
			if err != nil {
				slog.Warn("face: cannot unpack puploc cascade", "error", err)
				d.puplocCasc = nil
			}
		}
	}

	// Optionally load facial landmark point cascades.
	if flpDir != "" {
		plc := pigo.NewPuplocCascade()
		flpCascades, err := plc.ReadCascadeDir(flpDir)
		if err != nil {
			slog.Warn("face: cannot read FLP cascades, landmarks will be unavailable", "dir", flpDir, "error", err)
		} else {
			d.flpCascades = flpCascades
		}
	}

	return d, nil
}

// Detect runs face detection on raw image bytes (JPEG or PNG) and returns
// all detected faces with bounding boxes, landmarks, and cropped regions.
func (d *Detector) Detect(imgBytes []byte) ([]DetectedFace, error) {
	src, err := pigo.DecodeImage(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, fmt.Errorf("face: decode image: %w", err)
	}

	bounds := src.Bounds()
	imgW := bounds.Max.X
	imgH := bounds.Max.Y

	pixels := pigo.RgbToGrayscale(src)

	cParams := pigo.CascadeParams{
		MinSize:     d.MinSize,
		MaxSize:     d.MaxSize,
		ShiftFactor: d.ShiftFactor,
		ScaleFactor: d.ScaleFactor,
		ImageParams: pigo.ImageParams{
			Pixels: pixels,
			Rows:   imgH,
			Cols:   imgW,
			Dim:    imgW,
		},
	}

	imgParams := cParams.ImageParams

	// Run detection at angle 0.
	dets := d.classifier.RunCascade(cParams, 0.0)
	dets = d.classifier.ClusterDetections(dets, d.IoUThresh)

	var results []DetectedFace
	for _, det := range dets {
		if det.Q < d.MinQuality {
			continue
		}

		// Pigo returns {Row, Col, Scale} where Row/Col is the center and
		// Scale is the diameter. Convert to top-left + width/height.
		half := det.Scale / 2
		x := det.Col - half
		y := det.Row - half
		w := det.Scale
		h := det.Scale

		// Clamp to image bounds.
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x+w > imgW {
			w = imgW - x
		}
		if y+h > imgH {
			h = imgH - y
		}

		df := DetectedFace{
			BBoxX:      x,
			BBoxY:      y,
			BBoxW:      w,
			BBoxH:      h,
			BBoxPctX:   float64(x) / float64(imgW),
			BBoxPctY:   float64(y) / float64(imgH),
			BBoxPctW:   float64(w) / float64(imgW),
			BBoxPctH:   float64(h) / float64(imgH),
			Confidence: det.Q,
		}

		// ── Pupil / eye detection ────────────────────────────────
		var leftEye, rightEye *pigo.Puploc

		if d.puplocCasc != nil {
			leftPuploc := &pigo.Puploc{
				Row:      det.Row - int(0.075*float64(det.Scale)),
				Col:      det.Col - int(0.175*float64(det.Scale)),
				Scale:    float32(det.Scale) * 0.25,
				Perturbs: 63,
			}
			rightPuploc := &pigo.Puploc{
				Row:      det.Row - int(0.075*float64(det.Scale)),
				Col:      det.Col + int(0.185*float64(det.Scale)),
				Scale:    float32(det.Scale) * 0.25,
				Perturbs: 63,
			}

			leftEye = d.puplocCasc.RunDetector(*leftPuploc, imgParams, 0.0, false)
			rightEye = d.puplocCasc.RunDetector(*rightPuploc, imgParams, 0.0, false)
		}

		// ── Facial landmark points ───────────────────────────────
		if d.flpCascades != nil && leftEye != nil && rightEye != nil {
			var landmarks []LandmarkPoint

			// Add eye positions.
			if leftEye.Row > 0 && leftEye.Col > 0 {
				landmarks = append(landmarks, LandmarkPoint{Name: "left_eye", Row: leftEye.Row, Col: leftEye.Col})
			}
			if rightEye.Row > 0 && rightEye.Col > 0 {
				landmarks = append(landmarks, LandmarkPoint{Name: "right_eye", Row: rightEye.Row, Col: rightEye.Col})
			}

			// Run each FLP cascade to get additional landmark points.
			for name, cascades := range d.flpCascades {
				for _, flpc := range cascades {
					flp := flpc.GetLandmarkPoint(leftEye, rightEye, imgParams, 63, false)
					if flp != nil && flp.Row > 0 && flp.Col > 0 {
						landmarks = append(landmarks, LandmarkPoint{Name: name, Row: flp.Row, Col: flp.Col})
						break // take best from each cascade group
					}
				}
			}
			df.Landmarks = landmarks
		}

		// ── Crop face region with padding ────────────────────────
		padding := 0.2
		cropX := int(float64(x) - padding*float64(w))
		cropY := int(float64(y) - padding*float64(h))
		cropW := int(float64(w) * (1 + 2*padding))
		cropH := int(float64(h) * (1 + 2*padding))
		if cropX < 0 {
			cropX = 0
		}
		if cropY < 0 {
			cropY = 0
		}
		if cropX+cropW > imgW {
			cropW = imgW - cropX
		}
		if cropY+cropH > imgH {
			cropH = imgH - cropY
		}
		df.CroppedFace = src.SubImage(image.Rect(cropX, cropY, cropX+cropW, cropY+cropH))

		results = append(results, df)
	}

	return results, nil
}
