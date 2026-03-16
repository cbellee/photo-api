package facedetect

import "math"

// FingerprintDims is the number of dimensions in the landmark fingerprint vector.
const FingerprintDims = 10

// ComputeFingerprint derives a normalised geometric ratio vector from facial
// landmark points. The resulting vector is resolution-independent and captures
// the relative spatial relationships between facial features.
//
// The function looks for landmarks named:
//
//	left_eye, right_eye, lp44 (nose tip), lp81 (mouth left), lp82 (mouth right),
//	lp84 (mouth bottom), lp93 (chin), lp38 (nose bridge)
//
// If any essential landmarks (eyes) are missing the function returns a zero vector.
func ComputeFingerprint(landmarks []LandmarkPoint, faceW, faceH int) [FingerprintDims]float64 {
	var fp [FingerprintDims]float64

	m := landmarkMap(landmarks)
	le, leOk := m["left_eye"]
	re, reOk := m["right_eye"]

	if !leOk || !reOk || faceW == 0 || faceH == 0 {
		return fp
	}

	fw := float64(faceW)
	fh := float64(faceH)

	// Inter-eye distance.
	interEye := dist(le, re)

	// fp[0]: inter-eye distance / face width
	fp[0] = interEye / fw

	// fp[1]: face aspect ratio (width / height)
	fp[1] = fw / fh

	// Eye midpoint.
	eyeMidRow := float64(le.Row+re.Row) / 2
	eyeMidCol := float64(le.Col+re.Col) / 2

	// Nose (lp44 from Pigo FLP cascades).
	if nose, ok := m["lp44"]; ok {
		// fp[2]: eye-to-nose vertical distance / face height
		fp[2] = math.Abs(float64(nose.Row)-eyeMidRow) / fh
		// fp[3]: nose horizontal offset from eye midpoint / face width
		fp[3] = (float64(nose.Col) - eyeMidCol) / fw
		// fp[4]: left eye to nose distance / face width
		fp[4] = dist(le, nose) / fw
		// fp[5]: right eye to nose distance / face width
		fp[5] = dist(re, nose) / fw

		// Mouth landmarks (lp81 = mouth left, lp82 = mouth right).
		ml, mlOk := m["lp81"]
		mr, mrOk := m["lp82"]
		if mlOk && mrOk {
			// fp[6]: nose-to-mouth vertical distance / face height
			mouthMidRow := float64(ml.Row+mr.Row) / 2
			fp[6] = math.Abs(mouthMidRow-float64(nose.Row)) / fh
			// fp[7]: mouth width / face width
			fp[7] = dist(ml, mr) / fw
			// fp[8]: mouth width / inter-eye distance
			if interEye > 0 {
				fp[8] = dist(ml, mr) / interEye
			}
		}
	}

	// fp[9]: left eye to right eye angle (normalised to -1..1).
	eyeAngle := math.Atan2(float64(re.Row-le.Row), float64(re.Col-le.Col))
	fp[9] = eyeAngle / math.Pi

	return fp
}

// landmarkMap builds a name→point lookup from a slice of landmarks.
func landmarkMap(lm []LandmarkPoint) map[string]LandmarkPoint {
	m := make(map[string]LandmarkPoint, len(lm))
	for _, p := range lm {
		m[p.Name] = p
	}
	return m
}

// dist computes the Euclidean distance between two landmark points.
func dist(a, b LandmarkPoint) float64 {
	dr := float64(a.Row - b.Row)
	dc := float64(a.Col - b.Col)
	return math.Sqrt(dr*dr + dc*dc)
}
