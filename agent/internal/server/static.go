package server

import (
	"bytes"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/afrugalpenguin/spotdash/agent/web"
)

// contentTypes maps an extension to the type the panel is served with.
// mime.TypeByExtension reads the Windows registry, where .js is often
// text/plain, and browsers refuse a module served that way.
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".ico":  "image/x-icon",
}

// HandleStatic serves the embedded panel UI from the root, behind the token.
func (s *Server) HandleStatic() {
	s.Handle("/", s.staticHandler())
}

func (s *Server) staticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean first so a path cannot climb out of the embedded filesystem.
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}

		data, err := web.Files.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if ct, known := contentTypes[strings.ToLower(path.Ext(name))]; known {
			w.Header().Set("Content-Type", ct)
		}
		// The binary is rebuilt often and the device caches aggressively.
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	})
}
