package server

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// HandleFile serves a single file from disk at pattern, behind the token. The
// path is resolved on every request because the file changes, as album art does
// on each track. A missing file is a 404, which the face renders as no art.
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
		// The cover changes with the track, and a stale image is hard to diagnose
		// on a panel with no address bar.
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(data); err != nil {
			s.log.Debug("writing file response", "path", pattern, "error", err)
		}
	}))
}
