package server

import (
	"net/http"
	"testing"
)

func TestHandleOpenNeedsNoToken(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.HandleOpen("/spotify/callback", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := do(t, srv, http.MethodGet, "/spotify/callback", "")

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418", rec.Code)
	}
}

func TestHandleOpenIsSeparateFromProtectedRoutes(t *testing.T) {
	// Registering one open route must not open a protected one.
	srv, _ := newTestServer(t)
	srv.HandleOpen("/spotify/callback", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.Handle("/spotify/connect", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := do(t, srv, http.MethodGet, "/spotify/connect", "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
