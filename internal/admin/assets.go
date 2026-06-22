package admin

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

var assetFS = mustSub(staticFiles, "static")

func ServePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFileFS(w, r, assetFS, "index.html")
}

func ServeAsset(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/admin/assets/", http.FileServerFS(assetFS)).ServeHTTP(w, r)
}

func ServeFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	http.ServeFileFS(w, r, assetFS, "favicon.ico")
}

func ServeFaviconSVG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	http.ServeFileFS(w, r, assetFS, "favicon.svg")
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
