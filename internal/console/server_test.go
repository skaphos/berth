package console

import (
	"net/http"
	"testing"
)

func TestNewServerUsesProvidedHandler(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	server := NewServer(handler)

	if server.Handler() != handler {
		t.Fatal("handler was not preserved")
	}
}

func TestNewServerDefaultsHandler(t *testing.T) {
	t.Parallel()

	server := NewServer(nil)
	if server.Handler() == nil {
		t.Fatal("default handler is nil")
	}
}
