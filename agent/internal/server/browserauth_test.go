package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A browser navigating to the panel cannot set an Authorization header, and the
// stylesheet and modules it then requests carry neither a header nor a query
// string. Without a browser path the UI can never load itself.

func TestQueryTokenAuthenticatesTheInitialLoad(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/?token="+testToken, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestWrongQueryTokenIsRejected(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/?token=wrong", "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestQueryTokenSetsASessionCookie(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/?token="+testToken, "")

	cookie := findCookie(rec.Result().Cookies(), sessionCookieName)
	if cookie == nil {
		t.Fatal("no session cookie was set, so the page's own stylesheet and modules would 401")
	}
	if !cookie.HttpOnly {
		t.Error("the session cookie must be HttpOnly so page scripts cannot read the token back out")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Error("the session cookie must be SameSite=Strict")
	}
	if cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
		t.Error("the session cookie must expire with the browser session rather than persist to disk")
	}
	if cookie.Path != "/" {
		t.Errorf("cookie Path = %q, want /", cookie.Path)
	}
}

func TestSessionCookieAuthenticatesSubsequentRequests(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	first := do(t, srv, http.MethodGet, "/?token="+testToken, "")
	cookie := findCookie(first.Result().Cookies(), sessionCookieName)
	if cookie == nil {
		t.Fatal("no session cookie to test with")
	}

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200. The page's own modules load with the cookie alone", rec.Code)
	}
}

func TestForgedSessionCookieIsRejected(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-the-token"})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHeaderAuthDoesNotSetACookie(t *testing.T) {
	// A programmatic client with the header should not be handed browser state
	// it never asked for.
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/", "Bearer "+testToken)

	if findCookie(rec.Result().Cookies(), sessionCookieName) != nil {
		t.Error("a header-authenticated request should not receive a session cookie")
	}
}

func TestTokenIsNotReflectedInTheResponse(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/?token="+testToken, "")

	if strings.Contains(rec.Body.String(), testToken) {
		t.Error("the response body echoes the token back")
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
