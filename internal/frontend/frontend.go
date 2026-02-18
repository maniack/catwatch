package frontend

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:generate sh -c "esbuild src/index.js --bundle --loader:.js=jsx --jsx=transform --jsx-factory=React.createElement --jsx-fragment=React.Fragment --target=es2017 --minify --sourcemap --outfile=static/js/app.bundle.js"

//go:embed static/*
var assets embed.FS

// FS returns the frontend assets filesystem.
// If devel is true, it serves from the real filesystem (useful for live editing),
// otherwise it serves the embedded assets.
func FS(devel bool) (http.FileSystem, error) {
	if devel {
		return http.Dir("internal/frontend/static"), nil
	}
	f, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	return http.FS(f), nil
}
