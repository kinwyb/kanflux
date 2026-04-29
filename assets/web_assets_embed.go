//go:build embedassets

package assets

import (
	"embed"
	"io/fs"
)

//go:embed web/dist/*
var webDist embed.FS

// WebDist returns the embedded web dist filesystem.
var WebDist fs.FS

func init() {
	if sub, err := fs.Sub(webDist, "web/dist"); err == nil {
		WebDist = sub
	}
}
