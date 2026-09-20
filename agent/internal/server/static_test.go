package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestStaticFilesRequireTheToken(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/", "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRootServesTheIndexPage(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/", "Bearer "+testToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="panel"`) {
		t.Errorf("body is not the panel page:\n%s", truncate(body))
	}
}

func TestStaticServesTheFaceModules(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	for _, path := range []string{"/app.js", "/style.css", "/faces/clock.js", "/faces/status.js", "/faces/telemetry.js", "/faces/rim.js", "/dev.html", "/settings.html", "/settings.js"} {
		rec := do(t, srv, http.MethodGet, path, "Bearer "+testToken)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}

func TestJavaScriptIsServedWithAUsableContentType(t *testing.T) {
	// A module served as text/plain is refused by the browser.
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/app.js", "Bearer "+testToken)

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type for app.js = %q, want a javascript type", ct)
	}
}

func TestMissingStaticFileReturnsNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/nope.js", "Bearer "+testToken)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestStaticDoesNotServeFilesOutsideWeb(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	for _, path := range []string{"/../config.json", "/../go.mod"} {
		rec := do(t, srv, http.MethodGet, path, "Bearer "+testToken)
		if rec.Code == http.StatusOK {
			t.Errorf("GET %s = 200, want 404", path)
		}
	}
}

func truncate(s string) string {
	if len(s) > 400 {
		return s[:400] + "..."
	}
	return s
}
