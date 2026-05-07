package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skaphos/berth/internal/lease"
)

func newTestServer() (*httptest.Server, *lease.Manager) {
	mgr := lease.NewManager(lease.NewMemStore())
	srv := httptest.NewServer(NewMux(mgr))
	return srv, mgr
}

func postJSON(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+path, bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestAcquireRoundTrip(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer()
	defer srv.Close()

	resp := postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/acquire", AcquireRequest{Holder: "h1", TTLSeconds: 30})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out LeaseResponse
	decode(t, resp, &out)
	if !out.Acquired {
		t.Fatal("expected Acquired=true")
	}
	if out.FencingToken != 1 {
		t.Fatalf("FencingToken = %d, want 1", out.FencingToken)
	}
}

func TestAcquireSecondHolderReports409Style200WithAcquiredFalse(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer()
	defer srv.Close()

	postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/acquire", AcquireRequest{Holder: "h1", TTLSeconds: 60}).Body.Close()

	resp := postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/acquire", AcquireRequest{Holder: "h2", TTLSeconds: 60})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (held-by-other is not an HTTP error)", resp.StatusCode)
	}
	var out LeaseResponse
	decode(t, resp, &out)
	if out.Acquired {
		t.Fatal("expected Acquired=false")
	}
	if out.Holder != "h1" {
		t.Fatalf("Holder = %q, want h1", out.Holder)
	}
}

func TestAcquireRejectsBadInput(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer()
	defer srv.Close()

	cases := map[string]AcquireRequest{
		"missing holder": {Holder: "", TTLSeconds: 30},
		"zero ttl":       {Holder: "h", TTLSeconds: 0},
		"negative ttl":   {Holder: "h", TTLSeconds: -1},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			resp := postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/acquire", req)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			resp.Body.Close()
		})
	}
}

func TestAcquireRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer()
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1alpha1/namespaces/ns/leases/a/acquire", "application/json",
		strings.NewReader(`{"holder":"h","ttlSeconds":30,"unexpected":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on unknown field", resp.StatusCode)
	}
}

func TestRenewExtendsAndRejectsStaleToken(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer()
	defer srv.Close()

	resp := postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/acquire", AcquireRequest{Holder: "h1", TTLSeconds: 60})
	var acq LeaseResponse
	decode(t, resp, &acq)

	good := postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/renew", RenewRequest{Holder: "h1", FencingToken: acq.FencingToken, TTLSeconds: 60})
	var renewed LeaseResponse
	decode(t, good, &renewed)
	if !renewed.Acquired {
		t.Fatal("Renew should succeed")
	}

	stale := postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/renew", RenewRequest{Holder: "h1", FencingToken: acq.FencingToken + 99, TTLSeconds: 60})
	var staleRes LeaseResponse
	decode(t, stale, &staleRes)
	if staleRes.Acquired {
		t.Fatal("Renew with stale token must not succeed")
	}
}

func TestReleaseAndStaleConflict(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer()
	defer srv.Close()

	resp := postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/acquire", AcquireRequest{Holder: "h1", TTLSeconds: 60})
	var acq LeaseResponse
	decode(t, resp, &acq)

	good := postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/release", ReleaseRequest{Holder: "h1", FencingToken: acq.FencingToken})
	if good.StatusCode != http.StatusNoContent {
		t.Fatalf("release status = %d, want 204", good.StatusCode)
	}
	good.Body.Close()

	postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/acquire", AcquireRequest{Holder: "h2", TTLSeconds: 60}).Body.Close()

	conflict := postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/release", ReleaseRequest{Holder: "h1", FencingToken: acq.FencingToken})
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("release status = %d, want 409", conflict.StatusCode)
	}
	conflict.Body.Close()
}

func TestReleaseValidatesArgs(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer()
	defer srv.Close()

	resp := postJSON(t, srv, "/v1alpha1/namespaces/ns/leases/a/release", ReleaseRequest{Holder: "", FencingToken: 1})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}
