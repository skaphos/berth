package main

import (
	"strings"
	"testing"
)

func TestResolveStoreBackend_Explicit(t *testing.T) {
	cases := []struct {
		name    string
		backend string
		want    string
		wantErr string
	}{
		{name: "mem", backend: "mem", want: "mem"},
		{name: "k8s", backend: "k8s", want: "k8s"},
		{name: "sql", backend: "sql", want: "sql"},
		{name: "unknown", backend: "etcd", wantErr: "--store-backend must be one of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveStoreBackend(storeConfig{backend: tc.backend})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestResolveStoreBackend_LegacyFallback(t *testing.T) {
	cases := []struct {
		name      string
		namespace string
		want      string
	}{
		{name: "empty namespace falls back to mem", namespace: "", want: "mem"},
		{name: "set namespace falls back to k8s", namespace: "berth", want: "k8s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveStoreBackend(storeConfig{coordinationNamespace: tc.namespace})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestValidateStoreConfig(t *testing.T) {
	cases := []struct {
		name    string
		backend string
		cfg     storeConfig
		wantErr string
	}{
		// mem ----------------------------------------------------------
		{
			name:    "mem accepts no other flags",
			backend: "mem",
			cfg:     storeConfig{sqlMigrate: "auto"},
		},
		{
			name:    "mem rejects coordination flags",
			backend: "mem",
			cfg:     storeConfig{coordinationNamespace: "berth", sqlMigrate: "auto"},
			wantErr: "--coordination-namespace and --coordination-kubeconfig are only valid with --store-backend=k8s",
		},
		{
			name:    "mem rejects sql flags",
			backend: "mem",
			cfg:     storeConfig{sqlDriver: "postgres", sqlMigrate: "auto"},
			wantErr: "--sql-* flags are only valid with --store-backend=sql",
		},

		// k8s ----------------------------------------------------------
		{
			name:    "k8s with namespace is valid",
			backend: "k8s",
			cfg:     storeConfig{coordinationNamespace: "berth", sqlMigrate: "auto"},
		},
		{
			name:    "k8s with namespace + kubeconfig is valid",
			backend: "k8s",
			cfg: storeConfig{
				coordinationNamespace:  "berth",
				coordinationKubeconfig: "/etc/berth/coord.kubeconfig",
				sqlMigrate:             "auto",
			},
		},
		{
			name:    "k8s requires namespace",
			backend: "k8s",
			cfg:     storeConfig{sqlMigrate: "auto"},
			wantErr: "--coordination-namespace is required when --store-backend=k8s",
		},
		{
			name:    "k8s rejects sql flags",
			backend: "k8s",
			cfg: storeConfig{
				coordinationNamespace: "berth",
				sqlDSN:                "postgres://...",
				sqlMigrate:            "auto",
			},
			wantErr: "--sql-* flags are only valid with --store-backend=sql",
		},

		// sql ----------------------------------------------------------
		{
			name:    "sql with dsn is valid",
			backend: "sql",
			cfg: storeConfig{
				sqlDriver:  "postgres",
				sqlDSN:     "postgres://user:pass@host/berth",
				sqlMigrate: "auto",
			},
		},
		{
			name:    "sql with dsn-file is valid",
			backend: "sql",
			cfg: storeConfig{
				sqlDriver:  "postgres",
				sqlDSNFile: "/etc/berth/dsn",
				sqlMigrate: "auto",
			},
		},
		{
			name:    "sql accepts migrate=off",
			backend: "sql",
			cfg: storeConfig{
				sqlDriver:  "sqlite",
				sqlDSN:     "file:berth.db",
				sqlMigrate: "off",
			},
		},
		{
			name:    "sql rejects coordination flags",
			backend: "sql",
			cfg: storeConfig{
				coordinationNamespace: "berth",
				sqlDriver:             "postgres",
				sqlDSN:                "postgres://...",
				sqlMigrate:            "auto",
			},
			wantErr: "--coordination-* flags are only valid with --store-backend=k8s",
		},
		{
			name:    "sql requires driver",
			backend: "sql",
			cfg: storeConfig{
				sqlDSN:     "postgres://...",
				sqlMigrate: "auto",
			},
			wantErr: "--sql-driver is required when --store-backend=sql",
		},
		{
			name:    "sql rejects unknown driver",
			backend: "sql",
			cfg: storeConfig{
				sqlDriver:  "etcd",
				sqlDSN:     "x",
				sqlMigrate: "auto",
			},
			wantErr: "--sql-driver must be one of",
		},
		{
			name:    "sql requires dsn or dsn-file",
			backend: "sql",
			cfg: storeConfig{
				sqlDriver:  "postgres",
				sqlMigrate: "auto",
			},
			wantErr: "one of --sql-dsn or --sql-dsn-file is required when --store-backend=sql",
		},
		{
			name:    "sql rejects both dsn and dsn-file",
			backend: "sql",
			cfg: storeConfig{
				sqlDriver:  "postgres",
				sqlDSN:     "postgres://...",
				sqlDSNFile: "/etc/berth/dsn",
				sqlMigrate: "auto",
			},
			wantErr: "--sql-dsn and --sql-dsn-file are mutually exclusive",
		},
		{
			name:    "sql rejects unknown migrate value",
			backend: "sql",
			cfg: storeConfig{
				sqlDriver:  "postgres",
				sqlDSN:     "postgres://...",
				sqlMigrate: "yolo",
			},
			wantErr: "--sql-migrate must be one of",
		},

		// internal ------------------------------------------------------
		{
			name:    "unknown backend is rejected",
			backend: "etcd",
			cfg:     storeConfig{sqlMigrate: "auto"},
			wantErr: "unknown backend",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStoreConfig(tc.backend, tc.cfg)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestResolveAuthMode(t *testing.T) {
	cases := []struct {
		name     string
		authMode string
		backend  string
		want     string
	}{
		{name: "explicit none wins over k8s default", authMode: "none", backend: "k8s", want: "none"},
		{name: "explicit oidc wins over mem default", authMode: "oidc", backend: "mem", want: "oidc"},
		{name: "mem implies none", backend: "mem", want: "none"},
		{name: "k8s implies static-keys", backend: "k8s", want: "static-keys"},
		{name: "sql implies static-keys", backend: "sql", want: "static-keys"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveAuthMode(tc.authMode, tc.backend)
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBuildStore_SQLNotImplemented(t *testing.T) {
	_, err := buildStore("sql", storeConfig{
		sqlDriver:  "postgres",
		sqlDSN:     "postgres://...",
		sqlMigrate: "auto",
	})
	if err == nil || !strings.Contains(err.Error(), "SKA-316") {
		t.Fatalf("want error referencing SKA-316, got %v", err)
	}
}

func TestBuildStore_Mem(t *testing.T) {
	s, err := buildStore("mem", storeConfig{sqlMigrate: "auto"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("buildStore(mem) returned nil store")
	}
}
