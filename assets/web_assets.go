// Package assets provides embedded web assets.
// When web/dist does not exist (local dev), this fallback provides a nil FS.
//go:build !embedassets

package assets

import "io/fs"

// WebDist returns the embedded web dist filesystem. Returns nil if not embedded.
var WebDist fs.FS
