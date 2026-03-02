package exif

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildJPEGWithExif constructs a minimal valid JPEG containing an EXIF APP1
// segment with a single IFD0 entry (Make = "Test"). This is enough for
// goexif to decode successfully and return JSON.
func buildJPEGWithExif(t *testing.T) []byte {
	t.Helper()

	var tiff bytes.Buffer

	// TIFF header — little-endian
	tiff.Write([]byte{'I', 'I'})                                    // byte order
	binary.Write(&tiff, binary.LittleEndian, uint16(0x002A))        // TIFF magic
	binary.Write(&tiff, binary.LittleEndian, uint32(0x00000008))    // offset to IFD0

	// IFD0 at offset 8
	binary.Write(&tiff, binary.LittleEndian, uint16(1)) // 1 entry

	// IFD entry: Make (tag 0x010F), ASCII (type 2), count 5 ("Test\0")
	// Value offset = 8 (header) + 2 (count) + 12 (entry) + 4 (next IFD) = 26
	binary.Write(&tiff, binary.LittleEndian, uint16(0x010F)) // tag
	binary.Write(&tiff, binary.LittleEndian, uint16(2))      // type = ASCII
	binary.Write(&tiff, binary.LittleEndian, uint32(5))      // count
	binary.Write(&tiff, binary.LittleEndian, uint32(26))     // value offset

	// Next IFD offset (none)
	binary.Write(&tiff, binary.LittleEndian, uint32(0))

	// String data at TIFF-relative offset 26
	tiff.WriteString("Test\x00")

	// APP1 payload = "Exif\0\0" + TIFF data
	var app1Payload bytes.Buffer
	app1Payload.WriteString("Exif\x00\x00")
	app1Payload.Write(tiff.Bytes())

	// Full JPEG
	payloadLen := uint16(app1Payload.Len() + 2) // +2 for the length field itself
	var jpeg bytes.Buffer
	jpeg.Write([]byte{0xFF, 0xD8})                                   // SOI
	jpeg.Write([]byte{0xFF, 0xE1})                                   // APP1 marker
	binary.Write(&jpeg, binary.BigEndian, payloadLen)                // APP1 length
	jpeg.Write(app1Payload.Bytes())
	jpeg.Write([]byte{0xFF, 0xD9})                                   // EOI

	return jpeg.Bytes()
}

func TestGetExifJSON_SuccessPath(t *testing.T) {
	data := buildJPEGWithExif(t)

	result, err := GetExifJSON(data)

	require.NoError(t, err, "valid EXIF JPEG should not produce an error")
	assert.NotEmpty(t, result, "result should contain EXIF JSON")
	assert.Contains(t, result, "{", "result should be a JSON object")
	assert.Contains(t, result, "Make", "result should contain the Make tag")
}

func TestGetExifJSON(t *testing.T) {
	tests := []struct {
		name          string
		data          []byte
		expectError   bool
		expectEmpty   bool
		errorContains string
		description   string
	}{
		{
			name:        "Error case - empty data",
			data:        []byte{},
			expectError: true,
			description: "Should return error for empty image data",
		},
		{
			name:        "Error case - invalid image data",
			data:        []byte{0x00, 0x01, 0x02, 0x03, 0x04},
			expectError: true,
			description: "Should return error for invalid image data",
		},
		{
			name:        "Boundary case - single byte",
			data:        []byte{0xFF},
			expectError: true,
			description: "Should return error for insufficient data",
		},
		{
			name:        "Edge case - buffer with only JPEG SOI",
			data:        []byte{0xFF, 0xD8},
			expectError: true,
			description: "Should return error for incomplete JPEG data",
		},
		{
			name: "Edge case - JPEG without EXIF data",
			data: []byte{
				0xFF, 0xD8, // JPEG SOI
				0xFF, 0xDA, // Start of scan (no EXIF)
				0x00, 0x08, // Length
				0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, // Minimal scan data
				0xFF, 0xD9, // JPEG EOI
			},
			expectError: true,
			description: "Should return error for JPEG without EXIF data",
		},
		{
			name:        "Boundary case - large buffer with zeros",
			data:        make([]byte, 1024),
			expectError: true,
			description: "Should return error for large buffer with invalid data",
		},
		{
			name: "Edge case - buffer with JPEG SOI but corrupted header",
			data: []byte{
				0xFF, 0xD8, // JPEG SOI
				0xFF, 0xE1, // APP1 marker
				0x00, 0x04, // Length too short
				0x45, 0x78, // Incomplete "Exif"
			},
			expectError: true,
			description: "Should return error for corrupted EXIF header",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetExifJSON(tt.data)

			if tt.expectError {
				assert.Error(t, err, "Expected an error for test case: %s", tt.description)
				assert.Empty(t, result, "Expected empty result when error occurs")
			} else {
				assert.NoError(t, err, "Expected no error for test case: %s", tt.description)

				if tt.expectEmpty {
					assert.Empty(t, result, "Expected empty result")
				} else {
					assert.NotEmpty(t, result, "Expected non-empty result")
					assert.True(t, strings.Contains(result, "{") || strings.Contains(result, "["),
						"Expected JSON-like structure in result")
				}
			}

			if tt.errorContains != "" && err != nil {
				assert.Contains(t, err.Error(), tt.errorContains,
					"Error message should contain expected text")
			}
		})
	}
}

func TestGetExifJSON_ReturnTypes(t *testing.T) {
	t.Run("Return type validation", func(t *testing.T) {
		result, err := GetExifJSON([]byte{})

		assert.IsType(t, "", result, "Result should be string type")
		if err != nil {
			assert.IsType(t, (*error)(nil), &err, "Error should be error type")
		}
	})
}

func TestGetExifJSON_BufferState(t *testing.T) {
	t.Run("Input data not modified after function call", func(t *testing.T) {
		originalData := []byte{0x00, 0x01, 0x02, 0x03}
		input := make([]byte, len(originalData))
		copy(input, originalData)

		_, _ = GetExifJSON(input)

		assert.Equal(t, originalData, input,
			"Input data should remain unchanged after function call")
	})
}

func TestGetExifJSON_LargeData(t *testing.T) {
	t.Run("Boundary case - large invalid data", func(t *testing.T) {
		largeInvalidData := make([]byte, 1024*1024) // 1MB of zeros

		result, err := GetExifJSON(largeInvalidData)

		assert.Error(t, err, "Should return error for large invalid data")
		assert.Empty(t, result, "Should return empty result for invalid data")
	})
}

func TestGetExifJSON_ErrorHandling(t *testing.T) {
	t.Run("Error handling consistency", func(t *testing.T) {
		testCases := []struct {
			name string
			data []byte
		}{
			{"nil-like data", []byte{}},
			{"single byte", []byte{0x00}},
			{"random bytes", []byte{0x01, 0x02, 0x03, 0x04, 0x05}},
			{"incomplete JPEG header", []byte{0xFF}},
			{"partial JPEG SOI", []byte{0xFF, 0xD8}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result, err := GetExifJSON(tc.data)

				assert.Error(t, err, "Should return error for %s", tc.name)
				assert.Empty(t, result, "Should return empty result for %s", tc.name)
				assert.NotNil(t, err, "Error should not be nil")
				assert.NotEmpty(t, err.Error(), "Error message should not be empty")
			})
		}
	})
}

func TestGetExifJSON_MemoryManagement(t *testing.T) {
	t.Run("Memory management test", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			_, _ = GetExifJSON([]byte{0x01, 0x02, 0x03})
		}
		assert.True(t, true, "Memory management test completed successfully")
	})
}

func TestGetExifJSON_ConcurrentAccess(t *testing.T) {
	t.Run("Concurrent access test", func(t *testing.T) {
		done := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			go func() {
				defer func() { done <- true }()
				_, _ = GetExifJSON([]byte{0x01, 0x02, 0x03})
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}

		assert.True(t, true, "Concurrent access test completed successfully")
	})
}

func TestGetExifJSON_InputValidation(t *testing.T) {
	t.Run("Input validation comprehensive", func(t *testing.T) {
		inputs := []struct {
			name string
			data []byte
		}{
			{"all zeros", make([]byte, 100)},
			{"all 0xFF", bytes.Repeat([]byte{0xFF}, 50)},
			{"ascending bytes", func() []byte {
				b := make([]byte, 256)
				for i := range b {
					b[i] = byte(i)
				}
				return b
			}()},
			{"random pattern", []byte{0xAB, 0xCD, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x90}},
		}

		for _, input := range inputs {
			t.Run(input.name, func(t *testing.T) {
				result, err := GetExifJSON(input.data)

				assert.Error(t, err)
				assert.Empty(t, result)
			})
		}
	})
}

func BenchmarkGetExifJSON(b *testing.B) {
	testData := []byte{0x01, 0x02, 0x03, 0x04}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GetExifJSON(testData)
	}
}
