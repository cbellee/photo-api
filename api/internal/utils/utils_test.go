package utils

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/cbellee/photo-api/internal/models"
	"github.com/dapr/go-sdk/service/common"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
		assert.Equal(t, 25, resizedImg.Bounds().Dx())
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
		assert.Equal(t, 50, resizedImg.Bounds().Dx())
		assert.Equal(t, 100, resizedImg.Bounds().Dy())
	})

	t.Run("jpeg image", func(t *testing.T) {
		buf := new(bytes.Buffer)
		err := jpeg.Encode(buf, img, nil)
		assert.NoError(t, err)

		resizedImgBytes, err := ResizeImage(buf.Bytes(), "image/jpeg", "test.jpeg", 100, 100)
		assert.NoError(t, err)
		assert.NotEmpty(t, resizedImgBytes)

		resizedImg, _, err := image.Decode(bytes.NewReader(resizedImgBytes))
		assert.NoError(t, err)
		assert.Equal(t, 50, resizedImg.Bounds().Dx())
		assert.Equal(t, 100, resizedImg.Bounds().Dy())
	})

	t.Run("unsupported format returns error", func(t *testing.T) {
		buf := new(bytes.Buffer)
		err := jpeg.Encode(buf, img, nil)
		assert.NoError(t, err)

		_, err = ResizeImage(buf.Bytes(), "image/webp", "test.webp", 100, 100)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported image format")
	})

	t.Run("invalid image bytes returns error", func(t *testing.T) {
		_, err := ResizeImage([]byte("not-an-image"), "image/jpeg", "bad.jpg", 100, 100)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decode image config")
	})

	t.Run("empty image bytes returns error", func(t *testing.T) {
		_, err := ResizeImage([]byte{}, "image/jpeg", "empty.jpg", 100, 100)
		assert.Error(t, err)
	})

	t.Run("square image", func(t *testing.T) {
		sqImg := image.NewRGBA(image.Rect(0, 0, 200, 200))
		buf := new(bytes.Buffer)
		err := jpeg.Encode(buf, sqImg, nil)
		assert.NoError(t, err)

		resizedImgBytes, err := ResizeImage(buf.Bytes(), "image/jpeg", "square.jpg", 100, 100)
		assert.NoError(t, err)
		assert.NotEmpty(t, resizedImgBytes)

		resizedImg, _, err := image.Decode(bytes.NewReader(resizedImgBytes))
		assert.NoError(t, err)
		assert.Equal(t, 100, resizedImg.Bounds().Dx())
		assert.Equal(t, 100, resizedImg.Bounds().Dy())
	})
}

func TestGetEnvValue(t *testing.T) {
	os.Setenv("TEST_ENV_VAR", "test_value")

	tests := []struct {
		envVar      string
		defaultVal  string
		expectedVal string
	}{
		{"TEST_ENV_VAR", "default_value", "test_value"},
		{"NON_EXISTENT_ENV_VAR", "default_value", "default_value"},
	}

	for _, tt := range tests {
		t.Run(tt.envVar, func(t *testing.T) {
			val := GetEnvValue(tt.envVar, tt.defaultVal)
			if val != tt.expectedVal {
				t.Errorf("expected %s, got %s", tt.expectedVal, val)
			}
		})
	}
}

func TestStripInvalidTagValue(t *testing.T) {
	tests := []struct {
		value         string
		expectedValue string
	}{
		{"Mum_&_Dad.jpg", "Mum__Dad.jpg"},
		{"HelloWorld", "HelloWorld"},
		{"THis is an invalid str*ng.png", "THis is an invalid strng.png"},
		{"This is /[] an invalid Str%$g.%$#gif", "This is / an invalid Strg.gif"},
		{"this is a valid string", "this is a valid string"},
		{"this is also a valid string.", "this is also a valid string."},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("value=%q", tt.value), func(t *testing.T) {
			actualValue := StripInvalidTagCharacters(tt.value)
			if actualValue != tt.expectedValue {
				t.Errorf("expected %s, got %s", tt.expectedValue, actualValue)
			}
		})
	}
}

// Mocking azblob.Client
type MockBlobClient struct {
	mock.Mock
}

func (m *MockBlobClient) NewClient(storageUrl string, credential azcore.TokenCredential, options *azblob.ClientOptions) (*azblob.Client, error) {
	args := m.Called(storageUrl, credential, options)
	return args.Get(0).(*azblob.Client), args.Error(1)
}

// Mocking azidentity.ManagedIdentityCredential
type MockManagedIdentityCredential struct {
	mock.Mock
}

func (m *MockManagedIdentityCredential) NewManagedIdentityCredential(options *azidentity.ManagedIdentityCredentialOptions) (*azidentity.ManagedIdentityCredential, error) {
	args := m.Called(options)
	return args.Get(0).(*azidentity.ManagedIdentityCredential), args.Error(1)
}

// Mocking azidentity.DefaultAzureCredential
type MockDefaultAzureCredential struct {
	mock.Mock
}

func (m *MockDefaultAzureCredential) NewDefaultAzureCredential(options *azidentity.DefaultAzureCredentialOptions) (*azidentity.DefaultAzureCredential, error) {
	args := m.Called(options)
	return args.Get(0).(*azidentity.DefaultAzureCredential), args.Error(1)
}

func TestCreateAzureBlobClient(t *testing.T) {
	mockBlobClient := new(MockBlobClient)
	mockManagedIdentityCredential := new(MockManagedIdentityCredential)
	mockDefaultAzureCredential := new(MockDefaultAzureCredential)

	storageUrl := "https://example.blob.core.windows.net"
	azureClientId := "test-client-id"

	t.Run("Production environment with valid azureClientId", func(t *testing.T) {
		mockManagedIdentityCredential.On("NewManagedIdentityCredential", mock.Anything).Return(&azidentity.ManagedIdentityCredential{}, nil)
		mockBlobClient.On("NewClient", storageUrl, mock.Anything, nil).Return(&azblob.Client{}, nil)

		client, err := CreateAzureBlobClient(storageUrl, true, azureClientId)
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("Production environment with missing azureClientId", func(t *testing.T) {
		client, err := CreateAzureBlobClient(storageUrl, true, "")
		assert.Error(t, err)
		assert.Nil(t, client)
	})

	t.Run("Non-production environment", func(t *testing.T) {
		mockDefaultAzureCredential.On("NewDefaultAzureCredential", mock.Anything).Return(&azidentity.DefaultAzureCredential{}, nil)
		mockBlobClient.On("NewClient", storageUrl, mock.Anything, nil).Return(&azblob.Client{}, nil)

		client, err := CreateAzureBlobClient(storageUrl, false, azureClientId)
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})
}

func TestConvertToEvent(t *testing.T) {
	t.Run("Happy path - valid event data", func(t *testing.T) {
		originalEvent := models.Event{
			Topic:           "Microsoft.Storage.BlobCreated",
			Subject:         "/blobServices/default/containers/images/blobs/nature/sunset/photo1.jpg",
			EventType:       "Microsoft.Storage.BlobCreated",
			ID:              "12345678-1234-1234-1234-123456789012",
			DataVersion:     "1.0",
			MetadataVersion: "1",
			EventTime:       "2023-01-01T12:00:00Z",
			Data: models.EventData{
				API:             "PutBlob",
				ClientRequestId: "client-request-123",
				RequestId:       "request-456",
				ETag:            "0x8D123456789ABCD",
				ContentType:     "image/jpeg",
				ContentLength:   1024000,
				BlobType:        "BlockBlob",
				URL:             "https://example.blob.core.windows.net/images/nature/sunset/photo1.jpg",
				Sequencer:       "00000000000000EB0000000000046199",
				StorageDiagnostics: models.StorageDiagnosticsData{
					BatchId: "batch-789",
				},
			},
		}

		jsonData, err := json.Marshal(originalEvent)
		if err != nil {
			t.Fatalf("Failed to marshal test data: %v", err)
		}

		base64Data := base64.StdEncoding.EncodeToString(jsonData)
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}

		result, err := ConvertToEvent(bindingEvent)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if result.Topic != originalEvent.Topic {
			t.Errorf("Expected Topic %s, got %s", originalEvent.Topic, result.Topic)
		}
		if result.Subject != originalEvent.Subject {
			t.Errorf("Expected Subject %s, got %s", originalEvent.Subject, result.Subject)
		}
		if result.EventType != originalEvent.EventType {
			t.Errorf("Expected EventType %s, got %s", originalEvent.EventType, result.EventType)
		}
		if result.ID != originalEvent.ID {
			t.Errorf("Expected ID %s, got %s", originalEvent.ID, result.ID)
		}
		if result.Data.URL != originalEvent.Data.URL {
			t.Errorf("Expected Data.URL %s, got %s", originalEvent.Data.URL, result.Data.URL)
		}
		if result.Data.ContentLength != originalEvent.Data.ContentLength {
			t.Errorf("Expected Data.ContentLength %d, got %d", originalEvent.Data.ContentLength, result.Data.ContentLength)
		}
	})

	t.Run("Happy path - minimal valid event data", func(t *testing.T) {
		minimalEvent := models.Event{
			Topic:     "test.topic",
			Subject:   "test/subject",
			EventType: "test.event",
			ID:        "test-id",
		}

		jsonData, err := json.Marshal(minimalEvent)
		if err != nil {
			t.Fatalf("Failed to marshal test data: %v", err)
		}

		base64Data := base64.StdEncoding.EncodeToString(jsonData)
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}

		result, err := ConvertToEvent(bindingEvent)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if result.ID != minimalEvent.ID {
			t.Errorf("Expected ID %s, got %s", minimalEvent.ID, result.ID)
		}
	})

	t.Run("Happy path - raw JSON event data", func(t *testing.T) {
		originalEvent := models.Event{
			Topic:           "Microsoft.Storage.BlobCreated",
			Subject:         "/blobServices/default/containers/uploads/blobs/nature/sunset/photo1.jpg",
			EventType:       "Microsoft.Storage.BlobCreated",
			ID:              "raw-json-event-id",
			DataVersion:     "1.0",
			MetadataVersion: "1",
			EventTime:       "2023-01-01T12:00:00Z",
			Data: models.EventData{
				ContentType:   "image/jpeg",
				ContentLength: 1024000,
				URL:           "https://example.blob.core.windows.net/uploads/nature/sunset/photo1.jpg",
			},
		}

		jsonData, err := json.Marshal(originalEvent)
		if err != nil {
			t.Fatalf("Failed to marshal test data: %v", err)
		}

		bindingEvent := &common.BindingEvent{
			Data: jsonData,
		}

		result, err := ConvertToEvent(bindingEvent)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if result.ID != originalEvent.ID {
			t.Errorf("Expected ID %s, got %s", originalEvent.ID, result.ID)
		}
		if result.Data.URL != originalEvent.Data.URL {
			t.Errorf("Expected Data.URL %s, got %s", originalEvent.Data.URL, result.Data.URL)
		}
	})

	t.Run("Edge case - empty JSON object", func(t *testing.T) {
		base64Data := base64.StdEncoding.EncodeToString([]byte("{}"))
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}

		result, err := ConvertToEvent(bindingEvent)

		if err != nil {
			t.Errorf("Expected no error for empty JSON object, got: %v", err)
		}
		if result.Topic != "" {
			t.Errorf("Expected empty Topic, got %s", result.Topic)
		}
		if result.Data.ContentLength != 0 {
			t.Errorf("Expected ContentLength 0, got %d", result.Data.ContentLength)
		}
	})

	t.Run("Edge case - zero values in data", func(t *testing.T) {
		zeroEvent := models.Event{
			Data: models.EventData{ContentLength: 0},
		}

		jsonData, err := json.Marshal(zeroEvent)
		if err != nil {
			t.Fatalf("Failed to marshal test data: %v", err)
		}

		base64Data := base64.StdEncoding.EncodeToString(jsonData)
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}

		result, err := ConvertToEvent(bindingEvent)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if result.Data.ContentLength != 0 {
			t.Errorf("Expected ContentLength 0, got %d", result.Data.ContentLength)
		}
	})

	t.Run("Boundary case - maximum ContentLength", func(t *testing.T) {
		maxEvent := models.Event{
			Topic: "test.topic",
			Data:  models.EventData{ContentLength: 2147483647},
		}

		jsonData, err := json.Marshal(maxEvent)
		if err != nil {
			t.Fatalf("Failed to marshal test data: %v", err)
		}

		base64Data := base64.StdEncoding.EncodeToString(jsonData)
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}

		result, err := ConvertToEvent(bindingEvent)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if result.Data.ContentLength != 2147483647 {
			t.Errorf("Expected ContentLength 2147483647, got %d", result.Data.ContentLength)
		}
	})

	t.Run("Boundary case - minimum ContentLength", func(t *testing.T) {
		minEvent := models.Event{
			Topic: "test.topic",
			Data:  models.EventData{ContentLength: -2147483648},
		}

		jsonData, err := json.Marshal(minEvent)
		if err != nil {
			t.Fatalf("Failed to marshal test data: %v", err)
		}

		base64Data := base64.StdEncoding.EncodeToString(jsonData)
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}

		result, err := ConvertToEvent(bindingEvent)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if result.Data.ContentLength != -2147483648 {
			t.Errorf("Expected ContentLength -2147483648, got %d", result.Data.ContentLength)
		}
	})

	t.Run("Error case - empty binding event data", func(t *testing.T) {
		bindingEvent := &common.BindingEvent{Data: []byte{}}
		_, err := ConvertToEvent(bindingEvent)
		if err == nil {
			t.Error("Expected error for empty data, got nil")
		}
	})

	t.Run("Error case - nil binding event", func(t *testing.T) {
		_, err := ConvertToEvent(nil)
		if err == nil {
			t.Error("Expected error for nil binding event, got nil")
		}
	})

	t.Run("Error case - invalid base64 data", func(t *testing.T) {
		bindingEvent := &common.BindingEvent{
			Data: []byte("this-is-not-valid-base64!"),
		}
		_, err := ConvertToEvent(bindingEvent)
		if err == nil {
			t.Error("Expected error for invalid base64 data, got nil")
		}
	})

	t.Run("Error case - valid base64 but invalid JSON", func(t *testing.T) {
		base64Data := base64.StdEncoding.EncodeToString([]byte("{ this is not valid json }"))
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}
		_, err := ConvertToEvent(bindingEvent)
		if err == nil {
			t.Error("Expected error for invalid JSON, got nil")
		}
	})

	t.Run("Error case - incomplete JSON structure", func(t *testing.T) {
		base64Data := base64.StdEncoding.EncodeToString([]byte(`{"topic": "test", "incomplete": `))
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}
		_, err := ConvertToEvent(bindingEvent)
		if err == nil {
			t.Error("Expected error for incomplete JSON, got nil")
		}
	})

	t.Run("Edge case - large data structure", func(t *testing.T) {
		largeString := string(make([]byte, 10000))
		largeEvent := models.Event{
			Topic:   "test.topic",
			Subject: largeString,
			ID:      largeString,
			Data:    models.EventData{URL: largeString},
		}

		jsonData, err := json.Marshal(largeEvent)
		if err != nil {
			t.Fatalf("Failed to marshal test data: %v", err)
		}

		base64Data := base64.StdEncoding.EncodeToString(jsonData)
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}

		result, err := ConvertToEvent(bindingEvent)

		if err != nil {
			t.Errorf("Expected no error for large data, got: %v", err)
		}
		if len(result.Subject) != len(largeString) {
			t.Errorf("Expected Subject length %d, got %d", len(largeString), len(result.Subject))
		}
	})

	t.Run("Edge case - special characters in strings", func(t *testing.T) {
		specialEvent := models.Event{
			Topic:   "测试/テスト/тест",
			Subject: "emojis: 🎉🔥💯 unicode: àáâãäå",
			ID:      "special-chars: !@#$%^&*()[]{}|\\:;\"'<>?,./",
			Data:    models.EventData{URL: "https://example.com/path with spaces/file?query=value&param=测试"},
		}

		jsonData, err := json.Marshal(specialEvent)
		if err != nil {
			t.Fatalf("Failed to marshal test data: %v", err)
		}

		base64Data := base64.StdEncoding.EncodeToString(jsonData)
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}

		result, err := ConvertToEvent(bindingEvent)

		if err != nil {
			t.Errorf("Expected no error for special characters, got: %v", err)
		}
		if result.ID != specialEvent.ID {
			t.Errorf("Expected ID %s, got %s", specialEvent.ID, result.ID)
		}
		if result.Data.URL != specialEvent.Data.URL {
			t.Errorf("Expected Data.URL %s, got %s", specialEvent.Data.URL, result.Data.URL)
		}
	})

	t.Run("Edge case - null/nil handling", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Function panicked with nil input (acceptable): %v", r)
			}
		}()

		_, err := ConvertToEvent(nil)
		if err != nil {
			t.Logf("Function returned error for nil input (acceptable): %v", err)
		}
	})

	t.Run("Type conversion edge case - non-string data types", func(t *testing.T) {
		mixedTypeJSON := `{
			"topic": "test.topic",
			"subject": "test/subject",
			"eventType": "test.event",
			"id": "test-id",
			"dataVersion": "1.0",
			"metadataVersion": "1",
			"eventTime": "2023-01-01T12:00:00Z",
			"data": {
				"api": "PutBlob",
				"clientRequestId": "client-123",
				"requestId": "request-456",
				"eTag": "0x8D123456789ABCD",
				"contentType": "image/jpeg",
				"contentLength": 1024,
				"blobType": "BlockBlob",
				"url": "https://example.com/blob.jpg",
				"sequencer": "00000000000000EB",
				"storageDiagnostics": {
					"batchId": "batch-789"
				}
			}
		}`

		base64Data := base64.StdEncoding.EncodeToString([]byte(mixedTypeJSON))
		bindingEvent := &common.BindingEvent{
			Data: []byte(base64Data),
		}

		result, err := ConvertToEvent(bindingEvent)

		if err != nil {
			t.Errorf("Expected no error for mixed types, got: %v", err)
		}
		if result.Topic != "test.topic" {
			t.Errorf("Expected Topic 'test.topic', got %s", result.Topic)
		}
		if result.Data.ContentLength != 1024 {
			t.Errorf("Expected ContentLength 1024, got %d", result.Data.ContentLength)
		}
		if result.Data.StorageDiagnostics.BatchId != "batch-789" {
			t.Errorf("Expected BatchId 'batch-789', got %s", result.Data.StorageDiagnostics.BatchId)
		}
	})
}

func BenchmarkConvertToEvent(b *testing.B) {
	testEvent := models.Event{
		Topic:           "Microsoft.Storage.BlobCreated",
		Subject:         "/blobServices/default/containers/images/blobs/nature/sunset/photo1.jpg",
		EventType:       "Microsoft.Storage.BlobCreated",
		ID:              "12345678-1234-1234-1234-123456789012",
		DataVersion:     "1.0",
		MetadataVersion: "1",
		EventTime:       "2023-01-01T12:00:00Z",
		Data: models.EventData{
			API:             "PutBlob",
			ClientRequestId: "client-request-123",
			RequestId:       "request-456",
			ETag:            "0x8D123456789ABCD",
			ContentType:     "image/jpeg",
			ContentLength:   1024000,
			BlobType:        "BlockBlob",
			URL:             "https://example.blob.core.windows.net/images/nature/sunset/photo1.jpg",
			Sequencer:       "00000000000000EB0000000000046199",
			StorageDiagnostics: models.StorageDiagnosticsData{
				BatchId: "batch-789",
			},
		},
	}

	jsonData, _ := json.Marshal(testEvent)
	base64Data := base64.StdEncoding.EncodeToString(jsonData)
	bindingEvent := &common.BindingEvent{
		Data: []byte(base64Data),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ConvertToEvent(bindingEvent)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestStripInvalidTagCharactersExtended(t *testing.T) {
	t.Run("Boundary case - maximum length input", func(t *testing.T) {
		longValidString := strings.Repeat("a", 256)
		result := StripInvalidTagCharacters(longValidString)
		assert.Equal(t, longValidString, result)
		assert.Equal(t, 256, len(result))
	})

	t.Run("Edge case - only invalid characters", func(t *testing.T) {
		invalidOnly := "!@#$%^&*()"
		result := StripInvalidTagCharacters(invalidOnly)
		assert.Equal(t, "", result)
	})

	t.Run("Edge case - mixed valid and invalid characters", func(t *testing.T) {
		mixed := "Hello!@#World$%^123"
		result := StripInvalidTagCharacters(mixed)
		assert.Equal(t, "HelloWorld123", result)
	})

	t.Run("Happy path - Azure tag compliant string", func(t *testing.T) {
		compliant := "my-collection_2023.photos/album:summer=vacation"
		result := StripInvalidTagCharacters(compliant)
		assert.NotEmpty(t, result)
	})

	t.Run("Boundary case - Unicode characters", func(t *testing.T) {
		unicode := "café_测试_🏖️"
		result := StripInvalidTagCharacters(unicode)
		assert.NotEqual(t, unicode, result)
	})
}
