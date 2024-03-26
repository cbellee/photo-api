package models

import "time"

type StorageConfig struct {
	StorgeURL            string
	StorageAccount       string
	StorageKey           string
	StorageContainer     string
	ThumbsContainerName  string
	ImagesContainerName  string
	UploadsContainerName string
	Suffix               string
}

type Blob struct {
	Name     string
	Path     string
	Tags     map[string]string
	MetaData map[string]string
}

type Photo struct {
	Src         string    `json:"src"`
	Name        string    `json:"name"`
	Width       int32     `json:"width"`
	Height      int32     `json:"height"`
	Ratio       float32   `json:"ratio"`
	Album       string    `json:"album"`
	Collection  string    `json:"collection"`
	Description string    `json:"description"`
	DateTaken   time.Time `json:"dateTaken"`
}

type Album struct {
	Name string `json:"name"`
}

type Collection struct {
	Name string `json:"name"`
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
