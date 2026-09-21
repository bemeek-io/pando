package console

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist holds the built console.
//
// The all: prefix is needed because Vite writes assets into a directory the
// embed package would otherwise skip — anything beginning with _ or . is
// excluded by default, and a hashed asset directory is one upgrade away from
// being named that.
//
//go:embed all:dist
var dist embed.FS

// Handler serves the console.
//
// Mounted on Pando's own paths and never as a catch-all: the fallback route
// belongs to the proxy (R-023), because "there is no bypass" is structural —
// a path that is not one of Pando's own is a request to an app. A console
// mounted as the fallback would shadow every app whose slug it did not
// recognize, and the failure would look like the app had disappeared.
//
// Returns false when the console was not built, so a developer running `go run`
// without `make console` gets a plain message rather than a panic at startup or
// a blank page.
func Handler() (http.Handler, bool) {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(root, "index.html"); err != nil {
		return nil, false
	}

	files := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		// Hashed assets are immutable by construction — the filename changes
		// when the content does — so they are cached hard. index.html is not,
		// or an upgraded Pando would serve an old console until every browser
		// happened to revalidate.
		if strings.HasPrefix(clean, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)
			return
		}

		// Anything else is a console route, which the client router owns.
		// Serving index.html for it is what makes a deep link work on a reload.
		//
		// index.html carries no-cache however it is reached. It used to get the
		// header only on the fallback path, so a request for /index.html — the
		// one URL that names the file directly — was served with no
		// Cache-Control at all, and an upgraded Pando could serve an old
		// console from a browser cache indefinitely. The condition is about
		// what is being served, not about which branch reached it.
		if _, err := fs.Stat(root, clean); err != nil || clean == "." {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			clean = "index.html"
		}
		if clean == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	}), true
}
