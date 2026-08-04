//go:build !webui_embed

package main

import (
	"io/fs"
	"os"
)

// Development builds serve frontend/dist from disk when it exists. Vite is
// still the preferred development UI because it provides hot reload.
func frontendAssets() fs.FS {
	if _, err := os.Stat("frontend/dist/index.html"); err != nil {
		return nil
	}
	return os.DirFS("frontend/dist")
}
