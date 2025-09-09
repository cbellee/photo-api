package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/cbellee/photo-api/internal/models"
	"github.com/stretchr/testify/assert"
)

// Test helper functions
func createTestRequest(method, url string, body []byte) (*http.Request, error) {
	if body != nil {
		return http.NewRequest(method, url, bytes.NewReader(body))
	}
	return http.NewRequest(method, url, nil)
}

// Test tagListHandler
func TestTagListHandler(t *testing.T) {
	mockClient := &azblob.Client{}
	storageUrl := "https://example.blob.core.windows.net"

	t.Run("Handler returns proper structure", func(t *testing.T) {
		handler := tagListHandler(mockClient, storageUrl)
		assert.NotNil(t, handler)
	})
}

// Test photoHandler creation
func TestPhotoHandler(t *testing.T) {
	mockClient := &azblob.Client{}
	storageUrl := "https://example.blob.core.windows.net"

	t.Run("Handler creation", func(t *testing.T) {
		handler := photoHandler(mockClient, storageUrl)
		assert.NotNil(t, handler)
	})

	t.Run("HTTP request creation", func(t *testing.T) {
		req, err := createTestRequest("GET", "/api/test-collection/test-album", nil)
		assert.NoError(t, err)
		assert.Equal(t, "GET", req.Method)
		assert.Equal(t, "/api/test-collection/test-album", req.URL.Path)
	})
}

// Test data structure validation
func TestDataStructures(t *testing.T) {
	t.Run("Photo model structure", func(t *testing.T) {
		photo := models.Photo{
			Src:        "https://example.com/photo.jpg",
			Name:       "photo.jpg",
			Width:      1920,
			Height:     1080,
			Album:      "test-album",
			Collection: "test-collection",
		}

		assert.Equal(t, "photo.jpg", photo.Name)
		assert.Equal(t, 1920, photo.Width)
		assert.Equal(t, 1080, photo.Height)
		assert.Equal(t, "test-album", photo.Album)
		assert.Equal(t, "test-collection", photo.Collection)
	})

	t.Run("ImageTags model structure", func(t *testing.T) {
		tags := models.ImageTags{
			Description:     "Test photo",
			Collection:      "test-collection",
			Album:           "test-album",
			Type:            "image/jpeg",
			CollectionImage: true,
			AlbumImage:      false,
			IsDeleted:       false,
		}

		assert.Equal(t, "Test photo", tags.Description)
		assert.True(t, tags.CollectionImage)
		assert.False(t, tags.AlbumImage)
	})
}

// Test environment variable handling
func TestEnvironmentVariables(t *testing.T) {
	t.Run("Global variables are initialized", func(t *testing.T) {
		assert.NotEmpty(t, serviceName)
		assert.NotEmpty(t, servicePort)
		assert.NotEmpty(t, uploadsContainerName)
		assert.NotEmpty(t, imagesContainerName)
		assert.NotEmpty(t, jwksURL)
		assert.NotEmpty(t, roleName)
		assert.NotZero(t, memoryLimitMb)
	})
}

// Test URL construction
func TestURLConstruction(t *testing.T) {
	t.Run("Storage URL construction", func(t *testing.T) {
		storageUrl := fmt.Sprintf("https://%s.%s", storageConfig.StorageAccount, storageConfig.StorageAccountSuffix)
		assert.Contains(t, storageUrl, "https://")
		assert.Contains(t, storageUrl, ".blob.core.windows.net")
	})

	t.Run("Blob path construction", func(t *testing.T) {
		collection := "nature"
		album := "sunset"
		filename := "photo1.jpg"
		fileNameWithPrefix := fmt.Sprintf("%s/%s/%s", collection, album, filename)

		assert.Equal(t, "nature/sunset/photo1.jpg", fileNameWithPrefix)
	})
}

// Test query string construction
func TestQueryConstruction(t *testing.T) {
	t.Run("Collection image query", func(t *testing.T) {
		query := fmt.Sprintf("@container='%s' and collectionImage='true'", imagesContainerName)
		expected := fmt.Sprintf("@container='%s' and collectionImage='true'", imagesContainerName)
		assert.Equal(t, expected, query)
	})

	t.Run("Photo query", func(t *testing.T) {
		collection := "test-collection"
		album := "test-album"
		query := fmt.Sprintf("@container='%s' AND collection='%s' AND album='%s' AND isDeleted='false'", imagesContainerName, collection, album)
		assert.Contains(t, query, "test-collection")
		assert.Contains(t, query, "test-album")
		assert.Contains(t, query, "isDeleted='false'")
	})
}

// Test JSON encoding/decoding
func TestJSONHandling(t *testing.T) {
	t.Run("Photo JSON marshaling", func(t *testing.T) {
		photo := models.Photo{
			Src:        "https://example.com/photo.jpg",
			Name:       "photo.jpg",
			Width:      1920,
			Height:     1080,
			Album:      "test-album",
			Collection: "test-collection",
		}

		jsonData, err := json.Marshal(photo)
		assert.NoError(t, err)
		assert.Contains(t, string(jsonData), "photo.jpg")

		var unmarshaled models.Photo
		err = json.Unmarshal(jsonData, &unmarshaled)
		assert.NoError(t, err)
		assert.Equal(t, photo.Name, unmarshaled.Name)
	})
}

// Test CORS configuration
func TestCORSConfiguration(t *testing.T) {
	t.Run("CORS origins parsing", func(t *testing.T) {
		corsString := "http://localhost:5173,https://gallery.bellee.net"
		corsOrigins := strings.Split(corsString, ",")

		assert.Len(t, corsOrigins, 2)
		assert.Contains(t, corsOrigins, "http://localhost:5173")
		assert.Contains(t, corsOrigins, "https://gallery.bellee.net")
	})
}

// Test memory limits
func TestMemoryConstraints(t *testing.T) {
	t.Run("Memory limit calculation", func(t *testing.T) {
		memoryLimit := memoryLimitMb << 20 // 32MB in bytes
		expectedBytes := int64(32 * 1024 * 1024)
		assert.Equal(t, expectedBytes, memoryLimit)
	})
}

// Test all handler creators
func TestAllHandlerCreation(t *testing.T) {
	mockClient := &azblob.Client{}
	storageUrl := "https://example.blob.core.windows.net"
	testRole := "test.role"
	testJwks := "https://test.jwks.url"

	t.Run("Collection handler creation", func(t *testing.T) {
		handler := collectionHandler(mockClient, storageUrl)
		assert.NotNil(t, handler)
	})

	t.Run("Album handler creation", func(t *testing.T) {
		handler := albumHandler(mockClient, storageUrl)
		assert.NotNil(t, handler)
	})

	t.Run("Upload handler creation", func(t *testing.T) {
		handler := uploadHandler(mockClient, storageUrl, testRole, testJwks)
		assert.NotNil(t, handler)
	})

	t.Run("Update handler creation", func(t *testing.T) {
		handler := updateHandler(mockClient, storageUrl, testRole, testJwks)
		assert.NotNil(t, handler)
	})
}

// Test HTTP method validation
func TestHTTPMethods(t *testing.T) {
	t.Run("Valid HTTP methods", func(t *testing.T) {
		validMethods := []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD"}
		for _, method := range validMethods {
			req, err := createTestRequest(method, "/api/test", nil)
			assert.NoError(t, err)
			assert.Equal(t, method, req.Method)
		}
	})

	t.Run("Request with body", func(t *testing.T) {
		body := []byte(`{"test": "data"}`)
		req, err := createTestRequest("POST", "/api/test", body)
		assert.NoError(t, err)
		assert.Equal(t, "POST", req.Method)
		assert.NotNil(t, req.Body)
	})

	t.Run("Request without body", func(t *testing.T) {
		req, err := createTestRequest("GET", "/api/test", nil)
		assert.NoError(t, err)
		assert.Equal(t, "GET", req.Method)
	})
}

// Test path value extraction
func TestPathValueExtraction(t *testing.T) {
	t.Run("Extract collection and album from path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/nature/sunset", nil)
		req.SetPathValue("collection", "nature")
		req.SetPathValue("album", "sunset")

		collection := req.PathValue("collection")
		album := req.PathValue("album")

		assert.Equal(t, "nature", collection)
		assert.Equal(t, "sunset", album)
	})

	t.Run("Extract with URL encoding", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/test%20collection/test%20album", nil)
		req.SetPathValue("collection", "test collection")
		req.SetPathValue("album", "test album")

		collection := req.PathValue("collection")
		album := req.PathValue("album")

		assert.Equal(t, "test collection", collection)
		assert.Equal(t, "test album", album)
	})
}

// Test string conversion utilities
func TestStringConversions(t *testing.T) {
	t.Run("String to int conversion", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected int64
			hasError bool
		}{
			{"1920", 1920, false},
			{"0", 0, false},
			{"-100", -100, false},
			{"invalid", 0, true},
			{"", 0, true},
		}

		for _, tc := range testCases {
			result, err := strconv.ParseInt(tc.input, 10, 32)
			if tc.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		}
	})

	t.Run("String to bool conversion", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected bool
			hasError bool
		}{
			{"true", true, false},
			{"false", false, false},
			{"1", true, false},
			{"0", false, false},
			{"invalid", false, true},
			{"", false, true},
		}

		for _, tc := range testCases {
			result, err := strconv.ParseBool(tc.input)
			if tc.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		}
	})

	t.Run("Int to string conversion", func(t *testing.T) {
		testCases := []struct {
			input    int
			expected string
		}{
			{1920, "1920"},
			{0, "0"},
			{-100, "-100"},
		}

		for _, tc := range testCases {
			result := strconv.Itoa(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})

	t.Run("Bool to string conversion", func(t *testing.T) {
		testCases := []struct {
			input    bool
			expected string
		}{
			{true, "true"},
			{false, "false"},
		}

		for _, tc := range testCases {
			result := strconv.FormatBool(tc.input)
			assert.Equal(t, tc.expected, result)
		}
	})
}

// Test extended data structures
func TestExtendedDataStructures(t *testing.T) {
	t.Run("Photo model with all fields", func(t *testing.T) {
		photo := models.Photo{
			Src:             "https://example.com/photo.jpg",
			Name:            "photo.jpg",
			Width:           1920,
			Height:          1080,
			Album:           "test-album",
			Collection:      "test-collection",
			Description:     "Test description",
			ExifData:        `{"camera": "Canon"}`,
			IsDeleted:       false,
			Orientation:     1,
			AlbumImage:      true,
			CollectionImage: false,
		}

		assert.Equal(t, "photo.jpg", photo.Name)
		assert.Equal(t, 1920, photo.Width)
		assert.Equal(t, 1080, photo.Height)
		assert.Equal(t, "test-album", photo.Album)
		assert.Equal(t, "test-collection", photo.Collection)
		assert.Equal(t, "Test description", photo.Description)
		assert.Contains(t, photo.ExifData, "Canon")
		assert.False(t, photo.IsDeleted)
		assert.Equal(t, 1, photo.Orientation)
		assert.True(t, photo.AlbumImage)
		assert.False(t, photo.CollectionImage)
	})

	t.Run("Blob model structure", func(t *testing.T) {
		tags := map[string]string{
			"collection": "nature",
			"album":      "sunset",
			"isDeleted":  "false",
		}
		metadata := map[string]string{
			"Width":  "1920",
			"Height": "1080",
		}

		blob := models.Blob{
			Name:     "nature/sunset/photo1.jpg",
			Path:     "https://example.blob.core.windows.net/images/nature/sunset/photo1.jpg",
			Tags:     tags,
			MetaData: metadata,
		}

		assert.Equal(t, "nature/sunset/photo1.jpg", blob.Name)
		assert.Contains(t, blob.Path, "photo1.jpg")
		assert.Equal(t, "nature", blob.Tags["collection"])
		assert.Equal(t, "sunset", blob.Tags["album"])
		assert.Equal(t, "1920", blob.MetaData["Width"])
		assert.Equal(t, "1080", blob.MetaData["Height"])
	})

	t.Run("StorageConfig structure", func(t *testing.T) {
		config := models.StorageConfig{
			StorageAccount:       "teststorage",
			StorageAccountSuffix: "blob.core.windows.net",
		}

		assert.Equal(t, "teststorage", config.StorageAccount)
		assert.Equal(t, "blob.core.windows.net", config.StorageAccountSuffix)

		// Test URL construction with config
		storageUrl := fmt.Sprintf("https://%s.%s", config.StorageAccount, config.StorageAccountSuffix)
		assert.Equal(t, "https://teststorage.blob.core.windows.net", storageUrl)
	})
}

// Test complex query construction
func TestComplexQueryConstruction(t *testing.T) {
	t.Run("Collection query with special characters", func(t *testing.T) {
		collection := "test-collection"
		query := fmt.Sprintf("@container='%s' and collection='%s' and albumImage='true'", imagesContainerName, collection)

		assert.Contains(t, query, imagesContainerName)
		assert.Contains(t, query, "test-collection")
		assert.Contains(t, query, "albumImage='true'")
	})

	t.Run("Album query construction", func(t *testing.T) {
		collection := "nature"
		album := "mountains"
		query := fmt.Sprintf("@container='%s' AND collection='%s' AND album='%s' AND isDeleted='false'", imagesContainerName, collection, album)

		assert.Contains(t, query, "nature")
		assert.Contains(t, query, "mountains")
		assert.Contains(t, query, "isDeleted='false'")
	})

	t.Run("Collection image fallback query", func(t *testing.T) {
		query := fmt.Sprintf("@container='%s'", imagesContainerName)
		expected := fmt.Sprintf("@container='%s'", imagesContainerName)
		assert.Equal(t, expected, query)
	})
}

// Test JSON marshaling for different data types
func TestAdvancedJSONHandling(t *testing.T) {
	t.Run("ImageTags JSON marshaling", func(t *testing.T) {
		tags := models.ImageTags{
			Description:     "Beautiful sunset",
			Collection:      "nature",
			Album:           "sunset",
			Type:            "image/jpeg",
			CollectionImage: true,
			AlbumImage:      false,
			IsDeleted:       false,
		}

		jsonData, err := json.Marshal(tags)
		assert.NoError(t, err)
		assert.Contains(t, string(jsonData), "Beautiful sunset")
		assert.Contains(t, string(jsonData), "nature")
		assert.Contains(t, string(jsonData), "sunset")

		var unmarshaled models.ImageTags
		err = json.Unmarshal(jsonData, &unmarshaled)
		assert.NoError(t, err)
		assert.Equal(t, tags.Description, unmarshaled.Description)
		assert.Equal(t, tags.Collection, unmarshaled.Collection)
		assert.Equal(t, tags.Album, unmarshaled.Album)
	})

	t.Run("Map to JSON conversion", func(t *testing.T) {
		testMap := map[string]string{
			"collection":      "nature",
			"album":           "sunset",
			"collectionImage": "true",
			"albumImage":      "false",
			"isDeleted":       "false",
		}

		jsonData, err := json.Marshal(testMap)
		assert.NoError(t, err)

		var unmarshaled map[string]string
		err = json.Unmarshal(jsonData, &unmarshaled)
		assert.NoError(t, err)
		assert.Equal(t, testMap["collection"], unmarshaled["collection"])
		assert.Equal(t, testMap["album"], unmarshaled["album"])
	})

	t.Run("Array JSON marshaling", func(t *testing.T) {
		photos := []models.Photo{
			{
				Src:        "https://example.com/photo1.jpg",
				Name:       "photo1.jpg",
				Width:      1920,
				Height:     1080,
				Collection: "nature",
				Album:      "sunset",
			},
			{
				Src:        "https://example.com/photo2.jpg",
				Name:       "photo2.jpg",
				Width:      1600,
				Height:     900,
				Collection: "nature",
				Album:      "mountains",
			},
		}

		jsonData, err := json.Marshal(photos)
		assert.NoError(t, err)
		assert.Contains(t, string(jsonData), "photo1.jpg")
		assert.Contains(t, string(jsonData), "photo2.jpg")

		var unmarshaled []models.Photo
		err = json.Unmarshal(jsonData, &unmarshaled)
		assert.NoError(t, err)
		assert.Len(t, unmarshaled, 2)
		assert.Equal(t, photos[0].Name, unmarshaled[0].Name)
		assert.Equal(t, photos[1].Name, unmarshaled[1].Name)
	})
}

// Test environment variable edge cases
func TestEnvironmentVariableEdgeCases(t *testing.T) {
	t.Run("Default values are used", func(t *testing.T) {
		// These should use default values since env vars aren't set in test
		assert.Equal(t, "photoService", serviceName)
		assert.Equal(t, "8080", servicePort)
		assert.Equal(t, "uploads", uploadsContainerName)
		assert.Equal(t, "images", imagesContainerName)
		assert.Equal(t, "photo.upload", roleName)
		assert.Equal(t, int64(32), memoryLimitMb)
	})

	t.Run("Storage config validation", func(t *testing.T) {
		assert.NotEmpty(t, storageConfig.StorageAccount)
		assert.NotEmpty(t, storageConfig.StorageAccountSuffix)
		assert.Contains(t, storageConfig.StorageAccountSuffix, "blob.core.windows.net")
	})

	t.Run("Boolean flag validation", func(t *testing.T) {
		// In test environment, should be false since CONTAINER_APP_NAME is not set
		assert.False(t, isProduction)
	})
}

// Test file naming patterns
func TestFileNamingPatterns(t *testing.T) {
	t.Run("File path construction", func(t *testing.T) {
		collection := "nature"
		album := "sunset"
		filename := "IMG_001.jpg"

		fileNameWithPrefix := fmt.Sprintf("%s/%s/%s", collection, album, filename)
		expectedPath := "nature/sunset/IMG_001.jpg"

		assert.Equal(t, expectedPath, fileNameWithPrefix)
	})

	t.Run("Blob URL construction", func(t *testing.T) {
		storageUrl := "https://example.blob.core.windows.net"
		container := "images"
		blobName := "nature/sunset/photo.jpg"

		fullPath := fmt.Sprintf("%s/%s/%s", storageUrl, container, blobName)
		expected := "https://example.blob.core.windows.net/images/nature/sunset/photo.jpg"

		assert.Equal(t, expected, fullPath)
	})

	t.Run("Tag map construction", func(t *testing.T) {
		tags := make(map[string]string)
		tags["name"] = "nature/sunset/photo.jpg"
		tags["description"] = "Beautiful sunset photo"
		tags["collection"] = "nature"
		tags["album"] = "sunset"
		tags["isDeleted"] = "false"
		tags["collectionImage"] = "true"
		tags["albumImage"] = "false"

		assert.Len(t, tags, 7)
		assert.Equal(t, "nature/sunset/photo.jpg", tags["name"])
		assert.Equal(t, "Beautiful sunset photo", tags["description"])
		assert.Equal(t, "false", tags["isDeleted"])
	})
}

// Test HTTP response patterns
func TestHTTPResponsePatterns(t *testing.T) {
	t.Run("Content-Type header setting", func(t *testing.T) {
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/json")

		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	})

	t.Run("JSON encoding to response", func(t *testing.T) {
		w := httptest.NewRecorder()
		testData := map[string]string{"test": "data"}

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(testData)

		assert.NoError(t, err)
		assert.Contains(t, w.Body.String(), "test")
		assert.Contains(t, w.Body.String(), "data")
	})
}

// Test context operations
func TestContextOperations(t *testing.T) {
	t.Run("Background context creation", func(t *testing.T) {
		ctx := context.Background()
		assert.NotNil(t, ctx)
		assert.NoError(t, ctx.Err())
	})

	t.Run("Context with cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		assert.NotNil(t, ctx)
		assert.NotNil(t, cancel)

		cancel()
		assert.Error(t, ctx.Err())
	})
}

// Benchmark tests
func BenchmarkJSONMarshaling(b *testing.B) {
	photo := models.Photo{
		Src:        "https://example.com/photo.jpg",
		Name:       "photo.jpg",
		Width:      1920,
		Height:     1080,
		Album:      "test-album",
		Collection: "test-collection",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(photo)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStringFormatting(b *testing.B) {
	collection := "nature"
	album := "sunset"
	filename := "photo.jpg"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%s/%s/%s", collection, album, filename)
	}
}

func BenchmarkMapOperations(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tags := make(map[string]string)
		tags["collection"] = "nature"
		tags["album"] = "sunset"
		tags["isDeleted"] = "false"
		_ = tags["collection"]
	}
}

// Test error handling patterns
func TestErrorHandling(t *testing.T) {
	t.Run("String conversion errors", func(t *testing.T) {
		invalidStrings := []string{"", "invalid", "1.5", "true", "null"}
		for _, str := range invalidStrings {
			_, err := strconv.ParseInt(str, 10, 32)
			if str == "" || str == "invalid" || str == "1.5" || str == "true" || str == "null" {
				assert.Error(t, err, "Expected error for input: %s", str)
			}
		}
	})

	t.Run("JSON decoding errors", func(t *testing.T) {
		invalidJSON := []string{
			"",
			"{invalid}",
			`{"incomplete":`,
			"null",
			"[]",
		}

		for _, jsonStr := range invalidJSON {
			var result map[string]string
			err := json.Unmarshal([]byte(jsonStr), &result)
			if jsonStr != "null" && jsonStr != "[]" {
				assert.Error(t, err, "Expected error for JSON: %s", jsonStr)
			}
		}
	})
}

// Test edge cases in data processing
func TestDataProcessingEdgeCases(t *testing.T) {
	t.Run("Empty photo array handling", func(t *testing.T) {
		photos := []models.Photo{}
		jsonData, err := json.Marshal(photos)
		assert.NoError(t, err)
		assert.Equal(t, "[]", string(jsonData))

		var unmarshaled []models.Photo
		err = json.Unmarshal(jsonData, &unmarshaled)
		assert.NoError(t, err)
		assert.Len(t, unmarshaled, 0)
	})

	t.Run("Nil map handling", func(t *testing.T) {
		var tags map[string]string
		assert.Nil(t, tags)

		// Initialize map
		tags = make(map[string]string)
		assert.NotNil(t, tags)
		assert.Len(t, tags, 0)
	})

	t.Run("Empty string handling in path construction", func(t *testing.T) {
		collection := ""
		album := ""
		filename := "photo.jpg"

		fileNameWithPrefix := fmt.Sprintf("%s/%s/%s", collection, album, filename)
		expected := "//photo.jpg"

		assert.Equal(t, expected, fileNameWithPrefix)
	})
}

// Test HTTP status code patterns
func TestHTTPStatusCodes(t *testing.T) {
	t.Run("Common HTTP status codes", func(t *testing.T) {
		statusCodes := map[int]string{
			200: "OK",
			201: "Created",
			304: "Not Modified",
			400: "Bad Request",
			401: "Unauthorized",
			404: "Not Found",
			500: "Internal Server Error",
		}

		for code, description := range statusCodes {
			assert.Greater(t, code, 0)
			assert.NotEmpty(t, description)
		}
	})

	t.Run("Response writer status setting", func(t *testing.T) {
		w := httptest.NewRecorder()
		w.WriteHeader(http.StatusNotFound)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// Test multipart form parsing patterns
func TestMultipartFormPatterns(t *testing.T) {
	t.Run("Memory limit calculations", func(t *testing.T) {
		limits := []int64{1, 16, 32, 64, 128}
		for _, limit := range limits {
			bytes := limit << 20 // Convert MB to bytes
			expected := limit * 1024 * 1024
			assert.Equal(t, expected, bytes)
		}
	})

	t.Run("Form data structure", func(t *testing.T) {
		metadata := models.ImageTags{
			Description:     "Test upload",
			Collection:      "test",
			Album:           "uploads",
			Type:            "image/jpeg",
			CollectionImage: false,
			AlbumImage:      false,
			IsDeleted:       false,
		}

		jsonData, err := json.Marshal(metadata)
		assert.NoError(t, err)

		var unmarshaled models.ImageTags
		err = json.Unmarshal(jsonData, &unmarshaled)
		assert.NoError(t, err)
		assert.Equal(t, metadata.Description, unmarshaled.Description)
		assert.Equal(t, metadata.Type, unmarshaled.Type)
	})
}

// Test configuration validation
func TestConfigurationValidation(t *testing.T) {
	t.Run("Required environment variables exist", func(t *testing.T) {
		requiredVars := []string{
			serviceName,
			servicePort,
			uploadsContainerName,
			imagesContainerName,
			roleName,
		}

		for _, variable := range requiredVars {
			assert.NotEmpty(t, variable, "Required variable should not be empty")
		}
	})

	t.Run("Storage configuration completeness", func(t *testing.T) {
		assert.NotEmpty(t, storageConfig.StorageAccount)
		assert.NotEmpty(t, storageConfig.StorageAccountSuffix)

		// Validate storage URL construction
		storageURL := fmt.Sprintf("https://%s.%s",
			storageConfig.StorageAccount,
			storageConfig.StorageAccountSuffix)

		assert.Contains(t, storageURL, "https://")
		assert.Contains(t, storageURL, storageConfig.StorageAccount)
		assert.Contains(t, storageURL, storageConfig.StorageAccountSuffix)
	})

	t.Run("Memory limit bounds", func(t *testing.T) {
		assert.Greater(t, memoryLimitMb, int64(0))
		assert.LessOrEqual(t, memoryLimitMb, int64(1024)) // Max 1GB seems reasonable
	})
}

// Test query parameter edge cases
func TestQueryParameterEdgeCases(t *testing.T) {
	t.Run("Special characters in collection names", func(t *testing.T) {
		collections := []string{
			"test-collection",
			"test_collection",
			"test collection",
			"test.collection",
			"123collection",
		}

		for _, collection := range collections {
			query := fmt.Sprintf("@container='%s' and collection='%s'",
				imagesContainerName, collection)
			assert.Contains(t, query, collection)
			assert.Contains(t, query, imagesContainerName)
		}
	})

	t.Run("Boolean values in queries", func(t *testing.T) {
		boolValues := []bool{true, false}
		for _, val := range boolValues {
			query := fmt.Sprintf("@container='%s' and isDeleted='%s'",
				imagesContainerName, strconv.FormatBool(val))

			expected := fmt.Sprintf("@container='%s' and isDeleted='%s'",
				imagesContainerName, strconv.FormatBool(val))

			assert.Equal(t, expected, query)
		}
	})
}

// Test array and slice operations
func TestArraySliceOperations(t *testing.T) {
	t.Run("Photo array manipulation", func(t *testing.T) {
		photos := []models.Photo{}
		assert.Len(t, photos, 0)

		// Add photos
		photo1 := models.Photo{Name: "photo1.jpg", Collection: "nature"}
		photo2 := models.Photo{Name: "photo2.jpg", Collection: "architecture"}

		photos = append(photos, photo1)
		photos = append(photos, photo2)

		assert.Len(t, photos, 2)
		assert.Equal(t, "photo1.jpg", photos[0].Name)
		assert.Equal(t, "photo2.jpg", photos[1].Name)
	})

	t.Run("CORS origins array handling", func(t *testing.T) {
		corsString := "http://localhost:3000,https://app.example.com,https://api.example.com"
		origins := strings.Split(corsString, ",")

		assert.Len(t, origins, 3)
		assert.Equal(t, "http://localhost:3000", origins[0])
		assert.Equal(t, "https://app.example.com", origins[1])
		assert.Equal(t, "https://api.example.com", origins[2])
	})
}

// Test timeout and cancellation patterns
func TestTimeoutCancellationPatterns(t *testing.T) {
	t.Run("Context deadline exceeded", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Simulate cancellation
		cancel()

		select {
		case <-ctx.Done():
			assert.Error(t, ctx.Err())
			assert.Contains(t, ctx.Err().Error(), "canceled")
		default:
			t.Error("Expected context to be cancelled")
		}
	})

	t.Run("Background context properties", func(t *testing.T) {
		ctx := context.Background()
		assert.NotNil(t, ctx)
		assert.NoError(t, ctx.Err())

		// Background context should never be done
		select {
		case <-ctx.Done():
			t.Error("Background context should never be done")
		default:
			// Expected behavior
		}
	})
}
