package lease

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestManagerRejectsOversizedHolderBeforeStorage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	key := Key{Namespace: "ns", Name: "a"}
	mgr := NewManager(nil)
	for _, holder := range []string{strings.Repeat("x", 254), strings.Repeat("é", 127)} {
		if _, err := mgr.Acquire(ctx, key, holder, time.Minute); err == nil {
			t.Fatal("Acquire accepted oversized holder")
		}
		if _, err := mgr.Renew(ctx, key, holder, 1, time.Minute); err == nil {
			t.Fatal("Renew accepted oversized holder")
		}
	}
}
func TestManagerMaximumHolderRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	key := Key{Namespace: "ns", Name: "a"}
	mgr := NewManager(NewMemStore())
	holder := strings.Repeat("x", 253)
	got, err := mgr.Acquire(ctx, key, holder, time.Minute)
	if err != nil || !got.Acquired || got.Holder != holder {
		t.Fatalf("Acquire=%+v err=%v", got, err)
	}
	renewed, err := mgr.Renew(ctx, key, holder, got.FencingToken, time.Minute)
	if err != nil || !renewed.Acquired || renewed.Holder != holder || renewed.FencingToken != got.FencingToken {
		t.Fatalf("Renew=%+v err=%v", renewed, err)
	}
	if err := mgr.Release(ctx, key, holder, got.FencingToken); err != nil {
		t.Fatal(err)
	}
}
