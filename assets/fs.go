// Package assets provides embedded web assets for the gateway.
package assets

import "io/fs"

// WebDist is the web dist filesystem (set by embed_embed.go at root level).
var WebDist fs.FS
