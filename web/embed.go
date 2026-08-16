// Package web embeds the built React UI so the compiled binary is
// self-contained: no node_modules and no loose files at runtime.
package web

import (
	"embed"
	"errors"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// ErrNotBuilt means the UI has not been built yet. Run `npm install && npm run
// build` in web/ to produce dist/.
var ErrNotBuilt = errors.New("web UI is not built: run `npm install && npm run build` in web/")

// Assets returns the built UI rooted at dist/.
func Assets() (fs.FS, error) {
	assets, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil, ErrNotBuilt
	}
	return assets, nil
}
