package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/cbellee/photo-api/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mocking azblob.Client
type MockBlobClient struct {
	mock.Mock
}

// Mocking utils.GetBlobTagList
func MockGetBlobTagList(client *azblob.Client, containerName, storageUrl string, ctx context.Context) (map[string]string, error) {
	args := client.Called(containerName, storageUrl, ctx)
	return args.Get(0).(map[string]string), args.Error(1)
}

func TestTagListHandler(t *testing.T) {
	mockBlobClient := new(MockBlobClient)
	storageUrl := "https://example.blob.core.windows.net"
	imagesContainerName := "images"

	// Mock the GetBlobTagList function
	utils.GetBlobTagList = MockGetBlobTagList

	t.Run("Successful response", func(t *testing.T) {
		expectedTagList := map[string]string{"tag1": "value1", "tag2": "value2"}
		mockBlobClient.On("GetBlobTagList", imagesContainerName, storageUrl, mock.Anything).Return(expectedTagList, nil)

		req, err := http.NewRequest("GET", "/tags", nil)
		assert.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := tagListHandler(mockBlobClient, storageUrl)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var actualTagList map[string]string
		err = json.NewDecoder(rr.Body).Decode(&actualTagList)
		assert.NoError(t, err)
		assert.Equal(t, expectedTagList, actualTagList)
	})

	t.Run("Error response", func(t *testing.T) {
		mockBlobClient.On("GetBlobTagList", imagesContainerName, storageUrl, mock.Anything).Return(nil, assert.AnError)

		req, err := http.NewRequest("GET", "/tags", nil)
		assert.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := tagListHandler(mockBlobClient, storageUrl)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}
