//go:build !embedassets

package main

import "github.com/kinwyb/kanflux/assets"

// init ensures WebDist is nil when built without embedassets.
func init() {
	assets.WebDist = nil
}
