package serve

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist holds the built React UI, rsync'd from `ui/dist/` by `make ui`.
// Committed contents survive a fresh clone; the directory itself is in
// .gitignore so source tarballs don't carry stale artifacts.
//
//go:embed all:dist
var dist embed.FS

// distFS is the rooted view of the embed (i.e. dropping the leading
// "dist/" prefix) so HTTP serves /index.html etc. directly.
func distFS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// Compile-time invariant: the dist directory exists because
		// `make ui` ran before `go build`. Panic loudly if not.
		panic("serve: dist embed missing — run `make ui` before `go build`: " + err.Error())
	}
	return sub
}

// assetHandler serves the embedded React UI from the root. /api/* and
// /events/* return 404 here so future handlers can claim those
// prefixes cleanly via mux.Handle without HTML leaking through.
//
// SPA routing: any path that isn't a static asset and isn't /api /
// /events falls back to index.html so client-side routes (e.g. /s/:name)
// resolve correctly on a hard refresh or deep link.
func assetHandler() http.Handler {
	root := distFS()
	files := http.FileServer(http.FS(root))
	indexHTML, _ := fs.ReadFile(root, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/events/") {
			http.NotFound(w, r)
			return
		}
		// Try the static FS first; on miss, serve index.html so the
		// React router can take over.
		if path != "/" {
			rel := strings.TrimPrefix(path, "/")
			if _, err := fs.Stat(root, rel); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(indexHTML)
	})
}
