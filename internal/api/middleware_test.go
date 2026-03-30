package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChainMiddlewareAppliesInOrder(t *testing.T) {
	t.Parallel()

	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Order", "handler")
		w.WriteHeader(http.StatusNoContent)
	})

	first := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-Order", "first-before")
			next.ServeHTTP(w, r)
			w.Header().Add("X-Order", "first-after")
		})
	}

	second := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-Order", "second-before")
			next.ServeHTTP(w, r)
			w.Header().Add("X-Order", "second-after")
		})
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	ChainMiddleware(base, first, second).ServeHTTP(recorder, request)

	got := recorder.Header().Values("X-Order")
	want := []string{"first-before", "second-before", "handler", "second-after", "first-after"}
	if len(got) != len(want) {
		t.Fatalf("header count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("header[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestChainMiddlewareWithoutMiddlewareReturnsHandler(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	called := false
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	})

	ChainMiddleware(base).ServeHTTP(recorder, request)

	if !called {
		t.Fatal("base handler was not called")
	}
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
}
