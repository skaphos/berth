package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/lease"
)

func TestLeaseBodyLimits(t *testing.T) {
	t.Parallel()
	const limit = 4096
	for _, operation := range []string{"acquire", "renew", "release"} {
		valid := `{ "holder":"h", "ttlSeconds":30 }`
		if operation == "renew" {
			valid = `{"holder":"h","ttlSeconds":30,"fencingToken":1}`
		}
		if operation == "release" {
			valid = `{"holder":"h","fencingToken":1}`
		}
		for _, tc := range []struct {
			name, body string
			want       int
		}{
			{"too large", strings.Repeat(" ", limit+1), 413},
			{"valid prefix and oversized tail", `{"holder":"h"}` + strings.Repeat(" ", limit), 413},
			{"unknown field", `{"unknown":1}`, 400},
			{"two objects", valid + `{}`, 400},
			{"trailing garbage", valid + `x`, 400},
		} {
			for _, knownLength := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/known=%v", operation, tc.name, knownLength), func(t *testing.T) {
					mux := NewMux(lease.NewManager(nil), nil, nil)
					req := httptest.NewRequest(http.MethodPost, "/v1alpha1/namespaces/ns/leases/a/"+operation, strings.NewReader(tc.body))
					if !knownLength {
						req.ContentLength = -1
						req.TransferEncoding = []string{"chunked"}
					}
					rr := httptest.NewRecorder()
					mux.ServeHTTP(rr, req)
					if rr.Code != tc.want {
						t.Fatalf("status=%d want %d: %s", rr.Code, tc.want, rr.Body.String())
					}
				})
			}
		}
	}
	for _, size := range []int{limit, limit + 1} {
		body := `{"holder":"h","ttlSeconds":30}`
		body += strings.Repeat(" ", size-len(body))
		mux := NewMux(lease.NewManager(lease.NewMemStore()), nil, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1alpha1/namespaces/ns/leases/a/acquire", strings.NewReader(body)))
		want := 200
		if size > limit {
			want = 413
		}
		if rr.Code != want {
			t.Fatalf("size=%d status=%d want %d", size, rr.Code, want)
		}
	}
}

func TestLeaseHolderByteLimits(t *testing.T) {
	t.Parallel()
	for _, holder := range []string{strings.Repeat("x", 253), strings.Repeat("x", 254), strings.Repeat("é", 126), strings.Repeat("é", 127)} {
		for _, op := range []string{"acquire", "renew"} {
			body, _ := json.Marshal(map[string]any{"holder": holder, "ttlSeconds": 30})
			if op == "renew" {
				body, _ = json.Marshal(map[string]any{"holder": holder, "ttlSeconds": 30, "fencingToken": 1})
			}
			rr := httptest.NewRecorder()
			NewMux(lease.NewManager(lease.NewMemStore()), nil, nil).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1alpha1/namespaces/ns/leases/a/"+op, bytes.NewReader(body)))
			want := 200
			if len(holder) > 253 {
				want = 400
			}
			if rr.Code != want {
				t.Fatalf("%s holder bytes=%d status=%d want %d", op, len(holder), rr.Code, want)
			}
		}
	}
}

func TestLeaseBodyChunkedAndAuthentication(t *testing.T) {
	t.Parallel()
	authn := auth.NewStaticAuthenticator(map[string]auth.Identity{"key": {Holder: "team", Tenant: "team"}})
	srv := httptest.NewServer(NewMux(lease.NewManager(nil), authn, nil))
	defer srv.Close()
	for _, token := range []string{"", "key"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/v1alpha1/namespaces/ns/leases/a/acquire", io.NopCloser(strings.NewReader(strings.Repeat(" ", 4097))))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := 413
		if token == "" {
			want = 401
		}
		if resp.StatusCode != want {
			t.Fatalf("authenticated=%v: status=%d want %d", token != "", resp.StatusCode, want)
		}
	}
}

func TestReleaseLegacyOversizedHolder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := lease.NewMemStore()
	key := lease.Key{Namespace: "ns", Name: "a"}
	holder := "team/" + strings.Repeat("x", 254)
	record := &lease.Record{Key: key, Holder: holder, FencingToken: 7, AcquiredAt: time.Now(), RenewedAt: time.Now(), TTL: time.Minute}
	if err := store.Put(ctx, 0, record); err != nil {
		t.Fatal(err)
	}
	authn := auth.NewStaticAuthenticator(map[string]auth.Identity{"key": {Holder: "team", Tenant: "team"}})
	mux := NewMux(lease.NewManager(store), authn, nil)
	body, _ := json.Marshal(ReleaseRequest{Holder: holder, FencingToken: 7})
	req := httptest.NewRequest(http.MethodPost, "/v1alpha1/namespaces/ns/leases/a/release", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer key")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 204 {
		t.Fatalf("status=%d: %s", rr.Code, rr.Body.String())
	}
	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Holder != "" || got.FencingToken != 7 {
		t.Fatalf("legacy release did not preserve tombstone: %+v", got)
	}
}

func TestEscapedHolderLimit(t *testing.T) {
	t.Parallel()
	for _, size := range []int{253, 254} {
		body := `{"holder":"` + strings.Repeat(`\u0061`, size) + `","ttlSeconds":30}`
		rr := httptest.NewRecorder()
		NewMux(lease.NewManager(lease.NewMemStore()), nil, nil).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1alpha1/namespaces/ns/leases/a/acquire", strings.NewReader(body)))
		want := 200
		if size > 253 {
			want = 400
		}
		if rr.Code != want {
			t.Fatalf("escaped size=%d status=%d: %s", size, rr.Code, rr.Body.String())
		}
	}
}
