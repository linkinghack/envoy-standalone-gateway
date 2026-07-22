// Package console embeds the production management SPA into the esgw binary.
package console

import (
	"embed"
	"io/fs"
)

//go:embed all:ui
var content embed.FS

// Assets returns the Vite output rooted at index.html.
func Assets() fs.FS {
	assets, err := fs.Sub(content, "ui")
	if err != nil {
		panic(err)
	}
	return assets
}
