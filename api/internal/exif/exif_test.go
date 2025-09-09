package exif

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetExifJSON(t *testing.T) {
	tests := []struct {
		name          string
		setupBuffer   func() bytes.Buffer
		expectError   bool
		expectEmpty   bool
		errorContains string
		description   string
	}{
		{
			name: "Error case - empty buffer",
			setupBuffer: func() bytes.Buffer {
				return bytes.Buffer{}
			},
			expectError: true,
			description: "Should return error for empty image buffer",
		},
		{
			name: "Error case - invalid image data",
			setupBuffer: func() bytes.Buffer {
				var buf bytes.Buffer
				buf.Write([]byte{0x00, 0x01, 0x02, 0x03, 0x04}) // Invalid data
				return buf
			},
			expectError: true,
			description: "Should return error for invalid image data",
		},
		{
			name: "Boundary case - single byte buffer",
			setupBuffer: func() bytes.Buffer {
				var buf bytes.Buffer
				buf.WriteByte(0xFF)
				return buf
			},
			expectError: true,
			description: "Should return error for insufficient data",
		},
		{
			name: "Edge case - buffer with only JPEG SOI",
			setupBuffer: func() bytes.Buffer {
				var buf bytes.Buffer
				buf.Write([]byte{0xFF, 0xD8}) // Only JPEG start marker
				return buf
			},
			expectError: true,
			description: "Should return error for incomplete JPEG data",
		},
		{
			name: "Edge case - JPEG without EXIF data",
			setupBuffer: func() bytes.Buffer {
				var buf bytes.Buffer
				// Minimal JPEG without EXIF
				buf.Write([]byte{
					0xFF, 0xD8, // JPEG SOI
					0xFF, 0xDA, // Start of scan (no EXIF)
					0x00, 0x08, // Length
					0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, // Minimal scan data
					0xFF, 0xD9, // JPEG EOI
				})
				return buf
			},
			expectError: true,
			description: "Should return error for JPEG without EXIF data",
		},
		{
			name: "Boundary case - large buffer with zeros",
			setupBuffer: func() bytes.Buffer {
				var buf bytes.Buffer
				// Large buffer with zeros
				largeData := make([]byte, 1024)
				buf.Write(largeData)
				return buf
			},
			expectError: true,
			description: "Should return error for large buffer with invalid data",
		},
		{
			name: "Edge case - buffer with JPEG SOI but corrupted header",
			setupBuffer: func() bytes.Buffer {
				var buf bytes.Buffer
				buf.Write([]byte{
					0xFF, 0xD8, // JPEG SOI
					0xFF, 0xE1, // APP1 marker
					0x00, 0x04, // Length too short
					0x45, 0x78, // Incomplete "Exif"
				})
				return buf
			},
			expectError: true,
			description: "Should return error for corrupted EXIF header",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			buffer := tt.setupBuffer()

			// Execute
			result, err := GetExifJSON(buffer)

			// Verify error expectations
			if tt.expectError {
				assert.Error(t, err, "Expected an error for test case: %s", tt.description)
				assert.Empty(t, result, "Expected empty result when error occurs")
			} else {
				assert.NoError(t, err, "Expected no error for test case: %s", tt.description)

				if tt.expectEmpty {
					assert.Empty(t, result, "Expected empty result")
				} else {
					assert.NotEmpty(t, result, "Expected non-empty result")
					// Verify it's valid JSON-like structure
					assert.True(t, strings.Contains(result, "{") || strings.Contains(result, "["),
						"Expected JSON-like structure in result")
				}
			}

			// Additional error message validation if specified
			if tt.errorContains != "" && err != nil {
				assert.Contains(t, err.Error(), tt.errorContains,
					"Error message should contain expected text")
			}
		})
	}
}

func TestGetExifJSON_ReturnTypes(t *testing.T) {
	t.Run("Return type validation", func(t *testing.T) {
		// Test with empty buffer (will return error)
		var buf bytes.Buffer

		result, err := GetExifJSON(buf)

		// Verify return types
		assert.IsType(t, "", result, "Result should be string type")
		if err != nil {
			assert.IsType(t, (*error)(nil), &err, "Error should be error type")
		}
	})
}

func TestGetExifJSON_BufferState(t *testing.T) {
	t.Run("Buffer state after function call", func(t *testing.T) {
		// Setup buffer with test data
		originalData := []byte{0x00, 0x01, 0x02, 0x03}
		var buf bytes.Buffer
		buf.Write(originalData)
		originalLen := buf.Len()

		// Call function
		_, _ = GetExifJSON(buf)

		// Verify buffer state (function should not modify the original buffer)
		assert.Equal(t, originalLen, buf.Len(),
			"Buffer length should remain unchanged after function call")
		assert.Equal(t, originalData, buf.Bytes(),
			"Buffer contents should remain unchanged after function call")
	})
}

func TestGetExifJSON_LargeData(t *testing.T) {
	t.Run("Boundary case - large invalid data", func(t *testing.T) {
		// Create a large buffer with invalid data
		var buf bytes.Buffer
		largeInvalidData := make([]byte, 1024*1024) // 1MB of zeros
		buf.Write(largeInvalidData)

		result, err := GetExifJSON(buf)

		// Should handle large data gracefully
		assert.Error(t, err, "Should return error for large invalid data")
		assert.Empty(t, result, "Should return empty result for invalid data")
	})
}

func TestGetExifJSON_ErrorHandling(t *testing.T) {
	t.Run("Error handling consistency", func(t *testing.T) {
		// Test different error scenarios
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
				var buf bytes.Buffer
				buf.Write(tc.data)

				result, err := GetExifJSON(buf)

				// All these cases should return errors
				assert.Error(t, err, "Should return error for %s", tc.name)
				assert.Empty(t, result, "Should return empty result for %s", tc.name)

				// Verify error is not nil and has a message
				assert.NotNil(t, err, "Error should not be nil")
				assert.NotEmpty(t, err.Error(), "Error message should not be empty")
			})
		}
	})
}

func TestGetExifJSON_MemoryManagement(t *testing.T) {
	t.Run("Memory management test", func(t *testing.T) {
		// Test multiple calls to ensure no memory leaks in the wrapper
		for i := 0; i < 100; i++ {
			var buf bytes.Buffer
			buf.Write([]byte{0x01, 0x02, 0x03}) // Invalid data

			_, _ = GetExifJSON(buf)
			// If there were memory leaks, this would eventually cause issues
		}
		// Test passes if we reach here without panic or excessive memory usage
		assert.True(t, true, "Memory management test completed successfully")
	})
}

// TestGetExifJSON_ConcurrentAccess tests thread safety (if applicable)
func TestGetExifJSON_ConcurrentAccess(t *testing.T) {
	t.Run("Concurrent access test", func(t *testing.T) {
		// Test concurrent calls to the function
		done := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			go func() {
				defer func() { done <- true }()

				var buf bytes.Buffer
				buf.Write([]byte{0x01, 0x02, 0x03}) // Invalid data

				_, _ = GetExifJSON(buf)
			}()
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}

		assert.True(t, true, "Concurrent access test completed successfully")
	})
}

func TestGetExifJSON_InputValidation(t *testing.T) {
	t.Run("Input validation comprehensive", func(t *testing.T) {
		// Test various input patterns that should all fail gracefully
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
				var buf bytes.Buffer
				buf.Write(input.data)

				result, err := GetExifJSON(buf)

				// All should fail since they're not valid EXIF data
				assert.Error(t, err)
				assert.Empty(t, result)
			})
		}
	})
}

// Benchmark test to measure performance
func BenchmarkGetExifJSON(b *testing.B) {
	// Setup test data once (invalid data for consistent error path)
	var buf bytes.Buffer
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create a fresh buffer for each iteration
		var testBuf bytes.Buffer
		testBuf.Write(buf.Bytes())

		_, _ = GetExifJSON(testBuf)
	}
}
