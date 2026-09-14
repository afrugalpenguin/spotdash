package server

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// HandleFile serves a single file from disk at pattern, behind the token.
//
// The path is resolved per request rather than captured once, because the file
// it points at is expected to change: the real Spotify provider will replace
// the cover on every track change.
//
// A missing file is a 404 rather than an error. The face is designed to render
// without art, so a cover that has not arrived yet costs a blank space and
// nothing more.
func (s *Server) HandleFile(pattern string, resolve func() string) {
	s.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := resolve()
		if target == "" {
			http.NotFound(w, r)
			return
		}

		data, err := os.ReadFile(target)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				s.log.Warn("serving file failed", "path", pattern, "error", err)
			}
			http.NotFound(w, r)
			return
		}

		if ct, known := contentTypes[strings.ToLower(path.Ext(target))]; known {
			w.Header().Set("Content-Type", ct)
		}
		// The cover changes with the track, and a panel with no address bar is
		// a miserable place to diagnose a stale image.
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(data); err != nil {
			s.log.Debug("writing file response", "path", pattern, "error", err)
		}
	}))
}
