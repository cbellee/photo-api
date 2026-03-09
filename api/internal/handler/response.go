package handler

// mutationResponse is the JSON body returned by mutation endpoints (rename,
// soft-delete, restore). Using a named struct instead of map[string]interface{}
// gives compile-time safety and self-documenting field names.
type mutationResponse struct {
	Message           string   `json:"message"`
	Affected          int      `json:"affected"`
	Errors            []string `json:"errors,omitempty"`
	NewName           string   `json:"newName,omitempty"`
	CollectionDeleted bool     `json:"collectionDeleted,omitempty"`
}
