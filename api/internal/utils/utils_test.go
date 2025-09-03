package utils

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"

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
		assert.Equal(t, 100, resizedImg.Bounds().Dx())
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
		assert.Equal(t, 100, resizedImg.Bounds().Dx())
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
		assert.Equal(t, 100, resizedImg.Bounds().Dx())
		assert.Equal(t, 100, resizedImg.Bounds().Dy())
	})
}

// TestGetEnvValue tests the GetEnvValue function from the utils package
func TestGetEnvValue(t *testing.T) {
	// Set an environment variable
	os.Setenv("TEST_ENV_VAR", "test_value")

	// Test cases
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

	// Test cases
	tests := []struct {
		value  string
		expectedValue string
	}{
		{"Mum_&_Dad.jpg", "Mum__Dad.jpg"},
		{"HelloWorld", "HelloWorld"},
		{"THis is an invalid str*ng.png", "THisisaninvalidstrng.png"},
		{"This is /   [] an invalid Str%$g.%$#gif", "Thisis/aninvalidStrg.gif"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
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

	t.Run("Error creating ManagedIdentityCredential", func(t *testing.T) {
		mockManagedIdentityCredential.On("NewManagedIdentityCredential", mock.Anything).Return(nil, errors.New("credential error"))

		client, err := CreateAzureBlobClient(storageUrl, true, azureClientId)
		assert.Error(t, err)
		assert.Nil(t, client)
	})

	t.Run("Error creating DefaultAzureCredential", func(t *testing.T) {
		mockDefaultAzureCredential.On("NewDefaultAzureCredential", mock.Anything).Return(nil, errors.New("credential error"))

		client, err := CreateAzureBlobClient(storageUrl, false, azureClientId)
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}
