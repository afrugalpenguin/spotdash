package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A browser cannot set an Authorization header, and the modules the page then
// requests carry neither a header nor a query string.

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
		t.Fatal("no session cookie set")
	}
	if !cookie.HttpOnly {
		t.Error("cookie HttpOnly = false, want true")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Error("cookie SameSite is not Strict")
	}
	if cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
		t.Error("cookie has Expires or MaxAge, want a session cookie")
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
		t.Fatal("no session cookie")
	}

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
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
	// A programmatic client gets no browser state.
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/", "Bearer "+testToken)

	if findCookie(rec.Result().Cookies(), sessionCookieName) != nil {
		t.Error("header auth received a session cookie, want none")
	}
}

func TestTokenIsNotReflectedInTheResponse(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleStatic()

	rec := do(t, srv, http.MethodGet, "/?token="+testToken, "")

	if strings.Contains(rec.Body.String(), testToken) {
		t.Error("response body contains the token")
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
