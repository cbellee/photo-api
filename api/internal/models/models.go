package models

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type StorageConfig struct {
	StorgeURL            string
	StorageAccount       string
	StorageAccountSuffix string
	StorageKey           string
	StorageContainer     string
	ThumbsContainerName  string
	ImagesContainerName  string
	UploadsContainerName string
}

type ImageTags struct {
	Description     string `json:"description"`
	Name            string `json:"name"`
	Collection      string `json:"collection"`
	CollectionImage bool   `json:"collectionImage"`
	AlbumImage      bool   `json:"albumImage"`
	Album           string `json:"album"`
	Type            string `json:"type"`
	IsDeleted       bool   `json:"isDeleted"`
	Orientation     int    `json:"orientation"`
}

type Blob struct {
	Name     string
	Path     string
	Tags     map[string]string
	MetaData map[string]string
}

type Photo struct {
	Src             string    `json:"src"`
	Name            string    `json:"name"`
	Width           int       `json:"width"`
	Height          int       `json:"height"`
	Album           string    `json:"album"`
	Collection      string    `json:"collection"`
	Description     string    `json:"description"`
	DateTaken       time.Time `json:"dateTaken"`
	ExifData        string    `json:"exifData"`
	IsDeleted       bool      `json:"isDeleted"`
	Orientation     int       `json:"orientation"`
	AlbumImage      bool      `json:"albumImage"`
	CollectionImage bool      `json:"collectionImage"`
}

type Album struct {
	Name string `json:"name"`
}

type Collection struct {
	Name string `json:"name"`
}

type MyClaims struct {
	Roles []string `json:"roles"`
	jwt.RegisteredClaims
}

type Event struct {
	Topic           string
	Subject         string
	EventType       string
	Id              string
	DataVersion     string
	MetadataVersion string
	EventTime       string
	Data            struct {
		Api                string
		ClientRequestId    string
		RequestId          string
		ETag               string
		ContentType        string
		ContentLength      int32
		BlobType           string
		Url                string
		Sequencer          string
		StorageDiagnostics struct {
			BatchId string
		}
	}
}
