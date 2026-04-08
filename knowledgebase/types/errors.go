package types

import "errors"

// Common errors
var (
	ErrEmptyContent         = errors.New("content cannot be empty")
	ErrDocumentNotFound     = errors.New("document not found")
	ErrInvalidConfig        = errors.New("invalid configuration")
	ErrStoreNotInitialized  = errors.New("store not initialized")
	ErrEmbedderNotSet       = errors.New("embedder not set")
	ErrUnsupportedStoreType = errors.New("unsupported store type")
)