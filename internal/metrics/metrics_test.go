package metrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/skaphos/berth/internal/api"
	"github.com/skaphos/berth/internal/lease"
)

func TestObserveRequestBoundsMethodLabels(t *testing.T) {
	t.Parallel()
	m := New()
	methods := []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace, http.MethodPatch}
	for _, method := range methods {
		m.ObserveRequest("unmatched", method, http.StatusMethodNotAllowed, time.Millisecond)
	}
	for i := range 100 {
		m.ObserveRequest("unmatched", fmt.Sprintf("CUSTOM%d", i), http.StatusMethodNotAllowed, time.Millisecond)
	}
	// Method tokens are case-sensitive; alternate casing and invalid direct-call
	// input must not allocate more series or be confused with standard methods.
	for _, method := range []string{"get", "Get", "", "OTHER", "GET ", "Méthod", strings.Repeat("X", 4096)} {
		m.ObserveRequest("unmatched", method, http.StatusMethodNotAllowed, time.Millisecond)
	}
	for _, method := range methods {
		if got := testutil.ToFloat64(m.reqTotal.WithLabelValues("unmatched", method, "405")); got != 1 {
			t.Fatalf("count for %s = %v, want 1", method, got)
		}
	}
	if got := testutil.ToFloat64(m.reqTotal.WithLabelValues("unmatched", "OTHER", "405")); got != 107 {
		t.Fatalf("OTHER count = %v, want 107", got)
	}
	for name, collector := range map[string]prometheus.Collector{
		"counter": m.reqTotal, "histogram": m.reqDuration,
	} {
		if got := testutil.CollectAndCount(collector); got != len(methods)+1 {
			t.Errorf("%s series = %d, want %d", name, got, len(methods)+1)
		}
	}
}

func TestMetricsMiddlewareBoundsUnauthenticatedMethods(t *testing.T) {
	t.Parallel()
	m := New()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := api.MetricsMiddleware(m)(mux)
	for _, path := range []string{"/healthz", "/missing"} {
		want := http.StatusMethodNotAllowed
		if path == "/missing" {
			want = http.StatusNotFound
		}
		for i := range 100 {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(fmt.Sprintf("CUSTOM%d", i), path, nil))
			if rr.Code != want {
				t.Fatalf("%s status = %d, want %d", path, rr.Code, want)
			}
			if want == http.StatusMethodNotAllowed && rr.Header().Get("Allow") != "GET, HEAD" {
				t.Fatalf("Allow = %q, want GET, HEAD", rr.Header().Get("Allow"))
			}
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(method, "/healthz", nil))
		if rr.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d, want 204", method, rr.Code)
		}
	}
	for _, collector := range []prometheus.Collector{m.reqTotal, m.reqDuration} {
		if got := testutil.CollectAndCount(collector); got != 4 {
			t.Errorf("series = %d, want 4 (two OTHER statuses, GET, HEAD)", got)
		}
	}
}

func TestObserveRequestCountsAndInflight(t *testing.T) {
	t.Parallel()

	m := New()
	m.ObserveRequest("POST /acquire", http.MethodPost, http.StatusOK, 2*time.Millisecond)
	m.ObserveRequest("POST /acquire", http.MethodPost, http.StatusOK, 3*time.Millisecond)

	got := testutil.ToFloat64(m.reqTotal.WithLabelValues("POST /acquire", http.MethodPost, "200"))
	if got != 2 {
		t.Fatalf("requests_total = %v, want 2", got)
	}

	m.IncInflight()
	m.IncInflight()
	m.DecInflight()
	if got := testutil.ToFloat64(m.reqInflight); got != 1 {
		t.Fatalf("requests_inflight = %v, want 1", got)
	}
}

func TestObserveOutcomeCountsPerLabel(t *testing.T) {
	t.Parallel()

	m := New()
	m.ObserveOutcome("acquired")
	m.ObserveOutcome("acquired")
	m.ObserveOutcome("held-by-other")

	if got := testutil.ToFloat64(m.leaseOutcomes.WithLabelValues("acquired")); got != 2 {
		t.Fatalf("lease_outcomes{acquired} = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.leaseOutcomes.WithLabelValues("held-by-other")); got != 1 {
		t.Fatalf("lease_outcomes{held-by-other} = %v, want 1", got)
	}
}

func TestWrapStoreIsTransparentAndRecords(t *testing.T) {
	t.Parallel()

	m := New()
	store := m.WrapStore("mem", lease.NewMemStore())
	ctx := context.Background()
	key := lease.Key{Namespace: "ns", Name: "a"}
	rec := &lease.Record{Key: key, Holder: "h", FencingToken: 1, RenewedAt: time.Now(), TTL: time.Minute}

	// Create, then a conflicting create, then read it back: results must be
	// forwarded unchanged.
	if err := store.Put(ctx, 0, rec); err != nil {
		t.Fatalf("put create: %v", err)
	}
	if err := store.Put(ctx, 0, rec); !errors.Is(err, lease.ErrConflict) {
		t.Fatalf("second create err = %v, want ErrConflict", err)
	}
	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Holder != "h" {
		t.Fatalf("holder = %q, want h", got.Holder)
	}

	// Three store calls observed; outcomes split ok/conflict.
	if n := testutil.CollectAndCount(m.storeDuration); n != 3 {
		t.Fatalf("store call observations = %d, want 3", n)
	}
}

func TestOutcomeForClassifiesSentinels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		want string
	}{
		{nil, "ok"},
		{lease.ErrConflict, "conflict"},
		{lease.ErrNotFound, "notfound"},
		{errors.New("boom"), "error"},
	}
	for _, tc := range cases {
		if got := outcomeFor(tc.err); got != tc.want {
			t.Fatalf("outcomeFor(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestHandlerExposesBerthSeries(t *testing.T) {
	t.Parallel()

	m := New()
	m.ObserveRequest("GET /healthz", http.MethodGet, http.StatusOK, time.Millisecond)
	m.ObserveOutcome("acquired")

	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"berth_apiserver_requests_total",
		"berth_apiserver_request_duration_seconds",
		"berth_apiserver_requests_inflight",
		"berth_apiserver_lease_outcomes_total",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("scrape body missing %q", want)
		}
	}
}

func TestServeShutsDownOnContextCancel(t *testing.T) {
	t.Parallel()

	m := New()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- m.Serve(ctx, "127.0.0.1:0") }()

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil on clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after context cancel")
	}
}
