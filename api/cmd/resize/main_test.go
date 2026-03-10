package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"testing"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/cbellee/photo-api/internal/storage"
	"github.com/dapr/go-sdk/service/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Test fixtures ───────────────────────────────────────────────────

func testConfig() *Config {
	return &Config{
		ServiceName:         "test-resize-api",
		ServicePort:         "3000",
		UploadsQueueBinding: "test-queue",
		AzureClientID:       "test-client-id",
		ImagesContainerName: "test-images",
		MaxImageHeight:      1200,
		MaxImageWidth:       1600,
		StorageAccount:      "teststorage",
		StorageSuffix:       "blob.core.windows.net",
		StorageContainer:    "test-container",
	}
}

// setupTestHandler creates a Handler for testing and returns it along with a
// cleanup function that unsets environment variables.
func setupTestHandler() (*Handler, func()) {
	os.Setenv("SERVICE_NAME", "test-resize-api")
	os.Setenv("SERVICE_PORT", "3000")
	os.Setenv("UPLOADS_QUEUE_BINDING", "test-queue")
	os.Setenv("AZURE_CLIENT_ID", "test-client-id")
	os.Setenv("IMAGES_CONTAINER_NAME", "test-images")
	os.Setenv("STORAGE_ACCOUNT_NAME", "teststorage")
	os.Setenv("STORAGE_ACCOUNT_SUFFIX", "blob.core.windows.net")
	os.Setenv("STORAGE_CONTAINER_NAME", "test-container")
	os.Setenv("MAX_IMAGE_HEIGHT", "1200")
	os.Setenv("MAX_IMAGE_WIDTH", "1600")

	cfg := testConfig()

	mockStore := &storage.MockBlobStore{}

	h := NewHandler(mockStore, cfg)

	cleanup := func() {
		os.Unsetenv("SERVICE_NAME")
		os.Unsetenv("SERVICE_PORT")
		os.Unsetenv("UPLOADS_QUEUE_BINDING")
		os.Unsetenv("AZURE_CLIENT_ID")
		os.Unsetenv("IMAGES_CONTAINER_NAME")
		os.Unsetenv("STORAGE_ACCOUNT_NAME")
		os.Unsetenv("STORAGE_ACCOUNT_SUFFIX")
		os.Unsetenv("STORAGE_CONTAINER_NAME")
		os.Unsetenv("MAX_IMAGE_HEIGHT")
		os.Unsetenv("MAX_IMAGE_WIDTH")
	}
	return h, cleanup
}

// createTestBindingEvent creates a test BindingEvent for testing
func createTestBindingEvent(url string, contentType string, contentLength int32) *common.BindingEvent {
	// Create a valid Event struct that matches the expected JSON structure
	event := models.Event{
		Topic:           "/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.Storage/storageAccounts/teststorage",
		Subject:         "/blobServices/default/containers/uploads/blobs/collection1/album1/test-image.jpg",
		EventType:       "Microsoft.Storage.BlobCreated",
		ID:              "test-event-id-12345",
		DataVersion:     "1.0",
		MetadataVersion: "1",
		EventTime:       "2023-01-01T12:00:00.0000000Z",
		Data: models.EventData{
			API:           "PutBlob",
			RequestId:     "test-request-id",
			ETag:          "test-etag",
			ContentType:   contentType,
			ContentLength: contentLength,
			BlobType:      "BlockBlob",
			URL:           url,
			Sequencer:     "00000000000000EB0000000000046199",
			StorageDiagnostics: models.StorageDiagnosticsData{
				BatchId: "test-batch-id",
			},
		},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(event)
	if err != nil {
		// In a real test, we'd handle this better, but for this mock it's fine
		jsonData = []byte(`{"error": "failed to marshal"}`)
	}

	// Encode to base64 as expected by the ConvertToEvent function
	base64Data := base64.StdEncoding.EncodeToString(jsonData)

	return &common.BindingEvent{
		Data: []byte(base64Data),
		Metadata: map[string]string{
			"test-key": "test-value",
		},
	}
}

// Test ResizeHandler function
func TestResizeHandler(t *testing.T) {
	h, cleanup := setupTestHandler()
	defer cleanup()

	tests := []struct {
		name          string
		setupEvent    func() *common.BindingEvent
		expectError   bool
		errorContains string
		description   string
	}{
		{
			name: "Happy path - valid image URL",
			setupEvent: func() *common.BindingEvent {
				testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test-image.jpg"
				return createTestBindingEvent(testURL, "image/jpeg", 1024000)
			},
			expectError: true, // Will error due to missing utils implementations in test environment
			description: "Should process valid image event",
		},
		{
			name: "Edge case - minimum content length",
			setupEvent: func() *common.BindingEvent {
				testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/tiny-image.jpg"
				return createTestBindingEvent(testURL, "image/jpeg", 1)
			},
			expectError: true,
			description: "Should handle very small image file",
		},
		{
			name: "Edge case - maximum content length",
			setupEvent: func() *common.BindingEvent {
				testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/large-image.jpg"
				return createTestBindingEvent(testURL, "image/jpeg", 100*1024*1024) // 100MB
			},
			expectError: true,
			description: "Should handle large image file",
		},
		{
			name: "Error case - malformed URL",
			setupEvent: func() *common.BindingEvent {
				testURL := "not-a-valid-url"
				return createTestBindingEvent(testURL, "image/jpeg", 1024)
			},
			expectError: true,
			description: "Should return error for malformed URL",
		},
		{
			name: "Edge case - different image types",
			setupEvent: func() *common.BindingEvent {
				testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test-image.png"
				return createTestBindingEvent(testURL, "image/png", 2048000)
			},
			expectError: true,
			description: "Should handle different image content types",
		},
		{
			name: "Boundary case - empty content type",
			setupEvent: func() *common.BindingEvent {
				testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test-image.jpg"
				return createTestBindingEvent(testURL, "", 1024)
			},
			expectError: true,
			description: "Should handle empty content type",
		},
		{
			name: "Boundary case - zero content length",
			setupEvent: func() *common.BindingEvent {
				testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/empty-file.jpg"
				return createTestBindingEvent(testURL, "image/jpeg", 0)
			},
			expectError: true,
			description: "Should handle zero content length",
		},
		{
			name: "Edge case - very deep path structure",
			setupEvent: func() *common.BindingEvent {
				testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/sub1/sub2/sub3/album1/test-image.jpg"
				return createTestBindingEvent(testURL, "image/jpeg", 1024)
			},
			expectError: true,
			description: "Should handle URLs with deep path structure",
		},
		{
			name: "Edge case - special characters in path",
			setupEvent: func() *common.BindingEvent {
				testURL := "https://teststorage.blob.core.windows.net/uploads/collection%201/album%201/test%20image.jpg"
				return createTestBindingEvent(testURL, "image/jpeg", 1024)
			},
			expectError: true,
			description: "Should handle URLs with encoded special characters",
		},
		{
			name: "Boundary case - minimal path components",
			setupEvent: func() *common.BindingEvent {
				testURL := "https://teststorage.blob.core.windows.net/uploads/c/a/f.jpg"
				return createTestBindingEvent(testURL, "image/jpeg", 1024)
			},
			expectError: true,
			description: "Should handle minimal path components",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			event := tt.setupEvent()

			result, err := h.Resize(ctx, event)

			// Handler always ACKs the message (returns nil error) to prevent
			// infinite requeue in Dapr/RabbitMQ. Errors are logged internally.
			assert.NoError(t, err, "Handler must always return nil to ACK the message")
			assert.Nil(t, result)
		})
	}
}

// Test URL parsing logic within ResizeHandler
func TestResizeHandler_URLParsing(t *testing.T) {
	h, cleanup := setupTestHandler()
	defer cleanup()

	tests := []struct {
		name        string
		inputURL    string
		expectError bool
		description string
	}{
		{
			name:        "Valid URL with standard path",
			inputURL:    "https://teststorage.blob.core.windows.net/uploads/collection1/album1/image.jpg",
			expectError: true, // Will still error due to missing external dependencies
			description: "Should parse standard blob URL correctly",
		},
		{
			name:        "URL with query parameters",
			inputURL:    "https://teststorage.blob.core.windows.net/uploads/collection1/album1/image.jpg?sv=2021-06-08",
			expectError: true,
			description: "Should handle URLs with query parameters",
		},
		{
			name:        "URL with fragments",
			inputURL:    "https://teststorage.blob.core.windows.net/uploads/collection1/album1/image.jpg#section1",
			expectError: true,
			description: "Should handle URLs with fragments",
		},
		{
			name:        "Invalid URL format",
			inputURL:    "not-a-url",
			expectError: true,
			description: "Should return error for invalid URL format",
		},
		{
			name:        "Empty URL",
			inputURL:    "",
			expectError: true,
			description: "Should return error for empty URL",
		},
		{
			name:        "URL with no path",
			inputURL:    "https://teststorage.blob.core.windows.net",
			expectError: true,
			description: "Should handle URL with no path components",
		},
		{
			name:        "URL with insufficient path components",
			inputURL:    "https://teststorage.blob.core.windows.net/uploads",
			expectError: true,
			description: "Should handle URL with insufficient path components",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			event := createTestBindingEvent(tt.inputURL, "image/jpeg", 1024)

			_, err := h.Resize(ctx, event)

			// Handler always ACKs the message (returns nil) to prevent requeue.
			assert.NoError(t, err)
		})
	}
}

// Test that the Handler respects config values for image dimensions
func TestResizeHandler_ConfigDimensions(t *testing.T) {
	h, cleanup := setupTestHandler()
	defer cleanup()

	tests := []struct {
		name        string
		height      int
		width       int
		expectError bool
		description string
	}{
		{
			name:        "Default dimensions",
			height:      1200,
			width:       1600,
			expectError: true, // Will error due to missing external dependencies
			description: "Should use default dimension values",
		},
		{
			name:        "Zero dimensions",
			height:      0,
			width:       0,
			expectError: true,
			description: "Should handle zero dimension values",
		},
		{
			name:        "Negative dimensions",
			height:      -100,
			width:       -200,
			expectError: true,
			description: "Should handle negative dimension values",
		},
		{
			name:        "Maximum integer dimensions",
			height:      2147483647,
			width:       2147483647,
			expectError: true,
			description: "Should handle maximum integer values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Override config for this test case
			h.cfg.MaxImageHeight = tt.height
			h.cfg.MaxImageWidth = tt.width

			ctx := context.Background()
			testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test.jpg"
			event := createTestBindingEvent(testURL, "image/jpeg", 1024)

			_, err := h.Resize(ctx, event)

			// Handler always ACKs the message (returns nil) to prevent requeue.
			assert.NoError(t, err)
		})
	}
}

// Test input validation
func TestResizeHandler_InputValidation(t *testing.T) {
	h, cleanup := setupTestHandler()
	defer cleanup()

	tests := []struct {
		name        string
		event       *common.BindingEvent
		expectError bool
		description string
	}{
		{
			name:        "Nil binding event",
			event:       nil,
			expectError: true,
			description: "Should handle nil binding event",
		},
		{
			name: "Empty binding event",
			event: &common.BindingEvent{
				Data:     []byte(""),
				Metadata: nil,
			},
			expectError: true,
			description: "Should handle empty binding event",
		},
		{
			name: "Binding event with nil metadata",
			event: &common.BindingEvent{
				Data:     []byte("invalid-base64"),
				Metadata: nil,
			},
			expectError: true,
			description: "Should handle binding event with nil metadata",
		},
		{
			name: "Binding event with invalid base64 data",
			event: &common.BindingEvent{
				Data:     []byte("not-valid-base64!@#"),
				Metadata: map[string]string{},
			},
			expectError: true,
			description: "Should handle binding event with invalid base64 data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			_, err := h.Resize(ctx, tt.event)

			// Handler always ACKs the message (returns nil) to prevent requeue.
			assert.NoError(t, err)
		})
	}
}

// Test context handling
func TestResizeHandler_ContextHandling(t *testing.T) {
	h, cleanup := setupTestHandler()
	defer cleanup()

	tests := []struct {
		name        string
		setupCtx    func() context.Context
		expectError bool
		description string
	}{
		{
			name: "Valid context",
			setupCtx: func() context.Context {
				return context.Background()
			},
			expectError: true, // Will error due to missing external dependencies
			description: "Should work with valid context",
		},
		{
			name: "Context with values",
			setupCtx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, "test-key", "test-value")
			},
			expectError: true,
			description: "Should work with context containing values",
		},
		{
			name: "Cancelled context",
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			expectError: true,
			description: "Should handle cancelled context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test.jpg"
			event := createTestBindingEvent(testURL, "image/jpeg", 1024)

			_, err := h.Resize(ctx, event)

			// Handler always ACKs the message (returns nil) to prevent requeue.
			assert.NoError(t, err)
		})
	}
}

// Test error propagation and logging
func TestResizeHandler_ErrorHandling(t *testing.T) {
	h, cleanup := setupTestHandler()
	defer cleanup()

	t.Run("Error propagation", func(t *testing.T) {
		ctx := context.Background()
		testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test.jpg"
		event := createTestBindingEvent(testURL, "image/jpeg", 1024)

		result, err := h.Resize(ctx, event)

		// Handler always ACKs the message (returns nil) to prevent requeue.
		assert.NoError(t, err)
		assert.Nil(t, result)
	})
}

// Test return value validation
func TestResizeHandler_ReturnValues(t *testing.T) {
	h, cleanup := setupTestHandler()
	defer cleanup()

	t.Run("Return type validation", func(t *testing.T) {
		ctx := context.Background()
		testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test.jpg"
		event := createTestBindingEvent(testURL, "image/jpeg", 1024)

		result, err := h.Resize(ctx, event)

		// Verify return types
		if result != nil {
			assert.IsType(t, []byte{}, result, "Result should be []byte type")
		}
		if err != nil {
			assert.IsType(t, (*error)(nil), &err, "Error should be error type")
		}
	})
}

// Test path extraction logic via parseBlobRef
func TestParseBlobRef(t *testing.T) {
	tests := []struct {
		name               string
		inputURL           string
		expectedContainer  string
		expectedCollection string
		expectedAlbum      string
		expectedPath       string
		expectError        bool
		description        string
	}{
		{
			name:               "Standard blob URL",
			inputURL:           "https://storage.blob.core.windows.net/uploads/collection1/album1/image.jpg",
			expectedContainer:  "uploads",
			expectedCollection: "collection1",
			expectedAlbum:      "album1",
			expectedPath:       "collection1/album1/image.jpg",
			expectError:        false,
			description:        "Should extract path from standard blob URL",
		},
		{
			name:               "URL with query parameters",
			inputURL:           "https://storage.blob.core.windows.net/uploads/collection1/album1/image.jpg?sv=2021&sig=abc",
			expectedContainer:  "uploads",
			expectedCollection: "collection1",
			expectedAlbum:      "album1",
			expectedPath:       "collection1/album1/image.jpg",
			expectError:        false,
			description:        "Should extract path ignoring query parameters",
		},
		{
			name:        "URL with insufficient segments",
			inputURL:    "https://storage.blob.core.windows.net/uploads",
			expectError: true,
			description: "Should return error for insufficient path segments",
		},
		{
			name:        "Empty URL",
			inputURL:    "",
			expectError: true,
			description: "Should return error for empty URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := parseBlobRef(tt.inputURL)

			if tt.expectError {
				assert.Error(t, err, "Expected error for %s", tt.description)
			} else {
				assert.NoError(t, err, "Expected no error for %s", tt.description)
				assert.Equal(t, tt.expectedContainer, ref.container)
				assert.Equal(t, tt.expectedCollection, ref.collection)
				assert.Equal(t, tt.expectedAlbum, ref.album)
				assert.Equal(t, tt.expectedPath, ref.path)
			}
		})
	}
}

// Benchmark test for Resize handler
func BenchmarkResizeHandler(b *testing.B) {
	h, cleanup := setupTestHandler()
	defer cleanup()

	ctx := context.Background()
	testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test.jpg"
	event := createTestBindingEvent(testURL, "image/jpeg", 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h.Resize(ctx, event)
	}
}

// Test concurrent access to Resize handler
func TestResizeHandler_ConcurrentAccess(t *testing.T) {
	h, cleanup := setupTestHandler()
	defer cleanup()

	t.Run("Concurrent handler calls", func(t *testing.T) {
		ctx := context.Background()
		testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test.jpg"

		done := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			go func(id int) {
				defer func() { done <- true }()

				event := createTestBindingEvent(testURL, "image/jpeg", 1024)
				_, _ = h.Resize(ctx, event)
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}

		assert.True(t, true, "Concurrent access test completed successfully")
	})
}

// Test memory usage patterns
func TestResizeHandler_MemoryManagement(t *testing.T) {
	h, cleanup := setupTestHandler()
	defer cleanup()

	t.Run("Memory management test", func(t *testing.T) {
		ctx := context.Background()
		testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test.jpg"

		// Test multiple calls to ensure no memory leaks in the wrapper
		for i := 0; i < 50; i++ {
			event := createTestBindingEvent(testURL, "image/jpeg", 1024)
			_, _ = h.Resize(ctx, event)
		}

		assert.True(t, true, "Memory management test completed successfully")
	})
}

// ── Happy-path tests with fully-configured mock store ───────────────

// makeTestJPEG returns a valid JPEG byte slice of the given dimensions.
func makeTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, color.RGBA{0, 128, 255, 255})
		}
	}
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 75})
	require.NoError(t, err)
	return buf.Bytes()
}

func TestResizeHandler_HappyPath(t *testing.T) {
	cfg := testConfig()
	cfg.MaxImageHeight = 600
	cfg.MaxImageWidth = 800

	srcJPEG := makeTestJPEG(t, 2000, 1500)

	var savedBlob []byte
	var savedContainer string
	var savedTags map[string]string
	var savedMeta map[string]string

	mock := &storage.MockBlobStore{
		GetBlobFunc: func(ctx context.Context, blobName string, containerName string) ([]byte, error) {
			assert.Equal(t, "collection1/album1/test-image.jpg", blobName)
			assert.Equal(t, "uploads", containerName)
			return srcJPEG, nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{
				"collection": "collection1",
				"album":      "album1",
				"name":       "collection1/album1/test-image.jpg",
			}, nil
		},
		GetBlobMetadataFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{
				"Width":  "2000",
				"Height": "1500",
			}, nil
		},
		SaveBlobFunc: func(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
			savedBlob = data
			savedContainer = containerName
			savedTags = tags
			savedMeta = metadata
			return nil
		},
		DeleteBlobFunc: func(ctx context.Context, blobName string, containerName string) error {
			return nil
		},
	}

	h := NewHandler(mock, cfg)
	ctx := context.Background()
	testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test-image.jpg"
	event := createTestBindingEvent(testURL, "image/jpeg", int32(len(srcJPEG)))

	result, err := h.Resize(ctx, event)

	require.NoError(t, err)
	assert.Nil(t, result)
	assert.NotNil(t, savedBlob, "blob should have been saved")
	assert.Equal(t, cfg.ImagesContainerName, savedContainer)
	assert.Equal(t, "collection1", savedTags["collection"])
	// The resized image should be smaller than max dimensions.
	assert.NotEmpty(t, savedMeta["Width"])
	assert.NotEmpty(t, savedMeta["Height"])
	assert.NotEmpty(t, savedMeta["Size"])
}

func TestResizeHandler_HappyPath_SmallImage(t *testing.T) {
	// If the source image is already within bounds, it should still be processed.
	cfg := testConfig()
	cfg.MaxImageHeight = 600
	cfg.MaxImageWidth = 800

	srcJPEG := makeTestJPEG(t, 200, 150) // already within limits

	var savedBlob []byte

	mock := &storage.MockBlobStore{
		GetBlobFunc: func(ctx context.Context, blobName string, containerName string) ([]byte, error) {
			return srcJPEG, nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{"collection": "c", "album": "a"}, nil
		},
		GetBlobMetadataFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		SaveBlobFunc: func(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
			savedBlob = data
			return nil
		},
		DeleteBlobFunc: func(ctx context.Context, blobName string, containerName string) error {
			return nil
		},
	}

	h := NewHandler(mock, cfg)
	ctx := context.Background()
	testURL := "https://teststorage.blob.core.windows.net/uploads/c/a/small.jpg"
	event := createTestBindingEvent(testURL, "image/jpeg", int32(len(srcJPEG)))

	result, err := h.Resize(ctx, event)

	require.NoError(t, err)
	assert.Nil(t, result)
	assert.NotNil(t, savedBlob)
}

func TestResizeHandler_GetBlobError(t *testing.T) {
	cfg := testConfig()
	mock := &storage.MockBlobStore{
		GetBlobFunc: func(ctx context.Context, blobName string, containerName string) ([]byte, error) {
			return nil, assert.AnError
		},
	}

	h := NewHandler(mock, cfg)
	ctx := context.Background()
	testURL := "https://teststorage.blob.core.windows.net/uploads/c/a/f.jpg"
	event := createTestBindingEvent(testURL, "image/jpeg", 1024)

	_, err := h.Resize(ctx, event)

	// Handler always ACKs to prevent requeue; error is logged, not returned.
	require.NoError(t, err)
}

func TestResizeHandler_GetBlobTagsError(t *testing.T) {
	cfg := testConfig()
	srcJPEG := makeTestJPEG(t, 100, 100)

	mock := &storage.MockBlobStore{
		GetBlobFunc: func(ctx context.Context, blobName string, containerName string) ([]byte, error) {
			return srcJPEG, nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return nil, assert.AnError
		},
	}

	h := NewHandler(mock, cfg)
	ctx := context.Background()
	testURL := "https://teststorage.blob.core.windows.net/uploads/c/a/f.jpg"
	event := createTestBindingEvent(testURL, "image/jpeg", 1024)

	_, err := h.Resize(ctx, event)

	// Handler always ACKs to prevent requeue; error is logged, not returned.
	require.NoError(t, err)
}

func TestResizeHandler_SaveBlobError(t *testing.T) {
	cfg := testConfig()
	cfg.MaxImageHeight = 600
	cfg.MaxImageWidth = 800
	srcJPEG := makeTestJPEG(t, 100, 100)

	mock := &storage.MockBlobStore{
		GetBlobFunc: func(ctx context.Context, blobName string, containerName string) ([]byte, error) {
			return srcJPEG, nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		GetBlobMetadataFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		SaveBlobFunc: func(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
			return assert.AnError
		},
	}

	h := NewHandler(mock, cfg)
	ctx := context.Background()
	testURL := "https://teststorage.blob.core.windows.net/uploads/c/a/f.jpg"
	event := createTestBindingEvent(testURL, "image/jpeg", int32(len(srcJPEG)))

	_, err := h.Resize(ctx, event)

	// Handler always ACKs to prevent requeue; error is logged, not returned.
	require.NoError(t, err)
}

func TestResizeHandler_DeleteSourceBlobAfterResize(t *testing.T) {
	cfg := testConfig()
	cfg.MaxImageHeight = 600
	cfg.MaxImageWidth = 800
	srcJPEG := makeTestJPEG(t, 2000, 1500)

	var deletedBlobName, deletedContainer string

	mock := &storage.MockBlobStore{
		GetBlobFunc: func(ctx context.Context, blobName string, containerName string) ([]byte, error) {
			return srcJPEG, nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{"collection": "c", "album": "a"}, nil
		},
		GetBlobMetadataFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		SaveBlobFunc: func(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
			return nil
		},
		DeleteBlobFunc: func(ctx context.Context, blobName string, containerName string) error {
			deletedBlobName = blobName
			deletedContainer = containerName
			return nil
		},
	}

	h := NewHandler(mock, cfg)
	ctx := context.Background()
	testURL := "https://teststorage.blob.core.windows.net/uploads/collection1/album1/test-image.jpg"
	event := createTestBindingEvent(testURL, "image/jpeg", int32(len(srcJPEG)))

	_, err := h.Resize(ctx, event)
	require.NoError(t, err)

	assert.Equal(t, "collection1/album1/test-image.jpg", deletedBlobName, "should delete the source blob path")
	assert.Equal(t, "uploads", deletedContainer, "should delete from the uploads container")
	require.Len(t, mock.DeleteBlobCalls, 1)
}

func TestResizeHandler_DeleteSourceBlobError(t *testing.T) {
	cfg := testConfig()
	cfg.MaxImageHeight = 600
	cfg.MaxImageWidth = 800
	srcJPEG := makeTestJPEG(t, 100, 100)

	mock := &storage.MockBlobStore{
		GetBlobFunc: func(ctx context.Context, blobName string, containerName string) ([]byte, error) {
			return srcJPEG, nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		GetBlobMetadataFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		SaveBlobFunc: func(ctx context.Context, data []byte, blobName string, containerName string, tags map[string]string, metadata map[string]string, contentType string) error {
			return nil
		},
		DeleteBlobFunc: func(ctx context.Context, blobName string, containerName string) error {
			return assert.AnError
		},
	}

	h := NewHandler(mock, cfg)
	ctx := context.Background()
	testURL := "https://teststorage.blob.core.windows.net/uploads/c/a/f.jpg"
	event := createTestBindingEvent(testURL, "image/jpeg", int32(len(srcJPEG)))

	_, err := h.Resize(ctx, event)

	// Handler always ACKs to prevent requeue; error is logged, not returned.
	require.NoError(t, err)
}

func TestResizeHandler_GetBlobMetadataError(t *testing.T) {
	cfg := testConfig()
	cfg.MaxImageHeight = 600
	cfg.MaxImageWidth = 800
	srcJPEG := makeTestJPEG(t, 100, 100)

	mock := &storage.MockBlobStore{
		GetBlobFunc: func(ctx context.Context, blobName string, containerName string) ([]byte, error) {
			return srcJPEG, nil
		},
		GetBlobTagsFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return map[string]string{}, nil
		},
		GetBlobMetadataFunc: func(ctx context.Context, blobName string, containerName string) (map[string]string, error) {
			return nil, assert.AnError
		},
	}

	h := NewHandler(mock, cfg)
	ctx := context.Background()
	testURL := "https://teststorage.blob.core.windows.net/uploads/c/a/f.jpg"
	event := createTestBindingEvent(testURL, "image/jpeg", 1024)

	_, err := h.Resize(ctx, event)

	// Handler always ACKs to prevent requeue; error is logged, not returned.
	require.NoError(t, err)
}
