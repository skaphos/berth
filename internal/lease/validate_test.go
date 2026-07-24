package lease

import (
	"strings"
	"testing"
)

func TestValidateKey(t *testing.T) {
	t.Parallel()

	long63 := strings.Repeat("a", 63)
	long64 := strings.Repeat("a", 64)
	long253 := strings.Repeat("a", 253)

	tests := []struct {
		name    string
		key     Key
		wantErr string // empty = valid; otherwise a substring of the error
	}{
		{name: "simple", key: Key{Namespace: "tenant-a", Name: "ingest"}},
		{name: "dotted name is legal", key: Key{Namespace: "a", Name: "b.c"}},
		{name: "single chars", key: Key{Namespace: "a", Name: "b"}},
		{name: "max namespace", key: Key{Namespace: long63, Name: "x"}},
		{name: "numeric segments", key: Key{Namespace: "ns1", Name: "1lease"}},

		{name: "dotted namespace", key: Key{Namespace: "a.b", Name: "c"}, wantErr: "invalid namespace"},
		{name: "collision twin of a/b.c", key: Key{Namespace: "a.b", Name: "c"}, wantErr: "invalid namespace"},
		{name: "empty namespace", key: Key{Namespace: "", Name: "x"}, wantErr: "invalid namespace"},
		{name: "uppercase namespace", key: Key{Namespace: "Tenant", Name: "x"}, wantErr: "invalid namespace"},
		{name: "underscore namespace", key: Key{Namespace: "a_b", Name: "x"}, wantErr: "invalid namespace"},
		{name: "namespace too long", key: Key{Namespace: long64, Name: "x"}, wantErr: "invalid namespace"},
		{name: "leading dash namespace", key: Key{Namespace: "-a", Name: "x"}, wantErr: "invalid namespace"},

		{name: "empty name", key: Key{Namespace: "a", Name: ""}, wantErr: "invalid name"},
		{name: "uppercase name", key: Key{Namespace: "a", Name: "Lease"}, wantErr: "invalid name"},
		{name: "underscore name", key: Key{Namespace: "a", Name: "x_y"}, wantErr: "invalid name"},
		{name: "trailing dot name", key: Key{Namespace: "a", Name: "x."}, wantErr: "invalid name"},
		{name: "name too long", key: Key{Namespace: "a", Name: long253 + "a"}, wantErr: "invalid name"},

		{name: "combined too long", key: Key{Namespace: "abc", Name: long253}, wantErr: "at most"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateKey(tt.key)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateKey(%v) = %v, want nil", tt.key, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateKey(%v) = %v, want error containing %q", tt.key, err, tt.wantErr)
			}
		})
	}
}
