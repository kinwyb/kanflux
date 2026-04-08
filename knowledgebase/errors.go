package knowledgebase

import "errors"

// Common errors
var (
	// ErrEmptyContent is returned when trying to store empty content.
	ErrEmptyContent = errors.New("content cannot be empty")

	// ErrDocumentNotFound is returned when a document is not found.
	ErrDocumentNotFound = errors.New("document not found")

	// ErrInvalidConfig is returned when configuration is invalid.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrStoreNotInitialized is returned when store is not initialized.
	ErrStoreNotInitialized = errors.New("store not initialized")

	// ErrEmbedderNotSet is returned when embedder is required but not set.
	ErrEmbedderNotSet = errors.New("embedder not set")

	// ErrUnsupportedStoreType is returned for unsupported store types.
	ErrUnsupportedStoreType = errors.New("unsupported store type")
)