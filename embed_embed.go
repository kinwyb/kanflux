//go:build embedassets

package main

import (
	"io/fs"
	"embed"

	"github.com/kinwyb/kanflux/assets"
)

//go:embed web/dist/*
var webDist embed.FS

// init registers the embedded web FS with the assets package.
func init() {
	if sub, err := fs.Sub(webDist, "web/dist"); err == nil {
		assets.WebDist = sub
	}
}
