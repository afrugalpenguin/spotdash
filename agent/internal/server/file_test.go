package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

func TestHandleFileServesTheFile(t *testing.T) {
	srv, _ := newTestServer(t)
	path := writeTempFile(t, "cover.png", "pretend this is a png")
	srv.HandleFile("/art/spotify", func() string { return path })

	rec := do(t, srv, http.MethodGet, "/art/spotify", "Bearer "+testToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "pretend this is a png" {
		t.Errorf("body = %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Errorf("Content-Type = %q, want image/png from the extension", ct)
	}
}

func TestHandleFileRequiresTheToken(t *testing.T) {
	srv, _ := newTestServer(t)
	path := writeTempFile(t, "cover.png", "cover bytes")
	srv.HandleFile("/art/spotify", func() string { return path })

	rec := do(t, srv, http.MethodGet, "/art/spotify", "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandleFileIsReadPerRequest(t *testing.T) {
	// The real provider will replace the cover on every track change, so the
	// handler must not capture the contents once at startup.
	srv, _ := newTestServer(t)
	path := writeTempFile(t, "cover.png", "first cover")
	srv.HandleFile("/art/spotify", func() string { return path })

	first := do(t, srv, http.MethodGet, "/art/spotify", "Bearer "+testToken)
	if err := os.WriteFile(path, []byte("second cover"), 0o600); err != nil {
		t.Fatalf("replacing the file: %v", err)
	}
	second := do(t, srv, http.MethodGet, "/art/spotify", "Bearer "+testToken)

	if first.Body.String() == second.Body.String() {
		t.Error("the handler served stale contents after the file changed")
	}
	if second.Body.String() != "second cover" {
		t.Errorf("body = %q, want the replaced contents", second.Body.String())
	}
}

func TestHandleFileReturnsNotFoundWhenTheFileIsGone(t *testing.T) {
	// A missing cover must not take the panel down. The face renders without
	// art, which it is designed to do.
	srv, _ := newTestServer(t)
	srv.HandleFile("/art/spotify", func() string {
		return filepath.Join(t.TempDir(), "absent.png")
	})

	rec := do(t, srv, http.MethodGet, "/art/spotify", "Bearer "+testToken)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleFileWithNoPathConfiguredIsNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleFile("/art/spotify", func() string { return "" })

	rec := do(t, srv, http.MethodGet, "/art/spotify", "Bearer "+testToken)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
