package acquire

import (
	"os"
	"testing"
)

func TestStateWriteAndRead(t *testing.T) {
	s := NewState(t.TempDir())

	if err := s.WriteAcquired("east:prod:pod:x", 7); err != nil {
		t.Fatalf("WriteAcquired: %v", err)
	}

	holder, err := s.ReadHolder()
	if err != nil || holder != "east:prod:pod:x" {
		t.Fatalf("ReadHolder = %q, %v", holder, err)
	}
	token, err := s.ReadToken()
	if err != nil || token != 7 {
		t.Fatalf("ReadToken = %d, %v", token, err)
	}
	if !s.IsHealthy() {
		t.Error("expected healthy marker after WriteAcquired")
	}
}

func TestStateMarkerToggle(t *testing.T) {
	s := NewState(t.TempDir())

	if err := s.MarkHealthy(); err != nil {
		t.Fatalf("MarkHealthy: %v", err)
	}
	if !s.IsHealthy() {
		t.Fatal("want healthy")
	}
	if err := s.MarkUnhealthy(); err != nil {
		t.Fatalf("MarkUnhealthy: %v", err)
	}
	if s.IsHealthy() {
		t.Fatal("want unhealthy after MarkUnhealthy")
	}
	// Idempotent: removing an absent marker is not an error.
	if err := s.MarkUnhealthy(); err != nil {
		t.Fatalf("MarkUnhealthy (second) should be a no-op: %v", err)
	}
}

func TestStateReadMissing(t *testing.T) {
	s := NewState(t.TempDir())
	if _, err := s.ReadHolder(); err == nil {
		t.Error("expected error reading absent holder")
	}
	if _, err := s.ReadToken(); err == nil {
		t.Error("expected error reading absent token")
	}
}

func TestInstallCheckBinary(t *testing.T) {
	s := NewState(t.TempDir())
	if err := s.InstallCheckBinary(); err != nil {
		t.Fatalf("InstallCheckBinary: %v", err)
	}
	info, err := os.Stat(s.CheckPath())
	if err != nil {
		t.Fatalf("stat check binary: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("check binary should be executable")
	}
	if info.Size() == 0 {
		t.Error("check binary should be non-empty")
	}
}
